package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"kero/internal/remote"
	"kero/locales"
)

// The quick Add a Host is one dialog for every kind of machine. Its tests
// read like the form does: what an address becomes, what the finder's
// arrows and Tab do to the field, and where the focus lands once a host is
// saved.

// addDraftFor puts a dialog in hand, already showing the candidates a scan
// would have found, so no test waits on a scan and none reads the real
// ~/.ssh/config.
func addDraftFor(d *hostAddDraft, kind hostKind, cands ...string) {
	d.typed = map[hostKind]hostAddFields{}
	d.shown = map[hostKind][]string{kind: cands}
	d.looking = map[hostKind]bool{}
	d.kind = kind
	d.kindSel = hostKindID(kind)
	d.open = true
	d.fieldsOf(kind)
	d.cands = slices.Clone(cands)
	d.sshAt, d.wslAt = time.Now(), time.Now()
}

// addTerminals gives the test app's panes real terminals, so their handles
// are bound at all and a focus can be asked about.
func addTerminals(t *testing.T, a *App) {
	t.Helper()
	for _, id := range []string{"a", "b"} {
		term, err := terminal.New(terminal.Options{Conn: idleConn{make(chan struct{})}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { term.Close() })
		a.terms[id] = &termEntry{term: term}
	}
}

func TestDeviceAddComposedAddress(t *testing.T) {
	for _, tc := range []struct {
		kind                   hostKind
		host, user, port, want string
	}{
		// A bare host is the whole address.
		{kindSSH, "devbox", "", "", "devbox"},
		// The parts make ssh's own destination, in its order.
		{kindSSH, "devbox", "me", "", "me@devbox"},
		{kindSSH, "devbox", "", "2222", "devbox:2222"},
		{kindSSH, "devbox", "me", "2222", "me@devbox:2222"},
		{kindSSH, " [::1] ", "", "2222", "[::1]:2222"},
		// An address typed whole is left as it was typed, its own scheme
		// included: remote.ParseAddr reads the scheme whatever its case.
		{kindSSH, "ssh://me@other:22", "me", "2222", "ssh://me@other:22"},
		{kindSSH, "HTTP://devbox:7373", "", "", "HTTP://devbox:7373"},
		// A distribution takes the scheme its kind stands for.
		{kindWSL, "Ubuntu", "", "", "wsl://Ubuntu"},
		{kindWSL, "wsl://Debian", "", "", "wsl://Debian"},
		{kindWSL, "WSL://Debian", "", "", "WSL://Debian"},
		{kindWSL, "", "", "", ""},
	} {
		if got := hostAddAddrOf(tc.kind, tc.host, tc.user, tc.port); got != tc.want {
			t.Errorf("hostAddAddrOf(%d, %q, %q, %q) = %q, want %q",
				tc.kind, tc.host, tc.user, tc.port, got, tc.want)
		}
	}
}

func TestDeviceAddSubmit(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH)

	// Nothing typed: the address is what is missing, and the dialog stays.
	if a.hostAddSubmit(d) {
		t.Fatal("an empty address added a host")
	}
	if d.err != locales.SetHostNeedsAddress() {
		t.Errorf("empty address said %q", d.err)
	}
	if !d.open {
		t.Error("a refused address closed the dialog")
	}

	// A bare SSH destination takes its name from the target as typed.
	d.addr, d.err = "me@devbox", ""
	if !a.hostAddSubmit(d) {
		t.Fatalf("submit failed: %s", d.err)
	}
	h := a.set.Host("devbox")
	if h == nil {
		t.Fatalf("hosts = %v, want devbox", a.set.Hosts)
	}
	if h.Addr != "me@devbox" {
		t.Errorf("devbox reads as %q", h.Addr)
	}
	if d.open {
		t.Error("the dialog stayed open after a host was added")
	}

	// The parts make the address, and an empty name takes the host's.
	d = &a.hostAdd
	addDraftFor(d, kindSSH)
	d.addr, d.user, d.port = "gitcode.com", "me", "2222"
	if !a.hostAddSubmit(d) {
		t.Fatalf("parted submit failed: %s", d.err)
	}
	if g := a.set.Host("gitcode"); g == nil || g.Addr != "me@gitcode.com:2222" {
		t.Errorf("gitcode reads as %+v", g)
	}
	if d.addr != "" || d.user != "" || d.port != "" || d.name != "" {
		t.Errorf("the form kept %q, %q, %q, %q", d.addr, d.user, d.port, d.name)
	}

	// A distribution keeps its own scheme, and its name.
	d = &a.hostAdd
	addDraftFor(d, kindWSL, "Debian")
	d.addr = "Debian"
	if !a.hostAddSubmit(d) {
		t.Fatalf("wsl submit failed: %s", d.err)
	}
	if w := a.set.Host("Debian"); w == nil || w.Addr != "wsl://Debian" {
		t.Errorf("Debian reads as %+v", w)
	}

	// A name already taken is said, not overwritten.
	d = &a.hostAdd
	addDraftFor(d, kindSSH)
	d.addr, d.name = "other@devbox", "devbox"
	if a.hostAddSubmit(d) {
		t.Fatal("a taken name added a second host")
	}
	if want := locales.SetHostNameTaken("devbox"); d.err != want {
		t.Errorf("a taken name said %q, want %q", d.err, want)
	}
	if len(a.set.Hosts) != 3 {
		t.Errorf("a refused name changed the hosts: %v", a.set.Hosts)
	}

	// An address nothing can reach is the parser's to refuse.
	d.addr, d.name, d.err = "not a host", "", ""
	if a.hostAddSubmit(d) {
		t.Fatal("an address with a space added a host")
	}
	if d.err == "" {
		t.Error("the parser's refusal never reached the form")
	}
}

