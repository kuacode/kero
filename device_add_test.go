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
	d.open = true
	d.fieldsOf(kind)
	d.cands = slices.Clone(cands)
	d.sshAt, d.wslAt = time.Now(), time.Now()
	d.sshN, d.wslN = len(cands), len(cands)
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

func TestDeviceAddWslPrefix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// A bare distribution takes the scheme its kind stands for.
		{"Ubuntu", "wsl://Ubuntu"},
		{"Debian", "wsl://Debian"},
		// Anything already naming a scheme is left as it was typed, case
		// included: remote.ParseAddr reads the scheme whatever its case.
		{"wsl://Debian", "wsl://Debian"},
		{"WSL://Debian", "WSL://Debian"},
		{"http://devbox:7373", "http://devbox:7373"},
		{"", ""},
	} {
		if got := hostAddAddrOf(kindWSL, tc.in, "", ""); got != tc.want {
			t.Errorf("hostAddAddrOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// The other kinds are taken exactly as typed: no prefix, no rewrite.
	if got := hostAddAddrOf(kindSSH, "devbox", "", ""); got != "devbox" {
		t.Errorf("kind SSH rewrote the address to %q", got)
	}
}

// 选择 WSL 发行版时，名称与远程地址必须同时填入，且键盘连续浏览时两者
// 保持同步。点击与键盘两条路径都验证。
func TestDeviceAddWslPickFillsBothFields(t *testing.T) {
	// 键盘路径：Down 逐行选择，名称和地址一起变。
	t.Run("keyboard", func(t *testing.T) {
		a := testApp(t)
		d := &a.hostAdd
		addDraftFor(d, kindWSL, "Ubuntu", "Debian")
		d.distros = []remote.Distro{{Name: "Ubuntu", Default: true}, {Name: "Debian"}}
		tt := ui.NewTester(a.view, 1100, 700)
		tt.Frame()

		// WSL 表单同时有“名称”和“远程地址”两个字段的标签。
		if !tt.HasText(locales.HostAddFieldName()) {
			t.Errorf("the Name field is missing: %v", tt.Texts())
		}
		if !tt.HasText(locales.HostAddFieldRemote()) {
			t.Errorf("the Remote address field is missing: %v", tt.Texts())
		}

		tt.Key(0, ui.KeyDown)
		tt.Frame()
		if d.addr != "Ubuntu" || d.name != "Ubuntu" {
			t.Errorf("Down left addr %q, name %q, want both Ubuntu", d.addr, d.name)
		}
		tt.Key(0, ui.KeyDown)
		tt.Frame()
		if d.addr != "Debian" || d.name != "Debian" {
			t.Errorf("second Down left addr %q, name %q, want both Debian", d.addr, d.name)
		}
		tt.Key(0, ui.KeyUp)
		tt.Frame()
		if d.addr != "Ubuntu" || d.name != "Ubuntu" {
			t.Errorf("Up left addr %q, name %q, want both Ubuntu", d.addr, d.name)
		}
	})

	// 点击路径：点击候选行同样同时填入两者，提交后得到 wsl:// 地址。
	t.Run("click", func(t *testing.T) {
		a := testApp(t)
		d := &a.hostAdd
		addDraftFor(d, kindWSL, "Ubuntu", "Debian")
		d.distros = []remote.Distro{{Name: "Ubuntu", Default: true}, {Name: "Debian"}}
		tt := ui.NewTester(a.view, 1100, 700)
		tt.Frame()

		// Debian 只在候选行里出现，点它不会误中字段或提示文案。
		if err := tt.Click("Debian"); err != nil {
			t.Fatalf("clicking the Debian row: %v", err)
		}
		tt.Frame()
		if d.addr != "Debian" || d.name != "Debian" {
			t.Errorf("click left addr %q, name %q, want both Debian", d.addr, d.name)
		}
		if !a.hostAddSubmit(d) {
			t.Fatalf("wsl submit failed: %s", d.err)
		}
		if w := a.set.Host("Debian"); w == nil || w.Addr != "wsl://Debian" {
			t.Errorf("Debian reads as %+v", w)
		}
	})
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
	if len(a.set.Hosts) != 2 {
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
	// Along the rail to the next kind and back: the address stays put.
	d.store()
	d.kind = kindWSL
	d.fieldsOf(d.kind)
	if d.addr != "" {
		t.Errorf("the WSL form opened with %q", d.addr)
	}
	d.addr = "Debian"
	d.store()
	d.kind = kindSSH
	d.fieldsOf(d.kind)
	if d.addr != "me@devbox" {
		t.Errorf("back to SSH reads %q, want the typed address", d.addr)
	}
	if d.armed != "" {
		t.Errorf("a new kind came with armed %q", d.armed)
	}
	// An added host clears only the form it came from.
	d.name = "devbox"
	if !a.hostAddSubmit(d) {
		t.Fatalf("submit failed: %s", d.err)
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
	if !tt.HasText(locales.HostAddFieldSSH()) {
		t.Errorf("the form never showed %q: %v", locales.HostAddFieldSSH(), tt.Texts())
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
}

func TestDeviceAddBadgeCounts(t *testing.T) {
	a := testApp(t)
	d := &a.hostAdd
	addDraftFor(d, kindSSH, "devbox", "build")
	d.distros = []remote.Distro{{Name: "Debian", Default: true}, {Name: "kiri"}}
	d.wslN = len(d.distros)
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	// The rail quotes each scan's count, so a badge says what the finder
	// holds before anyone opens it. The badge is a label of its own, so
	// the count is looked for whole, not as a fragment of some other text.
	if !slices.Contains(tt.Texts(), "2") {
		t.Errorf("the SSH badge never showed its count: %v", tt.Texts())
	}
	// WSL has a rail row, and a badge of its own, only where a
	// distribution can be started at all.
	if runtime.GOOS == "windows" {
		if !slices.Contains(tt.Texts(), locales.HostAddKindWSL()) {
			t.Errorf("the WSL row is missing from the rail: %v", tt.Texts())
		}
	} else if slices.Contains(tt.Texts(), locales.HostAddKindWSL()) {
		t.Error("the rail offered WSL where it cannot run")
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
