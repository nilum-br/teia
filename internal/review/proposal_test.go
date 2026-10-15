package review

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

func TestProposalPreservesEvidenceAndValidatesExportBeforeWriting(t *testing.T) {
	s := fixture()
	s.Nodes[0].Labels = map[string]string{"k8s.namespace": "shop", "private-token": "should-not-export"}
	s.Nodes[0].Addresses = []string{"postgres://should-not-export"}
	s.Edges[0].Evidence[0].Retained = true
	s.Nodes[0].Identity = []model.IdentityResolution{{Input: "orders-api", ResolvedID: "orders", Method: model.IdentityAlias,
		Source: "tempo", SourceType: "tempo", Basis: "configured-alias", Detail: "Configured alias", TimeBasis: "unknown"}}
	s.Edges[0].Identity = &model.ConnectionIdentity{From: []model.IdentityResolution{{Input: "10.0.0.1", ResolvedID: "orders", Method: model.IdentityIPTemporal,
		Source: "tempo", SourceType: "tempo", TimeBasis: "sample-envelope", ObservedAt: s.GeneratedAt, Retained: true,
		Candidates: []model.IdentityCandidate{{ID: "orders", Source: "docker", SourceType: "docker", Retained: true}}, Alternatives: []string{"orders-old"}}}}
	before, _ := json.Marshal(s)
	r := ProposalRequest{From: "orders", To: "db", Rationale: "Persist orders\n```\n<script>test</script>"}
	p, err := Propose(s, r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Propose(s, r)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("same input produced a different artifact", err)
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) || p.Source.SnapshotID != digest(before) || p.Source.InputKind != "provided-snapshot" {
		t.Fatal("proposal changed or misidentified input")
	}
	if !reflect.DeepEqual(p.Connection.Evidence, s.Edges[0].Evidence) || p.Connection.Endpoints[0].Namespace != "shop" {
		t.Fatal("lost provenance", p.Connection)
	}
	if !reflect.DeepEqual(p.Connection.Resolution, s.Edges[0].Identity) || !reflect.DeepEqual(p.Connection.Endpoints[0].Resolution, s.Nodes[0].Identity) {
		t.Fatal("lost structured identity provenance", p.Connection)
	}
	encoded, _ := json.Marshal(p)
	if bytes.Contains(encoded, []byte("should-not-export")) || len(p.Files) != 3 {
		t.Fatal("exported unrelated topology metadata or missing files")
	}
	docs, err := declared.Parse([]byte(p.Files[0].Content))
	if err != nil || len(docs) != 1 || len(declared.Validate(docs[0])) != 0 || docs[0].Spec.Egress[0].Reason != r.Rationale {
		t.Fatalf("invalid fragment or changed rationale: %v %+v", err, docs)
	}
	if !strings.Contains(p.Files[1].Content, "````text\n") || !strings.Contains(p.Files[1].Content, "retained observations") {
		t.Fatal("untrusted Markdown fence or retained context lost")
	}
	var archive bytes.Buffer
	if err := Bundle(&archive, p); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil || len(z.File) != 3 {
		t.Fatal("invalid ZIP", err)
	}
	for i, file := range z.File {
		f, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(f)
		f.Close()
		if err != nil || file.Name != p.Files[i].Name || string(body) != p.Files[i].Content {
			t.Fatal("bundle differs from reviewed files", err)
		}
	}
	r.Rationale = "A different reason"
	changed, _ := Propose(s, r)
	if changed.ID == p.ID || changed.Source.SnapshotID != p.Source.SnapshotID {
		t.Fatal("proposal identity must distinguish rationale while preserving source identity")
	}
	p.Connection.Resolution.From[0].Alternatives[0] = "changed"
	p.Connection.Resolution.From[0].Candidates[0].ID = "changed"
	p.Connection.Endpoints[0].Resolution[0].Detail = "changed"
	if got, _ := json.Marshal(s); !bytes.Equal(before, got) {
		t.Fatal("returned identity metadata aliases the source snapshot")
	}
}

func TestProposalRejectsUnsafeOrUnsupportedAcceptance(t *testing.T) {
	cases := map[string]func(*model.Snapshot, *ProposalRequest){
		"declared":           func(s *model.Snapshot, _ *ProposalRequest) { s.Edges[0].Declared = true },
		"not observed":       func(s *model.Snapshot, _ *ProposalRequest) { s.Edges[0].Observed = false },
		"unknown edge":       func(_ *model.Snapshot, r *ProposalRequest) { r.To = "other" },
		"missing endpoint":   func(s *model.Snapshot, _ *ProposalRequest) { s.Nodes = s.Nodes[:1] },
		"duplicate endpoint": func(s *model.Snapshot, _ *ProposalRequest) { s.Nodes = append(s.Nodes, s.Nodes[0]) },
		"invalid port":       func(s *model.Snapshot, _ *ProposalRequest) { s.Edges[0].Port = 65536 },
		"empty rationale":    func(_ *model.Snapshot, r *ProposalRequest) { r.Rationale = "  " },
		"long rationale":     func(_ *model.Snapshot, r *ProposalRequest) { r.Rationale = strings.Repeat("x", 4097) },
		"control rationale":  func(_ *model.Snapshot, r *ProposalRequest) { r.Rationale = "x\x00y" },
		"missing time":       func(s *model.Snapshot, _ *ProposalRequest) { s.GeneratedAt = time.Time{} },
		"address identity": func(s *model.Snapshot, r *ProposalRequest) {
			s.Nodes[1].ID = "10.0.0.1"
			s.Edges[0].To = "10.0.0.1"
			r.To = "10.0.0.1"
		},
		"lossy identity": func(s *model.Snapshot, r *ProposalRequest) {
			s.Nodes[1].ID = "region/db"
			s.Edges[0].To = "region/db"
			r.To = "region/db"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := fixture()
			r := ProposalRequest{From: "orders", To: "db", Rationale: "persist orders"}
			mutate(&s, &r)
			p, err := Propose(s, r)
			if err == nil || len(p.Files) != 0 {
				t.Fatalf("invalid acceptance produced artifact: %+v %v", p, err)
			}
		})
	}
}
