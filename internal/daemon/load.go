package daemon

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"

	"kero/internal/proto"
)

// LocalLoad reads this machine's load now: the CPU busy percent over a
// short interval and memory used of total, both through gopsutil.
func LocalLoad() proto.SystemLoad {
	used, total := memLoad()
	return proto.SystemLoad{CPU: cpuPercentNow(), MemUsed: used, MemTotal: total}
}

// cpuPercentNow samples the CPU busy percent since the last call. A short
// fixed window (say 120ms) reads 0 on an idle machine: in two ticks of the
// clock it often sees no CPU time at all. Percent(0) instead averages from
// gopsutil's previous sample, so the window is the caller's interval (the
// daemon broadcasts every couple of seconds), which tracks load without
// flickering to 0. It returns an error on the very first sample; a host
// connecting then just shows 0 until the next push.
func cpuPercentNow() int {
	pcts, err := cpu.Percent(0, false)
	if err != nil || len(pcts) == 0 {
		return 0
	}
	p := pcts[0]
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return int(p)
}

// seedLoadBaseline primes gopsutil's from-last-call CPU sample. Without it
// the first reading has no sample to subtract and shows 0% until the next
// push; seeding at startup makes the first request read the time since.
func seedLoadBaseline() {
	_, _ = cpu.Percent(0, false)
}

// memLoad reads memory used and total in bytes through gopsutil.
func memLoad() (used, total uint64) {
	v, err := mem.VirtualMemory()
	if err != nil || v == nil || v.Total == 0 {
		return 0, 0
	}
	return v.Used, v.Total
}
