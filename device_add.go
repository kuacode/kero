package main

import (
	"context"
	"math"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"kero/internal/remote"
	"kero/internal/settings"
	"kero/locales"
)

// The picker's Add a Host is a quick action of its own: one dialog that
// adds a host the way Settings' Hosts page does, without dropping the
// user into Settings, where the rest of the program's configuration would
// get in the way of one address. It borrows sheprd's judgement, not its
// look:
//
//   - One dialog covers every kind of host, and each kind keeps its own
//     form, so switching between them keeps what was typed.
//   - The list under the field is a finder, not a judge. Typing filters
//     it, Tab completes the dimmed rest of a match, Up and Down pick a
//     row, and Enter adds exactly what the field shows.
//   - An empty name takes the device's name from the target as typed.
//   - In a narrow window the rail shows only the logos.
//
// What sheprd does that this dialog deliberately does not: Bonjour
// discovery (kero answers mdns, it does not go asking), a "skip host key
// verification" switch, and holding credentials in memory. SSH password
// and passphrase prompts are the next round's work, so there is no
// half-built field for them here: see the note in hostAddSubmit.
//
// Address-level duplicate detection is the other thing worth stealing
// later: an SSH alias, me@devbox, and ssh://devbox:22 are one machine, and
// kero today only compares names.

// hostKind is the rail's way of reaching a machine.
type hostKind int

const (
	kindSSH hostKind = iota
	kindWSL
)

// The candidate caches say how long a badge may quote a lookup that has
// not been opened again since: long enough that opening the dialog twice
// in a row costs no second scan, short enough that a distro installed in
// the meantime shows up on its own.
const (
	sshCandidateTTL = 30 * time.Second
	wslCandidateTTL = 60 * time.Second
)

// hostAddFields holds the dialog's fields per kind, so switching the rail
// keeps what was written. user, port and password are SSH-only.
type hostAddFields struct {
	user, host, port, password, name string
}

// hostAddDraft is the dialog's state, living on the App while it is open.
type hostAddDraft struct {
	open bool
	kind hostKind

	// typed keeps each kind's fields apart, so moving along the rail
	// keeps what was written. The fields, in order, are user, host, port,
	// password, and name: user, port and password are SSH-only, left empty
	// by the other kinds.
	typed    map[hostKind]hostAddFields
	addr     string
	name     string
	user     string
	port     string
	password string

	err string

	// shown holds what each kind's last lookup found, and cands is the
	// part of it the form is drawing now.
	shown map[hostKind][]string
	cands []string

	// armed is the field's text at the moment a candidate was picked.
	// While the field still reads exactly like that, it is the row shown
	// as chosen; one keystroke ends the match.
	armed string
	hi    int
	// reveal asks for the chosen row to be scrolled into view, once.
	reveal bool

	// looking keeps one scan per kind in flight, and look is the
	// generation of the newest one, so a slow answer cannot overwrite a
	// newer one.
	look    uint64
	looking map[hostKind]bool

	// refocus asks the address field to take the focus back after a kind
	// was picked from the rail: the field built for the new kind is a
	// different node, and AutoFocus only answers in the frame a node is
	// born in — which the dialog's own opening frame already is.
	refocus bool

	// live says the dialog is on screen, or has only this frame left it.
	// It is what lets the view tidy up a closing that lands after the
	// build — a submission — without stealing the focus on every frame
	// the dialog was never opened in.
	live bool

	// sshAt, wslAt: when the last scan ran. sshN, wslN: what its badge may
	// quote. distros is kept for the default distro's tag.
	sshAt, wslAt time.Time
	sshN, wslN   int
	distros      []remote.Distro
}

// fieldsOf reads back the fields this kind was left with.
func (d *hostAddDraft) fieldsOf(k hostKind) {
	f := d.typed[k]
	d.addr, d.name, d.user, d.port, d.password = f.host, f.name, f.user, f.port, f.password
}

// store puts the fields back where they belong.
func (d *hostAddDraft) store() {
	d.typed[d.kind] = hostAddFields{d.user, d.addr, d.port, d.password, d.name}
}

