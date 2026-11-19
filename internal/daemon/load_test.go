package daemon

import (
	"encoding/json"
	"testing"

	"kero/internal/proto"
)

// system.load answers with this machine's load, on a "system" event the
// sidebar's host header shows.
func TestSystemLoadRPC(t *testing.T) {
	d := listenDaemon(t)
	v, err := d.call(nil, "system.load", nil)
	if err != nil {
		t.Fatal(err)
	}
	load, ok := v.(proto.SystemLoad)
	if !ok {
		t.Fatalf("system.load: %T", v)
	}
	if load.CPU < 0 || load.CPU > 100 {
		t.Errorf("CPU %d out of range", load.CPU)
	}
	if load.MemTotal == 0 || load.MemUsed > load.MemTotal {
		t.Errorf("memory %d of %d", load.MemUsed, load.MemTotal)
	}
}

// The RPC also answers as JSON, as a control connection sees it.
func TestSystemLoadRPCJSON(t *testing.T) {
	d := listenDaemon(t)
	raw, _ := json.Marshal(map[string]any{"x": 1})
	v, err := d.call(nil, "system.load", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(proto.SystemLoad); !ok {
		t.Fatalf("system.load: %T", v)
	}
}

// The CPU is the busy fraction of the ticks between two samples, so a
// short moment that landed on an idle tick does not read as 0 for long.
func TestCPUPercent(t *testing.T) {
	for _, c := range []struct {
		b0, t0, b1, t1 uint64
		want           int
	}{
		{0, 0, 50, 100, 50},
		{100, 1000, 100, 1100, 0},  // all idle
		{100, 1000, 130, 1100, 30}, // 30 of 100 busy
		{100, 1000, 100, 1000, 0},  // not advanced
		{100, 1000, 90, 1100, 0},   // went back: no fraction
		{0, 0, 200, 100, 100},      // past 100, clamped
	} {
		if got := cpuPercent(c.b0, c.t0, c.b1, c.t1); got != c.want {
			t.Errorf("cpuPercent(%d,%d,%d,%d) = %d, want %d", c.b0, c.t0, c.b1, c.t1, got, c.want)
		}
	}
}

// loadLocked tells the load with the CPU it measured last, and memory now.
func TestLoadLocked(t *testing.T) {
	d := &Daemon{cpuPct: 42}
	l := d.loadLocked()
	if l.CPU != 42 {
		t.Errorf("CPU %d, want 42", l.CPU)
	}
	if l.MemTotal == 0 || l.MemUsed > l.MemTotal {
		t.Errorf("memory %d of %d", l.MemUsed, l.MemTotal)
	}
}
