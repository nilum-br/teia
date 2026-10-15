// Package evidencelinks builds public investigation links from explicit
// templates and graph context. It never reads source URLs, headers or secrets.
package evidencelinks

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// Template has a fixed public URL and separately encoded query parameters.
// Tokens are {{id}}, {{from}}, {{to}}, {{namespace}}, {{tech}},
// {{fromNamespace}}, {{toNamespace}}, {{fromTech}}, {{toTech}}, {{protocol}},
// {{period.from}}, {{period.to}}, {{period.fromMs}} and {{period.toMs}}.
type Template struct {
	Label    string            `yaml:"label"`
	Kind     string            `yaml:"kind"`   // traces, logs, dashboard
	Target   string            `yaml:"target"` // node or edge
	URL      string            `yaml:"url"`
	Source   string            `yaml:"source"` // optional exact evidence source name
	Params   map[string]string `yaml:"params"`
	Requires []string          `yaml:"requires"`
	Explore  *Explore          `yaml:"explore"`
}

// Explore builds Grafana's versioned panes JSON. Each query token MUST use
// |json, e.g. { resource.service.name = {{id|json}} }, so a name cannot inject
// query syntax. JSON serialization and URL encoding happen after expansion.
type Explore struct {
	DatasourceUID string `yaml:"datasourceUID"`
	Type          string `yaml:"type"` // tempo or loki
	Query         string `yaml:"query"`
}

var token = regexp.MustCompile(`\{\{([A-Za-z][A-Za-z0-9.]*)(\|json)?\}\}`)
var names = map[string]bool{
	"id": true, "from": true, "to": true, "namespace": true, "tech": true,
	"fromNamespace": true, "toNamespace": true, "fromTech": true, "toTech": true,
	"protocol": true, "period.from": true, "period.to": true,
	"period.fromMs": true, "period.toMs": true,
}

func sensitive(key string) bool {
	key = strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(key))
	for _, part := range []string{"password", "secret", "token", "apikey", "authorization", "credential"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// SafeURL deliberately accepts only absolute http(s) destinations without
// userinfo, fragments or credential query keys. No executable/relative URLs.
func SafeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t\\") {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range q {
		if sensitive(key) {
			return false
		}
	}
	return true
}

func validateText(text string, quoted bool) error {
	for _, m := range token.FindAllStringSubmatch(text, -1) {
		if !names[m[1]] {
			return fmt.Errorf("unknown context field %q", m[1])
		}
		if quoted && m[2] != "|json" {
			return fmt.Errorf("Explore query context field %q needs |json quoting", m[1])
		}
	}
	if strings.Contains(token.ReplaceAllString(text, ""), "{{") || strings.Contains(token.ReplaceAllString(text, ""), "}}") {
		return fmt.Errorf("invalid context token")
	}
	return nil
}

