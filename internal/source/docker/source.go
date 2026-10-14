// Package docker is an inventory source: it lists running containers so the
// graph also shows workloads that produce no traffic data, and so addresses
// seen by other sources can be resolved back to a workload name.
//
// Labels on a container tune how it shows up:
//
//	teia.node:   node ID (defaults to the compose service or container name)
//	teia.kind:   service | database | cache | queue | external
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

const DefaultSocket = "/var/run/docker.sock"

type Config struct {
	Socket string
	// Project limits discovery to one compose project.
	Project string
}

type Source struct {
	name   string
	cfg    Config
	client *http.Client
}

func New(name string, cfg Config) *Source {
	if cfg.Socket == "" {
		cfg.Socket = DefaultSocket
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", cfg.Socket)
		},
	}
	return &Source{name: name, cfg: cfg, client: &http.Client{Transport: transport, Timeout: 10 * time.Second}}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "docker" }

type container struct {
	Names           []string          `json:"Names"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	if _, err := os.Stat(s.cfg.Socket); errors.Is(err, fs.ErrNotExist) {
		obs.Degraded = true
		obs.Message = "docker socket not found"
		obs.Findings = append(obs.Findings, s.socketFinding(false))
		return obs, nil
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
		return obs, fmt.Errorf("listing containers: %w", err)
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
			Labels: map[string]string{"docker.container": strings.TrimPrefix(first(c.Names), "/")},
		}
		if n.Kind == "" {
			n.Kind = model.KindUnknown
		}
		for _, nw := range c.NetworkSettings.Networks {
			if nw.IPAddress != "" {
				n.Addresses = append(n.Addresses, nw.IPAddress)
			}
		}
		obs.Nodes = append(obs.Nodes, n)
	}
	return obs, nil
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
		f.Detail = fmt.Sprintf("Permission denied on %s, so containers without traffic stay invisible and IP addresses cannot be resolved to workloads.", s.cfg.Socket)
	} else {
		f.Title = "Docker socket is not mounted"
		f.Detail = fmt.Sprintf("%s does not exist inside the teia container.", s.cfg.Socket)
	}
	return f
}
