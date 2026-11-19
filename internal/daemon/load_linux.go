//go:build linux

package daemon

import (
	"os"
	"strconv"
	"strings"
	"time"

	"kero/internal/proto"
)

// LocalLoad reads this machine's load now, sampling the CPU over a short
// moment apart. It is a stateless ask, for a client that wants it once;
// the daemon's periodic push samples the counters itself, over a longer
// interval, for a steadier figure.
func LocalLoad() proto.SystemLoad {
	used, total := memLoad()
	return proto.SystemLoad{CPU: procCPU(), MemUsed: used, MemTotal: total}
}

func procCPU() int {
	b0, t0, ok := readCPUTicks()
	if !ok {
		return 0
	}
	// The counters move slowly; a moment apart is enough.
	time.Sleep(120 * time.Millisecond)
	b1, t1, ok := readCPUTicks()
	if !ok {
		return 0
	}
	return cpuPercent(b0, t0, b1, t1)
}

// readCPUTicks reads /proc/stat's first line once: the busy and total
// ticks of all CPUs, without waiting.
func readCPUTicks() (busy, total uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	var v [8]uint64
	for i := 0; i < len(v) && i+1 < len(f); i++ {
		v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
	}
	// user nice system idle iowait irq softirq steal.
	idle := v[3] + v[4]
	for _, x := range v {
		total += x
	}
	return total - idle, total, true
}

// memLoad reads memory used and total, from meminfo.
func memLoad() (used, total uint64) {
	total = procMeminfo("MemTotal:")
	avail := procMeminfo("MemAvailable:")
	if total == 0 || avail > total {
		return 0, total
	}
	return total - avail, total
}

func procMeminfo(key string) uint64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, key) {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return 0
		}
		// kB.
		v, _ := strconv.ParseUint(f[1], 10, 64)
		return v * 1024
	}
	return 0
}
