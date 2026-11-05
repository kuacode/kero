package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/locales"
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

// The picker's quick jumps are its icons, no words: the root by the machine
// — This PC and its drives on Windows, the root elsewhere — then the home by
// the house, each naming where it points on hover.
func TestPickerRoots(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 900)
	home := proto.Home()
	a.picker = folderPicker{open: true, host: a.local(), list: &proto.FSList{Dir: home, Home: home}}
	tt.Frame()
	for _, gone := range []string{locales.PickerHome(), locales.PickerRoot()} {
		if slices.Contains(tt.Texts(), gone) {
			t.Errorf("quick jumps still name %q; texts %q", gone, tt.Texts())
		}
	}
}

// The quick jumps are the root, then the home, wherever the listing is: the
// root (This PC on Windows) where the host's paths start, the home where the
// user's folders are.
func TestPickerRootsList(t *testing.T) {
	a := testApp(t)
	home := proto.Home()
	b := &host{name: "b", info: proto.HostInfo{OS: "linux", Home: "/home/me"}}
	a.picker = folderPicker{open: true, host: b, list: &proto.FSList{
		Dir: home, Home: home,
	}}
	roots := a.pickerRoots()
	if len(roots) != 2 {
		t.Fatalf("quick jumps = %d, want 2 (%+v)", len(roots), roots)
	}
	if roots[0].name != locales.PickerRoot() || roots[0].path != "/" || !roots[0].root || roots[0].thisPC {
		t.Errorf("root jump = %+v", roots[0])
	}
	if roots[1].name != locales.PickerHome() || roots[1].path != home || roots[1].root || roots[1].thisPC {
		t.Errorf("home jump = %+v", roots[1])
	}
}

// A host of Windows starts at This PC, its drives, not /, and its root jump
// carries that.
func TestPickerRootsWindows(t *testing.T) {
	a := testApp(t)
	home := proto.Home()
	w := &host{name: "w", info: proto.HostInfo{OS: "windows", Home: `C:\Users\me`}}
	a.picker = folderPicker{open: true, host: w, list: &proto.FSList{
		Dir: home, Home: home,
	}}
	roots := a.pickerRoots()
	if len(roots) != 2 {
		t.Fatalf("quick jumps = %d, want 2 (%+v)", len(roots), roots)
	}
	if roots[0].name != locales.PickerRoot() || roots[0].path != proto.ThisPC || !roots[0].root || !roots[0].thisPC {
		t.Errorf("root jump = %+v", roots[0])
	}
	if roots[1].name != locales.PickerHome() || roots[1].path != home || roots[1].root || roots[1].thisPC {
		t.Errorf("home jump = %+v", roots[1])
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

// A drive path typed in the picker of a Windows host opens its folder as the
// project, nested folders and all, on that host.
func TestPickerDrivePathOpensProjectOnWindowsHost(t *testing.T) {
	a := testApp(t)
	const dir = `D:\A\B\C`
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
			return proto.Project{ID: "typed", Dir: p.Dir}, nil
		case "create":
			return proto.Session{ID: "typed-session", Project: "typed"}, nil
		default:
			return nil, errors.New("unexpected method " + method)
		}
	})
	defer stop()
	w := &host{name: "w", info: proto.HostInfo{OS: "windows", Home: `C:\Users\me`}, cl: cl, conn: connReady}
	a.hosts = append(a.hosts, w)
	a.picker = folderPicker{open: true, host: w}
	tt := ui.NewTester(a.view, 1100, 900)
	tt.Frame()
	tt.Type(dir)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	tt.Frame()
}
