package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/internal/remote"
	"kero/internal/settings"

	"kero/locales"
)

// withHost adds a remote host to a test app, connected, with a session of
// its own.
func withHost(t *testing.T, a *App, name string) *host {
	t.Helper()
	addr, _ := remote.ParseAddr("me@" + name)
	h := &host{name: name, cfg: settings.Host{Name: name, Addr: addr.String()}, addr: addr, conn: connReady,
		info: proto.HostInfo{Hostname: name, OS: "linux", Arch: "amd64", Home: "/home/me", Kero: "2.1.0", Server: true}}
	a.hosts = append(a.hosts, h)
	a.set.Hosts = append(a.set.Hosts, h.cfg)
	a.applied = a.set.Clone()
	h.loaded = true
	// As setState takes it, but for the Dock's badge, which needs the app running.
	a.mergeState(h, proto.State{
		Projects: []proto.Project{{ID: "rp-" + name, Name: "api", Dir: "/home/me/api"}},
		Sessions: []proto.Session{{
			ID: "rs-" + name, Project: "rp-" + name, Name: "agent on " + name,
			Pane:  proto.Pane{ID: "rpa-" + name, Dir: "/home/me/api", Status: proto.Waiting, Progress: -1},
			Panes: []proto.Pane{{ID: "rpa-" + name, Dir: "/home/me/api", Status: proto.Waiting, Progress: -1}},
		}},
	})
	return h
}

// The mark before a host's name tells how the app reaches it: this
// machine's own, an SSH host, a WSL distribution, or a host over HTTP(S).
// A host's mark is its operating system's logo, the computer's when the
// host has not told its OS yet; a linux host's is its distribution's,
// when it told one, as the user asked Debian and Ubuntu to read apart.
func TestHostOriginIcon(t *testing.T) {
	a := testApp(t)
	want := osIcons[runtime.GOOS]
	if runtime.GOOS == "linux" {
		if ic, ok := distroIcons[remote.DistroID()]; ok {
			want = ic
		}
	}
	if got := a.hostOriginIcon(a.local()); got != want {
		t.Errorf("this machine's mark is not its own: got %v want %v", got, want)
	}
	for _, scheme := range []string{"ssh", "wsl", "http", "https"} {
		addr, err := remote.ParseAddr(scheme + "://build")
		if err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		h := &host{name: "build", addr: addr}
		if got := a.hostOriginIcon(h); got != hostIcon {
			t.Errorf("%s: a host that has not answered keeps the computer's mark", scheme)
		}
	}
	// A host's kerod tells its OS, which the mark shows.
	h := &host{name: "build", info: proto.HostInfo{OS: "linux"}}
	if got := a.hostOriginIcon(h); got != osIcons["linux"] {
		t.Errorf("a linux host's mark is not linux's")
	}
	// A linux host that tells a distribution shows it, as the user asked.
	for _, distro := range []string{"debian", "ubuntu", "arch", "fedora"} {
		ic, ok := distroIcons[distro]
		if !ok {
			t.Fatalf("no mark for %s", distro)
		}
		h := &host{name: "build", info: proto.HostInfo{OS: "linux", Distro: distro}}
		if got := a.hostOriginIcon(h); got != ic {
			t.Errorf("a %s host's mark is not %s's", distro, distro)
		}
	}
	// A linux host whose distro we have no mark for keeps the linux mark.
	h = &host{name: "build", info: proto.HostInfo{OS: "linux", Distro: "gentoo"}}
	if got := a.hostOriginIcon(h); got != osIcons["linux"] {
		t.Errorf("a linux host of an unknown distro keeps linux's mark")
	}
	// A non-linux host ignores its distro.
	h = &host{name: "build", info: proto.HostInfo{OS: "darwin", Distro: "debian"}}
	if got := a.hostOriginIcon(h); got != osIcons["darwin"] {
		t.Errorf("a darwin host's mark is not darwin's")
	}
}