// openHostAdd shows the dialog. The picker's Add a Host row calls it.
func (a *App) openHostAdd() {
	d := &a.hostAdd
	if d.typed == nil {
		d.typed = map[hostKind]hostAddFields{}
	}
	if d.shown == nil {
		d.shown = map[hostKind][]string{}
	}
	if d.looking == nil {
		d.looking = map[hostKind]bool{}
	}
	d.open = true
	d.err = ""
	d.armed = ""
	d.hi, d.reveal = 0, false
	d.refocus = true
	d.fieldsOf(d.kind)
	a.hostAddPull()
}

// kindsShown lists the rail. WSL is Windows' own, and its row is not a
// promise of a working distro: an address typed under it stays an address.
func kindsShown() []hostKind {
	if runtime.GOOS == "windows" {
		return []hostKind{kindSSH, kindWSL}
	}
	return []hostKind{kindSSH}
}

// hostAddView builds the dialog. view.go mounts it beside the palette and
// the picker, in the group of things that cover the whole window.
func (a *App) hostAddView(c *ui.Context, pal *palette) {
	d := &a.hostAdd
	if !d.open {
		// The frame after a closing: the finder's rows and the pick go
		// with the dialog, so reopening starts from nothing, and the
		// window's keyboard goes back where it was.
		if d.live {
			d.live = false
			d.cands, d.armed = nil, ""
			a.focusTerminal()
		}
		return
	}
	d.live = true
	ui.DialogBase(c.Key("host-add"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay)
		a.hostAddPanel(c, d, pal, panel)
	})
}

func (a *App) hostAddPanel(c *ui.Context, d *hostAddDraft, pal *palette, panel ui.Element) {
	floating(panel.Width(600).MaxHeight(460).Radius(14).Background(pal.popover), pal).Clip()

	// The close sits where this window style puts its controls: apple's
	// red light at the left, builtin's and windows' close at the right.
	// It only closes the dialog — a dialog overlays the window and holds
	// none of the window's own minimize or maximize.
	header := ui.Row(c).Height(42).Shrink(0).Padding(0, 16).Gap(8).AlignItems(ui.Center)
	mode := a.controlsMode()
	if mode == "apple" {
		header.Children(func() {
			dialogClose(c, pal, mode, &d.open)
			ui.Box(c).Width(8).Shrink(0)
			ui.Icon(c, plusIcon).Size(14, 14).TextColor(pal.accent)
			ui.Text(c, locales.SetHostAdd()).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine()
			ui.Spacer(c)
		})
	} else {
		header.Children(func() {
			ui.Icon(c, plusIcon).Size(14, 14).TextColor(pal.accent)
			ui.Text(c, locales.SetHostAdd()).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine()
			ui.Spacer(c)
			dialogClose(c, pal, mode, &d.open)
		})
	}
	ui.Box(c).Height(1).Shrink(0).Background(pal.text.Alpha(0.07))

	body := ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch)
	body.Children(func() {
		a.hostAddRail(c, d, pal)
		a.hostAddForm(c, d, pal)
	})
}

