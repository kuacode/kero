package daemon

// cpuPercent is the busy fraction of the ticks between two samples of the
// CPU counters, in percent, clamped to 0..100. A sample that did not
// advance, or went back, has no fraction to tell.
func cpuPercent(busy0, total0, busy1, total1 uint64) int {
	if total1 <= total0 || busy1 < busy0 {
		return 0
	}
	pct := int(100 * (busy1 - busy0) / (total1 - total0))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
