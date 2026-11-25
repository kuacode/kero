package daemon

import (
	"encoding/json"
	"testing"
	"time"

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

// LocalLoad reads real values: a CPU percent in range, and memory used
// within total.
func TestLocalLoad(t *testing.T) {
	load := LocalLoad()
	if load.CPU < 0 || load.CPU > 100 {
		t.Errorf("CPU %d out of range", load.CPU)
	}
	if load.MemTotal == 0 || load.MemUsed > load.MemTotal {
		t.Errorf("memory %d of %d", load.MemUsed, load.MemTotal)
	}
}

// A CPU reading over an explicit interval is in range. Taking a positive
// interval is what keeps gopsutil from using its process-wide baseline,
// which the sampler relies on so that readers cannot shorten the window.
func TestReadCPUPercent(t *testing.T) {
	pct, ok := readCPUPercent(20 * time.Millisecond)
	if !ok {
		t.Fatal("no CPU reading")
	}
	if pct < 0 || pct > 100 {
		t.Errorf("CPU %d out of range", pct)
	}
}

// The load the RPC answers is the cached CPU value: reading it must not
// take a new sample, or a burst of readers would make the figure collapse
// toward no time at all. Cache a known value and answer repeatedly; every
// answer must be the cached one.
func TestSystemLoadRPCReadsCachedCPU(t *testing.T) {
	old := cpuBusy.Load()
	cpuBusy.Store(37)
	t.Cleanup(func() { cpuBusy.Store(old) })
	d := listenDaemon(t)
	for i := 0; i < 3; i++ {
		v, err := d.call(nil, "system.load", nil)
		if err != nil {
			t.Fatal(err)
		}
		if load := v.(proto.SystemLoad); load.CPU != 37 {
			t.Errorf("call %d: CPU %d, want cached 37", i, load.CPU)
		}
	}
}
