package declared

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// Source reads every flows file under a set of paths.
type Source struct {
	name  string
	paths []string
}

func New(name string, paths []string) *Source {
	return &Source{name: name, paths: paths}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "declared" }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	files, err := s.files()
	if err != nil {
		return obs, err
	}
	var bad []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return obs, err
		}
		docs, err := Parse(data)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		for _, doc := range docs {
			if errs := Validate(doc); len(errs) > 0 {
				bad = append(bad, fmt.Sprintf("%s: %v", file, errs[0]))
				continue
			}
			add(&obs, doc)
		}
	}
	if len(bad) > 0 {
		obs.Degraded = true
		obs.Message = fmt.Sprintf("%d invalid flows file(s)", len(bad))
		obs.Findings = append(obs.Findings, model.Finding{
			Rule:     "invalid-flows-file",
			Kind:     model.FindingSource,
			Severity: model.SeverityWarning,
			Subject:  s.name,
			Title:    "Some flows files could not be read",
			Detail:   strings.Join(bad, "\n"),
			Fix: &model.Fix{
				Summary:  "Validate the files locally before committing.",
				Snippet:  "teia validate path/to/flows.yaml",
				Language: "shell",
			},
		})
	}
	return obs, nil
}

func add(obs *source.Observation, doc Document) {
	owner, ownerKind := ParseRef(doc.Spec.Owner)
	if doc.Spec.Kind != "" {
		ownerKind = model.NodeKind(doc.Spec.Kind)
	} else if ownerKind == model.KindUnknown {
		ownerKind = model.KindService
	}
	obs.Nodes = append(obs.Nodes, source.NodeObservation{
		ID:     owner,
		Kind:   ownerKind,
		Labels: map[string]string{"backstage.ref": doc.Spec.Owner, "teia.declared": "true"},
	})
	for _, f := range doc.Spec.Egress {
		to, kind := ParseRef(f.To)
		if f.Kind != "" {
			kind = model.NodeKind(f.Kind)
		}
		obs.Nodes = append(obs.Nodes, source.NodeObservation{
			ID:     to,
			Kind:   kind,
			Labels: map[string]string{"backstage.ref": f.To},
		})
		obs.Edges = append(obs.Edges, source.EdgeObservation{
			From:     owner,
			To:       to,
			Protocol: f.Protocol,
			Port:     f.Port,
			Declared: true,
			Reason:   f.Reason,
		})
	}
}

func (s *Source) files() ([]string, error) {
	var files []string
	for _, root := range s.paths {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".yaml", ".yml":
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", root, err)
		}
	}
	return files, nil
}