// dialogClose is the dialog's one close button, at its header's right, in
// the window's controls style: an apple light, a windows block or a Kero
// rounded X. It only closes the dialog: the dialog overlays the window, so
// none of the window's own controls come on it.
func dialogClose(c *ui.Context, pal *palette, mode string, open *bool) {
	switch mode {
	case "apple":
		// A single red light, as the close of the traffic lights, at the
		// right of the header.
		b := ui.ButtonBase(c.Key("close-dialog")).Size(14, 14).Radius(7).Center().Background(ui.RGB(255, 95, 86)).Label(locales.CommonClose()).Tooltip(locales.CommonClose())
		if b.Clicked() {
			*open = false
		}
	case "windows":
		// A windows block: a rounded X over the header's height, red on
		// hover, as the window's own close is.
		b := ui.ButtonBase(c.Key("close-dialog")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(locales.CommonClose()).Tooltip(locales.CommonClose())
		if b.Hovered() || b.Pressed() {
			b.Background(pal.danger).TextColor(ui.RGB(255, 255, 255))
		}
		b.Children(func() { ui.Icon(c, closeIcon).Size(16, 16) })
		if b.Clicked() {
			*open = false
		}
	default:
		// Kero's own rounded X.
		b := ui.ButtonBase(c.Key("close-dialog")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(locales.CommonClose()).Tooltip(locales.CommonClose())
		if b.Hovered() || b.Pressed() {
			b.Background(pal.danger).TextColor(ui.RGB(255, 255, 255))
		}
		b.Children(func() { ui.Icon(c, closeIcon).Size(16, 16) })
		if b.Clicked() {
			*open = false
		}
	}
}

// hostAddRail is the column of kinds. A narrow window drops the words and
// keeps the logos.
func (a *App) hostAddRail(c *ui.Context, d *hostAddDraft, pal *palette) {
	winW, _ := c.Size()
	narrow := winW < 720

	rail := ui.Column(c).Padding(10, 10, 12, 10).Gap(2).Shrink(0)
	if narrow {
		rail.Padding(10, 6, 12, 6).Width(56).AlignItems(ui.Center)
	} else {
		rail.Width(212)
	}

	rail.Children(func() {
		for _, k := range kindsShown() {
			item := ui.Row(c.Key(int(k))).Height(32).Gap(10).AlignItems(ui.Center).Radius(8)
			if narrow {
				item.Padding(0, 0).Justify(ui.Center)
			} else {
				item.Padding(0, 10)
			}
			if pal.dark {
				item.Border(1, ui.Transparent) // room for the chosen item's edge
			}
			sel := d.kind == k
			bg := ui.Transparent
			switch {
			case sel:
				bg = pal.rowSelected
				chosen(item, pal)
			case item.Hovered():
				bg = pal.rowHover
			}
			item.Background(item.AnimateColor("bg", bg, ui.Animation{Duration: 110 * time.Millisecond}))

			// A quick tap can press and release between frames; keep its click too.
			if (item.Pressed() || item.Clicked()) && !sel {
				d.store()
				d.kind = k
				d.fieldsOf(k)
				d.err = ""
				d.armed = ""
				d.hi, d.reveal = 0, false
				d.refocus = true // the field is a different node from here on
				a.hostAddPull()
			}

			icon := serverIcon
			switch k {
			case kindWSL:
				icon = deviceIcon
			}
			item.Children(func() {
				mark := ui.Icon(c, icon).Size(15, 15).Shrink(0).TextColor(pal.textMuted)
				if sel {
					mark.TextColor(pal.accent)
				}
				if narrow {
					return // the logos are the whole rail
				}
				ui.Text(c, hostAddKind(k)).FontSize(13.5).TextColor(pal.text).SingleLine()
				hostAddBadge(c, d, pal, k)
			})
		}
	})
}

// hostAddBadge is the count of what a kind can offer, once that is known.
func hostAddBadge(c *ui.Context, d *hostAddDraft, pal *palette, k hostKind) {
	n := 0
	switch k {
	case kindSSH:
		if !d.looking[k] {
			n = d.sshN
		}
	case kindWSL:
		if !d.looking[k] {
			n = d.wslN
		}
	}
	if n <= 0 {
		return
	}
	ui.Row(c).Height(18).Padding(0, 5).Radius(4).Background(pal.kbd).Shrink(0).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c, itoa(n)).FontSize(10.5).FontWeight(600).TextColor(pal.textMuted).SingleLine()
	})
}