// Each host's state replaces its own part of what the app shows, in the
// hosts' order, and leaves the others', with what the app changed in them
// before their kerods' states come.
func TestHostsMergeState(t *testing.T) {
	a := testApp(t)
	local := a.local()
	a.mergeState(local, a.st)
	build := withHost(t, a, "build")
	if got := len(a.st.Sessions); got != 2 {
		t.Fatalf("%d sessions", got)
	}
	if a.st.Sessions[0].ID != "s1" || a.st.Sessions[1].ID != "rs-build" {
		t.Errorf("order %s, %s", a.st.Sessions[0].ID, a.st.Sessions[1].ID)
	}
	for id, want := range map[string]*host{"s1": local, "a": local, "p1": local, "rs-build": build, "rpa-build": build, "rp-build": build, "unknown": local} {
		if a.hostOf(id) != want {
			t.Errorf("hostOf(%s) = %q", id, a.hostOf(id).name)
		}
	}
	// A rename the app shows at once stays as this machine's state comes.
	a.session("rs-build").Name = "renamed"
	a.mergeState(local, proto.State{Projects: a.projectsOf(local), Sessions: a.sessionsOf(local)})
	if s := a.session("rs-build"); s == nil || s.Name != "renamed" {
		t.Errorf("the remote session after a local state: %+v", s)
	}
	// A session the app made on a host shows before the host says so.
	a.addPending(build, proto.Session{ID: "new", Project: "rp-build", Panes: []proto.Pane{{ID: "np"}}})
	a.mergeState(local, proto.State{Projects: a.projectsOf(local), Sessions: a.sessionsOf(local)})
	if a.session("new") == nil || a.hostOf("np") != build {
		t.Error("a pending session of a host went with this machine's state")
	}
	// A host let go of takes its sessions along.
	a.closeHost(build)
	if a.session("rs-build") != nil || a.session("s1") == nil {
		t.Errorf("after closing the host: %d sessions", len(a.st.Sessions))
	}
}

// The sidebar shows this machine's projects, unlabeled, then each host's
// under its name; a session of a host keeps its host's name in the
// window's title and its notifications.
func TestSidebarHosts(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	withHost(t, a, "build")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"demo", "build", "api", "agent on build"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in the sidebar: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "sidebar-hosts")
	ids := a.visibleOrder()
	if len(ids) != 2 || ids[0] != "s1" || ids[1] != "rs-build" {
		t.Errorf("order %q", ids)
	}
	s := a.session("rs-build")
	if got := a.hostedName(s); got != "agent on build · build" {
		t.Errorf("hosted name %q", got)
	}
	if got := a.hostedName(a.session("s1")); got != "demo" {
		t.Errorf("this machine's session is named %q", got)
	}
	// A remote group folds as this machine's do.
	a.toggleProject("rp-build")
	if ids := a.visibleOrder(); len(ids) != 1 {
		t.Errorf("folded: %q", ids)
	}
}

// The sidebar is flat: a remote group carries its host's name, and the
// host has a row of its own while it wants the user — a host it can't
// reach, or one it reaches but that holds nothing, whose header is the
// only way to start a session there.
func TestSidebarFlat(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	empty := withHost(t, a, "empty")
	a.mergeState(empty, proto.State{})
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	shot(t, tt, "sidebar-flat")
	// build is named by its header, then its group's name: no tag.
	if n := countText(tt.Texts(), "build"); n != 1 {
		t.Errorf("build shows %d times: %q", n, tt.Texts())
	}
	hdr, ok := tt.Find("build")
	api, ok2 := tt.Find("api")
	if !ok || !ok2 || hdr.Y >= api.Y {
		t.Errorf("build's header %+v is not above its group's name %+v", hdr, api)
	}
	// A host the app reaches keeps its header with nothing under it, so
	// its + is within reach. One still connecting shows nowhere.
	if !tt.HasText("empty") {
		t.Errorf("a reached host without sessions has no header: %q", tt.Texts())
	}
	empty.conn, empty.loaded = connConnecting, false
	tt.Frame()
	if tt.HasText("empty") {
		t.Errorf("a host still connecting shows: %q", tt.Texts())
	}
	if a.hostNeedsRow(build) || a.hostNeedsRow(empty) {
		t.Error("rows for hosts that want nothing")
	}
	// Lost, its groups stay, its header faded; failing, it has a note.
	build.conn = connConnecting
	if a.hostNeedsRow(build) {
		t.Error("a host reconnecting has a row")
	}
	build.conn, build.err = connFailed, "Connection refused"
	if !a.hostNeedsRow(build) {
		t.Error("a host the app can't reach has no row")
	}
	tt.Frame()
	if !tt.HasText("Connection refused") || !tt.HasText("agent on build") {
		t.Errorf("failing: %q", tt.Texts())
	}
}

