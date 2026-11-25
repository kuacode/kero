package daemon

import (
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"

	"kero/internal/proto"
)

// cpuBusy is the last CPU busy percent sampled by sampleCPU. Keeping it
// cached is what makes the reading steady: the 2-second broadcast and the
// system.load RPC both read it, so neither takes its own sample that would
// reset gopsutil's process-wide baseline and shorten the other's window.
var cpuBusy atomic.Int64

// LocalLoad reads this machine's cached CPU busy percent and its memory now.
func LocalLoad() proto.SystemLoad {
	used, total := memLoad()
	return proto.SystemLoad{CPU: int(cpuBusy.Load()), MemUsed: used, MemTotal: total}
}

// sampleCPU refreshes cpuBusy every interval until quit closes. A positive
// interval makes gopsutil take both readings itself, so the window is the
// interval — a second, as the OS task managers show — rather than the time
// since some other caller happened to touch its global baseline. One sample
// a second also keeps the cost off the panes' tick and the RPC path.
func sampleCPU(interval time.Duration, quit <-chan struct{}) {
	for {
		select {
		case <-quit:
			return
		default:
		}
		if pct, ok := readCPUPercent(interval); ok {
			cpuBusy.Store(int64(pct))
		}
	}
}

// readCPUPercent takes one gopsutil reading over interval and clamps it to
// 0..100. It reports false when no reading came back, leaving the last
// cached value in place.
func readCPUPercent(interval time.Duration) (int, bool) {
	pcts, err := cpu.Percent(interval, false)
	if err != nil || len(pcts) == 0 {
		return 0, false
	}
	p := pcts[0]
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return int(p), true
}

// memLoad reads memory used and total in bytes through gopsutil.
func memLoad() (used, total uint64) {
	v, err := mem.VirtualMemory()
	if err != nil || v == nil || v.Total == 0 {
		return 0, 0
	}
	return v.Used, v.Total
}