// hostAddForm is everything to the right of the rail.
func (a *App) hostAddForm(c *ui.Context, d *hostAddDraft, pal *palette) {
	col := ui.Column(c).Grow(1).MinWidth(0).Padding(14, 20, 16, 20).Gap(10)
	col.AlignItems(ui.Stretch)

	col.Children(func() {
		ui.Text(c, hostAddDetail(d.kind)).FontSize(12.5).TextColor(pal.textMuted).LineHeight(1.4).Shrink(0)

		// The fields sit on a soft panel, as Settings' groups do.
		// addr 承载查找器（候选列表、键盘导航），name 在选择发行版时一并填入。
		var addr, name ui.Element
		groupRows = 0
		ui.Column(c).Radius(12).Background(pal.chip).Padding(4, 16).Gap(0).Children(func() {
			// Name first
			hostAddFieldRow(c, pal, locales.HostAddFieldName(), func() {
				name = textField(c, "host-add-name", &d.name,
					locales.SetHostNamePlaceholder(defaultHostName(strings.TrimSpace(d.addr))), 0, pal)
				name.OnSubmit(func() { a.hostAddSubmit(d) })
			})

			// SSH target (host) and Port on the same row
			if d.kind == kindSSH {
				hostAddFieldRow(c, pal, locales.HostAddFieldSSH(), func() {
					ui.Row(c).Gap(8).Children(func() {
						addr = a.hostAddAddrField(c, d, pal, name)
						addr.OnSubmit(func() { a.hostAddSubmit(d) })
						// Port field on the same row, smaller
						port := textField(c, "host-add-port", &d.port, locales.HostAddPortPlaceholder(), 0, pal)
						port.Width(80).OnSubmit(func() { a.hostAddSubmit(d) })
					})
				})

				// User and Password on the same row
				hostAddFieldRow(c, pal, locales.HostAddFieldUser(), func() {
					ui.Row(c).Gap(8).Children(func() {
						user := textField(c, "host-add-user", &d.user, locales.HostAddUserPlaceholder(), 0, pal)
						user.OnSubmit(func() { a.hostAddSubmit(d) })
						password := textField(c, "host-add-password", &d.password, locales.HostAddPasswordPlaceholder(), 0, pal)
						password.OnSubmit(func() { a.hostAddSubmit(d) })
					})
				})
			} else {
				// WSL 的远程地址字段：承载发行版查找器，选择后同时填入名称。
				hostAddFieldRow(c, pal, locales.HostAddFieldRemote(), func() {
					addr = a.hostAddAddrField(c, d, pal, name)
					addr.OnSubmit(func() { a.hostAddSubmit(d) })
				})
			}
		})

		if hostAddWants(d.kind) {
			hostAddCandidates(c, d, pal, addr, name)
		}

		if d.err != "" {
			ui.Text(c, d.err).FontSize(12).TextColor(pal.danger).SingleLine().Shrink(0)
		}

		ui.Spacer(c)
		ui.Row(c).Gap(8).Justify(ui.End).Shrink(0).Children(func() {
			if button(c, "host-add", locales.CommonAdd(), "↩", true, pal) {
				a.hostAddSubmit(d)
			}
		})
	})
}

// hostAddFieldRow is one row of the form's group: a label above a field.
func hostAddFieldRow(c *ui.Context, pal *palette, label string, control func()) {
	if groupRows++; groupRows > 1 {
		ui.Box(c).Height(1).Shrink(0).Background(pal.text.Alpha(0.07))
	}
	ui.Column(c).Padding(8, 0).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(pal.textMuted).SingleLine()
		control()
	})
}

// hostAddAddrField is the field the whole dialog turns on: it filters the
// list below it, completes from it, and is what Enter adds. name 是名称
// 字段，WSL 选择发行版时一并填入；SSH 侧由 kindWSL 守卫不会触碰它。
func (a *App) hostAddAddrField(c *ui.Context, d *hostAddDraft, pal *palette, name ui.Element) ui.Element {
	addr := textField(c, "host-add-addr", &d.addr, hostAddPlaceholder(d.kind), 0, pal).Font("monospace")
	// The field is what the dialog opens on. AutoFocus answers only in the
	// frame a node is born in, which is the frame the dialog is.
	addr.AutoFocus()
	// The rail handing the field back after a kind was picked is the other
	// moment: Focus() takes it in any frame, this one included.
	if d.refocus {
		d.refocus = false
		addr.Focus()
	}

	// Typing ends the match with a candidate and starts the filter again.
	addr.OnChange(func() {
		if d.addr == d.armed {
			return // the finder's own fill, not something the user typed
		}
		d.armed = ""
		d.hi, d.reveal = 0, true
	})
	addr.OnSubmit(func() { a.hostAddSubmit(d) })

	// Up and Down pick a row, and take its text into the field. The rows
	// are the ones drawn, which typing has thinned: the arrows and the
	// list below must count the same way. Down takes the first row while
	// nothing is picked yet, Up the last, as a picker does.
	addr.OnShortcut(0, ui.KeyDown, func() {
		shown := hostAddShown(d)
		if len(shown) == 0 {
			return
		}
		a.hostAddPick(d, addr, name, shown, hostAddNext(d, len(shown), 1))
	})
	addr.OnShortcut(0, ui.KeyUp, func() {
		shown := hostAddShown(d)
		if len(shown) == 0 {
			return
		}
		a.hostAddPick(d, addr, name, shown, hostAddNext(d, len(shown), -1))
	})

	// Tab completes the dimmed rest of the nearest match instead of
	// moving on: the name is the part that may stay empty, and the rail
	// answers to the pointer.
	addr.OnShortcut(0, ui.KeyTab, func() {
		shown := hostAddShown(d)
		if m := hostAddComplete(hostAddBare(d, d.addr), shown); m != "" {
			a.hostAddPick(d, addr, name, shown, max(slices.Index(shown, m), 0))
		}
	})
	return addr
}

