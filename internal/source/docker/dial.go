package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// endpoint is where the Docker Engine API listens.
type endpoint struct {
	scheme string // unix, npipe or tcp
	addr   string // socket path, pipe path or host:port
}

func (e endpoint) String() string {
	if e.scheme == "npipe" {
		return "npipe://" + filepath.ToSlash(e.addr)
	}
	return e.scheme + "://" + e.addr
}

// resolveEndpoint picks the API endpoint: explicit config first, then
// DOCKER_HOST, then the usual place for this OS and runtime.
func resolveEndpoint(host string) (endpoint, error) {
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if host == "" {
		return defaultEndpoint(), nil
	}
	if strings.HasPrefix(host, "/") {
		return endpoint{"unix", host}, nil
	}
	scheme, rest, ok := strings.Cut(host, "://")
	if !ok {
		return endpoint{}, fmt.Errorf("docker host %q: expected unix://, npipe:// or tcp://", host)
	}
	switch scheme {
	case "unix":
		return endpoint{"unix", rest}, nil
	case "npipe":
		// npipe:////./pipe/docker_engine -> \\.\pipe\docker_engine
		return endpoint{"npipe", strings.ReplaceAll(rest, "/", `\`)}, nil
	case "tcp", "http":
		return endpoint{"tcp", rest}, nil
	}
	return endpoint{}, fmt.Errorf("docker host %q: unsupported scheme %q", host, scheme)
}

func defaultEndpoint() endpoint {
	if runtime.GOOS == "windows" {
		return endpoint{"npipe", `\\.\pipe\docker_engine`}
	}
	candidates := []string{"/var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".docker", "run", "docker.sock"),     // Docker Desktop (macOS, Linux)
			filepath.Join(home, ".orbstack", "run", "docker.sock"),   // OrbStack
			filepath.Join(home, ".colima", "default", "docker.sock"), // Colima
			filepath.Join(home, ".rd", "docker.sock"),                // Rancher Desktop
		)
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "podman", "podman.sock"), filepath.Join(xdg, "docker.sock"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return endpoint{"unix", c}
		}
	}
	return endpoint{"unix", candidates[0]}
}

func (e endpoint) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	switch e.scheme {
	case "unix":
		return d.DialContext(ctx, "unix", e.addr)
	case "tcp":
		return d.DialContext(ctx, "tcp", e.addr)
	case "npipe":
		return dialPipe(ctx, e.addr)
	}
	return nil, fmt.Errorf("unsupported scheme %q", e.scheme)
}
