// Package config loads teia.yaml and builds the configured sources.
//
// Every source is optional and read-only: teia uses whatever a platform
// already runs. See docs/sources.md.
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilum-br/teia/internal/engine"
	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/notify"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/declared"
	"github.com/nilum-br/teia/internal/source/docker"
	"github.com/nilum-br/teia/internal/source/kubernetes"
	"github.com/nilum-br/teia/internal/source/postgres"
	"github.com/nilum-br/teia/internal/source/prometheus"
	"github.com/nilum-br/teia/internal/source/query"
	"github.com/nilum-br/teia/internal/source/tempo"
)

type Config struct {
	Listen   string            `yaml:"listen"`
	Interval time.Duration     `yaml:"interval"`
	Timeout  time.Duration     `yaml:"timeout"`
	Aliases  map[string]string `yaml:"aliases"`
	Ignore   []string          `yaml:"ignore"`
	// Retention keeps an observed edge alive after its last sighting
	// (default: three intervals), so sampled signals do not make it blink.
	Retention time.Duration `yaml:"retention"`
	Health    Health        `yaml:"health"`
	Notify    Notify        `yaml:"notify"`
	Sources   Sources       `yaml:"sources"`
}

// Health sets the error-rate thresholds for yellow and red (0..1).
type Health struct {
	Degraded float64 `yaml:"degraded"`
	Failing  float64 `yaml:"failing"`
}

// Notify sends changes between rounds to webhooks.
type Notify struct {
	Webhooks []Webhook `yaml:"webhooks"`
}

type Webhook struct {
	URL     string            `yaml:"url"`
	Format  string            `yaml:"format"` // json or slack
	Events  []string          `yaml:"events"` // e.g. edge.added, node.health; empty means all
	Headers map[string]string `yaml:"headers"`
}

// HealthThresholds returns the thresholds with defaults filled in.
func (c *Config) HealthThresholds() graph.HealthThresholds {
	t := graph.DefaultHealth
	if c.Health.Degraded > 0 {
		t.Degraded = c.Health.Degraded
	}
	if c.Health.Failing > 0 {
		t.Failing = c.Health.Failing
	}
	return t
}

// Notifiers builds the configured webhooks.
func (c *Config) Notifiers() []engine.Notifier {
	var out []engine.Notifier
	for _, w := range c.Notify.Webhooks {
		var types []model.ChangeType
		for _, e := range w.Events {
			types = append(types, model.ChangeType(e))
		}
		out = append(out, notify.NewWebhook(w.URL, w.Format, types, w.Headers))
	}
	return out
}

type Sources struct {
	Declared   *Declared      `yaml:"declared"`
	Prometheus []Prometheus   `yaml:"prometheus"`
	Tempo      []Tempo        `yaml:"tempo"`
	Queries    []Query        `yaml:"queries"`
	Kubernetes *Kubernetes    `yaml:"kubernetes"`
	Docker     *Docker        `yaml:"docker"`
	Postgres   []PostgresNode `yaml:"postgres"`
}

type Declared struct {
	Paths []string `yaml:"paths"`
}

// Prometheus is any Prometheus-compatible query API (Prometheus, Thanos,
// Mimir, VictoriaMetrics, Grafana Cloud).
type Prometheus struct {
	Name    string            `yaml:"name"`
	URL     string            `yaml:"url"`
	Window  time.Duration     `yaml:"window"`
	Presets []string          `yaml:"presets"` // empty or [auto]: detect
	Headers map[string]string `yaml:"headers"`
}

type Tempo struct {
	Name             string            `yaml:"name"`
	URL              string            `yaml:"url"`
	Window           time.Duration     `yaml:"window"`
	TracesPerService int               `yaml:"tracesPerService"`
	MaxTraces        int               `yaml:"maxTraces"`
	Headers          map[string]string `yaml:"headers"`
}

// Query maps any PromQL or LogQL query to edges.
type Query struct {
	Name          string            `yaml:"name"`
	Dialect       string            `yaml:"dialect"` // prometheus or loki
	URL           string            `yaml:"url"`
	Query         string            `yaml:"query"`
	Window        time.Duration     `yaml:"window"`
	From          string            `yaml:"from"`
	To            string            `yaml:"to"`
	FromAddress   bool              `yaml:"fromAddress"`
	ToAddress     bool              `yaml:"toAddress"`
	Protocol      string            `yaml:"protocol"`
	ProtocolLabel string            `yaml:"protocolLabel"`
	ToKind        string            `yaml:"toKind"`
	Headers       map[string]string `yaml:"headers"`
}

type Kubernetes struct {
	Kubeconfig        string   `yaml:"kubeconfig"`
	Context           string   `yaml:"context"`
	Namespaces        []string `yaml:"namespaces"`
	ExcludeNamespaces []string `yaml:"excludeNamespaces"`
}

type Docker struct {
	// Host is a Docker endpoint; empty means DOCKER_HOST, then the OS default.
	Host string `yaml:"host"`
	// Socket is a shorthand for host: unix://<socket>.
	Socket  string `yaml:"socket"`
	Project string `yaml:"project"`
}

