package declared

import (
	"context"
	"errors"
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

// readFile is swapped by tests: as root, no file is unreadable.
var readFile = os.ReadFile

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	files, bad, err := s.files()
	if err != nil {
		return obs, err
	}
	// A file that cannot be read or parsed is reported, not fatal: the
	// other teams' flows files still count.
	for _, file := range files {
		data, err := readFile(file)
		if err != nil {
			bad = append(bad, problem(file, err))
			continue
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
		obs.Message = fmt.Sprintf("%d unreadable or invalid flows file(s)", len(bad))
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

// skipDirs never hold a team's flows files, only other projects' YAML
// (dependencies, build output, VCS and Terraform state), and can be huge.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".terraform": true}

// maxFileSize skips generated manifests and API specs: a flows file is a
// few kilobytes.
const maxFileSize = 1 << 20

// files lists the YAML files under the configured paths, and the
// subdirectories that could not be read. The walk stays in the tree it was
// given: a link is followed only when it ends inside it, as a mounted
// ConfigMap's do. The configured paths themselves may be links (a mounted
// volume).
func (s *Source) files() (files, problems []string, err error) {
	for _, root := range s.paths {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
		err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
			isRoot, real := path == resolved, path
			// Report paths under the configured name, not the link target.
			if rel, relErr := filepath.Rel(resolved, path); relErr == nil {
				path = filepath.Join(root, rel)
			}
			if err != nil {
				if isRoot {
					return err
				}
				problems = append(problems, problem(path, err))
				return nil
			}
			if d.IsDir() {
				// kubelet keeps a mounted ConfigMap's files in ..<timestamp>
				// and links each name to it: read them once, by their names.
				if !isRoot && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), "..")) {
					return filepath.SkipDir
				}
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".yaml", ".yml":
			default:
				return nil
			}
			info, ok, err := target(resolved, real, d)
			if err != nil {
				problems = append(problems, problem(path, err))
				return nil
			}
			if ok && info.Size() <= maxFileSize {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", root, err)
		}
	}
	return files, problems, nil
}

// target describes what the walk reads at path: the file itself, or what a
// link points to when it ends inside root, as a mounted ConfigMap's links do
// (orders.yaml -> ..data/orders.yaml). ok is false for anything else: a link
// out of the tree or dangling (kubelet swaps ..data while it updates), a
// socket, a device.
func target(root, path string, d fs.DirEntry) (info fs.FileInfo, ok bool, err error) {
	if d.Type().IsRegular() {
		info, err = d.Info()
		return info, err == nil, err
	}
	if d.Type()&fs.ModeSymlink == 0 {
		return nil, false, nil
	}
	dest, err := filepath.EvalSymlinks(path)
	if err == nil && within(root, dest) {
		if info, err = os.Stat(dest); err != nil {
			return nil, false, err
		}
		return info, info.Mode().IsRegular(), nil
	}
	return nil, false, nil
}

// within reports whether path is root or below it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// problem renders a file error once: fs errors already quote their path.
func problem(path string, err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	return fmt.Sprintf("%s: %v", path, err)
}