// A remote host heads its projects, folds them on a click of its header,
// and HideEmptyHosts leaves one with no sessions out.
func TestSidebarHostGroups(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	// build's header names it, and its project "api" sits under it.
	if !tt.HasText("build") || !tt.HasText("api") {
		t.Fatalf("no host header or project: %q", tt.Texts())
	}
	if got := a.visibleOrder(); !slices.Contains(got, "rs-build") {
		t.Fatalf("build's session is not in the order: %q", got)
	}
	// Folding the host hides its projects; the shown session stays.
	a.toggleHost(build)
	if got := a.visibleOrder(); slices.Contains(got, "rs-build") {
		t.Errorf("folded host still shows: %q", got)
	}
	a.selected = "rs-build"
	if got := a.visibleOrder(); !slices.Contains(got, "rs-build") {
		t.Errorf("folded host does not keep the shown session: %q", got)
	}
	a.toggleHost(build)
	if got := a.visibleOrder(); !slices.Contains(got, "rs-build") {
		t.Errorf("unfolded host: %q", got)
	}
	// HideEmptyHosts leaves the host with no sessions out.
	a.set.HideEmptyHosts = true
	a.st.Sessions = nil
	a.mergeState(build, proto.State{Projects: a.projectsOf(build)})
	tt.Frame()
	if tt.HasText("build") {
		t.Errorf("hide-empty-hosts still shows an empty host: %q", tt.Texts())
	}
	// ... but a host the app can't reach still shows, to say so.
	build.conn, build.err = connFailed, "Connection refused"
	tt.Frame()
	if !tt.HasText("build") || !tt.HasText("Connection refused") {
		t.Errorf("a failing host is hidden: %q", tt.Texts())
	}
}

// The footer's picker lists the hosts, checks the ones the sidebar shows,
// and hiding one leaves it out; the host shown stays listed.
func TestSidebarDeviceFilter(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	gpu := withHost(t, a, "gpu")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	// All hosts show, and the picker names them all.
	if !tt.HasText("build") || !tt.HasText("gpu") {
		t.Fatalf("hosts not listed: %q", tt.Texts())
	}
	if !tt.HasText(locales.SidebarDeviceAll()) {
		t.Errorf("picker does not say All Hosts: %q", tt.Texts())
	}
	// The picker's list checks the hosts shown.
	if err := tt.Click(locales.SidebarDeviceAll()); err != nil {
		t.Fatalf("no picker: %v", err)
	}
	menu := tt.Menu()
	if !slices.Contains(menu, locales.HostThisComputer()) || !slices.Contains(menu, "build") || !slices.Contains(menu, "gpu") {
		t.Errorf("picker lists %q", menu)
	}
	// The picker is also where a host is added: the palette, and the
	// Settings Hosts page behind it, is the quick way to.
	if !slices.Contains(menu, locales.SetHostAdd()) {
		t.Errorf("picker has no add-host: %q", menu)
	}
	tt.CloseMenu()
	// Hiding gpu leaves it out; build stays.
	a.toggleDevice(gpu)
	tt.Frame()
	if !a.deviceShown(build) || a.deviceShown(gpu) {
		t.Errorf("filter: build %v gpu %v", a.deviceShown(build), a.deviceShown(gpu))
	}
	if tt.HasText("gpu") {
		t.Errorf("gpu still listed: %q", tt.Texts())
	}
	if got := a.deviceFilterLabel(); got != locales.SidebarDevicePicked(2) {
		t.Errorf("picker label = %q", got)
	}
	// The host of the session shown stays listed, even when hidden.
	a.selected = "rs-gpu"
	tt.Frame()
	if !tt.HasText("gpu") {
		t.Errorf("shown session's host is hidden: %q", tt.Texts())
	}
	// Showing every host again is All.
	a.toggleDevice(gpu)
	if !a.deviceAll() {
		t.Errorf("nothing hidden is not All: %q", a.prefs.DeviceFilter)
	}
	tt.Frame()
	if !tt.HasText("gpu") {
		t.Errorf("gpu not back: %q", tt.Texts())
	}
}

