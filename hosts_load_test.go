package main

import (
	"testing"

	"kero/internal/client"
	"kero/internal/proto"
	"kero/internal/settings"
)

// A "system" event from a host's daemon caches its load, which the host
// header's bars show.
func TestHostLoadEvent(t *testing.T) {
	a := testApp(t)
	build := withHost(t, a, "build")
	cl := &client.Client{}
	build.cl = cl
	load := proto.SystemLoad{CPU: 42, MemUsed: 8 << 30, MemTotal: 16 << 30}
	a.event(build, cl, proto.Message{Event: "system", System: &load})
	if build.load != load {
		t.Fatalf("load %+v, want %+v", build.load, load)
	}
	// Another host's event is its own: it comes on that host's connection.
	other := withHost(t, a, "gpu")
	other.cl = cl
	a.event(other, cl, proto.Message{Event: "system", System: &proto.SystemLoad{CPU: 7}})
	if build.load != load || other.load.CPU != 7 {
		t.Errorf("loads mixed: build %+v, gpu %+v", build.load, other.load)
	}
	// A stale event, from a connection the host has dropped, is not taken:
	// build now has another connection, but the load still comes on the
	// old one.
	build.cl = &client.Client{}
	a.event(build, cl, proto.Message{Event: "system", System: &proto.SystemLoad{CPU: 1}})
	if build.load != load {
		t.Errorf("a dropped connection's load was taken: %+v", build.load)
	}
}

// The header's tooltip says the load the bars show.
func TestLoadTip(t *testing.T) {
	a := testApp(t)
	build := withHost(t, a, "build")
	build.load = proto.SystemLoad{CPU: 42, MemUsed: 8 << 30, MemTotal: 16 << 30}
	if got := a.loadTip(build); got != "build: CPU 42% · Mem 50%" {
		t.Errorf("loadTip = %q", got)
	}
	// With no memory reported, the bars show only the CPU.
	build.load = proto.SystemLoad{CPU: 0}
	if got := a.loadTip(build); got != "build: CPU 0% · Mem —" {
		t.Errorf("loadTip = %q", got)
	}
}

// The load the header shows is on by default, so a host's CPU and memory
// are read without the user's asking, and off from the settings.
func TestShowSystemLoadDefault(t *testing.T) {
	if !settings.Default().ShowSystemLoad {
		t.Error("a host's load is not on by default")
	}
	a := testApp(t)
	if !a.set.ShowSystemLoad {
		t.Error("a fresh app's setting is off")
	}
	a.set.ShowSystemLoad = false
	if a.set.ShowSystemLoad {
		t.Error("turning the setting off failed")
	}
}
