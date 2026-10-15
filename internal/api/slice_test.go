package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
)

// slicingEngine answers a sliced snapshot naming the selector it was given.
type slicingEngine struct {
	*fakeEngine
	asked []string
	// operations, when set, are the sliced round's own.
	operations *model.OperationContract
}

func (s *slicingEngine) Sliced(sel slice.Selector) model.Snapshot {
	s.asked = append(s.asked, sel.String())
	snap := s.snap
	snap.Nodes = []model.Node{{ID: "app-a"}}
	snap.Slice = &model.SliceScope{Selector: sel, Matched: true, Metrics: "scoped"}
	if s.operations != nil {
		snap.Operations = s.operations
	}
	return snap
}

func TestSliceParameter(t *testing.T) {
	eng := &slicingEngine{fakeEngine: newFakeEngine()}
	h := Handler(eng, Options{})
	for _, path := range []string{"snapshot", "graph", "findings", "coverage", "journeys", "operations", "grafana/nodes", "grafana/edges", "grafana/findings", "export/matrix.csv"} {
		if rec := get(h, "/api/v1/"+path+"?slice=tenant:a,journey:notification", nil); rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if eng.asked[0] != "journey:notification,tenant:a" {
		t.Fatalf("selector = %v", eng.asked)
	}
	var snap model.Snapshot
	if err := json.Unmarshal(get(h, "/api/v1/snapshot?slice=journey:notification", nil).Body.Bytes(), &snap); err != nil ||
		snap.Slice == nil || snap.Slice.Selector["journey"] != "notification" || snap.Nodes[0].ID != "app-a" {
		t.Fatalf("sliced snapshot = %+v (%v)", snap, err)
	}
	var whole model.Snapshot
	if err := json.Unmarshal(get(h, "/api/v1/snapshot", nil).Body.Bytes(), &whole); err != nil || whole.Slice != nil || whole.Nodes[0].ID != "orders" {
		t.Fatal("without ?slice= the whole snapshot is served")
	}
	for _, bad := range []string{"journey", "journey:a,journey:b", "a,b:c", ":x"} {
		if rec := get(h, "/api/v1/snapshot?slice="+bad, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status %d", bad, rec.Code)
		}
	}
	if rec := get(Handler(newFakeEngine(), Options{}), "/api/v1/snapshot?slice=journey:a", nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("an engine that cannot slice must say so, got %d", rec.Code)
	}
}

func TestDimensionsEndpoint(t *testing.T) {
	eng := newFakeEngine()
	eng.snap.Dimensions = map[string][]string{"journey": {"notification", "whatsapp"}}
	var got map[string][]string
	if err := json.Unmarshal(get(Handler(eng, Options{}), "/api/v1/dimensions", nil).Body.Bytes(), &got); err != nil || len(got["journey"]) != 2 {
		t.Fatalf("dimensions = %v (%v)", got, err)
	}
}

func TestStreamSendsSlices(t *testing.T) {
	eng := &slicingEngine{fakeEngine: newFakeEngine()}
	ts := httptest.NewServer(Handler(eng, Options{}))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/v1/stream?slice=journey:whatsapp")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %v %v", resp, err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	var snap model.Snapshot
	if _, data := nextEvent(t, r); json.Unmarshal([]byte(data), &snap) != nil || snap.Slice == nil || snap.Slice.Selector["journey"] != "whatsapp" {
		t.Fatalf("first sliced event = %s", data)
	}
	eng.updates <- []byte(`{"round":2}`)
	if _, data := nextEvent(t, r); json.Unmarshal([]byte(data), &snap) != nil || snap.Slice == nil {
		t.Fatalf("every update must be sliced: %s", data)
	}
}

// Frames cannot show that they were sliced: the first node row and the
// coverage row carry the slice they hold, and the values a panel can offer.
func TestGrafanaFramesSayWhichSliceTheyHold(t *testing.T) {
	eng := &slicingEngine{fakeEngine: newFakeEngine()}
	eng.snap.Dimensions = map[string][]string{"journey": {"browse", "checkout"}}
	h := Handler(eng, Options{})
	type metadata struct{ Slice, Dimensions string }
	frames := func(query string) (node, coverage metadata) {
		var nodes []metadata
		if err := json.Unmarshal(get(h, "/api/v1/grafana/nodes"+query, nil).Body.Bytes(), &nodes); err != nil || len(nodes) == 0 {
			t.Fatalf("nodes%s: %v", query, err)
		}
		if err := json.Unmarshal(get(h, "/api/v1/coverage"+query, nil).Body.Bytes(), &coverage); err != nil {
			t.Fatalf("coverage%s: %v", query, err)
		}
		return nodes[0], coverage
	}
	node, coverage := frames("")
	if node.Slice != "" || coverage.Slice != "" || node.Dimensions != `{"journey":["browse","checkout"]}` || coverage.Dimensions != node.Dimensions {
		t.Fatalf("whole frames = %+v / %+v", node, coverage)
	}
	node, coverage = frames("?slice=journey:checkout")
	var scope model.SliceScope
	if err := json.Unmarshal([]byte(node.Slice), &scope); err != nil || scope.Selector["journey"] != "checkout" || !scope.Matched || coverage.Slice != node.Slice {
		t.Fatalf("sliced frames = %+v / %+v (%v)", node, coverage, err)
	}
	if node.Dimensions != `{"journey":["browse","checkout"]}` {
		t.Fatalf("a sliced frame still offers every value, got %s", node.Dimensions)
	}
}