// hostAddNext is the row the arrows move on to: the first one while
// nothing is picked yet, Down from the pick and Up back from it after
// that, and never past either end.
func hostAddNext(d *hostAddDraft, n, step int) int {
	if d.armed == "" {
		// Nothing picked yet, which a keystroke has just ended: Down takes
		// the first row the list shows and Up the last.
		if step < 0 {
			return n - 1
		}
		return 0
	}
	return min(max(d.hi+step, 0), n-1)
}

// hostAddPick takes one of the rows shown into the field. Writing the
// draft's value alone is not enough once the field has been typed in: the
// input keeps its own text, and a write to the value never reaches it. So
// the pick arrives as an edit of the field's own text, which also puts the
// caret after it, where the next keystroke belongs; and the change it
// reports goes out with d.armed equal to what was written, so the change
// handler reads it as a pick and not as a keystroke.
//
// WSL 的选择器在填入远程地址的同时，把发行版名填入名称字段（若用户尚未
// 自行填写名称）。name 为 ui.Element{} 时表示该 kind 没有名称字段（目前
// 不会出现，保留以防 SSH 侧调用）。
func (a *App) hostAddPick(d *hostAddDraft, addr, name ui.Element, shown []string, hi int) {
	s := shown[hi]
	d.hi, d.reveal = hi, true
	// A candidate that carries a user keeps it in its own field, so the
	// host field stays clean and the user field is filled in alongside.
	host := s
	if d.kind == kindSSH {
		if at := strings.Index(s, "@"); at >= 0 {
			d.user, host = s[:at], s[at+1:]
		}
	}
	// A WSL row writes its scheme into the field, so the address reads
	// wsl://Debian rather than a bare distro name.
	text := hostAddCandText(d, host)
	d.armed, d.err = text, ""
	if d.addr != text {
		addr.ReplaceText(0, math.MaxInt, text)
		d.addr = text
	}
	// WSL：选择发行版时把名称一并填成发行版名。和地址字段一样，这是
	// 用户主动选择的直接结果，键盘连续浏览时名称与地址始终同步。name
	// 为 ui.Element{}（零值）时跳过，避免对不存在的字段写入。
	if d.kind == kindWSL && name != (ui.Element{}) {
		name.ReplaceText(0, math.MaxInt, host)
		d.name = host
	}
}

// hostAddBare is the distro or host a field's text names, without the scheme
// a WSL address carries: the rows under the field are bare names.
func hostAddBare(d *hostAddDraft, s string) string {
	if d.kind == kindWSL {
		return strings.TrimPrefix(s, "wsl://")
	}
	return s
}

// hostAddCandText is what a picked row writes into the address field: a WSL
// address reads as wsl://<distro>, the way a host's address does, rather than
// as a bare distro name.
func hostAddCandText(d *hostAddDraft, s string) string {
	if d.kind == kindWSL {
		return "wsl://" + s
	}
	return s
}

// hostAddShown lists the rows the finder draws for what the field says:
// every candidate while the field still reads like one of them, and the
// ones holding its text as a part of it after that.
func hostAddShown(d *hostAddDraft) []string {
	q := strings.TrimSpace(hostAddBare(d, d.addr))
	if q == "" || q == strings.TrimSpace(hostAddBare(d, d.armed)) {
		return d.cands
	}
	keep := []string{}
	for _, s := range d.cands {
		if strings.Contains(strings.ToLower(s), strings.ToLower(q)) {
			keep = append(keep, s)
		}
	}
	return keep
}

