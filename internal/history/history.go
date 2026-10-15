// Package history persists bounded, immutable discovery snapshots. It never
// interpolates measurements or substitutes collection time for observation time.
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

const SchemaVersion = 1

var ErrNotFound = errors.New("historical snapshot is unavailable")
var validID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[a-f0-9]{16}$`)

type Config struct {
	Directory    string        `yaml:"directory"`
	Retention    time.Duration `yaml:"retention"`
	MaxBytes     int64         `yaml:"maxBytes"`
	MaxSnapshots int           `yaml:"maxSnapshots"`
}

func (c Config) defaults() Config {
	if c.Retention == 0 {
		c.Retention = 7 * 24 * time.Hour
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 256 << 20
	}
	if c.MaxSnapshots == 0 {
		c.MaxSnapshots = 10000
	}
	return c
}

func (c Config) Validate() error {
	if c.Retention < 0 {
		return errors.New("history.retention must not be negative")
	}
	if c.MaxBytes < 0 {
		return errors.New("history.maxBytes must not be negative")
	}
	if c.MaxSnapshots < 0 || c.MaxSnapshots > 100000 {
		return errors.New("history.maxSnapshots must be between 0 and 100000")
	}
	if c.Directory == "" && (c.Retention != 0 || c.MaxBytes != 0 || c.MaxSnapshots != 0) {
		return errors.New("history.directory is required when history limits are set")
	}
	return nil
}

type Entry struct {
	ID          string    `json:"id"`
	Round       int64     `json:"round"`
	GeneratedAt time.Time `json:"generatedAt"`
	RecordedAt  time.Time `json:"recordedAt"`
	Nodes       int       `json:"nodes"`
	Edges       int       `json:"edges"`
	Bytes       int64     `json:"bytes"`
}

type Period struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type Listing struct {
	Enabled          bool    `json:"enabled"`
	SchemaVersion    int     `json:"schemaVersion"`
	Coverage         string  `json:"coverage"`
	Unavailable      bool    `json:"unavailable"`
	RequestedPeriod  *Period `json:"requestedPeriod,omitempty"`
	AvailablePeriod  *Period `json:"availablePeriod,omitempty"`
	Snapshots        []Entry `json:"snapshots"`
	RetentionSeconds float64 `json:"retentionSeconds"`
	MaxBytes         int64   `json:"maxBytes"`
	MaxSnapshots     int     `json:"maxSnapshots"`
	WriteError       string  `json:"writeError,omitempty"`
}

type record struct {
	SchemaVersion int                             `json:"schemaVersion"`
	ID            string                          `json:"id"`
	RecordedAt    time.Time                       `json:"recordedAt"`
	Snapshot      model.Snapshot                  `json:"snapshot"`
	Nodes         map[string]model.EntityPresence `json:"nodes"`
	Edges         map[string]model.EntityPresence `json:"edges"`
}

type Store struct {
	mu         sync.RWMutex
	cfg        Config
	entries    []Entry
	now        func() time.Time
	writeError string
}

// Open recovers published records and migrates the explicitly supported v0
// envelope. Incomplete temporary files are removed; corrupt or newer schemas
// fail startup without modifying them. One Teia process owns a directory.
func Open(cfg Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Directory == "" {
		return nil, nil
	}
	cfg = cfg.defaults()
	path, err := filepath.Abs(cfg.Directory)
	if err != nil {
		return nil, err
	}
	cfg.Directory = path
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, fmt.Errorf("creating history directory: %w", err)
	}
	s := &Store{cfg: cfg, now: time.Now}
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), ".teia-history-") && strings.HasSuffix(file.Name(), ".tmp") {
			if err := os.Remove(filepath.Join(cfg.Directory, file.Name())); err != nil {
				return nil, fmt.Errorf("removing interrupted history write: %w", err)
			}
			continue
		}
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		if file.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("history %s: symbolic links are not supported", file.Name())
		}
		path := filepath.Join(cfg.Directory, file.Name())
		r, size, err := readRecord(path, cfg.MaxBytes)
		if err != nil {
			return nil, fmt.Errorf("history %s: %w", file.Name(), err)
		}
		if r.Snapshot.GeneratedAt.IsZero() {
			return nil, fmt.Errorf("history %s: missing snapshot generatedAt", file.Name())
		}
		migrated := r.SchemaVersion == 0
		if migrated {
			r.SchemaVersion = SchemaVersion
			r.ID = snapshotID(r.Snapshot)
			if r.RecordedAt.IsZero() {
				r.RecordedAt = r.Snapshot.GeneratedAt
			}
			r.Nodes, r.Edges = presence(r.Snapshot, record{})
			data, err := json.Marshal(r)
			if err != nil {
				return nil, err
			}
			if int64(len(data)) > cfg.MaxBytes {
				return nil, errors.New("migrated history record exceeds history.maxBytes")
			}
			if err := s.publish(r.ID, data); err != nil {
				return nil, fmt.Errorf("migrating history: %w", err)
			}
			if file.Name() != r.ID+".json" {
				if err := os.Remove(path); err != nil {
					return nil, err
				}
			}
			size = int64(len(data))
		} else if r.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("history %s: unsupported schemaVersion %d (supported %d)", file.Name(), r.SchemaVersion, SchemaVersion)
		}
		if !validID.MatchString(r.ID) || r.ID != snapshotID(r.Snapshot) {
			return nil, fmt.Errorf("history %s: invalid snapshot identity", file.Name())
		}
		if !migrated && file.Name() != r.ID+".json" {
			return nil, fmt.Errorf("history %s: filename does not match snapshot identity", file.Name())
		}
		if !slices.ContainsFunc(s.entries, func(e Entry) bool { return e.ID == r.ID }) {
			s.entries = append(s.entries, entry(r, size))
		}
	}
	s.sort()
	if err := s.prune(s.now()); err != nil {
		return nil, err
	}
	return s, nil
}

func readRecord(path string, maxBytes int64) (record, int64, error) {
	var r record
	f, err := os.Open(path)
	if err != nil {
		return r, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return r, 0, err
	}
	if info.Size() > maxBytes {
		return r, 0, errors.New("record exceeds history.maxBytes")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return r, 0, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, 0, fmt.Errorf("invalid JSON: %w", err)
	}
	return r, int64(len(data)), nil
}

func snapshotID(s model.Snapshot) string {
	s.History = nil
	data, _ := json.Marshal(s)
	hash := sha256.Sum256(data)
	return s.GeneratedAt.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(hash[:8])
}

func entry(r record, size int64) Entry {
	return Entry{ID: r.ID, Round: r.Snapshot.Round, GeneratedAt: r.Snapshot.GeneratedAt, RecordedAt: r.RecordedAt, Nodes: len(r.Snapshot.Nodes), Edges: len(r.Snapshot.Edges), Bytes: size}
}

func (s *Store) sort() {
	slices.SortFunc(s.entries, func(a, b Entry) int {
		if c := a.GeneratedAt.Compare(b.GeneratedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// Save publishes one exact snapshot. A failed write never reports success and
// never deletes existing history to make space for an oversized record.
func (s *Store) Save(snapshot model.Snapshot) (string, error) {
	if s == nil {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.save(snapshot)
	if err != nil {
		s.writeError = "a snapshot could not be stored; check the server log"
	} else {
		s.writeError = ""
	}
	return id, err
}

func (s *Store) save(snapshot model.Snapshot) (string, error) {
	if snapshot.GeneratedAt.IsZero() {
		return "", errors.New("history snapshot requires generatedAt")
	}
	if snapshot.GeneratedAt.Before(s.now().Add(-s.cfg.Retention)) {
		return "", errors.New("snapshot is older than history.retention; previous history was preserved")
	}
	if snapshot.TimeMode == "historical" {
		return "", errors.New("cannot archive a historical replay as a new observation")
	}
	snapshot.History = nil
	id := snapshotID(snapshot)
	if slices.ContainsFunc(s.entries, func(e Entry) bool { return e.ID == id }) {
		return id, nil
	}
	var previous record
	for i := len(s.entries) - 1; i >= 0; i-- {
		if !s.entries[i].GeneratedAt.After(snapshot.GeneratedAt) {
			var err error
			previous, _, err = readRecord(filepath.Join(s.cfg.Directory, s.entries[i].ID+".json"), s.cfg.MaxBytes)
			if err != nil {
				return "", err
			}
			break
		}
	}
	nodes, edges := presence(snapshot, previous)
	r := record{SchemaVersion: SchemaVersion, ID: id, RecordedAt: s.now().UTC(), Snapshot: snapshot, Nodes: nodes, Edges: edges}
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("encoding history: %w", err)
	}
	if int64(len(data)) > s.cfg.MaxBytes {
		return "", errors.New("snapshot exceeds history.maxBytes; previous history was preserved")
	}
	if err := s.publish(id, data); err != nil {
		return "", err
	}
	s.entries = append(s.entries, entry(r, int64(len(data))))
	s.sort()
	if err := s.prune(s.now()); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) publish(id string, data []byte) error {
	f, err := os.CreateTemp(s.cfg.Directory, ".teia-history-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(s.cfg.Directory, id+".json")); err != nil {
		return err
	}
	return nil
}

func (s *Store) prune(now time.Time) error {
	var total int64
	for _, e := range s.entries {
		total += e.Bytes
	}
	cut := now.Add(-s.cfg.Retention)
	for len(s.entries) > 0 && (len(s.entries) > s.cfg.MaxSnapshots || total > s.cfg.MaxBytes || s.entries[0].GeneratedAt.Before(cut)) {
		e := s.entries[0]
		if err := os.Remove(filepath.Join(s.cfg.Directory, e.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pruning history: %w", err)
		}
		total -= e.Bytes
		s.entries = s.entries[1:]
	}
	return nil
}

// List reports discrete stored snapshots. No matching records means unavailable
// history, never proof of no traffic. Retention is also enforced while idle.
func (s *Store) List(from, to time.Time) (Listing, error) {
	result := Listing{SchemaVersion: SchemaVersion, Coverage: "discrete-snapshots", Unavailable: true, Snapshots: []Entry{}}
	if !from.IsZero() || !to.IsZero() {
		result.RequestedPeriod = &Period{From: from, To: to}
	}
	if s == nil {
		return result, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.prune(s.now()); err != nil {
		return result, err
	}
	result.Enabled = true
	result.RetentionSeconds = s.cfg.Retention.Seconds()
	result.MaxBytes = s.cfg.MaxBytes
	result.MaxSnapshots = s.cfg.MaxSnapshots
	result.WriteError = s.writeError
	if len(s.entries) > 0 {
		result.AvailablePeriod = &Period{From: s.entries[0].GeneratedAt, To: s.entries[len(s.entries)-1].GeneratedAt}
	}
	for _, e := range s.entries {
		if (from.IsZero() || !e.GeneratedAt.Before(from)) && (to.IsZero() || !e.GeneratedAt.After(to)) {
			result.Snapshots = append(result.Snapshots, e)
		}
	}
	result.Unavailable = len(result.Snapshots) == 0
	return result, nil
}

func (s *Store) Snapshot(id string) (model.Snapshot, error) {
	if s == nil || !validID.MatchString(id) {
		return model.Snapshot{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.prune(s.now()); err != nil {
		return model.Snapshot{}, err
	}
	if !slices.ContainsFunc(s.entries, func(e Entry) bool { return e.ID == id }) {
		return model.Snapshot{}, ErrNotFound
	}
	r, _, err := readRecord(filepath.Join(s.cfg.Directory, id+".json"), s.cfg.MaxBytes)
	if err != nil {
		return model.Snapshot{}, err
	}
	r.Snapshot.TimeMode = "historical"
	r.Snapshot.History = &model.HistoryContext{ID: r.ID, RecordedAt: r.RecordedAt, Availability: "stored-snapshot", Nodes: r.Nodes, Edges: r.Edges}
	return r.Snapshot, nil
}

// LastObservedEdge returns the retained snapshot carrying the latest known
// original observation. Capture order may differ because sources can deliver
// late data. Retained or timestamp-less evidence cannot establish a new call.
func (s *Store) LastObservedEdge(key string) (model.Edge, time.Time, bool, error) {
	if s == nil {
		return model.Edge{}, time.Time{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if err := s.prune(now); err != nil {
		return model.Edge{}, time.Time{}, false, err
	}
	var best model.Edge
	var capture, observed time.Time
	for i := len(s.entries) - 1; i >= 0; i-- {
		r, _, err := readRecord(filepath.Join(s.cfg.Directory, s.entries[i].ID+".json"), s.cfg.MaxBytes)
		if err != nil {
			return model.Edge{}, time.Time{}, false, err
		}
		for _, e := range r.Snapshot.Edges {
			if e.Key() != key {
				continue
			}
			if at := e.LatestTopologyObservedAt(now); at.After(observed) {
				best, capture, observed = e, r.Snapshot.GeneratedAt, at
			}
		}
	}
	return best, capture, !observed.IsZero(), nil
}

func presence(s model.Snapshot, previous record) (map[string]model.EntityPresence, map[string]model.EntityPresence) {
	nodes := map[string]model.EntityPresence{}
	edges := map[string]model.EntityPresence{}
	for _, n := range s.Nodes {
		addresses := slices.Clone(n.Addresses)
		slices.Sort(addresses)
		identity := fingerprint(n.ID, string(n.Kind), n.Labels["k8s.namespace"], strings.Join(addresses, ","))
		nodes[n.ID] = seen(s.GeneratedAt, identity, previous.Nodes[n.ID], n.Evidence)
	}
	for _, e := range s.Edges {
		identity := fingerprint(e.Key(), nodes[e.From].Identity, nodes[e.To].Identity)
		edges[e.Key()] = seen(s.GeneratedAt, identity, previous.Edges[e.Key()], e.Evidence)
	}
	return nodes, edges
}

func fingerprint(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func seen(at time.Time, identity string, previous model.EntityPresence, evidence []model.Evidence) model.EntityPresence {
	p := previous
	if p.Identity != identity || p.FirstSeen.IsZero() {
		p = model.EntityPresence{Identity: identity, FirstSeen: at, Episode: fingerprint(identity, at.Format(time.RFC3339Nano)), IdentityConfidence: "stable-id-and-addresses-only"}
	}
	p.LastSeen = at
	for _, ev := range evidence {
		first := ev.ObservedFrom
		if first.IsZero() {
			first = ev.ObservedAt
		}
		if !first.IsZero() && (p.FirstObservedAt.IsZero() || first.Before(p.FirstObservedAt)) {
			p.FirstObservedAt = first
		}
		if ev.ObservedAt.After(p.LastObservedAt) {
			p.LastObservedAt = ev.ObservedAt
		}
	}
	return p
}
