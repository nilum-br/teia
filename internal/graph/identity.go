package graph

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type identityResolver struct {
	canonical func(string) string
	addresses map[string][]model.IdentityCandidate
	now       time.Time
}

func newIdentityResolver(results []Result, canonical func(string) string, now time.Time) identityResolver {
	r := identityResolver{canonical: canonical, addresses: map[string][]model.IdentityCandidate{}, now: now}
	for _, result := range results {
		if result.Err != nil {
			continue
		}
		for _, node := range result.Observation.Nodes {
			for _, address := range node.Addresses {
				candidate := model.IdentityCandidate{ID: canonical(node.ID), Source: result.Source.Name(), SourceType: result.Source.Type(), Basis: node.IdentityBasis, Since: node.Since, CollectedAt: result.CollectedAt, Retained: result.Retained}
				if !slices.Contains(r.addresses[address], candidate) {
					r.addresses[address] = append(r.addresses[address], candidate)
				}
			}
		}
	}
	for address := range r.addresses {
		slices.SortFunc(r.addresses[address], func(a, b model.IdentityCandidate) int {
			if c := cmp.Compare(a.ID, b.ID); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Source, b.Source); c != 0 {
				return c
			}
			if c := cmp.Compare(a.SourceType, b.SourceType); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Basis, b.Basis); c != 0 {
				return c
			}
			if c := a.Since.Compare(b.Since); c != 0 {
				return c
			}
			if c := a.CollectedAt.Compare(b.CollectedAt); c != 0 {
				return c
			}
			if a.Retained == b.Retained {
				return 0
			}
			if a.Retained {
				return 1
			}
			return -1
		})
	}
	return r
}

// resolve preserves the common name/alias behavior. Conflicting inventory
// owners never collapse to whichever source happened to run last.
func (r identityResolver) resolve(input string) string {
	id := r.canonical(input)
	if candidates := r.addresses[strings.TrimPrefix(id, source.AddressPrefix)]; source.IsAddressID(id) && len(candidates) > 0 {
		owner := candidates[0].ID
		for _, c := range candidates[1:] {
			if c.ID != owner {
				return id
			}
		}
		return owner
	}
	return id
}

