package main

import (
	"testing"
	"time"

	"kero/internal/client"
	"kero/internal/proto"
	"kero/internal/settings"
	"kero/locales"
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
	if !build.hasLoad() {
		t.Errorf("a host that got a load does not say it has one")
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

// The row's one tooltip adds the jitter and the memory share the inline
// figures leave out, without repeating the latency and CPU already shown.
func TestStatsTip(t *testing.T) {
	a := testApp(t)
	build := withHost(t, a, "build")
	build.load = proto.SystemLoad{CPU: 42, MemUsed: 8 << 30, MemTotal: 16 << 30}
	build.loadAt = time.Now()
	build.observeRTT(15 * time.Millisecond)
	build.observeRTT(21 * time.Millisecond)
	// Jitter comes from the samples; the figures read inline are not said
	// twice. The name is on the row already, so the tip omits it too.
	want := locales.HostJitter(rttText(build.rttVar)) + " · CPU 42% · Mem 50%"
	if got := a.statsTip(build); got != want {
		t.Errorf("statsTip = %q, want %q", got, want)
	}
	// With no memory reported, only the CPU share is added.
	build.load = proto.SystemLoad{CPU: 0}
	build.loadAt = time.Now()
	build.rttSamples = 0
	if got := a.statsTip(build); got != "CPU 0% · Mem —" {
		t.Errorf("statsTip = %q", got)
	}
}

// A load's color grades with its size: the host's own while low, the
// palette's warning past the warn mark, its danger past the critical one.
func TestLoadColor(t *testing.T) {
	a := testApp(t)
	p := &lightPalette
	if got := a.loadColor(p, p.textMuted, 10, cpuWarn); got != p.textMuted {
		t.Error("a low load is colored")
	}
	if got := a.loadColor(p, p.textMuted, cpuWarn+1, cpuWarn); got != p.attention {
		t.Error("a warning load is not the warning color")
	}
	if got := a.loadColor(p, p.textMuted, loadMax+1, memWarn); got != p.danger {
		t.Error("a critical load is not the danger color")
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
