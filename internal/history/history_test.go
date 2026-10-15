package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func sample(at time.Time, round int64) model.Snapshot {
	return model.Snapshot{Round: round, GeneratedAt: at, TimeMode: "current", Nodes: []model.Node{
		{ID: "web", Kind: model.KindService, Addresses: []string{"10.0.0.1"}},
		{ID: "db", Kind: model.KindDatabase, Addresses: []string{"10.0.0.2"}},
	}, Edges: []model.Edge{{From: "web", To: "db", Observed: true, RequestRate: 2, Health: model.HealthHealthy, Evidence: []model.Evidence{
		{Source: "tempo", Signal: "traces", CollectedAt: at, ObservedAt: at.Add(-time.Minute), ObservedFrom: at.Add(-2 * time.Minute), WindowSeconds: 300, SampleCount: 2, Metrics: []string{"topology"}},
	}}}}
}

func archive(t *testing.T, cfg Config) *Store {
	t.Helper()
	if cfg.Directory == "" {
		cfg.Directory = t.TempDir()
	}
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func save(t *testing.T, s *Store, snapshot model.Snapshot) string {
	t.Helper()
	id, err := s.Save(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRestartAndReplayPreserveActualEvidence(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{})
	original := sample(at, 1)
	id := save(t, s, original)
	got, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeMode != "historical" || got.History.ID != id {
		t.Fatalf("unmarked replay: %+v", got)
	}
	if !reflect.DeepEqual(got.Edges, original.Edges) {
		t.Fatal("replay replaced original evidence or measurements")
	}
	p := got.History.Edges["web->db"]
	if !p.FirstSeen.Equal(at) || !p.LastObservedAt.Equal(at.Add(-time.Minute)) || !p.FirstObservedAt.Equal(at.Add(-2*time.Minute)) {
		t.Fatalf("presence and observation time mixed: %+v", p)
	}
	restarted := archive(t, s.cfg)
	after, err := restarted.Snapshot(id)
	if err != nil || !reflect.DeepEqual(got, after) {
		t.Fatalf("restart changed historical snapshot: %v", err)
	}
	second := sample(at.Add(time.Minute), 1) // process round counter may restart
	id2 := save(t, restarted, second)
	if id == id2 {
		t.Fatal("round reuse overwrote history")
	}
	latest, _ := restarted.Snapshot(id2)
	if !latest.History.Edges["web->db"].FirstSeen.Equal(at) {
		t.Fatal("restart reset first seen")
	}
}

func TestDisappearanceReappearanceAndIdentityReuse(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{})
	id1 := save(t, s, sample(at, 1))
	missing := sample(at.Add(time.Minute), 2)
	missing.Nodes = nil
	missing.Edges = nil
	save(t, s, missing)
	id3 := save(t, s, sample(at.Add(2*time.Minute), 3))
	before, _ := s.Snapshot(id1)
	after, _ := s.Snapshot(id3)
	if before.History.Nodes["web"].Episode == after.History.Nodes["web"].Episode || !after.History.Nodes["web"].FirstSeen.Equal(at.Add(2*time.Minute)) {
		t.Fatal("disappearance must start a new presence episode")
	}
	reused := sample(at.Add(3*time.Minute), 4)
	reused.Nodes[0].Addresses = []string{"10.0.0.99"}
	id4 := save(t, s, reused)
	changed, _ := s.Snapshot(id4)
	if after.History.Nodes["web"].Identity == changed.History.Nodes["web"].Identity || after.History.Edges["web->db"].Episode == changed.History.Edges["web->db"].Episode {
		t.Fatal("address identity change merged old evidence")
	}
	oldAgain, _ := s.Snapshot(id1)
	if oldAgain.Nodes[0].Addresses[0] != "10.0.0.1" {
		t.Fatal("current identity mutated old topology")
	}
}

func TestLateDataHasItsOwnOriginalSnapshotAndQueryPeriod(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{})
	latest := save(t, s, sample(at, 2))
	late := save(t, s, sample(at.Add(-time.Hour), 1))
	list, err := s.List(at.Add(-2*time.Hour), at.Add(-time.Minute))
	if err != nil || len(list.Snapshots) != 1 || list.Snapshots[0].ID != late || list.Coverage != "discrete-snapshots" {
		t.Fatalf("late data or period lost: %+v %v", list, err)
	}
	gap, _ := s.List(at.Add(-time.Minute), at.Add(-time.Second))
	if !gap.Unavailable || len(gap.Snapshots) != 0 || gap.AvailablePeriod == nil {
		t.Fatalf("gap advertised as measured: %+v", gap)
	}
	newest, _ := s.Snapshot(latest)
	if !newest.GeneratedAt.Equal(at) {
		t.Fatal("late insertion changed newer snapshot")
	}
}

