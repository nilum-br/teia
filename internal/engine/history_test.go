package engine

import (
	"context"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/source"
)

func TestEngineArchivesEachRoundWithoutChangingCurrentMode(t *testing.T) {
	store, err := history.Open(history.Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	src := &scripted{name: "traffic", typ: "tempo", steps: []func(context.Context) (source.Observation, error){returns(source.Observation{Edges: []source.EdgeObservation{{From: "web", To: "db", Signal: source.SignalTraces, ObservedAt: time.Now().Add(-time.Minute)}}})}}
	eng := New([]source.Source{src}, Options{History: store}, quiet())
	current := eng.Refresh(context.Background())
	if current.TimeMode != "current" || current.History != nil {
		t.Fatal("archiving relabelled current discovery")
	}
	list, err := store.List(time.Time{}, time.Time{})
	if err != nil || len(list.Snapshots) != 1 {
		t.Fatalf("round was not archived: %+v %v", list, err)
	}
	replay, err := store.Snapshot(list.Snapshots[0].ID)
	if err != nil || replay.TimeMode != "historical" || replay.Journeys == nil || !replay.Edges[0].Evidence[0].ObservedAt.Equal(current.Edges[0].Evidence[0].ObservedAt) {
		t.Fatalf("incomplete archived round: %+v %v", replay, err)
	}
}
