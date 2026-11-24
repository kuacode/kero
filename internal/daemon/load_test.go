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
