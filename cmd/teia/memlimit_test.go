package main

import (
	"errors"
	"io/fs"
	"runtime/debug"
	"testing"
)

func files(m map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if v, ok := m[path]; ok {
			return []byte(v), nil
		}
		return nil, fs.ErrNotExist
	}
}

func TestContainerMemoryLimit(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  int64
	}{
		{"cgroup v2", map[string]string{"/sys/fs/cgroup/memory.max": "536870912\n"}, 536870912},
		{"cgroup v2 without a limit", map[string]string{"/sys/fs/cgroup/memory.max": "max\n"}, 0},
		{"cgroup v1", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "1073741824"}, 1073741824},
		{"cgroup v1 without a limit", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712"}, 0},
		{"no cgroup", nil, 0},
		{"garbage", map[string]string{"/sys/fs/cgroup/memory.max": "lots"}, 0},
	}
	for _, c := range cases {
		if got := containerMemoryLimit(files(c.files)); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSetMemoryLimitKeepsAnExplicitGOMEMLIMIT(t *testing.T) {
	before := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(before)
	limit := files(map[string]string{"/sys/fs/cgroup/memory.max": "1000000000"})
	if got := setMemoryLimit(func(string) string { return "800MiB" }, limit); got != 0 || debug.SetMemoryLimit(-1) != before {
		t.Errorf("GOMEMLIMIT set: setMemoryLimit = %d and the limit changed", got)
	}
	if got := setMemoryLimit(func(string) string { return "" }, limit); got != 900000000 || debug.SetMemoryLimit(-1) != 900000000 {
		t.Errorf("container limit 1 GB: set %d, runtime has %d; want 90%%", got, debug.SetMemoryLimit(-1))
	}
	if got := setMemoryLimit(func(string) string { return "" }, func(string) ([]byte, error) { return nil, errors.New("no cgroup") }); got != 0 {
		t.Errorf("no container limit: set %d", got)
	}
}