// Every host's row in the picker has quick actions to restart it and to
// re-download its Kero. They act on that host without choosing the row,
// which would hide the device from the sidebar.
func TestSidebarDevicePickerActions(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	gpu := withHost(t, a, "gpu")
	build.rc, gpu.rc = &remote.Conn{}, &remote.Conn{}
	gpu.info.Updates = true // this one can be updated from the picker
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click(locales.SidebarDeviceAll()); err != nil {
		t.Fatalf("no picker: %v", err)
	}
	// The actions are on this machine's row too: its Kero is this app.
	for _, h := range a.hosts {
		name := a.hostName(h)
		for _, tip := range []string{locales.DevicePickerRestartTip(name), locales.DevicePickerRedownloadTip(name)} {
			if _, ok := tt.Find(tip); !ok {
				t.Errorf("no %q by %s: %q", tip, name, tt.Texts())
			}
		}
	}
	shown := make(map[*host]bool, len(a.hosts))
	for _, h := range a.hosts {
		shown[h] = a.deviceShown(h)
	}
	checkNoToggle := func(what string) {
		t.Helper()
		for h, want := range shown {
			if got := a.deviceShown(h); got != want {
				t.Errorf("%s hid or showed %s: got %v want %v", what, h.name, got, want)
			}
		}
	}
	// The confirmation covers the picker, so answer it before the next
	// action, and open the picker again if the focus change closed it.
	dismiss := func() {
		t.Helper()
		if a.confirm == nil {
			return
		}
		if err := tt.Click(locales.CommonCancel()); err != nil {
			t.Fatalf("no cancel: %v", err)
		}
		tt.Frame()
	}
	pick := func() {
		t.Helper()
		tt.Frame()
		if len(tt.Menu()) > 0 {
			return
		}
		if err := tt.Click(locales.SidebarDeviceAll()); err != nil {
			t.Fatalf("no picker: %v", err)
		}
	}
	act := func(tip, title string, devToast bool) {
		t.Helper()
		dismiss()
		pick()
		if err := tt.Click(tip); err != nil {
			t.Fatalf("no %s: %v", tip, err)
		}
		if a.confirm == nil {
			// A development build tells the user to update from the
			// release instead of asking to install one.
			if devToast && !released(mygo.App.Version()) && tt.HasText(locales.HostUpdateDev()) {
				checkNoToggle(tip)
				return
			}
			t.Fatalf("%s did nothing (toast %q)", tip, a.pendingToast)
		}
		if a.confirm.title != title {
			t.Errorf("%s asked about %q, want %q", tip, a.confirm.title, title)
		}
		checkNoToggle(tip)
	}
	// Restarting a reachable host asks first, on that host, and does not
	// choose the row it sits in.
	act(locales.DevicePickerRestartTip("build"), locales.HostRestartTitle("build"), false)
	// A host that has no Kero yet is offered an install instead.
	gpu.errKind = remote.ErrNotInstalled
	act(locales.DevicePickerRedownloadTip("gpu"), locales.HostInstallTitle("gpu"), false)
	// Re-downloading a host that runs Kero asks about updating it. On a
	// development build there is no release to offer, so it says so.
	gpu.errKind = nil
	act(locales.DevicePickerRedownloadTip("gpu"), locales.HostUpdateTitle("gpu", mygo.App.Version()), true)
	// A host the app cannot reach has nothing to restart or download.
	build.rc = nil
	dismiss()
	pick()
	if _, ok := tt.Find(locales.DevicePickerRestartTip("build")); !ok {
		t.Fatal("unreachable host's row disappeared")
	}
	tt.Click(locales.DevicePickerRestartTip("build")) // still on the row, but inert
	if a.confirm != nil {
		t.Error("disabled restart asked anyway")
	}
}

// A host in the picker can be edited and removed in place: the row carries
// quick actions for both, and a right click opens the same menu its header
// in the sidebar does.
func TestDevicePickerEditAndRemove(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	build.rc = &remote.Conn{}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click(locales.SidebarDeviceAll()); err != nil {
		t.Fatalf("no picker: %v", err)
	}
	// The row offers edit and remove, on top of restart and re-download.
	for _, want := range []string{locales.HostEdit(), locales.HostRemove()} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("picker has no %q for build: %q", want, tt.Texts())
		}
	}
	// Editing puts the host's row in edit mode on the Hosts page, the same
	// form Settings uses.
	a.startHostEdit(build)
	if a.ss.page != "hosts" || a.ss.hostEdit.editing != "build" || a.ss.hostEdit.addr != build.cfg.Addr {
		t.Fatalf("no edit of build: page %q, draft %+v", a.ss.page, a.ss.hostEdit)
	}
	et := ui.NewTester(a.settingsView, 820, 640)
	et.Frame()
	if !et.HasText(locales.CommonSave()) || !et.HasText(locales.CommonCancel()) {
		t.Errorf("the editor never showed: %q", et.Texts())
	}
	a.ss.hostEdit = hostDraft{}

	// Removing asks first, about that host, and without hiding it: the
	// question covers the picker.
	tt.Frame()
	if err := tt.Click(locales.HostRemove()); err != nil {
		t.Fatalf("no remove on the row: %v", err)
	}
	if a.confirm == nil || a.confirm.title != locales.HostRemoveTitle("build") {
		t.Fatalf("remove asked nothing: %+v", a.confirm)
	}
	if !a.deviceShown(build) {
		t.Error("removing hid the host before the answer")
	}
}

