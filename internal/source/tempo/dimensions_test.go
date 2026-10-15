package tempo

import (
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// The same app-a and app-b serve two journeys. Only the whatsapp trace calls
// redis; the redis client span has no journey attribute and inherits it.
func TestDimensionsFollowTheTrace(t *testing.T) {
	at := time.Unix(1000, 0)
	call := func(id, parent, service, kind string, attrs map[string]string, ms int) span {
		return span{id: id, parent: parent, service: service, kind: kind, attrs: attrs,
			dur: time.Duration(ms) * time.Millisecond, startedAt: at, endedAt: at.Add(time.Duration(ms) * time.Millisecond)}
	}
	whatsapp := []span{
		call("1", "", "app-a", "server", map[string]string{"business.journey": "whatsapp", "business.tenant": "a"}, 40),
		call("2", "1", "app-a", "client", map[string]string{"http.request.method": "GET"}, 35),
		call("3", "2", "app-b", "server", nil, 33),
		call("4", "3", "app-b", "client", map[string]string{"db.system": "redis", "peer.service": "redis"}, 2),
	}
	notification := []span{
		call("5", "", "app-a", "server", map[string]string{"business.journey": "notification", "business.tenant": "a"}, 170),
		call("6", "5", "app-a", "client", map[string]string{"http.request.method": "GET"}, 165),
		call("7", "6", "app-b", "server", map[string]string{"business.journey": "notification"}, 160),
	}
	p := newPairing(map[string]string{"journey": "business.journey", "tenant": "business.tenant"})
	p.pair(whatsapp)
	p.pair(notification)
	got := map[string][]float64{}
	for _, key := range p.sliceOrder {
		sl := p.slices[key]
		if sl.slice.RequestRate != 0 || !sl.slice.NoErrors {
			t.Errorf("a sample has no rate or failures: %+v", sl.slice)
		}
		got[sl.edge+" "+sl.slice.Dims["journey"]+"/"+sl.slice.Dims["tenant"]] = sl.durations
	}
	want := map[string][]float64{
		"app-a->app-b whatsapp/a":     {35},
		"app-b->redis whatsapp/a":     {2},
		"app-a->app-b notification/a": {165},
	}
	if len(got) != len(want) {
		t.Fatalf("slices = %v, want %v", got, want)
	}
	for k, v := range want {
		if len(got[k]) != 1 || got[k][0] != v[0] {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if p.edges["app-b->redis"] == nil || p.targets["redis"] != model.KindCache {
		t.Fatal("the edge itself is still discovered")
	}
}