func TestRetentionCountAndByteBounds(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{MaxSnapshots: 2, Retention: time.Hour})
	s.now = func() time.Time { return at }
	ids := []string{save(t, s, sample(at.Add(-3*time.Minute), 1)), save(t, s, sample(at.Add(-2*time.Minute), 2)), save(t, s, sample(at.Add(-time.Minute), 3))}
	if _, err := s.Snapshot(ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatal("oldest count-bound record survived")
	}
	list, _ := s.List(time.Time{}, time.Time{})
	if len(list.Snapshots) != 2 {
		t.Fatal("count limit not applied")
	}
	s.now = func() time.Time { return at.Add(2 * time.Hour) }
	list, _ = s.List(time.Time{}, time.Time{})
	if !list.Unavailable {
		t.Fatal("idle retention expiry not applied")
	}

	bytesStore := archive(t, Config{MaxBytes: 4500})
	bytesStore.now = func() time.Time { return at }
	for i := 0; i < 6; i++ {
		save(t, bytesStore, sample(at.Add(time.Duration(i)*time.Second), int64(i)))
	}
	list, _ = bytesStore.List(time.Time{}, time.Time{})
	var total int64
	for _, entry := range list.Snapshots {
		total += entry.Bytes
	}
	if total > 4500 || len(list.Snapshots) == 6 {
		t.Fatalf("byte cap ignored: %d bytes / %d records", total, len(list.Snapshots))
	}
	large := sample(at.Add(time.Minute), 9)
	large.Nodes[0].Labels = map[string]string{"blob": strings.Repeat("x", 5000)}
	if _, err := bytesStore.Save(large); err == nil {
		t.Fatal("oversized record accepted")
	}
	after, _ := bytesStore.List(time.Time{}, time.Time{})
	if len(after.Snapshots) != len(list.Snapshots) || after.WriteError == "" {
		t.Fatal("failed archive write deleted history or was invisible")
	}
}

