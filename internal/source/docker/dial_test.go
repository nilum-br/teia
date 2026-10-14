package docker

import "testing"

func TestResolveEndpoint(t *testing.T) {
	cases := []struct {
		in   string
		want endpoint
	}{
		{"unix:///var/run/docker.sock", endpoint{"unix", "/var/run/docker.sock"}},
		{"/run/user/1000/podman/podman.sock", endpoint{"unix", "/run/user/1000/podman/podman.sock"}},
		{"npipe:////./pipe/docker_engine", endpoint{"npipe", `\\.\pipe\docker_engine`}},
		{"tcp://127.0.0.1:2375", endpoint{"tcp", "127.0.0.1:2375"}},
	}
	for _, c := range cases {
		got, err := resolveEndpoint(c.in)
		if err != nil || got != c.want {
			t.Errorf("resolveEndpoint(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	if _, err := resolveEndpoint("ssh://host"); err == nil {
		t.Error("ssh:// should be rejected")
	}
}
