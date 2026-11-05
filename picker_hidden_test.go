package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
)

// The picker's Show Hidden button asks fs.list again, with the hidden
// folders too, and Hide Hidden folds them away again: the button toggles
// p.hidden and dispatches pickerGo with the listing's folder, which asks
// the host with the new value.
func TestPickerShowHidden(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 900)
	dir := t.TempDir()
	for _, d := range []string{".hidden", "shown"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The test server records what fs.list was asked, and serves the same
	// filter the daemon's fsList does: dotfiles only when asked.
	var lastHidden atomic.Bool
	var calls atomic.Int32
	cl, stop := projectClient(t, func(method string, raw json.RawMessage) (any, error) {
		if method != "fs.list" {
			t.Errorf("unexpected method %q", method)
			return nil, nil
		}
		var p proto.FSListParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		lastHidden.Store(p.Hidden)
		calls.Add(1)
		entries := []proto.FSEntry{}
		for _, name := range []string{".hidden", "shown"} {
			if !p.Hidden && name[0] == '.' {
				continue
			}
			entries = append(entries, proto.FSEntry{Name: name, Dir: true})
		}
		return proto.FSList{Dir: dir, Parent: filepath.Dir(dir), Home: dir, Entries: entries}, nil
	})
	defer stop()
	a.local().cl = cl
	a.picker = folderPicker{open: true, host: a.local()}
	a.pickerGo(dir)
	// pickerGo asks fs.list with hidden=false for the first listing.
	waitFor(t, func() bool { return calls.Load() >= 1 })
	if lastHidden.Load() {
		t.Error("the first fs.list asked with Hidden=true")
	}
	// pickerGo's a.update callback needs a live window, which the tester
	// has none of, so seed the list it would have assigned.
	a.picker.list = &proto.FSList{Dir: dir, Parent: filepath.Dir(dir),
		Entries: []proto.FSEntry{{Name: "shown", Dir: true}}}
	tt.Frame()
	if names := pickerNames(a); len(names) != 1 || names[0] != "shown" {
		t.Fatalf("folders before the button: %v", names)
	}
	// Show Hidden toggles p.hidden and pickerGo asks fs.list again, with
	// the hidden folders too.
	if err := tt.Click(locales.PickerShowHidden()); err != nil {
		t.Fatalf("no Show Hidden button: %v", err)
	}
	if !a.picker.hidden {
		t.Error("the button did not toggle hidden on")
	}
	waitFor(t, func() bool { return calls.Load() >= 2 })
	if !lastHidden.Load() {
		t.Error("Show Hidden did not ask fs.list with Hidden=true")
	}
	// Hide Hidden toggles it back and pickerGo asks fs.list without them.
	if err := tt.Click(locales.PickerHideHidden()); err != nil {
		t.Fatalf("no Hide Hidden button: %v", err)
	}
	if a.picker.hidden {
		t.Error("the button did not toggle hidden off")
	}
	waitFor(t, func() bool { return calls.Load() >= 3 })
	if lastHidden.Load() {
		t.Error("Hide Hidden did not ask fs.list with Hidden=false")
	}
}

// pickerNames are the folders the picker shows, skipping the parent row.
func pickerNames(a *App) []string {
	var names []string
	for _, it := range a.pickerItems() {
		if !it.up {
			names = append(names, it.name)
		}
	}
	return names
}

// waitFor polls done until it holds, or fails after a second.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	for range 200 {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}
