package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A round trip reads in milliseconds, or seconds once it drags, and
// under a millisecond with a sign that it is close.
func TestRTTText(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{300 * time.Microsecond, "<1ms"},
		{7 * time.Millisecond, "7ms"},
		{1499 * time.Microsecond, "1ms"},
		{999 * time.Millisecond, "999ms"},
		{1200 * time.Millisecond, "1.2s"},
		{2 * time.Second, "2.0s"},
	} {
		if got := rttText(c.d); got != c.want {
			t.Errorf("%v reads %q, want %q", c.d, got, c.want)
		}
	}
}

// A round trip folds into a moving average, as RFC 6298 has TCP keep its
// SRTT and RTTVAR: the first sample seeds both, then a slow one bends the
// figure by an eighth and the variance by a quarter, not becomes it.
func TestObserveRTT(t *testing.T) {
	h := &host{name: "build"}
	h.observeRTT(20 * time.Millisecond)
	if h.rtt != 20*time.Millisecond || h.rttVar != 10*time.Millisecond || h.rttSamples != 1 {
		t.Fatalf("the first sample seeds SRTT=%v RTTVAR=%v, want 20ms and 10ms", h.rtt, h.rttVar)
	}
	// A sample a hundred past the average bends it by an eighth of the
	// gap: 20 + 100/8 = 32.5ms, and the variance a quarter of the gap
	// from 10ms toward 100ms: (3*10+100)/4 = 32.5ms.
	h.observeRTT(120 * time.Millisecond)
	if want := 20*time.Millisecond + 100*time.Millisecond/8; h.rtt != want {
		t.Errorf("after a slow sample SRTT is %v, want %v", h.rtt, want)
	}
	if want := (3*10*time.Millisecond + 100*time.Millisecond) / 4; h.rttVar != want {
		t.Errorf("after a slow sample RTTVAR is %v, want %v", h.rttVar, want)
	}
	if h.rttSamples != 2 {
		t.Errorf("two samples in, the count is %d", h.rttSamples)
	}
	// A fast sample draws it back down: 32.5 - 12.5/8.
	h.observeRTT(20 * time.Millisecond)
	if h.rtt >= 32*time.Millisecond+time.Millisecond {
		t.Errorf("a fast sample did not draw SRTT down: %v", h.rtt)
	}
}

// A round trip is the muted text while it is quick, the warning's once it
// drags, and the danger's once it is far.
func TestRTTColor(t *testing.T) {
	a := testApp(t)
	for _, c := range []struct {
		d    time.Duration
		want ui.Color
	}{
		{20 * time.Millisecond, lightPalette.good},
		{rttSlow - time.Millisecond, lightPalette.good},
		{rttSlow, lightPalette.attention},
		{rttFar - time.Millisecond, lightPalette.attention},
		{rttFar, lightPalette.danger},
	} {
		h := &host{name: "build", rtt: c.d, rttSamples: 1}
		if got := a.rttColor(&lightPalette, h); got != c.want {
			t.Errorf("%v is %v, want %v", c.d, got, c.want)
		}
	}
}

// Every host shows a round trip once one was read on its present
// connection, this machine included; one that has not answered shows none.
func TestHasRTT(t *testing.T) {
	if (&host{name: ""}).hasRTT() {
		t.Error("this machine shows a round trip it has not read")
	}
	if (&host{name: "build"}).hasRTT() {
		t.Error("a host not measured shows a round trip")
	}
	local := &host{name: ""}
	local.observeRTT(time.Millisecond)
	if !local.hasRTT() {
		t.Error("a measured this machine shows no round trip")
	}
	h := &host{name: "build"}
	h.observeRTT(18 * time.Millisecond)
	if !h.hasRTT() {
		t.Error("a measured host shows no round trip")
	}
}

// The header reads a remote host's round trip, and its tip names it with
// the jitter once one came.
func TestSidebarHostLatency(t *testing.T) {
	a := testApp(t)
	h := withHost(t, a, "build")
	h.observeRTT(18 * time.Millisecond)
	h.observeRTT(20 * time.Millisecond)
	tt := ui.NewTester(a.view, 1100, 700)
	// 18 and 20 ms fold to 18.25, which reads as 18ms.
	if _, ok := tt.Find("18ms"); !ok {
		t.Fatalf("the header does not read the smoothed round trip; texts %q", tt.Texts())
	}
	tip := a.hostTip(h)
	if !strings.Contains(tip, "Latency 18ms") || !strings.Contains(tip, "jitter 7ms") {
		t.Errorf("the tip does not name the round trip and its jitter: %q", tip)
	}
}

// A connection gone takes its round trip with it: the next one is its
// own, no other's.
func TestLostForgetsRTT(t *testing.T) {
	h := &host{name: "build", conn: connReady}
	h.observeRTT(20 * time.Millisecond)
	h.forgetRTT()
	if h.hasRTT() || h.rttSamples != 0 || h.rttVar != 0 {
		t.Errorf("a connection gone keeps its round trip: %v/%v/%d", h.rtt, h.rttVar, h.rttSamples)
	}
}