func (r identityResolver) explain(input string, result Result, observation *source.EdgeObservation, hint model.IdentityMethod, basis string) (model.IdentityResolution, bool) {
	id := r.canonical(input)
	resolution := model.IdentityResolution{Input: input, ResolvedID: id, Method: model.IdentityName,
		Source: result.Source.Name(), SourceType: result.Source.Type(), Basis: basis, TimeBasis: "not-applicable",
		CollectedAt: result.CollectedAt, EvaluatedAt: result.Observation.EvaluatedAt, Retained: result.Retained,
		Detail: "The source supplied this name; normalization trims whitespace and lowercases names. Matching names do not independently prove a global workload identity."}
	if observation != nil {
		resolution.ObservedFrom, resolution.ObservedAt = observation.ObservedFrom, observation.ObservedAt
		resolution.WindowSeconds = observation.Window.Seconds()
	}
	if NormalizeID(input) != id && !source.IsAddressID(id) {
		resolution.Method, resolution.Basis = model.IdentityAlias, "configured-alias"
		resolution.Detail = "An explicit configured alias maps the normalized input to this identity; no address ownership is inferred."
		return resolution, false
	}
	if !source.IsAddressID(id) {
		switch {
		case hint != "":
			resolution.Method = hint
			resolution.Detail = "The source names this entity using " + basis + "."
		case result.Source.Type() == "declared" || observation != nil && observation.Declared:
			resolution.Method, resolution.Basis = model.IdentityExplicit, "flow-declaration"
			resolution.Detail = "A flows declaration explicitly names this entity. This records intent, not observed communication."
		}
		return resolution, false
	}
	resolution.TimeBasis = "unknown"
	resolution.Candidates = slices.Clone(r.addresses[strings.TrimPrefix(id, source.AddressPrefix)])
	if len(resolution.Candidates) == 0 {
		resolution.Method = model.IdentityUnresolved
		resolution.Detail = "No successful inventory observation names an owner of this address. The address remains unresolved."
		return resolution, false
	}
	owner := resolution.Candidates[0].ID
	for _, candidate := range resolution.Candidates[1:] {
		if candidate.ID != owner {
			resolution.Method = model.IdentityAmbiguous
			resolution.Detail = "Inventories report different owners for the same address. No owner is selected; candidates are preserved independently of source order."
			return resolution, false
		}
	}
	resolution.ResolvedID = owner
	resolution.Method = model.IdentityInferred
	resolution.Detail = "A unique inventory owner supplies the name, but the available timestamps do not establish ownership throughout the observation."
	if observation == nil {
		return resolution, false
	}
	// Keep the existing conservative evaluation-window guard. A snapshot clock
	// may keep that guard conservative for older adapters, but it is never
	// presented as an original observation or a verified temporal match.
	at := result.Observation.EvaluatedAt
	switch {
	case !at.IsZero():
		resolution.TimeBasis = "evaluation-window"
	case !result.CollectedAt.IsZero():
		at, resolution.TimeBasis = result.CollectedAt, "collection-window"
	default:
		at = r.now
	}
	start := at.Add(-max(observation.Window, 0))
	if !observation.ObservedAt.IsZero() && observation.ObservedAt.Before(start) {
		start = observation.ObservedAt
	}
	if !observation.ObservedFrom.IsZero() && observation.ObservedFrom.Before(start) {
		start = observation.ObservedFrom
	}
	// A sample without a query window can establish its own original envelope.
	if observation.Window <= 0 && !observation.ObservedFrom.IsZero() && !observation.ObservedAt.IsZero() {
		start, at, resolution.TimeBasis = observation.ObservedFrom, observation.ObservedAt, "sample-envelope"
	} else if observation.Window <= 0 && observation.ObservedFrom.IsZero() && !observation.ObservedAt.IsZero() {
		start, at, resolution.TimeBasis = observation.ObservedAt, observation.ObservedAt, "sample-point"
	}
	validPeriod := !at.After(r.now) && !start.After(at) && !observation.ObservedFrom.After(r.now) && !observation.ObservedAt.After(r.now)
	if !observation.ObservedFrom.IsZero() && !observation.ObservedAt.IsZero() && observation.ObservedFrom.After(observation.ObservedAt) {
		validPeriod = false
	}
	known := resolution.TimeBasis != "unknown" && validPeriod
	missingSampleTime := observation.SampleCount > 0 && (observation.ObservedFrom.IsZero() || observation.ObservedAt.IsZero())
	known = known && !missingSampleTime
	for _, candidate := range resolution.Candidates {
		if !candidate.Since.IsZero() && candidate.Since.After(start) && validPeriod {
			resolution.ResolvedID, resolution.Method = id, model.IdentityAmbiguous
			resolution.Detail = "The current owner's reported start falls after the observation begins. The window may contain a previous IP owner; the connection is withheld instead of attributed to this workload."
			return resolution, true
		}
		known = known && !candidate.Since.IsZero() && !candidate.Retained
	}
	switch {
	case known:
		resolution.Method = model.IdentityIPTemporal
		resolution.Detail = "One canonical owner is reported and its inventory start covers the observation period. The start is a container, pod or Service timestamp, not an independently verified IP lease."
	case !validPeriod:
		resolution.Detail = "The inventory supplies a unique name, but invalid or future observation timestamps prevent a temporal ownership claim."
	case missingSampleTime:
		resolution.Detail = "Sampled calls lack a complete original time envelope; the query or collection time cannot establish ownership of every sampled call."
	case slices.ContainsFunc(resolution.Candidates, func(c model.IdentityCandidate) bool { return c.Retained }):
		resolution.Detail = "The name depends on retained inventory. Its original timestamps are preserved, but current address ownership has not been refreshed."
	}
	return resolution, false
}

func appendIdentity(have []model.IdentityResolution, entry model.IdentityResolution) []model.IdentityResolution {
	if !slices.ContainsFunc(have, func(e model.IdentityResolution) bool { return reflect.DeepEqual(e, entry) }) {
		return append(have, entry)
	}
	return have
}

func retainIdentity(entries []model.IdentityResolution) []model.IdentityResolution {
	out := slices.Clone(entries)
	for i := range out {
		out[i].Retained = true
		out[i].Candidates = slices.Clone(out[i].Candidates)
		for j := range out[i].Candidates {
			out[i].Candidates[j].Retained = true
		}
	}
	return out
}