func TestKnownSchemaMigrationAndInterruptedWriteRecovery(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().UTC().Truncate(time.Second)
	legacy := record{Snapshot: sample(at, 1)}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "legacy.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".teia-history-crash.tmp"), []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	s := archive(t, Config{Directory: dir})
	list, _ := s.List(time.Time{}, time.Time{})
	if len(list.Snapshots) != 1 {
		t.Fatalf("migration: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(dir, "legacy.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy duplicate retained")
	}
	if _, err := os.Stat(filepath.Join(dir, ".teia-history-crash.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted temporary write leaked outside the storage bound")
	}
	r, _, err := readRecord(filepath.Join(dir, list.Snapshots[0].ID+".json"), s.cfg.MaxBytes)
	if err != nil || r.SchemaVersion != SchemaVersion || !r.Snapshot.Edges[0].Evidence[0].ObservedAt.Equal(at.Add(-time.Minute)) {
		t.Fatalf("migration changed evidence: %+v %v", r, err)
	}
	if _, err := Open(s.cfg); err != nil {
		t.Fatalf("migrated archive cannot restart: %v", err)
	}
}

func TestCorruptAndFutureSchemasAreNotSilentlyRewritten(t *testing.T) {
	for _, raw := range []string{`{"schemaVersion":`, `{"schemaVersion":99,"snapshot":{"generatedAt":"2026-10-15T00:00:00Z"}}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(Config{Directory: dir}); err == nil {
			t.Fatalf("accepted corrupt/future schema %s", raw)
		}
		got, _ := os.ReadFile(path)
		if string(got) != raw {
			t.Fatal("failed startup modified source record")
		}
	}
}

func TestLastObservedEdgeSkipsRetentionAndUnknownOriginalTime(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{})
	save(t, s, sample(at.Add(-2*time.Minute), 1))
	retained := sample(at.Add(-time.Minute), 2)
	retained.Edges[0].Evidence[0].Retained = true
	save(t, s, retained)
	unknown := sample(at, 3)
	unknown.Edges[0].Evidence[0].ObservedAt = time.Time{}
	unknown.Edges[0].Evidence[0].ObservedFrom = time.Time{}
	save(t, s, unknown)
	edge, captured, ok, err := s.LastObservedEdge("web->db")
	if err != nil || !ok || !captured.Equal(at.Add(-2*time.Minute)) || !edge.Evidence[0].ObservedAt.Equal(at.Add(-3*time.Minute)) {
		t.Fatalf("invented observation time: %+v %v %v %v", edge, captured, ok, err)
	}
	var disabled *Store
	if list, err := disabled.List(time.Time{}, time.Time{}); err != nil || list.Enabled || !list.Unavailable {
		t.Fatalf("disabled archive: %+v %v", list, err)
	}
	if _, err := s.Snapshot("../../secret"); !errors.Is(err, ErrNotFound) {
		t.Fatal("accepted invalid snapshot ID")
	}
	late := sample(at.Add(time.Minute), 4)
	late.Edges[0].Evidence[0].ObservedAt = at.Add(-time.Hour)
	late.Edges[0].Evidence[0].ObservedFrom = at.Add(-2 * time.Hour)
	save(t, s, late)
	_, captured, ok, err = s.LastObservedEdge("web->db")
	if err != nil || !ok || !captured.Equal(at.Add(-2*time.Minute)) {
		t.Fatal("newer capture carrying older observations replaced last known original time")
	}
}

func TestLastObservedEdgeKeepsValidEarlierEvidenceAfterRestart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.Edge, time.Time)
	}{
		{"future", func(e *model.Edge, now time.Time) { e.Evidence[0].ObservedAt = now.Add(time.Hour) }},
		{"inverted", func(e *model.Edge, now time.Time) { e.Evidence[0].ObservedFrom = now.Add(time.Hour) }},
		{"non-topology", func(e *model.Edge, _ time.Time) { e.Evidence[0].Metrics = []string{"latency"} }},
		{"retained", func(e *model.Edge, _ time.Time) { e.Evidence[0].Retained = true }},
		{"not-observed", func(e *model.Edge, _ time.Time) { e.Observed = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			store := archive(t, Config{})
			store.now = func() time.Time { return now }
			older := sample(now.Add(-time.Hour), 1)
			save(t, store, older)
			newer := sample(now, 2)
			tc.mutate(&newer.Edges[0], now)
			save(t, store, newer)
			restarted := archive(t, store.cfg)
			restarted.now = func() time.Time { return now }
			edge, captured, found, err := restarted.LastObservedEdge("web->db")
			if err != nil || !found || !captured.Equal(older.GeneratedAt) || !edge.LatestTopologyObservedAt(now).Equal(older.Edges[0].Evidence[0].ObservedAt) {
				t.Fatalf("newer ineligible evidence hid the earlier call: capture=%s found=%v err=%v edge=%+v", captured, found, err, edge)
			}
		})
	}
}
