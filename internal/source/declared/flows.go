// Package declared reads intended communication from flows files that live
// in each service repository, next to the Backstage catalog-info.yaml.
package declared

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

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

// Parse decodes every YAML document in data. Documents of other kinds
// (e.g. a Backstage Component in the same file) are skipped.
func Parse(data []byte) ([]Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []Document
	for i := 0; ; i++ {
		var doc Document
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if doc.Kind != KindFlows {
			continue
		}
		docs = append(docs, doc)
	}
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