// The host header's right-click menu offers editing directly, beside the
// Settings page it already opened.
func TestHostHeaderMenuEdits(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	withHost(t, a, "build")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.RightClick("build"); err != nil {
		t.Fatalf("no header menu: %v", err)
	}
	menu := tt.Menu()
	if !slices.Contains(menu, locales.HostEdit()) {
		t.Errorf("header menu has no edit: %q", menu)
	}
	if !slices.Contains(menu, locales.HostRemove()) {
		t.Errorf("header menu has no remove: %q", menu)
	}
	tt.CloseMenu()
}

// The plus at a host's header opens the picker of its folders, which
// opens a project there; it does not start a session at once.
func TestHostHeaderPlusOpensPicker(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	build := withHost(t, a, "build")
	build.cl, _ = projectClient(t, func(method string, _ json.RawMessage) (any, error) {
		return nil, errors.New("unexpected method " + method)
	})
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	// The plus shows on hovering the header; hovering reveals it.
	hdr, ok := tt.Find("build")
	if !ok {
		t.Fatalf("no build header; texts %q", tt.Texts())
	}
	tt.Move(hdr.X+hdr.W/2, hdr.Y+hdr.H/2)
	tt.Frame()
	if err := tt.Click(locales.ActionOpenProject()); err != nil {
		t.Fatalf("no plus at the header: %v", err)
	}
	if !a.picker.open || a.picker.host != build {
		t.Fatalf("plus did not open the picker on %q: %+v", build.name, a.picker)
	}
}

// With hosts, Open Project lists any machine's folders, this one's too,
// with a switch between them.
func TestPickerHosts(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	withHost(t, a, "build")
	a.picker = folderPicker{open: true, host: a.local(), list: &proto.FSList{Dir: "/home/me", Entries: []proto.FSEntry{{Name: "api", Dir: true}}}}
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	shot(t, tt, "picker-hosts")
	for _, h := range a.hosts {
		if _, ok := tt.Find(locales.PickerTitle(h.label())); !ok {
			t.Errorf("no switch to %s: %q", h.label(), tt.Texts())
		}
	}
}

// A host the app can't reach says why, and offers what fixes it: Kero
// installed there, for one that has none.
func TestSidebarHostProblems(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	h := withHost(t, a, "build")
	h.conn, h.err, h.errKind = connFailed, "kero is not installed on build", remote.ErrNotInstalled
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if !tt.HasText("Kero isn't installed on build") || !tt.HasText("Install Kero…") {
		t.Errorf("no word of the missing Kero: %q", tt.Texts())
	}
	h.errKind = remote.ErrAuth
	h.err = "Permission denied (publickey)."
	tt.Frame()
	if !tt.HasText("build refused the connection") {
		t.Errorf("no word of the refusal: %q", tt.Texts())
	}
	shot(t, tt, "sidebar-host-refused")
	// Its sessions show as they were, faded, and their panes say why.
	a.selectSession("rs-build")
	tt.Frame()
	if !tt.HasText("Permission denied (publickey).") {
		t.Errorf("the pane of a lost host: %q", tt.Texts())
	}
	h.conn, h.errKind = connVersion, nil
	h.daemonApp = "1.0.0"
	tt.Frame()
	if !tt.HasText("Kero on build is another version") {
		t.Errorf("no word of the version: %q", tt.Texts())
	}
}

// Programs of a host drive the app only as the host's Remote Control
// lets them, whatever this machine's.
func TestRemoteControlPerHost(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	h := withHost(t, a, "build")
	a.set.RemoteControl = true
	if a.remoteControl(h) {
		t.Error("a host drives the app unasked")
	}
	if !a.remoteControl(a.local()) {
		t.Error("this machine's remote control is off")
	}
	h.cfg.RemoteControl = true
	if !a.remoteControl(h) {
		t.Error("a host the user trusts does not drive the app")
	}
}

