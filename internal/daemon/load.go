package daemon

import (
	"time"

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

// cpuPercentNow samples the CPU busy percent over a short interval.
func cpuPercentNow() int {
	pcts, err := cpu.Percent(120*time.Millisecond, false)
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

// memLoad reads memory used and total in bytes through gopsutil.
func memLoad() (used, total uint64) {
	v, err := mem.VirtualMemory()
	if err != nil || v == nil || v.Total == 0 {
		return 0, 0
	}
	return v.Used, v.Total
}