type PostgresNode struct {
	Node string `yaml:"node"`
	DSN  string `yaml:"dsn"`
}

// Load reads a config file. ${NAME} references to environment variables are
// expanded, so secrets can stay out of it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// envRef matches ${NAME}. Only the braced form is expanded, so "$" in
// queries (LogQL/PromQL regex anchors, the $window placeholder) is left alone.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces ${NAME} with the environment variable NAME.
func ExpandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(envRef.FindStringSubmatch(m)[1])
	})
}

// Parse reads a config document.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{Listen: ":8080", Interval: 30 * time.Second}
	if err := yaml.Unmarshal([]byte(ExpandEnv(string(data))), cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Health.Degraded < 0 || c.Health.Failing < 0 || c.Health.Degraded > 1 || c.Health.Failing > 1 {
		return fmt.Errorf("health thresholds are error rates between 0 and 1")
	}
	for i, w := range c.Notify.Webhooks {
		if w.URL == "" {
			return fmt.Errorf("notify.webhooks[%d].url is required", i)
		}
		if w.Format != "" && w.Format != "json" && w.Format != "slack" {
			return fmt.Errorf("notify.webhooks[%d].format must be json or slack", i)
		}
	}
	s := c.Sources
	for i, p := range s.Prometheus {
		if p.URL == "" {
			return fmt.Errorf("sources.prometheus[%d].url is required", i)
		}
		for _, name := range p.Presets {
			if name != "auto" && prometheus.Lookup(name) == nil {
				return fmt.Errorf("sources.prometheus[%d]: unknown preset %q", i, name)
			}
		}
	}
	for i, t := range s.Tempo {
		if t.URL == "" {
			return fmt.Errorf("sources.tempo[%d].url is required", i)
		}
	}
	for i, q := range s.Queries {
		if q.URL == "" || q.Query == "" || q.From == "" || q.To == "" {
			return fmt.Errorf("sources.queries[%d] needs url, query, from and to", i)
		}
		if q.Dialect != "" && q.Dialect != string(query.Prometheus) && q.Dialect != string(query.Loki) {
			return fmt.Errorf("sources.queries[%d].dialect must be prometheus or loki", i)
		}
	}
	for i, p := range s.Postgres {
		if p.Node == "" || p.DSN == "" {
			return fmt.Errorf("sources.postgres[%d] needs node and dsn", i)
		}
	}
	return nil
}

func named(kind, name string, i, total int) string {
	switch {
	case name != "":
		return kind + "/" + name
	case total > 1:
		return fmt.Sprintf("%s/%d", kind, i+1)
	}
	return kind
}

// BuildSources instantiates every configured source.
func (c *Config) BuildSources() []source.Source {
	var out []source.Source
	s := c.Sources
	if s.Declared != nil {
		out = append(out, declared.New("declared", s.Declared.Paths))
	}
	for i, p := range s.Prometheus {
		out = append(out, prometheus.New(named("prometheus", p.Name, i, len(s.Prometheus)), prometheus.Config{
			URL: p.URL, Window: p.Window, Presets: p.Presets, Headers: p.Headers,
		}))
	}
	for i, t := range s.Tempo {
		out = append(out, tempo.New(named("tempo", t.Name, i, len(s.Tempo)), tempo.Config{
			URL: t.URL, Window: t.Window, TracesPerService: t.TracesPerService, MaxTraces: t.MaxTraces, Headers: t.Headers,
		}))
	}
	for i, q := range s.Queries {
		dialect := query.Dialect(q.Dialect)
		if dialect == "" {
			dialect = query.Prometheus
		}
		out = append(out, query.New(named(string(dialect), q.Name, i, len(s.Queries)), query.Config{
			Dialect: dialect, URL: q.URL, Query: q.Query, Window: q.Window, Headers: q.Headers,
			From: q.From, To: q.To, FromAddress: q.FromAddress, ToAddress: q.ToAddress,
			Protocol: q.Protocol, ProtocolLabel: q.ProtocolLabel, ToKind: model.NodeKind(q.ToKind),
		}))
	}
	if s.Kubernetes != nil {
		out = append(out, kubernetes.New("kubernetes", kubernetes.Config{
			Kubeconfig: s.Kubernetes.Kubeconfig, Context: s.Kubernetes.Context,
			Namespaces: s.Kubernetes.Namespaces, ExcludeNamespaces: s.Kubernetes.ExcludeNamespaces,
		}))
	}
	if s.Docker != nil {
		host := s.Docker.Host
		if host == "" && s.Docker.Socket != "" {
			host = "unix://" + s.Docker.Socket
		}
		out = append(out, docker.New("docker", docker.Config{Host: host, Project: s.Docker.Project}))
	}
	for _, p := range s.Postgres {
		out = append(out, postgres.New("postgres/"+p.Node, postgres.Config{Node: p.Node, DSN: p.DSN}))
	}
	return out
}
