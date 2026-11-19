//go:build darwin

package daemon

import (
	"time"

	"golang.org/x/sys/unix"

	"kero/internal/proto"
)

// LocalLoad reads this machine's load, through sysctl: CPU busy percent
// from the cumulative kern.cp_time, and memory from hw.memsize with the
// vm.statistics64 counters. It reads CPU twice, a moment apart, for its
// busy fraction.
func LocalLoad() proto.SystemLoad {
	used, total := memLoad()
	return proto.SystemLoad{CPU: darwinCPU(), MemUsed: used, MemTotal: total}
}

// memLoad reads memory used and total, from hw.memsize and vm.statistics64.
func memLoad() (used, total uint64) {
	total = darwinMemTotal()
	used = darwinMemUsed(total)
	return used, total
}

// readCPUTicks reads the cumulative kern.cp_time counters, busy (user +
// nice + system) and total (busy + idle), in ticks.
func readCPUTicks() (busy, total uint64, ok bool) {
	b, err := unix.SysctlRaw("kern.cp_time")
	if err != nil || len(b) < 4*4 {
		return 0, 0, false
	}
	u := make([]uint32, len(b)/4)
	for i := range u {
		u[i] = uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
	}
	// user nice system idle.
	user, nice, system, idle := u[0], u[1], u[2], u[3]
	busy = uint64(user + nice + system)
	return busy, busy + uint64(idle), true
}

func darwinCPU() int {
	t0, total0, ok := readCPUTicks()
	if !ok {
		return 0
	}
	// The counters move slowly; a moment apart is enough.
	time.Sleep(120 * time.Millisecond)
	t1, total1, ok := readCPUTicks()
	if !ok || total1 <= total0 {
		return 0
	}
	busy := t1 - t0
	if busy == 0 {
		return 0
	}
	pct := int(100 * busy / (total1 - total0))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func darwinMemTotal() uint64 {
	v, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return v
}

func darwinMemUsed(total uint64) uint64 {
	if total == 0 {
		return 0
	}
	// vm_statistics64, in pages: free, active, inactive, wire, ... Used is
	// roughly active + wired (compressed is its own counter, folded in).
	b, err := unix.SysctlRaw("vm.statistics64")
	if err != nil || len(b) < 4*8 {
		return 0
	}
	u := make([]uint64, len(b)/8)
	for i := range u {
		u[i] = uint64(b[i*8]) | uint64(b[i*8+1])<<8 | uint64(b[i*8+2])<<16 | uint64(b[i*8+3])<<24 |
			uint64(b[i*8+4])<<32 | uint64(b[i*8+5])<<40 | uint64(b[i*8+6])<<48 | uint64(b[i*8+7])<<56
	}
	// vm_statistics64 order: free, active, inactive, wire, zero_fill, ...
	pageSize := uint64(4096)
	active, wire := u[1], u[3]
	used := (active + wire) * pageSize
	if used > total {
		return total
	}
	return used
}
