// Package config loads teia.yaml and builds the configured sources.
//
// Every source is optional and read-only: teia uses whatever a platform
// already runs. See docs/sources.md.
package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/engine"
	"github.com/nilum-br/teia/internal/evidencelinks"
	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/history"
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
	History history.Config `yaml:"history"`
	// Listen is the API address. ":8080" binds every interface: set
	// auth.token, or keep teia on a private network.
	Listen   string            `yaml:"listen"`
	Interval time.Duration     `yaml:"interval"`
	Timeout  time.Duration     `yaml:"timeout"`
	Aliases  map[string]string `yaml:"aliases"`
	Ignore   []string          `yaml:"ignore"`
	// Retention keeps an observed edge alive after its last sighting
	// (default: three intervals), so sampled signals do not make it blink.
	// A failing source's last good observation is reused for as long.
	Retention time.Duration `yaml:"retention"`
	Health    Health        `yaml:"health"`
	Notify    Notify        `yaml:"notify"`
	Auth      Auth          `yaml:"auth"`
	CORS      CORS          `yaml:"cors"`
	// MaxStreams caps concurrent /api/v1/stream clients (default 100).
	MaxStreams    int                      `yaml:"maxStreams"`
	Sources       Sources                  `yaml:"sources"`
	EvidenceLinks []evidencelinks.Template `yaml:"evidenceLinks"`
}

// Health sets the limits of each golden signal for amber and red.
type Health struct {
	HealthLimits `yaml:",inline"`
	Overrides    []HealthOverride `yaml:"overrides"`
}

// HealthLimits is shared by global defaults and targeted overrides. Zero
// inherits the surrounding global/default value; it never disables a signal.
type HealthLimits struct {
	// Degraded and Failing are error rates (0..1).
	Degraded float64       `yaml:"degraded"`
	Failing  float64       `yaml:"failing"`
	Latency  LatencyLimits `yaml:"latency"`
	// Saturation is the used share of a capacity (0..1), such as
	// connections out of max_connections.
	Saturation Limits `yaml:"saturation"`
	// Locks counts sessions waiting on a lock for over a second.
	Locks Limits `yaml:"locks"`
	// Backlog counts items waiting in a queue, such as consumer lag.
	Backlog Limits `yaml:"backlog"`
	// Lag is how far behind a queue's consumers are, or how long it went
	// without progress.
	Lag LatencyLimits `yaml:"lag"`
	// Restarts counts container restarts in the source's window.
	Restarts   Limits `yaml:"restarts"`
	Evictions  Limits `yaml:"evictions"`
	Rejections Limits `yaml:"rejections"`
}

// Limits is a degraded and failing pair; zero keeps the default.
type Limits struct {
	Degraded float64 `yaml:"degraded"`
	Failing  float64 `yaml:"failing"`
}

