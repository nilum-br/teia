package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

const containersJSON = `[
  {
    "Names": ["/shop-orders-db-1"],
    "Image": "postgres:18",
    "Created": 1760000000,
    "Labels": {
      "com.docker.compose.project": "shop",
      "com.docker.compose.service": "orders-db",
      "teia.kind": "database",
      "team": "payments"
    },
    "NetworkSettings": {"Networks": {"shop_default": {"IPAddress": "172.18.0.5"}}}
  },
  {
    "Names": ["/shop-gateway-1"],
    "Image": "traefik:v3",
    "Labels": {
      "com.docker.compose.service": "gateway",
      "traefik.http.middlewares.auth.basicauth.users": "admin:$apr1$H6uskkkW$IgXLP6ewTrSuBkTrqE8wj/",
      "app.token": "s3cr3t",
      "team": "edge"
    },
    "NetworkSettings": {"Networks": {"shop_default": {"IPAddress": "172.18.0.6"}}}
  }
]`

func TestDiscoverKindsAndLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(containersJSON))
	}))
	defer srv.Close()

	obs, err := New("docker", Config{Host: "tcp://" + strings.TrimPrefix(srv.URL, "http://")}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(obs.Nodes), obs.Nodes)
	}
	db, gw := obs.Nodes[0], obs.Nodes[1]
	if db.ID != "orders-db" || db.Kind != model.KindDatabase {
		t.Errorf("teia.kind should set the kind: %+v", db)
	}
	if gw.ID != "gateway" || gw.Kind != model.KindUnknown {
		t.Errorf("without teia.kind the inventory has no opinion on the kind: %+v", gw)
	}
	for _, k := range []string{"traefik.http.middlewares.auth.basicauth.users", "app.token", "com.docker.compose.service", "teia.kind"} {
		for _, n := range obs.Nodes {
			if _, ok := n.Labels[k]; ok {
				t.Errorf("%s: label %s should have been dropped", n.ID, k)
			}
		}
	}
	if gw.Labels["team"] != "edge" || gw.Labels["image"] != "traefik:v3" || db.Labels["compose.project"] != "shop" {
		t.Errorf("user and teia labels missing: %v / %v", gw.Labels, db.Labels)
	}
}

// A socket proxy that does not allow GET /containers answers 403: that is a
// missing grant, not a broken source.
func TestProxyRefusalIsAPermissionFinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Request forbidden by administrative rules.", http.StatusForbidden)
	}))
	defer srv.Close()

	obs, err := New("docker", Config{Host: "tcp://" + strings.TrimPrefix(srv.URL, "http://")}).Discover(context.Background())
	if err != nil {
		t.Fatalf("a refusal should be a finding, not an error: %v", err)
	}
	if !obs.Degraded || len(obs.Findings) != 1 {
		t.Fatalf("want a degraded round with one finding, got %+v", obs)
	}
	f := obs.Findings[0]
	if f.Rule != "docker-socket" || f.Kind != model.FindingPermission || f.Fix == nil || !strings.Contains(f.Fix.Snippet, "CONTAINERS: 1") {
		t.Errorf("finding = %+v, fix = %+v", f, f.Fix)
	}
}