// hostAddCandidates draws the rows under the field. With nothing found and
// no scan running, the whole block stays out of the way.
func hostAddCandidates(c *ui.Context, d *hostAddDraft, pal *palette, addr, name ui.Element) {
	shown := hostAddShown(d)
	if len(shown) == 0 && !d.looking[d.kind] {
		return
	}

	title := hostAddSource(d.kind)
	if d.looking[d.kind] {
		title += " …"
	}
	ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(pal.textFaint).Padding(4, 0, 0, 0).SingleLine()

	list := ui.Scroll(c.Key("host-add-cands")).MaxHeight(150).Gap(1)
	reveal := d.reveal
	d.reveal = false
	list.Children(func() {
		for i, s := range shown {
			row := ui.Row(c.Key(i)).Height(28).Padding(0, 10).Gap(8).AlignItems(ui.Center).Radius(7)
			// A row reads as picked only while the field says exactly that.
			if i == d.hi && s == hostAddBare(d, d.armed) {
				row.Background(pal.accent.Alpha(0.12))
			} else if row.Hovered() {
				row.Background(pal.rowHover)
			}
			if row.Clicked() {
				text := hostAddCandText(d, s)
				d.addr, d.armed = text, text
				d.hi, d.reveal = i, true
				d.err = ""
				d.refocus = true
				// WSL：点击发行版时一并填入名称字段，与键盘选择一致。
				if d.kind == kindWSL && name != (ui.Element{}) {
					name.ReplaceText(0, math.MaxInt, s)
					d.name = s
				}
				addr.Focus()
			}
			row.Children(func() {
				ui.Text(c, s).Font("monospace").FontSize(12.5).TextColor(pal.text).SingleLine().
					Ellipsis("…").Grow(1).MinWidth(0)
				if tag := hostAddTag(d, s); tag != "" {
					ui.Text(c, tag).FontSize(11).TextColor(pal.textFaint).SingleLine().Shrink(0)
				}
			})
			if reveal && i == d.hi {
				row.ScrollIntoView()
			}
		}
	})
}

// hostAddTag names what a row is: the distro WSL starts by default, or an
// alias out of the SSH config.
func hostAddTag(d *hostAddDraft, s string) string {
	if d.kind != kindSSH {
		for _, ds := range d.distros {
			if ds.Name == s && ds.Default {
				return locales.HostAddDefault()
			}
		}
		return ""
	}
	return locales.HostAddSourceSSH()
}

// hostAddComplete is the dimmed rest Tab takes: the shortest candidate
// that starts with what was typed, so a prefix lands on the name it
// belongs to rather than on a longer one.
func hostAddComplete(q string, cands []string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ""
	}
	best := ""
	for _, s := range cands {
		if !strings.HasPrefix(strings.ToLower(s), q) {
			continue
		}
		if best == "" || len(s) < len(best) {
			best = s
		}
	}
	return best
}

// hostAddWants says whether a kind has a list to offer. Both kinds do.
func hostAddWants(k hostKind) bool { return true }

// hostAddPull shows what the last scan found and starts a new one when
// that answer has gone stale. It runs outside the frame build.
func (a *App) hostAddPull() {
	d := &a.hostAdd
	d.cands = slices.Clone(d.shown[d.kind])
	d.hi, d.reveal, d.armed = 0, false, ""

	var stale bool
	switch d.kind {
	case kindSSH:
		stale = d.sshAt.IsZero() || time.Since(d.sshAt) > sshCandidateTTL
	case kindWSL:
		stale = d.wslAt.IsZero() || time.Since(d.wslAt) > wslCandidateTTL
	default:
		return
	}
	if stale && !d.looking[d.kind] {
		a.hostAddLook(d.kind)
	}
}

func (a *App) hostAddLook(k hostKind) {
	d := &a.hostAdd
	d.looking[k] = true
	gen := d.look + 1
	d.look = gen
	go func() {
		var names []string
		var ds []remote.Distro
		var err error
		switch k {
		case kindSSH:
			names, err = remote.ConfigAliases()
		case kindWSL:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ds, err = remote.Distros(ctx)
			names = wslNames(ds)
		}
		a.hostAddFound(k, gen, names, ds, err)
	}()
}