// LatencyLimits apply to the p95 of calls.
type LatencyLimits struct {
	Degraded time.Duration `yaml:"degraded"`
	Failing  time.Duration `yaml:"failing"`
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

// Auth protects every endpoint but /healthz.
type Auth struct {
	// Token is required as "Authorization: Bearer <token>"; browsers open
	// /?token=<token> once and keep a session cookie. Empty means open.
	Token string `yaml:"token"`
}

// CORS lets browser pages on other origins read /api/v1/*.
type CORS struct {
	// AllowedOrigins are exact origins such as https://grafana.example.com;
	// "*" allows any origin (never with credentials).
	AllowedOrigins []string `yaml:"allowedOrigins"`
}

// HealthThresholds returns the thresholds with defaults filled in.
func (c *Config) HealthThresholds() graph.HealthThresholds {
	t := graph.DefaultHealth
	return healthThresholds(c.Health.HealthLimits, t)
}

func healthThresholds(h HealthLimits, t graph.HealthThresholds) graph.HealthThresholds {
	if h.Degraded > 0 {
		t.Degraded = h.Degraded
	}
	if h.Failing > 0 {
		t.Failing = h.Failing
	}
	if d := h.Latency.Degraded; d > 0 {
		t.LatencyDegradedMs = float64(d) / float64(time.Millisecond)
	}
	if f := h.Latency.Failing; f > 0 {
		t.LatencyFailingMs = float64(f) / float64(time.Millisecond)
	}
	pick := func(set, def float64) float64 {
		if set > 0 {
			return set
		}
		return def
	}
	t.SaturationDegraded = pick(h.Saturation.Degraded, t.SaturationDegraded)
	t.SaturationFailing = pick(h.Saturation.Failing, t.SaturationFailing)
	t.LocksDegraded = pick(h.Locks.Degraded, t.LocksDegraded)
	t.LocksFailing = pick(h.Locks.Failing, t.LocksFailing)
	t.BacklogDegraded = pick(h.Backlog.Degraded, t.BacklogDegraded)
	t.BacklogFailing = pick(h.Backlog.Failing, t.BacklogFailing)
	t.LagDegradedSeconds = pick(h.Lag.Degraded.Seconds(), t.LagDegradedSeconds)
	t.LagFailingSeconds = pick(h.Lag.Failing.Seconds(), t.LagFailingSeconds)
	t.RestartsDegraded = pick(h.Restarts.Degraded, t.RestartsDegraded)
	t.RestartsFailing = pick(h.Restarts.Failing, t.RestartsFailing)
	t.EvictionsDegraded = pick(h.Evictions.Degraded, t.EvictionsDegraded)
	t.EvictionsFailing = pick(h.Evictions.Failing, t.EvictionsFailing)
	t.RejectionsDegraded = pick(h.Rejections.Degraded, t.RejectionsDegraded)
	t.RejectionsFailing = pick(h.Rejections.Failing, t.RejectionsFailing)
	return t
}

// Notifiers builds the configured webhooks.
func (c *Config) Notifiers() []engine.Notifier {
	var out []engine.Notifier
	for i, w := range c.Notify.Webhooks {
		var types []model.ChangeType
		for _, e := range w.Events {
			types = append(types, model.ChangeType(e))
		}
		out = append(out, notify.NewWebhook(fmt.Sprintf("notify.webhooks[%d]", i), w.URL, w.Format, types, w.Headers))
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
	Probes  []string          `yaml:"probes"`  // empty or [auto]: detect; [none]: off
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
	Errors        string            `yaml:"errors"`  // optional: failed requests/s, same from/to labels
	Latency       string            `yaml:"latency"` // optional: p95 in seconds, same from/to labels
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

// envRef matches ${NAME} and ${NAME:-default}. Only the braced form is
// expanded, so "$" in queries (LogQL/PromQL regex anchors, the $window
// placeholder) is left alone.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^}]*)?\}`)

// ExpandEnv replaces ${NAME} with the environment variable NAME, and
// ${NAME:-default} with default when NAME is unset or empty.
func ExpandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRef.FindStringSubmatch(m)
		if v := os.Getenv(sub[1]); v != "" || sub[2] == "" {
			return v
		}
		return strings.TrimPrefix(sub[2], ":-")
	})
}

