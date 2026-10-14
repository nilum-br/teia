// Package config loads teia.yaml and builds the configured sources.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/declared"
	"github.com/nilum-br/teia/internal/source/docker"
	"github.com/nilum-br/teia/internal/source/postgres"
	"github.com/nilum-br/teia/internal/source/servicegraph"
)

type Config struct {
	Listen   string            `yaml:"listen"`
	Interval time.Duration     `yaml:"interval"`
	Timeout  time.Duration     `yaml:"timeout"`
	Aliases  map[string]string `yaml:"aliases"`
	Ignore   []string          `yaml:"ignore"`
	Sources  Sources           `yaml:"sources"`
}

type Sources struct {
	Declared     *Declared      `yaml:"declared"`
	ServiceGraph *ServiceGraph  `yaml:"servicegraph"`
	Docker       *Docker        `yaml:"docker"`
	Postgres     []PostgresNode `yaml:"postgres"`
}

type Declared struct {
	Paths []string `yaml:"paths"`
}

type ServiceGraph struct {
	PrometheusURL string            `yaml:"prometheusURL"`
	Window        time.Duration     `yaml:"window"`
	RequestMetric string            `yaml:"requestMetric"`
	FailedMetric  string            `yaml:"failedMetric"`
	Headers       map[string]string `yaml:"headers"`
}

type Docker struct {
	Socket  string `yaml:"socket"`
	Project string `yaml:"project"`
}

type PostgresNode struct {
	Node string `yaml:"node"`
	DSN  string `yaml:"dsn"`
}

// Load reads a config file. Environment variables in the file are expanded,
// so secrets can stay out of it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Listen: ":8080", Interval: 30 * time.Second}
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(data))), cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Sources.ServiceGraph != nil && c.Sources.ServiceGraph.PrometheusURL == "" {
		return fmt.Errorf("sources.servicegraph.prometheusURL is required")
	}
	for i, p := range c.Sources.Postgres {
		if p.Node == "" || p.DSN == "" {
			return fmt.Errorf("sources.postgres[%d] needs node and dsn", i)
		}
	}
	return nil
}

// BuildSources instantiates every configured source.
func (c *Config) BuildSources() []source.Source {
	var out []source.Source
	s := c.Sources
	if s.Declared != nil {
		out = append(out, declared.New("declared", s.Declared.Paths))
	}
	if s.ServiceGraph != nil {
		out = append(out, servicegraph.New("servicegraph", servicegraph.Config{
			PrometheusURL: s.ServiceGraph.PrometheusURL,
			Window:        s.ServiceGraph.Window,
			RequestMetric: s.ServiceGraph.RequestMetric,
			FailedMetric:  s.ServiceGraph.FailedMetric,
			Headers:       s.ServiceGraph.Headers,
		}))
	}
	if s.Docker != nil {
		out = append(out, docker.New("docker", docker.Config{Socket: s.Docker.Socket, Project: s.Docker.Project}))
	}
	for _, p := range s.Postgres {
		out = append(out, postgres.New("postgres/"+p.Node, postgres.Config{Node: p.Node, DSN: p.DSN}))
	}
	return out
}