// hostAddFound is the scan's way back onto the UI thread.
func (a *App) hostAddFound(k hostKind, gen uint64, names []string, ds []remote.Distro, err error) {
	a.update(func() {
		d := &a.hostAdd
		d.looking[k] = false
		if gen != d.look {
			return // a newer scan answered already
		}
		if err != nil {
			names, ds = nil, nil // a badge for nothing found is honest
		}
		d.shown[k] = names
		switch k {
		case kindSSH:
			d.sshN, d.sshAt = len(names), time.Now()
		case kindWSL:
			d.wslN, d.wslAt, d.distros = len(names), time.Now(), ds
		}
		if !d.open || d.kind != k {
			return // the badge is set; the rows come when it is looked at
		}
		d.cands, d.hi, d.reveal, d.armed = slices.Clone(names), 0, false, ""
	})
}

// wslNames lists the distrs in the order wsl.exe gave them.
func wslNames(ds []remote.Distro) []string {
	names := make([]string, 0, len(ds))
	for _, x := range ds {
		names = append(names, x.Name)
	}
	return names
}

// hostAddSubmit adds the host the field shows, and closes. It says whether
// the host went in: a refusal keeps the dialog open with its reason in red.
func (a *App) hostAddSubmit(d *hostAddDraft) bool {
	addr := hostAddAddrOf(d.kind, strings.TrimSpace(d.addr), strings.TrimSpace(d.user), strings.TrimSpace(d.port))
	name := strings.TrimSpace(d.name)
	d.err = ""

	if addr == "" {
		d.err = locales.SetHostNeedsAddress()
		return false
	}
	if name == "" {
		name = defaultHostName(addr)
	}
	h, err := settings.ParseHost(name + "=" + addr)
	if err != nil {
		d.err = err.Error()
		return false
	}
	if a.set.Host(h.Name) != nil {
		d.err = locales.SetHostNameTaken(h.Name)
		return false
	}

	// Settings' own addHost only edits the draft on its page, which is
	// saved when that page is left. This is the palette's way instead:
	// the host is written and the devices re-read in one step.
	//
	// An SSH address that wants a password or a key passphrase is the next
	// round's work. The transport runs ssh(1) with SSH_ASKPASS_REQUIRE =
	// never (internal/remote/transport.go), so there is nothing here to
	// hand a prompt to; that file, and the helper beside it, are the change.
	s := a.set.Clone()
	s.Hosts = append(s.Hosts, h)
	a.applySettings(s)

	d.open = false
	d.typed[d.kind] = hostAddFields{}
	d.addr, d.name, d.user, d.port, d.err = "", "", "", "", ""
	a.focusTerminal()
	return true
}

// hostAddAddrOf is the one place the dialog rewrites what was typed. The
// rail is a finder, not a judge: an http:// address typed under SSH is
// still an address, and remote.ParseAddr is the arbiter. Only a bare
// distro name needs the scheme its kind stands for. SSH's user and port
// fields, when filled, wrap the host: [user@]host[:port]. An address that
// already names a scheme passes through untouched.
func hostAddAddrOf(k hostKind, addr, user, port string) string {
	if k == kindWSL && addr != "" && !strings.Contains(addr, "://") {
		return "wsl://" + addr
	}
	if k == kindSSH && addr != "" && !strings.Contains(addr, "://") {
		// Only add the user prefix when the host field has none.
		if user != "" && !strings.Contains(addr, "@") {
			addr = user + "@" + addr
		}
		// Only add the port suffix when the host field has none.
		if port != "" && !strings.Contains(addr, ":") {
			addr = addr + ":" + port
		}
	}
	return addr
}

// The rail's words, one set per kind.

func hostAddKind(k hostKind) string {
	switch k {
	case kindWSL:
		return locales.HostAddKindWSL()
	default:
		return locales.HostAddKindSSH()
	}
}

func hostAddDetail(k hostKind) string {
	switch k {
	case kindWSL:
		return locales.HostAddDetailWSL()
	default:
		return locales.HostAddDetailSSH()
	}
}

func hostAddSource(k hostKind) string {
	if k == kindWSL {
		return locales.HostAddFoundWSL()
	}
	return locales.HostAddFoundSSH()
}

func hostAddPlaceholder(k hostKind) string {
	if k == kindWSL {
		return locales.HostAddWSLPlaceholder()
	}
	return locales.SetHostAddressPlaceholder()
}