// unsetEnv lists the variables referenced without a default that are not
// set: an unset ${TEIA_TOKEN} would otherwise quietly turn auth off. Only
// YAML values count, not comments.
func unsetEnv(data []byte) []string {
	var doc yaml.Node
	var values []string
	if err := yaml.Unmarshal(data, &doc); err == nil {
		var walk func(n *yaml.Node)
		walk = func(n *yaml.Node) {
			if n.Kind == yaml.ScalarNode {
				values = append(values, n.Value)
			}
			for _, c := range n.Content {
				walk(c)
			}
		}
		walk(&doc)
	} else {
		values = []string{string(data)} // the decoder reports the syntax error
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		for _, sub := range envRef.FindAllStringSubmatch(v, -1) {
			name := sub[1]
			if _, set := os.LookupEnv(name); set || sub[2] != "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// unknownField matches yaml.v3's message for a key no field reads.
var unknownField = regexp.MustCompile(`field (\S+) not found in type \S+`)

// friendly rewords decoding errors for people editing teia.yaml.
func friendly(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	msgs := make([]string, len(te.Errors))
	for i, m := range te.Errors {
		msgs[i] = unknownField.ReplaceAllString(m, `unknown key "$1" (misspelled, misplaced, or not supported by this version)`)
	}
	return &yaml.TypeError{Errors: msgs}
}

// Parse reads a config document. Unknown keys are errors: a typo would
// otherwise silently drop a setting.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{Listen: ":8080", Interval: 30 * time.Second}
	dec := yaml.NewDecoder(strings.NewReader(ExpandEnv(string(data))))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing config: %w", friendly(err))
	}
	var errs []error
	if missing := unsetEnv(data); len(missing) > 0 {
		errs = append(errs, fmt.Errorf("environment variables not set: %s (write ${NAME:-default} for optional ones)", strings.Join(missing, ", ")))
	}
	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	return cfg, errors.Join(errs...)
}

// nodeKinds are the kinds a query may give its callees.
var nodeKinds = []model.NodeKind{
	model.KindService, model.KindDatabase, model.KindCache, model.KindQueue, model.KindExternal, model.KindUnknown,
}

// changeTypes are the events a webhook may subscribe to.
var changeTypes = []model.ChangeType{
	model.ChangeEdgeAdded, model.ChangeEdgeRemoved, model.ChangeEdgeState, model.ChangeEdgeHealth,
	model.ChangeNodeAdded, model.ChangeNodeRemoved, model.ChangeNodeHealth, model.ChangeNodeVisibility,
	model.ChangeFindingOpened, model.ChangeFindingResolved,
}

func oneOf[T ~string](v T, valid []T) bool {
	for _, x := range valid {
		if v == x {
			return true
		}
	}
	return false
}

func list[T ~string](valid []T) string {
	out := make([]string, len(valid))
	for i, v := range valid {
		out[i] = string(v)
	}
	return strings.Join(out, ", ")
}

// httpURL reports whether raw is an absolute http(s) URL.
func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// origin reports whether raw is a browser origin: scheme and host only.
func origin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		(u.Path == "" || u.Path == "/") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (c *Config) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	notNegative := func(key string, d time.Duration) {
		if d < 0 {
			bad("%s must not be negative (got %s)", key, d)
		}
	}

	notNegative("interval", c.Interval)
	notNegative("timeout", c.Timeout)
	notNegative("retention", c.Retention)
	if err := c.History.Validate(); err != nil {
		errs = append(errs, err)
	}
	for i, link := range c.EvidenceLinks {
		if err := evidencelinks.Validate(link); err != nil {
			bad("evidenceLinks[%d]: %s", i, err)
		}
	}
	if c.MaxStreams < 0 {
		bad("maxStreams must not be negative (got %d)", c.MaxStreams)
	}

	if c.Health.Degraded < 0 || c.Health.Failing < 0 || c.Health.Degraded > 1 || c.Health.Failing > 1 {
		bad("health thresholds are error rates between 0 and 1")
	} else if t := c.HealthThresholds(); t.Degraded > t.Failing {
		bad("health.degraded (%g) must not be above health.failing (%g); unset values default to %g and %g",
			t.Degraded, t.Failing, graph.DefaultHealth.Degraded, graph.DefaultHealth.Failing)
	}
	t := c.HealthThresholds()
	for _, l := range []struct {
		key                     string
		set                     Limits
		degraded, failing, most float64
	}{
		{"health.saturation", c.Health.Saturation, t.SaturationDegraded, t.SaturationFailing, 1},
		{"health.locks", c.Health.Locks, t.LocksDegraded, t.LocksFailing, 0},
		{"health.backlog", c.Health.Backlog, t.BacklogDegraded, t.BacklogFailing, 0},
		{"health.restarts", c.Health.Restarts, t.RestartsDegraded, t.RestartsFailing, 0},
		{"health.evictions", c.Health.Evictions, t.EvictionsDegraded, t.EvictionsFailing, 0},
		{"health.rejections", c.Health.Rejections, t.RejectionsDegraded, t.RejectionsFailing, 0},
		{"health.lag", Limits{Degraded: c.Health.Lag.Degraded.Seconds(), Failing: c.Health.Lag.Failing.Seconds()}, t.LagDegradedSeconds, t.LagFailingSeconds, 0},
	} {
		switch {
		case l.set.Degraded < 0 || l.set.Failing < 0:
			bad("%s limits must not be negative", l.key)
		case l.most > 0 && (l.set.Degraded > l.most || l.set.Failing > l.most):
			bad("%s limits are shares of a capacity, between 0 and 1", l.key)
		case l.degraded > l.failing:
			bad("%s.degraded (%g) must not be above %s.failing (%g), defaults included", l.key, l.degraded, l.key, l.failing)
		}
	}
	if c.Health.Latency.Degraded < 0 || c.Health.Latency.Failing < 0 {
		bad("health.latency limits must not be negative")
	} else if t := c.HealthThresholds(); t.LatencyDegradedMs > t.LatencyFailingMs {
		bad("health.latency.degraded (%s) must not be above health.latency.failing (%s); unset values default to %s and %s",
			model.FormatMs(t.LatencyDegradedMs), model.FormatMs(t.LatencyFailingMs),
			model.FormatMs(graph.DefaultHealth.LatencyDegradedMs), model.FormatMs(graph.DefaultHealth.LatencyFailingMs))
	}
	if err := c.validateHealthOverrides(); err != nil {
		errs = append(errs, err)
	}

	for i, w := range c.Notify.Webhooks {
		// Never quote the URL: for Slack it is the secret.
		switch {
		case w.URL == "":
			bad("notify.webhooks[%d].url is required", i)
		case !httpURL(w.URL):
			bad("notify.webhooks[%d].url must be an http or https URL", i)
		}
		if w.Format != "" && w.Format != "json" && w.Format != "slack" {
			bad("notify.webhooks[%d].format must be json or slack", i)
		}
		for _, e := range w.Events {
			if !oneOf(model.ChangeType(e), changeTypes) {
				bad("notify.webhooks[%d]: unknown event %q (one of %s)", i, e, list(changeTypes))
			}
		}
	}

	for i, o := range c.CORS.AllowedOrigins {
		if o != "*" && !origin(o) {
			bad(`cors.allowedOrigins[%d] must be an origin such as https://grafana.example.com (scheme and host, no path) or "*", got %q`, i, o)
		}
	}

	s := c.Sources
	for i, p := range s.Prometheus {
		if p.URL == "" {
			bad("sources.prometheus[%d].url is required", i)
		}
		notNegative(fmt.Sprintf("sources.prometheus[%d].window", i), p.Window)
		for _, name := range p.Presets {
			if name != "auto" && prometheus.Lookup(name) == nil {
				bad("sources.prometheus[%d]: unknown preset %q", i, name)
			}
		}
		for _, name := range p.Probes {
			switch {
			case name == "auto" || name == "none":
				if len(p.Probes) > 1 {
					bad("sources.prometheus[%d].probes: %q stands alone", i, name)
				}
			case prometheus.LookupProbe(name) == nil:
				bad("sources.prometheus[%d]: unknown probe %q (known: %s)", i, name, strings.Join(prometheus.ProbeNames(), ", "))
			}
		}
	}
	for i, t := range s.Tempo {
		if t.URL == "" {
			bad("sources.tempo[%d].url is required", i)
		}
		notNegative(fmt.Sprintf("sources.tempo[%d].window", i), t.Window)
		if t.TracesPerService < 0 || t.MaxTraces < 0 {
			bad("sources.tempo[%d]: tracesPerService and maxTraces must not be negative", i)
		}
	}
	for i, q := range s.Queries {
		if q.URL == "" || q.Query == "" || q.From == "" || q.To == "" {
			bad("sources.queries[%d] needs url, query, from and to", i)
		}
		if q.Dialect != "" && q.Dialect != string(query.Prometheus) && q.Dialect != string(query.Loki) {
			bad("sources.queries[%d].dialect must be prometheus or loki", i)
		}
		notNegative(fmt.Sprintf("sources.queries[%d].window", i), q.Window)
		if q.ToKind != "" && !oneOf(model.NodeKind(q.ToKind), nodeKinds) {
			bad("sources.queries[%d].toKind %q is not a kind (one of %s)", i, q.ToKind, list(nodeKinds))
		}
	}
	for i, p := range s.Postgres {
		if p.Node == "" || p.DSN == "" {
			bad("sources.postgres[%d] needs node and dsn", i)
		}
	}

	// Names identify sources in statuses, findings and edges: two sources
	// with one name would be mistaken for each other.
	seen := map[string]bool{}
	for _, p := range c.plan() {
		if seen[p.name] {
			bad("two sources are named %q: give each a distinct name", p.name)
		}
		seen[p.name] = true
	}
	return errors.Join(errs...)
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

// planned is a configured source before it is built: its name is known
// without connecting to anything, so validation can check it.
type planned struct {
	name  string
	build func(name string) source.Source
}

func (c *Config) plan() []planned {
	var out []planned
	add := func(name string, build func(string) source.Source) {
		out = append(out, planned{name: name, build: build})
	}
	s := c.Sources
	if s.Declared != nil {
		add("declared", func(n string) source.Source { return declared.New(n, s.Declared.Paths) })
	}
	for i, p := range s.Prometheus {
		add(named("prometheus", p.Name, i, len(s.Prometheus)), func(n string) source.Source {
			return prometheus.New(n, prometheus.Config{URL: p.URL, Window: p.Window, Presets: p.Presets, Probes: p.Probes, Headers: p.Headers})
		})
	}
	for i, t := range s.Tempo {
		add(named("tempo", t.Name, i, len(s.Tempo)), func(n string) source.Source {
			return tempo.New(n, tempo.Config{
				URL: t.URL, Window: t.Window, TracesPerService: t.TracesPerService, MaxTraces: t.MaxTraces, Headers: t.Headers,
			})
		})
	}
	for i, q := range s.Queries {
		dialect := query.Dialect(q.Dialect)
		if dialect == "" {
			dialect = query.Prometheus
		}
		add(named(string(dialect), q.Name, i, len(s.Queries)), func(n string) source.Source {
			return query.New(n, query.Config{
				Dialect: dialect, URL: q.URL, Query: q.Query, Errors: q.Errors, Latency: q.Latency, Window: q.Window, Headers: q.Headers,
				From: q.From, To: q.To, FromAddress: q.FromAddress, ToAddress: q.ToAddress,
				Protocol: q.Protocol, ProtocolLabel: q.ProtocolLabel, ToKind: model.NodeKind(q.ToKind),
			})
		})
	}
	if k := s.Kubernetes; k != nil {
		add("kubernetes", func(n string) source.Source {
			return kubernetes.New(n, kubernetes.Config{
				Kubeconfig: k.Kubeconfig, Context: k.Context, Namespaces: k.Namespaces, ExcludeNamespaces: k.ExcludeNamespaces,
			})
		})
	}
	if d := s.Docker; d != nil {
		add("docker", func(n string) source.Source {
			host := d.Host
			if host == "" && d.Socket != "" {
				host = "unix://" + d.Socket
			}
			return docker.New(n, docker.Config{Host: host, Project: d.Project})
		})
	}
	for _, p := range s.Postgres {
		add("postgres/"+p.Node, func(n string) source.Source {
			return postgres.New(n, postgres.Config{Node: p.Node, DSN: p.DSN})
		})
	}
	return out
}

// BuildSources instantiates every configured source.
func (c *Config) BuildSources() []source.Source {
	var out []source.Source
	for _, p := range c.plan() {
		out = append(out, p.build(p.name))
	}
	return out
}