func TestHostShortDir(t *testing.T) {
	h := &host{name: "b", info: proto.HostInfo{Home: "/home/me"}}
	for in, want := range map[string]string{"/home/me": "~", "/home/me/api": "~/api", "/home/meg": "/home/meg", "/srv": "/srv"} {
		if got := h.shortDir(in); got != want {
			t.Errorf("shortDir(%q) = %q", in, got)
		}
	}
	// A host of Windows: backslashes, and a drive's letter of either case.
	w := &host{name: "w", info: proto.HostInfo{OS: "windows", Home: `C:\Users\me`}}
	for in, want := range map[string]string{`C:\Users\me`: "~", `c:\users\me\api`: `~\api`, `C:\Users\meg`: `C:\Users\meg`, `D:\src`: `D:\src`} {
		if got := w.shortDir(in); got != want {
			t.Errorf("Windows: shortDir(%q) = %q", in, got)
		}
	}
	if got := w.joinPath(`C:\`, "src"); got != `C:\src` {
		t.Errorf("joinPath = %q", got)
	}
	// Quoted for PowerShell, the shell of Windows' panes, in which nothing
	// of a file's name runs.
	for in, want := range map[string]string{
		`C:\Temp\kero-paste\x\pasted-image.png`: `C:\Temp\kero-paste\x\pasted-image.png`,
		`C:\Temp\a b.png`:                       `'C:\Temp\a b.png'`,
		`C:\Temp\$(calc).png`:                   `'C:\Temp\$(calc).png'`,
		"C:\\Temp\\it\u2019s.png":               "'C:\\Temp\\it\u2019\u2019s.png'",
	} {
		if got := pastedPath(w.windows(), in); got != want {
			t.Errorf("pastedPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// A Windows host's file opens in its editor by name, whatever the name.
func TestEditorCommandWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("runs PowerShell")
	}
	w := &host{name: "w", info: proto.HostInfo{OS: "windows", Home: `C:\Users\me`}}
	for _, name := range []string{`C:\a b\x.txt`, "C:\\x\u2019+(Write-Output PWNED)+\u2019y.txt", `C:\$(Write-Output PWNED)'.txt`} {
		args := editorCommand(w, name)
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "VISUAL=", "EDITOR=[Console]::OutputEncoding = [Text.Encoding]::UTF8; Write-Output")
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != name {
			t.Errorf("%q: the editor got %q, %v", name, out, err)
		}
	}
}