func TestDeviceAddArrowsAndTab(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox", "gitcode.com", "home-server")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	// Down picks the first row and writes it into the field.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if d.addr != "devbox" || d.hi != 0 || d.armed != "devbox" {
		t.Errorf("Down left %q, hi %d, armed %q", d.addr, d.hi, d.armed)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if d.addr != "gitcode.com" || d.hi != 1 {
		t.Errorf("a second Down left %q, hi %d", d.addr, d.hi)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if d.addr != "devbox" || d.hi != 0 {
		t.Errorf("Up went back to %q, hi %d", d.addr, d.hi)
	}

	// The field really holds it: the next keystroke lands after the text
	// an arrow brought, not inside it.
	tt.Type("-1")
	tt.Frame()
	if d.addr != "devbox-1" {
		t.Errorf("typing after a pick left %q", d.addr)
	}
	// And typing ends the match: the field is a query again.
	if d.armed != "" {
		t.Errorf("typing left armed %q", d.armed)
	}

	// A query that no candidate holds, and the block goes away.
	if got := hostAddShown(d); len(got) != 0 {
		t.Errorf("devbox-1 filtered to %v", got)
	}

	// Back to a prefix that names a row: the list thins to the ones
	// holding it, and Tab completes the rest.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("git")
	tt.Frame()
	if d.addr != "git" {
		t.Fatalf("select-all and typing left %q", d.addr)
	}
	if want := []string{"gitcode.com"}; !slices.Equal(hostAddShown(d), want) {
		t.Errorf("git filtered to %v, want %v", hostAddShown(d), want)
	}
	if d.armed != "" {
		t.Errorf("a query came armed %q", d.armed)
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if d.addr != "gitcode.com" {
		t.Fatalf("Tab completed to %q", d.addr)
	}

	// Enter now adds exactly what the field shows.
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	h := a.set.Host("gitcode")
	if h == nil || h.Addr != "gitcode.com" {
		t.Errorf("Enter added nothing: %v, err %q", a.set.Hosts, d.err)
	}
	if d.open {
		t.Error("the dialog stayed open after Enter")
	}
}

func TestDeviceAddCompletesTheShortestMatch(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox", "devbox-alt", "build")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Type("dev")
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if d.addr != "devbox" {
		t.Errorf("Tab chose %q, want the shortest: devbox", d.addr)
	}
	// Nothing left to complete: Tab leaves the field alone.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if d.addr != "devbox" {
		t.Errorf("a second Tab changed %q", d.addr)
	}
	// A query no candidate starts with completes to nothing.
	if got := hostAddComplete("devb", []string{"gitcode.com", "home-server"}); got != "" {
		t.Errorf("a stranger completed to %q", got)
	}
}

func TestDeviceAddKeepsEachKindForm(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	tt.Type("me@devbox")
	tt.Frame()
	if d.addr != "me@devbox" {
		t.Fatalf("typed %q", d.addr)
	}
	d.user, d.port = "me", "2222"
	d.name = "devbox"
	// Along to WSL and back: the SSH form is where it was left.
	d.store()
	d.kind = kindWSL
	d.fieldsOf(d.kind)
	if d.addr != "" || d.user != "" || d.port != "" {
		t.Errorf("the WSL form opened with %q, %q, %q", d.addr, d.user, d.port)
	}
	d.addr = "Debian"
	d.store()
	d.kind = kindSSH
	d.fieldsOf(d.kind)
	if d.addr != "me@devbox" || d.user != "me" || d.port != "2222" || d.name != "devbox" {
		t.Errorf("back to SSH reads %q, %q, %q, %q", d.addr, d.user, d.port, d.name)
	}
	if d.armed != "" {
		t.Errorf("a new kind came with armed %q", d.armed)
	}
	// An added host clears only the form it came from.
	if !a.hostAddSubmit(d) {
		t.Fatalf("submit failed: %s", d.err)
	}
	if h := a.set.Host("devbox"); h == nil || h.Addr != "me@devbox:2222" {
		t.Fatalf("devbox reads as %+v", h)
	}
	d.fieldsOf(kindWSL)
	if d.addr != "Debian" {
		t.Errorf("adding an SSH host cleared the WSL form: %q", d.addr)
	}
}

func TestDeviceAddOpensFromThePickerAndClosesToTheTerminal(t *testing.T) {
	a := testApp(t)
	addTerminals(t, a)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox")
	d.open = false
	var ct *ui.Context
	tt := ui.NewTester(func(c *ui.Context) { a.view(c); ct = c }, 1100, 700)
	tt.Frame()

	// The picker's row is a quick action: the dialog opens in front of the
	// window it came from, and Settings stays closed.
	tt.Click(locales.SidebarDeviceAll())
	tt.ChooseMenuItem(locales.SetHostAdd())
	tt.Frame()
	if !d.open {
		t.Fatal("Add a Host never opened the dialog")
	}
	if a.setWin != nil {
		t.Error("Add a Host opened Settings")
	}
	if !tt.HasText(locales.HostAddHost()) {
		t.Errorf("the form never showed %q: %v", locales.HostAddHost(), tt.Texts())
	}
	if !tt.HasText("devbox") {
		t.Errorf("the finder's rows never showed: %v", tt.Texts())
	}

	// Enter adds the highlighted candidate, then the terminal takes the
	// focus back.
	d.addr = "devbox"
	d.armed = "devbox"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.set.Host("devbox") == nil {
		t.Fatalf("Enter added nothing: %v, err %q", a.set.Hosts, d.err)
	}
	if d.open {
		t.Fatal("the dialog stayed open")
	}
	tt.Frame()
	if !a.paneRef("a").Focused(ct) {
		t.Error("the terminal never got the focus back")
	}
	// The finder's rows go with the draft: reopening starts from nothing.
	if d.cands != nil || d.armed != "" {
		t.Errorf("the closed draft still shows %v, armed %q", d.cands, d.armed)
	}
}

func TestDeviceAddClosesFromTheHeader(t *testing.T) {
	a := testApp(t)
	addTerminals(t, a)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox")
	var ct *ui.Context
	tt := ui.NewTester(func(c *ui.Context) { a.view(c); ct = c }, 1100, 700)
	tt.Frame()
	tt.Click(locales.CommonClose())
	tt.Frame()
	if d.open {
		t.Error("Close left the dialog open")
	}
	if !a.paneRef("a").Focused(ct) {
		t.Error("the terminal never got the focus back")
	}
	// Escape is the other way out, and costs nothing.
	a.openHostAdd()
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if d.open {
		t.Error("Escape left the dialog open")
	}
	// Cancel is the third, and the pointer's.
	a.openHostAdd()
	tt.Frame()
	tt.Click(locales.CommonCancel())
	tt.Frame()
	if d.open {
		t.Error("Cancel left the dialog open")
	}
}

func TestDeviceAddKindSwitch(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	// WSL is a choice only where a distribution can be started at all.
	if runtime.GOOS == "windows" {
		if !tt.HasText(locales.HostAddKindWSL()) {
			t.Errorf("the kind switch never offered WSL: %v", tt.Texts())
		}
	} else if tt.HasText(locales.HostAddKindWSL()) {
		t.Error("the kind switch offered WSL where it cannot run")
	}

	tt.Type("me@devbox")
	tt.Frame()
	d.user, d.port = "me", "2222"
	// The switch keeps the form it leaves behind and takes back the one it
	// opens, and the field of the form in hand takes the focus.
	a.hostAddSwitch(d, kindWSL)
	if d.kind != kindWSL || d.kindSel != "wsl" {
		t.Errorf("the switch left kind %d, choice %q", d.kind, d.kindSel)
	}
	if d.addr != "" || d.user != "" || d.port != "" {
		t.Errorf("the WSL form opened with %q, %q, %q", d.addr, d.user, d.port)
	}
	if !d.refocus {
		t.Error("the field was never asked for the focus back")
	}
	a.hostAddSwitch(d, kindSSH)
	if d.addr != "me@devbox" || d.user != "me" || d.port != "2222" {
		t.Errorf("back to SSH reads %q, %q, %q", d.addr, d.user, d.port)
	}
	if d.kindSel != "ssh" {
		t.Errorf("the switch came back as %q", d.kindSel)
	}
}

func TestDeviceAddPickTakesTheUser(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "me@devbox")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()

	// A row written as user@host fills both rows, so the user is not typed
	// over again; the finder's field keeps the host half, which is what it
	// filters by.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if d.addr != "devbox" || d.user != "me" || d.armed != "devbox" {
		t.Errorf("Down left host %q, user %q, armed %q", d.addr, d.user, d.armed)
	}
}

