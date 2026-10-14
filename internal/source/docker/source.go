// Package docker is an inventory source: it lists running containers so the
// graph also shows workloads that produce no traffic data, and so addresses
// seen by other sources can be resolved back to a workload name.
//
// Labels on a container tune how it shows up:
//
//	teia.node:   node ID (defaults to the compose service or container name)
//	teia.kind:   service | database | cache | queue | external
//	teia.tech:   technology for the icon (postgresql, redis, kafka...)
//	teia.ignore: "true" to keep it out of the graph
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type Config struct {
	// Host is a Docker endpoint (unix:///var/run/docker.sock,
	// npipe:////./pipe/docker_engine, tcp://host:2375). Empty means
	// DOCKER_HOST, then the usual place for this OS.
	Host string
	// Project limits discovery to one compose project.
	Project string
}

type Source struct {
	name     string
	cfg      Config
	endpoint endpoint
	err      error
	client   *http.Client
}

func New(name string, cfg Config) *Source {
	s := &Source{name: name, cfg: cfg}
	s.endpoint, s.err = resolveEndpoint(cfg.Host)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return s.endpoint.dial(ctx)
		},
	}
	s.client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	return s
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "docker" }

type container struct {
	Names           []string          `json:"Names"`
	Image           string            `json:"Image"`
	Created         int64             `json:"Created"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	if s.err != nil {
		return obs, s.err
	}
	if s.endpoint.scheme == "unix" {
		if _, err := os.Stat(s.endpoint.addr); errors.Is(err, fs.ErrNotExist) {
			obs.Degraded = true
			obs.Message = "docker socket not found"
			obs.Findings = append(obs.Findings, s.socketFinding(false))
			return obs, nil
		}
	}

	u := "http://docker/containers/json"
	if s.cfg.Project != "" {
		filters, _ := json.Marshal(map[string][]string{"label": {"com.docker.compose.project=" + s.cfg.Project}})
		u += "?filters=" + url.QueryEscape(string(filters))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return obs, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, fs.ErrPermission) {
			obs.Degraded = true
			obs.Message = "permission denied on the docker socket"
			obs.Findings = append(obs.Findings, s.socketFinding(true))
			return obs, nil
		}
		return obs, fmt.Errorf("listing containers at %s: %w", s.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return obs, fmt.Errorf("docker answered HTTP %d", resp.StatusCode)
	}
	var containers []container
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return obs, fmt.Errorf("decoding container list: %w", err)
	}

	for _, c := range containers {
		n := source.NodeObservation{
			ID:     nodeID(c),
			Kind:   model.NodeKind(c.Labels["teia.kind"]),
			Ignore: c.Labels["teia.ignore"] == "true",
			Labels: userLabels(c.Labels),
		}
		n.Labels["docker.container"] = strings.TrimPrefix(first(c.Names), "/")
		n.Labels["image"] = c.Image
		if t := c.Labels["teia.tech"]; t != "" {
			n.Labels["teia.tech"] = t
		}
		if p := c.Labels["com.docker.compose.project"]; p != "" {
			n.Labels["compose.project"] = p
		}
		if n.Kind == "" {
			n.Kind = model.KindUnknown
		}
		if c.Created > 0 {
			n.Since = time.Unix(c.Created, 0)
		}
		for _, nw := range c.NetworkSettings.Networks {
			if nw.IPAddress != "" {
				n.Addresses = append(n.Addresses, nw.IPAddress)
			}
		}
		obs.Nodes = append(obs.Nodes, n)
	}
	obs.Message = fmt.Sprintf("%d containers at %s", len(containers), s.endpoint)
	return obs, nil
}

// machineLabelPrefixes are set by tools, not people; they would only add
// noise to grouping and filtering.
var machineLabelPrefixes = []string{"com.docker.", "org.opencontainers.", "desktop.docker.io/", "teia."}

func userLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		machine := false
		for _, p := range machineLabelPrefixes {
			if strings.HasPrefix(k, p) {
				machine = true
				break
			}
		}
		if !machine {
			out[k] = v
		}
	}
	return out
}

func nodeID(c container) string {
	if id := c.Labels["teia.node"]; id != "" {
		return id
	}
	if svc := c.Labels["com.docker.compose.service"]; svc != "" {
		return svc
	}
	return strings.TrimPrefix(first(c.Names), "/")
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func (s *Source) socketFinding(denied bool) model.Finding {
	f := model.Finding{
		Rule:     "docker-socket",
		Kind:     model.FindingPermission,
		Severity: model.SeverityWarning,
		Subject:  s.name,
		Fix: &model.Fix{
			Summary:  "Mount the Docker socket read-only into the teia container and run it with access to it.",
			Language: "yaml",
			Snippet: `services:
  teia:
    user: "0:0" # or the GID that owns the socket
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro`,
		},
	}
	if denied {
		f.Title = "teia cannot read the Docker socket"
		f.Detail = fmt.Sprintf("Permission denied on %s, so containers without traffic stay invisible and IP addresses cannot be resolved to workloads.", s.endpoint)
	} else {
		f.Title = "Docker socket not found"
		f.Detail = fmt.Sprintf("%s does not exist. Set sources.docker.host or DOCKER_HOST if your runtime (Podman, Colima, OrbStack, rootless Docker) listens elsewhere.", s.endpoint)
	}
	return f
}