func TestShellEscape(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/kero-1000/paste/x/pasted-image.png": "/tmp/kero-1000/paste/x/pasted-image.png",
		"/tmp/a b/it's (1).png":                   `/tmp/a\ b/it\'s\ \(1\).png`,
		"/tmp/$HOME;rm":                           `/tmp/\$HOME\;rm`,
	} {
		if got := shellEscape(in); got != want {
			t.Errorf("shellEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickerItems(t *testing.T) {
	a := testApp(t)
	linux := &host{name: "b", info: proto.HostInfo{OS: "linux", Home: "/home/me"}}
	a.picker = folderPicker{open: true, host: linux, list: &proto.FSList{Dir: "/home/me", Parent: "/home", Entries: []proto.FSEntry{
		{Name: "api", Dir: true, Repo: true}, {Name: "notes.txt"}, {Name: "web", Dir: true},
	}}}
	var got []string
	for _, it := range a.pickerItems() {
		got = append(got, it.name)
	}
	if strings.Join(got, ",") != "..,api,web" {
		t.Errorf("items %q", got)
	}
	a.picker.query = "wb"
	if items := a.pickerItems(); len(items) != 1 || items[0].path != "/home/me/web" {
		t.Errorf("filtered %+v", items)
	}
	for q, want := range map[string]bool{"~/x": true, "/srv": true, "~": true, "src": false, "~x": false} {
		if isPathQuery(q, false) != want {
			t.Errorf("isPathQuery(%q)", q)
		}
	}
	// A drive path names a folder on any client, so it is a path whether the
	// host runs Windows or not; the rest of the Windows forms stay Windows-only.
	for q, want := range map[string]bool{`C:\src`: true, `D:\A\B\C`: true, `D:/A/B/C`: true, "d:": false, `d:/x`: true, "src": false, "1:": false} {
		if isPathQuery(q, false) != want {
			t.Errorf("isPathQuery(%q)", q)
		}
	}
	for q, want := range map[string]bool{`C:\src`: true, `D:\A\B\C`: true, "d:": false, `d:/x`: true, `~\x`: true, `\\nas\share`: true, "src": false, "1:": false} {
		if isPathQuery(q, true) != want {
			t.Errorf("Windows: isPathQuery(%q)", q)
		}
	}
	_ = fmt.Sprint()
}

// A split in a remote session is the host's at once, as its terminal
// attaches there before the host's state says so.
func TestRemoteSplitIsTheHosts(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	h := withHost(t, a, "build")
	s := *a.session("rs-build")
	s.Panes = append(s.Panes, proto.Pane{ID: "split-pane"})
	a.replaceSession(h, s)
	if a.hostOf("split-pane") != h {
		t.Error("the new pane of a remote split is this machine's")
	}
}

// A host's program drives the app's actions only on that host's
// sessions, never on this machine's.
func TestRemoteActionsStayOnTheirHost(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	h := withHost(t, a, "build")
	a.selected = "s1" // this machine's
	a.from = h
	defer func() { a.from = nil }()
	for _, id := range []string{"pane.restart", "pane.split.right", "pane.zoom", "changes.toggle"} {
		if a.fromMay(id, nil) {
			t.Errorf("%s from build ran on this machine's session", id)
		}
	}
	if a.fromMay("session.new", map[string]string{"project": "p1"}) {
		t.Error("build started a session in this machine's project")
	}
	if !a.fromMay("session.new", nil) || !a.fromMay("sidebar.toggle", nil) {
		t.Error("an action of build's own refused")
	}
	a.selected = "rs-build"
	if !a.fromMay("pane.split.right", nil) {
		t.Error("build may not split its own session")
	}
	a.from = a.local()
	a.selected = "rs-build"
	if !a.fromMay("pane.split.right", nil) {
		t.Error("this machine's kero do refused")
	}
}

// A host newer than the app gets no Update, which would take it back.
func TestHostVersions(t *testing.T) {
	h := &host{name: "b", info: proto.HostInfo{Server: true, Updates: true, Kero: "9.0.0"}}
	if h.behind() {
		t.Error("a newer host is behind")
	}
	h.info.Kero = "0.1.0"
	h.daemonApp = "0.1.0"
	if h.stale() {
		t.Error("kerod of the bridge's version is stale")
	}
	h.conn, h.daemonApp, h.info.Kero = connReady, "0.1.0", "0.2.0"
	if !h.stale() {
		t.Error("kerod older than the Kero installed is not stale")
	}
	h.daemonApp = "0.3.0"
	if h.stale() {
		t.Error("kerod newer than the bridge that answered is stale")
	}
}

// A host of the desktop app that takes updates from here is told of as one
// to update; one that doesn't, as one to update there.
func TestDesktopHostVersionDetail(t *testing.T) {
	a := testApp(t)
	h := &host{name: "pc", info: proto.HostInfo{Kero: "0.1.0"}}
	if got := a.hostVersionDetail(h); got != locales.HostDesktopDetail("pc", "0.1.0", mygo.App.Version()) {
		t.Errorf("without remote updates: %q", got)
	}
	h.info.Updates = true
	if got := a.hostVersionDetail(h); got != locales.HostVersionDetail("pc", "0.1.0", mygo.App.Version()) {
		t.Errorf("with remote updates: %q", got)
	}
}

func TestPickerChosen(t *testing.T) {
	a := testApp(t)
	linux := &host{name: "b", info: proto.HostInfo{OS: "linux", Home: "/home/me"}}
	a.picker = folderPicker{open: true, host: linux, list: &proto.FSList{Dir: "/home/me", Parent: "/home", Entries: []proto.FSEntry{{Name: "api", Dir: true}}}}
	items := a.pickerItems()
	if got := a.pickerChosen(items); got != "/home/me" {
		t.Errorf("with the parent chosen, Open opens %q", got)
	}
	a.picker.index = 1
	if got := a.pickerChosen(items); got != "/home/me/api" {
		t.Errorf("with api chosen, Open opens %q", got)
	}
}

func countText(texts []string, s string) int {
	n := 0
	for _, t := range texts {
		if t == s {
			n++
		}
	}
	return n
}

// A host's color is its own, else its name's, which stays.
func TestHostColor(t *testing.T) {
	h := &host{name: "devbox"}
	if hostColorName(h) != hostColorName(&host{name: "DevBox"}) {
		t.Error("the color of a name changes with its case")
	}
	h.cfg.Color = "teal"
	if hostColorName(h) != "teal" {
		t.Error("the configured color is not taken")
	}
	if lightPalette.hostColor(h) == darkPalette.hostColor(h) {
		t.Error("one color for light and dark")
	}
	if lightPalette.hostColor(&host{}) != lightPalette.textMuted {
		t.Error("this machine is colored")
	}
}

// A host's color, picked where the host is, is saved: the settings are
// saved as they are applied, and the color was only in the app's memory.
func TestHostColorIsSaved(t *testing.T) {
	a := testApp(t)
	withHost(t, a, "build")
	a.setHostColor(a.hostNamed("build"), "teal")
	saved := settings.Load()
	h := saved.Host("build")
	if h == nil || h.Color != "teal" {
		t.Fatalf("the saved host is %+v, want the color teal", h)
	}
	if a.set.Host("build").Color != "teal" {
		t.Errorf("the app's host is %+v, want the color teal", a.set.Host("build"))
	}
}

// Dark, a remote group's tag and a remote pane's header in its host's
// color ($KERO_SHOTS).
func TestSidebarFlatDarkShot(t *testing.T) {
	a := testApp(t)
	a.mergeState(a.local(), a.st)
	withHost(t, a, "build").cfg.Color = "teal"
	withHost(t, a, "gpu")
	a.selectSession("rs-build")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.SetDark(true)
	tt.Frame()
	tt.Advance(time.Second) // colors ease from the light frame's
	tt.Frame()
	shot(t, tt, "sidebar-flat-dark")
}

// Removing a host from Settings asks there, not in the main window, and
// removes it once the user says so.
func TestRemoveHostAsksInSettings(t *testing.T) {
	a := testApp(t)
	withHost(t, a, "build")
	a.ss.page = "hosts"
	tt := ui.NewTester(a.settingsView, 820, 640)
	tt.Frame()
	if err := tt.Click(locales.HostRemove()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.confirm != nil || a.ss.confirm == nil {
		t.Fatalf("the question went to the main window: %+v, settings %+v", a.confirm, a.ss.confirm)
	}
	if !tt.HasText(locales.HostRemoveTitle("build")) {
		t.Fatalf("Settings shows no question; texts %q", tt.Texts())
	}
	shot(t, tt, "settings-remove-host")
	if err := tt.Click(locales.CommonRemove()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.ss.confirm != nil || len(a.set.Hosts) != 0 {
		t.Errorf("not removed: question %+v, hosts %+v", a.ss.confirm, a.set.Hosts)
	}
}

// Editing a host opens the shared dialog filled with it. Saving renames in
// place (connection and sessions kept); a new address reconnects; a name or
// address already taken is refused.
func TestEditHost(t *testing.T) {
	a := testApp(t)
	build := withHost(t, a, "build")
	withHost(t, a, "gpu")
	a.ss.page = "hosts"
	tt := ui.NewTester(a.settingsView, 820, 640)
	tt.Frame()
	if err := tt.Click(locales.HostEdit()); err != nil {
		t.Fatal(err)
	}
	// The edit dialog opens in the Settings window, where the card is.
	tt.Frame()
	if a.hostAdd.editing != "build" {
		t.Fatalf("edit dialog not filled: %+v", a.hostAdd)
	}
	if !a.hostAdd.settings || !tt.HasText(locales.HostEditTitle()) || !tt.HasText(locales.HostTestConnection()) {
		t.Fatalf("no edit form in Settings: %q", tt.Texts())
	}

	// A name already taken is refused; the dialog stays open.
	d := &a.hostAdd
	d.name = "gpu"
	if a.hostAddSubmit(d) {
		t.Fatal("a taken name saved")
	}
	if d.err != locales.SetHostNameTaken("gpu") {
		t.Fatalf("a taken name said %q", d.err)
	}

	// An address another host uses is refused too, even spelled with its
	// default port. The name check runs first, so use a fresh name.
	d.name, d.err = "builder", ""
	d.addr, d.port = "gpu", "22"
	if a.hostAddSubmit(d) {
		t.Fatal("a taken address saved")
	}
	if d.err != locales.SetHostAddrTaken("gpu") {
		t.Fatalf("a taken address said %q", d.err)
	}

	// Rename at the original address: the connection and sessions stay.
	d.err = ""
	d.addr, d.port = "build", ""
	if !a.hostAddSubmit(d) {
		t.Fatalf("rename failed: %s", d.err)
	}
	a.syncHosts()
	if h := a.hostNamed("builder"); h != build || a.set.Host("build") != nil || a.hostOf("rs-build") != build {
		t.Fatalf("renamed to %p, not %p; hosts %+v", h, build, a.set.Hosts)
	}
	if d.editing != "" || d.open {
		t.Errorf("dialog stayed in edit mode: %+v", d)
	}

	// A new address connects again.
	a.openHostEdit(build)
	d = &a.hostAdd
	d.user, d.addr = "", "builder.lan"
	if !a.hostAddSubmit(d) {
		t.Fatalf("address change failed: %s", d.err)
	}
	a.syncHosts()
	if h := a.hostNamed("builder"); h == nil || h == build || h.addr.String() != "builder.lan" {
		t.Fatalf("a new address kept the connection: %+v", h)
	}
}