// Validate rejects unsafe templates without including destination secrets in errors.
func Validate(t Template) error {
	if strings.TrimSpace(t.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if t.Target != "node" && t.Target != "edge" {
		return fmt.Errorf("target must be node or edge")
	}
	if t.Kind != "traces" && t.Kind != "logs" && t.Kind != "dashboard" {
		return fmt.Errorf("kind must be traces, logs or dashboard")
	}
	if !SafeURL(t.URL) || strings.Contains(t.URL, "{{") {
		return fmt.Errorf("url must be a fixed public http(s) URL without credentials or fragment")
	}
	for _, name := range t.Requires {
		if !names[name] {
			return fmt.Errorf("unknown required context field %q", name)
		}
	}
	for key, value := range t.Params {
		if sensitive(key) {
			return fmt.Errorf("credential query parameters are not allowed")
		}
		if err := validateText(value, false); err != nil {
			return err
		}
	}
	if e := t.Explore; e != nil {
		if (e.Type != "tempo" && e.Type != "loki") || e.DatasourceUID == "" || strings.TrimSpace(e.Query) == "" {
			return fmt.Errorf("explore needs type tempo/loki, datasourceUID and query")
		}
		if (e.Type == "tempo" && t.Kind != "traces") || (e.Type == "loki" && t.Kind != "logs") {
			return fmt.Errorf("explore type must match link kind")
		}
		if err := validateText(e.Query, true); err != nil {
			return err
		}
		if _, ok := t.Params["panes"]; ok {
			return fmt.Errorf("explore owns the panes parameter")
		}
		if _, ok := t.Params["schemaVersion"]; ok {
			return fmt.Errorf("explore owns the schemaVersion parameter")
		}
	}
	return nil
}

func expand(text string, values map[string]string) (string, bool) {
	ok := true
	text = token.ReplaceAllStringFunc(text, func(match string) string {
		m := token.FindStringSubmatch(match)
		value := values[m[1]]
		if value == "" {
			ok = false
		}
		if m[2] != "" {
			b, _ := json.Marshal(value)
			return string(b)
		}
		return value
	})
	return text, ok
}

// period uses observed sample bounds or an explicitly pinned evaluation and
// query lookback. A collection timestamp never becomes a measurement period.
// The envelope may combine evidence windows; it does not imply continuous data.
func period(evidence []model.Evidence, source string) (from, to time.Time) {
	for _, e := range evidence {
		if source != "" && e.Source != source {
			continue
		}
		a, b := e.ObservedFrom, e.ObservedAt
		if a.IsZero() || b.IsZero() || b.Before(a) {
			if e.EvaluatedAt.IsZero() || e.WindowSeconds <= 0 || math.IsNaN(e.WindowSeconds) || math.IsInf(e.WindowSeconds, 0) || e.WindowSeconds > float64(math.MaxInt64)/float64(time.Second) {
				continue
			}
			b = e.EvaluatedAt
			a = b.Add(-time.Duration(e.WindowSeconds * float64(time.Second)))
		}
		if from.IsZero() || a.Before(from) {
			from = a
		}
		if to.IsZero() || b.After(to) {
			to = b
		}
	}
	return
}

func namespace(n model.Node) string {
	for _, key := range []string{"k8s.namespace", "namespace", "service.namespace"} {
		if v := n.Labels[key]; v != "" {
			return v
		}
	}
	return ""
}

func build(t Template, values map[string]string, evidence []model.Evidence) (model.EvidenceLink, bool) {
	if Validate(t) != nil {
		return model.EvidenceLink{}, false
	}
	from, to := period(evidence, t.Source)
	if !from.IsZero() && !to.IsZero() {
		values["period.from"], values["period.to"] = from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)
		// Grafana ranges use milliseconds. Round outwards so the latest span's
		// fractional millisecond is included and point observations stay queryable.
		fromMS, toMS := from.UnixMilli(), to.UnixMilli()
		if !to.Equal(time.UnixMilli(toMS)) || toMS == fromMS {
			toMS++
		}
		values["period.fromMs"], values["period.toMs"] = strconv.FormatInt(fromMS, 10), strconv.FormatInt(toMS, 10)
	}
	for _, field := range t.Requires {
		if values[field] == "" {
			return model.EvidenceLink{}, false
		}
	}
	u, _ := url.Parse(t.URL)
	params := u.Query()
	for key, value := range t.Params {
		expanded, ok := expand(value, values)
		if !ok {
			return model.EvidenceLink{}, false
		}
		params.Set(key, expanded)
	}
	if e := t.Explore; e != nil {
		if from.IsZero() || to.IsZero() {
			return model.EvidenceLink{}, false
		}
		query, ok := expand(e.Query, values)
		if !ok {
			return model.EvidenceLink{}, false
		}
		ds := map[string]string{"uid": e.DatasourceUID, "type": e.Type}
		q := map[string]any{"refId": "A", "datasource": ds, "expr": query}
		if e.Type == "tempo" {
			delete(q, "expr")
			q["query"], q["queryType"] = query, "traceql"
		} else {
			q["queryType"], q["editorMode"] = "range", "code"
		}
		panes, _ := json.Marshal(map[string]any{"teia": map[string]any{"datasource": e.DatasourceUID, "queries": []any{q}, "range": map[string]string{"from": values["period.fromMs"], "to": values["period.toMs"]}}})
		params.Set("panes", string(panes))
		params.Set("schemaVersion", "1")
	}
	u.RawQuery = params.Encode()
	if !SafeURL(u.String()) {
		return model.EvidenceLink{}, false
	}
	description := "Configured dashboard"
	if t.Kind == "traces" {
		description = "Service/time search; not an exact trace"
	}
	if t.Kind == "logs" {
		description = "Service/time search; not correlated log lines"
	}
	return model.EvidenceLink{Label: t.Label, URL: u.String(), Kind: t.Kind, Description: description, PeriodFrom: from, PeriodTo: to}, true
}

// Apply attaches only links whose complete required context is known. Node
// searches can use evidence from their incident communications, including
// external services and resources that do not emit their own telemetry.
func Apply(s *model.Snapshot, templates []Template) {
	nodes := make(map[string]model.Node, len(s.Nodes))
	evidence := make(map[string][]model.Evidence, len(s.Nodes))
	for _, n := range s.Nodes {
		nodes[n.ID] = n
		evidence[n.ID] = append([]model.Evidence(nil), n.Evidence...)
	}
	for _, e := range s.Edges {
		evidence[e.From] = append(evidence[e.From], e.Evidence...)
		evidence[e.To] = append(evidence[e.To], e.Evidence...)
	}
	for i := range s.Nodes {
		n := &s.Nodes[i]
		n.Links = nil
		for _, t := range templates {
			if t.Target == "node" {
				if link, ok := build(t, map[string]string{"id": n.ID, "namespace": namespace(*n), "tech": n.Tech}, evidence[n.ID]); ok {
					n.Links = append(n.Links, link)
				}
			}
		}
	}
	for i := range s.Edges {
		e := &s.Edges[i]
		e.Links = nil
		for _, t := range templates {
			if t.Target == "edge" {
				if link, ok := build(t, map[string]string{"id": e.Key(), "from": e.From, "to": e.To, "fromNamespace": namespace(nodes[e.From]), "toNamespace": namespace(nodes[e.To]), "fromTech": nodes[e.From].Tech, "toTech": nodes[e.To].Tech, "protocol": e.Protocol}, e.Evidence); ok {
					e.Links = append(e.Links, link)
				}
			}
		}
	}
}