// writeSSHConfig makes home the process's home and puts an SSH
// configuration with the text given in it, so ConfigAliases reads that
// one file. The empty text makes no file at all: home holds no
// configuration.
func writeSSHConfig(t *testing.T, text string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	if text == "" {
		return
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSSHConfigAliases(t *testing.T) {
	writeSSHConfig(t, `
# a comment, and a pattern that is not a machine's name
Host * !gateway
	HostName 10.0.0.9
Host devbox
	User root
Host office
	HostName office.example.com
Host build=
Host git.example.com office.example.com

Host
`)
	got, err := remote.ConfigAliases()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"devbox", "office", "build", "git.example.com", "office.example.com"}; !slices.Equal(got, want) {
		t.Errorf("ConfigAliases() = %q, want %q", got, want)
	}
}

func TestSSHConfigNoFile(t *testing.T) {
	writeSSHConfig(t, "")
	got, err := remote.ConfigAliases()
	if err != nil || got != nil {
		t.Errorf("ConfigAliases() = %q, %v; want none, no error", got, err)
	}
}

// utf16LE is the text as UTF-16LE bytes, which is what wsl.exe writes
// behind a BOM. (unicode/utf16, as remote's own code uses it.)
func utf16LE(text string) []byte {
	runes := []rune(text)
	code := utf16.Encode(runes)
	out := make([]byte, 0, len(code)*2)
	for _, w := range code {
		out = append(out, byte(w), byte(w>>8))
	}
	return out
}

func TestWSLListParsing(t *testing.T) {
	// What wsl.exe -l -v writes behind a UTF-16LE BOM: a header, the
	// default distribution starred, and the state in a column of its own.
	text := strings.Join([]string{
		"  NAME            STATE           VERSION",
		"* Debian          Running         2",
		"  podman-machine  Stopped         2",
		"  kiri            Running         2",
	}, "\r\n")
	for _, bom := range []struct {
		name string
		head []byte
	}{
		{"utf16", []byte{0xff, 0xfe}},
		{"utf8", []byte{0xef, 0xbb, 0xbf}},
		{"plain", nil},
	} {
		t.Run(bom.name, func(t *testing.T) {
			// Behind a UTF-16 BOM, wsl.exe writes UTF-16LE; elsewhere it
			// writes UTF-8 as it does on a pipe.
			out := append(bom.head, []byte(text)...)
			if bom.name == "utf16" {
				out = append(bom.head, utf16LE(text)...)
			}
			got, err := remote.ParseWslList(out)
			if err != nil {
				t.Fatal(err)
			}
			want := []remote.Distro{
				{Name: "Debian", Default: true},
				{Name: "podman-machine"},
				{Name: "kiri"},
			}
			if !slices.Equal(got, want) {
				t.Errorf("ParseWslList(%s) = %+v, want %+v", bom.name, got, want)
			}
		})
	}
}

func TestWSLListLocalizedHeader(t *testing.T) {
	// The header is the line whose last column is not a number, whatever
	// language wsl.exe answers in.
	out := []byte("  DISTRIBUCIÓN     ESTADO          VERSIÓN\r\n  Debian           Running         2\r\n")
	got, err := remote.ParseWslList(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Debian" || got[0].Default {
		t.Errorf("ParseWslList(localized) = %+v, want Debian, not the default", got)
	}
}

func TestWSLDistrosOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Distros runs wsl.exe here")
	}
	got, err := remote.Distros(t.Context())
	if err != nil || got != nil {
		t.Errorf("Distros() = %+v, %v; want none, no error", got, err)
	}
}
