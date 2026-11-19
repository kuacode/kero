//go:build linux

package daemon

import (
	"os"
	"strconv"
	"strings"
	"time"

	"kero/internal/proto"
)

// LocalLoad reads this machine's load, from /proc: CPU busy percent from
// the stat counters, and memory from meminfo. It reads twice, a moment
// apart, for the CPU's busy fraction; the memory needs no second sample.
func LocalLoad() proto.SystemLoad {
	return proto.SystemLoad{CPU: procCPU(), MemUsed: procMemUsed(), MemTotal: procMemTotal()}
}

func procCPU() int {
	// /proc/stat's first line: the total and the idle of all CPUs, in
	// hundredths of a second, with the rest of the fields between.
	read := func() (total, idle uint64, ok bool) {
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
		idle = v[3] + v[4]
		for _, x := range v {
			total += x
		}
		return total, idle, true
	}
	t0, i0, ok := read()
	if !ok {
		return 0
	}
	// The counters move slowly; a moment apart is enough.
	time.Sleep(120 * time.Millisecond)
	t1, i1, ok := read()
	if !ok || t1 <= t0 {
		return 0
	}
	idle := i1 - i0
	busy := t1 - t0 - idle
	if busy == 0 {
		return 0
	}
	pct := int(100 * busy / (t1 - t0))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func procMemTotal() uint64 {
	return procMeminfo("MemTotal:")
}

func procMemUsed() uint64 {
	total := procMeminfo("MemTotal:")
	avail := procMeminfo("MemAvailable:")
	if total == 0 || avail > total {
		return 0
	}
	return total - avail
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
