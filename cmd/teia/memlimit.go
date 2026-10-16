package main

import (
	"runtime/debug"
	"strconv"
	"strings"
)

// memoryShare is the part of a container's memory limit Go's collector aims
// to stay under, leaving room for what the runtime does not count (stacks,
// buffers the kernel holds for teia).
const memoryShare = 0.9

// cgroupLimits are where Linux reports a container's memory limit: cgroup v2
// first, then v1.
var cgroupLimits = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// containerMemoryLimit reads the container's memory limit in bytes, zero
// when there is none or it cannot be read (no cgroup, another OS).
func containerMemoryLimit(read func(string) ([]byte, error)) int64 {
	for _, path := range cgroupLimits {
		data, err := read(path)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(data))
		if v == "max" {
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		// cgroup v1 reports "no limit" as a number near the int64 maximum.
		if err != nil || n <= 0 || n >= 1<<62 {
			return 0
		}
		return n
	}
	return 0
}

// setMemoryLimit gives Go's collector the container's memory limit, unless
// GOMEMLIMIT already says one: without it, the heap may grow to twice what
// is live before a collection, which a container's limit turns into an
// out-of-memory kill while a round builds the next map. It returns the limit
// set, zero when it set none.
func setMemoryLimit(getenv func(string) string, read func(string) ([]byte, error)) int64 {
	if getenv("GOMEMLIMIT") != "" {
		return 0
	}
	limit := containerMemoryLimit(read)
	if limit == 0 {
		return 0
	}
	soft := int64(float64(limit) * memoryShare)
	debug.SetMemoryLimit(soft)
	return soft
}
