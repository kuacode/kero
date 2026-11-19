package main

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The sidebar heads this machine too, "This Computer", and folds its
// projects on a click of the header, keeping the session shown.
func TestLocalHeader(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	local := a.local()
	if !local.loaded {
		t.Fatal("local not loaded")
	}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("This Computer") {
		t.Errorf("no This Computer header: %q", tt.Texts())
	}
	if !slices.Contains(a.visibleOrder(), "s1") {
		t.Fatalf("local session not in order: %q", a.visibleOrder())
	}
	// Folding the local host hides its projects; the shown session stays.
	a.selected = ""
	a.toggleHost(local)
	if slices.Contains(a.visibleOrder(), "s1") {
		t.Errorf("folded local still lists its session: %q", a.visibleOrder())
	}
	a.selected = "s1"
	if !slices.Contains(a.visibleOrder(), "s1") {
		t.Errorf("folded local does not keep the shown session: %q", a.visibleOrder())
	}
	a.toggleHost(local)
	a.selected = ""
	if !slices.Contains(a.visibleOrder(), "s1") {
		t.Errorf("unfolded local: %q", a.visibleOrder())
	}
}
