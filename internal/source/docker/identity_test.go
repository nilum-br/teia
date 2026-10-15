package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestInventoryRecordsNamingBasis(t *testing.T) {
	containers := []container{
		{Names: []string{"/container-one"}, Labels: map[string]string{"teia.node": "explicit-id", "com.docker.compose.service": "ignored-compose"}},
		{Names: []string{"/container-two"}, Labels: map[string]string{"com.docker.compose.service": "compose-id"}},
		{Names: []string{"/container-three"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(containers) }))
	defer server.Close()
	obs, err := New("docker", Config{Host: "tcp://" + strings.TrimPrefix(server.URL, "http://")}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"explicit-id", "compose-id", "container-three"}
	wantMethods := []model.IdentityMethod{model.IdentityExplicit, model.IdentityName, model.IdentityName}
	for i, node := range obs.Nodes {
		if node.ID != wantIDs[i] || node.IdentityMethod != wantMethods[i] || node.IdentityBasis == "" {
			t.Fatalf("node %d identity = %+v", i, node)
		}
	}
}
