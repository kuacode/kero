package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
)

// The wheel scrolls the picker's folders away from the one chosen.
func TestPickerScrolls(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 900)
	l := &proto.FSList{Dir: "/home/me", Parent: "/home"}
	for i := range 60 {
		l.Entries = append(l.Entries, proto.FSEntry{Name: fmt.Sprintf("folder%02d", i), Dir: true})
	}
	a.picker = folderPicker{open: true, host: a.local(), list: l}
	tt.Frame()
	r, ok := tt.Find("folder03")
	if !ok {
		t.Fatalf("no folders shown; texts %q", tt.Texts())
	}
	tt.Scroll(r.X+20, r.Y, 0, 300)
	tt.Frame()
	tt.Frame()
	if y := a.picker.scroll.Y; y < 100 {
		t.Errorf("the wheel scrolled the folders to %g, want 300", y)
	}
	// The keys still bring the one they choose into view.
	for range 40 {
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	if y := a.picker.scroll.Y; y <= 300 {
		t.Errorf("choosing folder39 left the folders at %g", y)
	}
}

// A path typed in the picker opens its folder as the project, whether it is
// this machine's or, from a Windows Kero, a drive's.
func TestPickerPathQueryOpensProject(t *testing.T) {
	a := testApp(t)
	a.picker = folderPicker{open: true, host: a.local()}
	tt := ui.NewTester(a.view, 1100, 900)
	dir := t.TempDir()
	opened := make(chan string, 1)
	cl, stop := projectClient(t, func(method string, raw json.RawMessage) (any, error) {
		switch method {
		case "project.create":
			var p proto.ProjectParams
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
			if p.Dir != dir {
				t.Errorf("project.create received %q, want %q", p.Dir, dir)
			}
			opened <- p.Dir
			return proto.Project{ID: "typed", Dir: p.Dir}, nil
		case "create":
			return proto.Session{ID: "typed-session", Project: "typed"}, nil
		default:
			return nil, errors.New("unexpected method " + method)
		}
	})
	defer stop()
	a.local().cl = cl
	a.update(func() { a.picker.query = dir })
	tt.Frame()
	tt.Type(dir)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	select {
	case got := <-opened:
		if got != dir {
			t.Fatalf("a path typed in opened %q, want %q", got, dir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a path typed in opened no project")
	}
	tt.Frame()
}
