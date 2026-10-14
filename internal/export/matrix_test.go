package export

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestMatrix(t *testing.T) {
	snap := model.Snapshot{
		Nodes: []model.Node{
			{ID: "orders", Kind: model.KindService, Addresses: []string{"10.0.0.2"}},
			{ID: "orders-db", Kind: model.KindDatabase, Addresses: []string{"10.0.0.5"}},
			{ID: "legacy", Kind: model.KindService},
		},
		Edges: []model.Edge{
			{From: "orders", To: "orders-db", Protocol: "postgresql", Port: 5432, Reason: "persist orders",
				Declared: true, Observed: true, State: model.EdgeOK, RequestRate: 1.5, Sources: []string{"declared", "servicegraph"}},
			{From: "legacy", To: "orders-db", Protocol: "postgresql", Observed: true, State: model.EdgeGhost, Sources: []string{"postgres/orders-db"}},
		},
	}

	var buf bytes.Buffer
	if err := Matrix(&buf, snap, MatrixOptions{Lang: "pt-BR"}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "origem" {
		t.Fatalf("unexpected rows: %v", rows)
	}
	want := []string{"orders", "10.0.0.2", "orders-db", "10.0.0.5", "database", "postgresql", "5432",
		"persist orders", "ok", "sim", "sim", "1.50", "declared servicegraph"}
	for i, v := range want {
		if rows[1][i] != v {
			t.Errorf("column %s = %q, want %q", rows[0][i], rows[1][i], v)
		}
	}
	if rows[2][8] != "nao declarado" {
		t.Errorf("ghost state label = %q", rows[2][8])
	}

	buf.Reset()
	if err := Matrix(&buf, snap, MatrixOptions{DeclaredOnly: true}); err != nil {
		t.Fatal(err)
	}
	rows, _ = csv.NewReader(&buf).ReadAll()
	if len(rows) != 2 || rows[0][0] != "source" {
		t.Errorf("declared-only in English should have header + 1 row, got %v", rows)
	}
}
