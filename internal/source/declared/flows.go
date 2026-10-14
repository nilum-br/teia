// Package declared reads intended communication from flows files that live
// in each service repository, next to the Backstage catalog-info.yaml.
package declared

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/model"
)

const (
	APIVersion = "teia.nilum.io/v1alpha1"
	KindFlows  = "Flows"
)

// Document is one Flows document.
type Document struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Spec struct {
	// Owner is the calling side, as a Backstage entity ref or plain name.
	Owner string `yaml:"owner" json:"owner"`
	// Kind optionally overrides the node kind of the owner.
	Kind   string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Egress []Flow `yaml:"egress" json:"egress"`
}

// Flow is one intended outgoing communication.
type Flow struct {
	To       string `yaml:"to" json:"to"`
	Kind     string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
	Reason   string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// Parse decodes the Flows documents in data. Everything else is skipped
// without complaint, since flows files share repositories with other YAML:
// documents of other kinds (a Backstage Component in the same file), lists
// (Ansible), and text that does not parse at all (Helm templates) unless it
// declares itself a Flows document.
func Parse(data []byte) ([]Document, error) {
	var docs []Document
	for i, raw := range documents(data) {
		var node yaml.Node
		if err := yaml.Unmarshal(raw, &node); err != nil {
			if flowsMarker.Match(raw) {
				return nil, fmt.Errorf("document %d: %w", i+1, err)
			}
			continue
		}
		if !isFlows(&node) {
			continue
		}
		var doc Document
		if err := node.Decode(&doc); err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// flowsMarker finds a top-level "kind: Flows" or teia apiVersion line, which
// tells a broken flows document from other YAML that does not parse.
var flowsMarker = regexp.MustCompile(`(?m)^(kind:[ \t]*["']?` + KindFlows + `["']?[ \t]*(#.*)?\r?$|apiVersion:[ \t]*["']?teia\.nilum\.io/)`)

// isFlows reports whether a parsed document is a mapping with kind: Flows.
func isFlows(doc *yaml.Node) bool {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false
	}
	m := doc.Content[0].Content
	for i := 0; i+1 < len(m); i += 2 {
		if m[i].Value == "kind" {
			return m[i+1].Kind == yaml.ScalarNode && m[i+1].Value == KindFlows
		}
	}
	return false
}

// documents splits a YAML stream on its "---" and "..." markers, so that
// one document that does not parse does not hide the others. Each document
// is padded with the blank lines before it, keeping the line numbers of
// parser errors those of the file. Comment-only stretches (a header before
// the first "---") are not documents.
func documents(data []byte) [][]byte {
	var docs [][]byte
	var cur []byte
	start := 0 // line where cur starts
	flush := func(next int) {
		if !blank(cur) {
			docs = append(docs, append(bytes.Repeat([]byte("\n"), start), cur...))
		}
		cur, start = nil, next
	}
	for i, line := range bytes.SplitAfter(data, []byte("\n")) {
		text := bytes.TrimRight(line, "\r\n")
		switch {
		case bytes.Equal(text, []byte("---")) || bytes.HasPrefix(text, []byte("--- ")) || bytes.HasPrefix(text, []byte("---\t")):
			flush(i)
			cur = append(cur, line[len("---"):]...) // "--- |" starts a scalar on the marker line
		case bytes.Equal(text, []byte("...")):
			flush(i + 1)
		default:
			cur = append(cur, line...)
		}
	}
	flush(0)
	return docs
}

// blank reports whether doc holds only whitespace and comments.
func blank(doc []byte) bool {
	for _, line := range bytes.Split(doc, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] != '#' {
			return false
		}
	}
	return true
}

// Validate returns every problem found in doc.
func Validate(doc Document) []error {
	var errs []error
	if doc.APIVersion != APIVersion {
		errs = append(errs, fmt.Errorf("apiVersion must be %q, got %q", APIVersion, doc.APIVersion))
	}
	if doc.Spec.Owner == "" {
		errs = append(errs, errors.New("spec.owner is required"))
	}
	if doc.Spec.Kind != "" && !validKind(doc.Spec.Kind) {
		errs = append(errs, fmt.Errorf("spec.kind %q is not one of %s", doc.Spec.Kind, kindList))
	}
	for i, f := range doc.Spec.Egress {
		if f.To == "" {
			errs = append(errs, fmt.Errorf("spec.egress[%d].to is required", i))
		}
		if f.Kind != "" && !validKind(f.Kind) {
			errs = append(errs, fmt.Errorf("spec.egress[%d].kind %q is not one of %s", i, f.Kind, kindList))
		}
		if f.Port < 0 || f.Port > 65535 {
			errs = append(errs, fmt.Errorf("spec.egress[%d].port %d is out of range", i, f.Port))
		}
	}
	return errs
}

var kinds = []model.NodeKind{
	model.KindService, model.KindDatabase, model.KindCache,
	model.KindQueue, model.KindExternal, model.KindUnknown,
}

var kindList = func() string {
	s := make([]string, len(kinds))
	for i, k := range kinds {
		s[i] = string(k)
	}
	return strings.Join(s, ", ")
}()

func validKind(k string) bool {
	for _, known := range kinds {
		if string(known) == k {
			return true
		}
	}
	return false
}

// ParseRef turns a Backstage-style entity ref ("component:default/orders",
// "resource:orders-db", "external:api.stripe.com" or just "orders") into a
// node ID and the kind the ref implies.
func ParseRef(ref string) (id string, kind model.NodeKind) {
	kind = model.KindUnknown
	name := ref
	if i := strings.Index(ref, ":"); i >= 0 {
		switch strings.ToLower(ref[:i]) {
		case "component", "api":
			kind = model.KindService
		case "external":
			kind = model.KindExternal
		}
		name = ref[i+1:]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(name), kind
}
