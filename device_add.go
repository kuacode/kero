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
// get in the way of one address. It is drawn as Settings draws its own
// pages — the same card, the same grouped rows and fields — so that adding
// a host from the picker reads as one more page of that window.
//
//   - One dialog covers both kinds of host, chosen at its top, and each
//     kind keeps its own form, so switching between them keeps what was
//     typed.
//   - The list under the Host field is a finder, not a judge. Typing
//     filters it, Tab completes the dimmed rest of a match, Up and Down
//     pick a row, and Enter adds exactly what the fields say.
//   - SSH is written as its own parts — host, user, port — and composed
//     into ssh's own destination, so nothing has to be typed around.
//   - An empty name takes the device's name from the target as typed.
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

// hostKind is how a host is reached: over SSH, or through a WSL
// distribution of this Windows machine.
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

// hostAddFields is what one kind's form holds: SSH's parts, or WSL's
// distribution and the host's name. Each kind keeps its own, so switching
// between them keeps what was typed.
type hostAddFields struct {
	addr, user, port, name string
}

// hostAddDraft is the dialog's state, living on the App while it is open.
type hostAddDraft struct {
	open bool
	kind hostKind
	// kindSel is the choice at the dialog's top, in the ids kinds use. The
	// form in hand follows it a frame after the control writes it.
	kindSel string

	// typed keeps each kind's form apart; addr, user, port and name are
	// the form in hand.
	typed      map[hostKind]hostAddFields
	addr, user string
	port, name string

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
	// was picked at the top: the field built for the new kind is a
	// different node, and AutoFocus only answers in the frame a node is
	// born in — which the dialog's own opening frame already is.
	refocus bool

	// live says the dialog is on screen, or has only this frame left it.
	// It is what lets the view tidy up a closing that lands after the
	// build — a submission — without stealing the focus on every frame
	// the dialog was never opened in.
	live bool

	// sshAt, wslAt: when the last scan ran. distros is kept for the
	// default distro's tag.
	sshAt, wslAt time.Time
	distros      []remote.Distro
}

// fieldsOf reads back the form this kind was left with.
func (d *hostAddDraft) fieldsOf(k hostKind) {
	f := d.typed[k]
	d.addr, d.user, d.port, d.name = f.addr, f.user, f.port, f.name
}

// store puts the form back where it belongs.
func (d *hostAddDraft) store() {
	d.typed[d.kind] = hostAddFields{d.addr, d.user, d.port, d.name}
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
	d.kindSel = hostKindID(d.kind)
	d.fieldsOf(d.kind)
	a.hostAddPull()
}

// kindsShown lists the kinds this machine can reach. WSL is Windows' own,
// and its row is not a promise of a working distro: a distribution typed
// under it stays a distribution.
func kindsShown() []hostKind {
	if runtime.GOOS == "windows" {
		return []hostKind{kindSSH, kindWSL}
	}
	return []hostKind{kindSSH}
}

// hostKindID is a kind as the segmented control names it.
func hostKindID(k hostKind) string {
	if k == kindWSL {
		return "wsl"
	}
	return "ssh"
}

// hostKindFromID is hostKindID's way back.
func hostKindFromID(id string) hostKind {
	if id == "wsl" {
		return kindWSL
	}
	return kindSSH
}

// hostAddView builds the dialog. view.go mounts it beside the palette and
// the picker, in the group of things that cover the whole window.
func (a *App) hostAddView(c *ui.Context, pal *palette) {
	// The dialog borrows Settings' row and group builders, which read the
	// search globals. Settings owns those, and its last search may have
	// left hits that would tint this dialog's rows. The dialog is no
	// search's concern, so it clears them for the frames it builds.
	searchHits, searchReveal = nil, ""

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
	// Settings' own surface — its card, corner and edge — lifted to read as
	// the overlay it is.
	floating(paned(panel.Width(560).MaxHeight(560).Radius(14).Background(pal.card), pal), pal).Clip()

	// The header is a page's, as Settings draws it: the title, and the way
	// out as a word rather than a bar of its own.
	ui.Row(c).Height(46).Shrink(0).Padding(0, 20).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SetHostAdd()).FontSize(15).FontWeight(650).TextColor(pal.text)
		ui.Spacer(c)
		b := ui.ButtonBase(c.Key("close")).Padding(4, 8).Radius(7).Label(locales.CommonClose())
		fg := pal.textMuted
		if b.Hovered() {
			fg = pal.text
			b.Background(pal.rowHover)
		}
		b.TextColor(fg).Children(func() {
			ui.Text(c, locales.CommonClose()).FontSize(12.5).SingleLine()
		})
		if b.Clicked() {
			d.open = false
		}
	})

	body := ui.Column(c).Grow(1).MinHeight(0).Padding(0, 20, 18, 20).Gap(14)
	body.AlignItems(ui.Stretch)
	body.Children(func() {
		a.hostAddKindSwitch(c, d, pal)
		a.hostAddForm(c, d, pal)
	})
}

// hostAddKindSwitch is the choice of kind, as Settings' segmented control:
// one choice on a Mac, two where a distribution can be started.
func (a *App) hostAddKindSwitch(c *ui.Context, d *hostAddDraft, pal *palette) {
	if d.kindSel == "" {
		d.kindSel = hostKindID(d.kind)
	}
	kinds := kindsShown()
	if len(kinds) < 2 {
		return
	}
	ids := make([]string, 0, len(kinds))
	labels := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ids = append(ids, hostKindID(k))
		labels = append(labels, hostAddKind(k))
	}
	ui.Row(c).Shrink(0).Children(func() {
		segmented(c, "host-kind", &d.kindSel, ids, labels)
	})
	if d.kindSel != hostKindID(d.kind) {
		a.hostAddSwitch(d, hostKindFromID(d.kindSel))
	}
}

// hostAddSwitch moves the dialog to another kind, keeping the form it
// leaves behind and taking back the one it opens.
func (a *App) hostAddSwitch(d *hostAddDraft, k hostKind) {
	if d.kind == k {
		return
	}
	d.store()
	d.kind = k
	d.kindSel = hostKindID(k)
	d.fieldsOf(k)
	d.err = ""
	d.armed = ""
	d.hi, d.reveal = 0, false
	d.refocus = true // the field is a different node from here on
	a.hostAddPull()
}

// hostAddForm is the form under the kind's choice: Settings' grouped rows,
// one field to a row.
func (a *App) hostAddForm(c *ui.Context, d *hostAddDraft, pal *palette) {
	var host ui.Element
	settingsGroup(c, pal, hostAddKind(d.kind), hostAddDetail(d.kind), func() {
		label := locales.HostAddHost()
		if d.kind == kindWSL {
			label = locales.HostAddFieldWSL()
		}
		settingRow(c, pal, label, "", func() {
			host = a.hostAddAddrField(c, d, pal)
		})
		if d.kind == kindSSH {
			settingRow(c, pal, locales.HostAddUser(), "", func() {
				textField(c, "host-add-user", &d.user, locales.HostAddUserPlaceholder(), 150, pal).
					OnSubmit(func() { a.hostAddSubmit(d) })
			})
			settingRow(c, pal, locales.HostAddPort(), "", func() {
				textField(c, "host-add-port", &d.port, locales.HostAddPortPlaceholder(), 80, pal).
					OnSubmit(func() { a.hostAddSubmit(d) })
			})
		}
		// The name may stay empty: it takes the host's name from the
		// target as the fields show it.
		settingRow(c, pal, locales.HostAddName(), "", func() {
			textField(c, "host-add-name", &d.name,
				locales.SetHostNamePlaceholder(defaultHostName(hostAddAddrOf(d.kind, d.addr, d.user, d.port))), 150, pal).
				OnSubmit(func() { a.hostAddSubmit(d) })
		})
	})

	hostAddCandidates(c, d, pal, host)

	if d.err != "" {
		ui.Text(c, d.err).FontSize(12).TextColor(pal.danger).SingleLine().Shrink(0)
	}

	ui.Spacer(c)
	ui.Row(c).Gap(8).Justify(ui.End).Shrink(0).Children(func() {
		if button(c, "host-add-cancel", locales.CommonCancel(), "esc", false, pal) {
			d.open = false
		}
		if button(c, "host-add", locales.CommonAdd(), "↩", true, pal) {
			a.hostAddSubmit(d)
		}
	})
}

// hostAddAddrField is the field the whole dialog turns on: it filters the
// list below it, completes from it, and is what Enter adds.
func (a *App) hostAddAddrField(c *ui.Context, d *hostAddDraft, pal *palette) ui.Element {
	addr := textField(c, "host-add-addr", &d.addr, hostAddPlaceholder(d.kind), 0, pal).Font("monospace")
	// The field is what the dialog opens on. AutoFocus answers only in the
	// frame a node is born in, which is the frame the dialog is.
	addr.AutoFocus()
	// The kind switch handing the field back after a kind was picked is the
	// other moment: Focus() takes it in any frame, this one included.
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
		a.hostAddPick(d, addr, shown, hostAddNext(d, len(shown), 1))
	})
	addr.OnShortcut(0, ui.KeyUp, func() {
		shown := hostAddShown(d)
		if len(shown) == 0 {
			return
		}
		a.hostAddPick(d, addr, shown, hostAddNext(d, len(shown), -1))
	})

	// Tab completes the dimmed rest of the nearest match instead of
	// moving on: the name is the part that may stay empty, and the kind
	// is chosen with the pointer or its control.
	addr.OnShortcut(0, ui.KeyTab, func() {
		shown := hostAddShown(d)
		if m := hostAddComplete(d.addr, shown); m != "" {
			a.hostAddPick(d, addr, shown, max(slices.Index(shown, m), 0))
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

// hostAddPick takes one of the rows shown into the form. Writing the
// draft's value alone is not enough once the field has been typed in: the
// input keeps its own text, and a write to the value never reaches it. So
// the pick arrives as an edit of the field's own text, which also puts the
// caret after it, where the next keystroke belongs; and the change it
// reports goes out with d.armed equal to what was written, so the change
// handler reads it as a pick and not as a keystroke. A row written as
// user@host fills the user too, so the user is not typed over again.
func (a *App) hostAddPick(d *hostAddDraft, addr ui.Element, shown []string, hi int) {
	s := shown[hi]
	host := s
	if at := strings.LastIndex(s, "@"); at >= 0 {
		d.user = s[:at]
		host = s[at+1:]
	}
	d.armed, d.err = host, ""
	d.hi, d.reveal = hi, true
	if d.addr != host {
		addr.ReplaceText(0, math.MaxInt, host)
		d.addr = host
	}
}

// hostAddShown lists the rows the finder draws for what the field says:
// every candidate while the field still reads like one of them, and the
// ones holding its text as a part of it after that.
func hostAddShown(d *hostAddDraft) []string {
	q := strings.TrimSpace(d.addr)
	if q == "" || q == d.armed {
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

// hostAddCandidates draws the rows under the field, as a group of this
// form's own. With nothing found and no scan running, the whole block
// stays out of the way.
func hostAddCandidates(c *ui.Context, d *hostAddDraft, pal *palette, addr ui.Element) {
	shown := hostAddShown(d)
	if len(shown) == 0 && !d.looking[d.kind] {
		return
	}

	title := hostAddSource(d.kind)
	if d.looking[d.kind] {
		title += " …"
	}
	settingsGroup(c, pal, title, "", func() {
		list := ui.Scroll(c.Key("host-add-cands")).MaxHeight(138)
		reveal := d.reveal
		d.reveal = false
		list.Children(func() {
			for i, s := range shown {
				row := ui.Row(c.Key(i)).Height(30).Gap(8).AlignItems(ui.Center).Radius(6)
				// A row reads as picked only while the field says exactly that.
				if i == d.hi && s == d.armed {
					row.Background(pal.accent.Alpha(0.12))
				} else if row.Hovered() {
					row.Background(pal.rowHover)
				}
				if row.Clicked() {
					d.armed, d.err = s, ""
					d.hi, d.reveal = i, true
					if d.addr != s {
						addr.ReplaceText(0, math.MaxInt, s)
						d.addr = s
					}
					d.refocus = true
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
	})
}

// hostAddTag names a row the finder cannot otherwise say: the distro WSL
// starts by default.
func hostAddTag(d *hostAddDraft, s string) string {
	if d.kind != kindWSL {
		return ""
	}
	for _, ds := range d.distros {
		if ds.Name == s && ds.Default {
			return locales.HostAddDefault()
		}
	}
	return ""
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
			names, ds = nil, nil // nothing found is honest
		}
		d.shown[k] = names
		switch k {
		case kindSSH:
			d.sshAt = time.Now()
		case kindWSL:
			d.wslAt, d.distros = time.Now(), ds
		}
		if !d.open || d.kind != k {
			return
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

// hostAddSubmit adds the host the form shows, and closes. It says whether
// the host went in: a refusal keeps the dialog open with its reason in red.
func (a *App) hostAddSubmit(d *hostAddDraft) bool {
	addr := hostAddAddrOf(d.kind, d.addr, d.user, d.port)
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
	d.addr, d.user, d.port, d.name, d.err = "", "", "", "", ""
	a.focusTerminal()
	return true
}

// hostAddAddrOf is the one place the dialog composes what it holds. The
// Host field is a finder, not a judge: an address typed whole into it is
// still an address, and remote.ParseAddr is the arbiter. Only a bare host
// takes its parts from the rows, and a distro's scheme from its kind.
func hostAddAddrOf(k hostKind, host, user, port string) string {
	host, user, port = strings.TrimSpace(host), strings.TrimSpace(user), strings.TrimSpace(port)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		return host
	}
	if k == kindWSL {
		return "wsl://" + host
	}
	// user@host[:port], the port after the bracket when the host is a
	// literal v6 address.
	dest := host
	if port != "" && (!strings.Contains(host, ":") || strings.HasPrefix(host, "[")) {
		dest += ":" + port
	}
	if user != "" && !strings.Contains(dest, "@") {
		dest = user + "@" + dest
	}
	return dest
}

// The dialog's words, one set per kind.

func hostAddKind(k hostKind) string {
	if k == kindWSL {
		return locales.HostAddKindWSL()
	}
	return locales.HostAddKindSSH()
}

func hostAddDetail(k hostKind) string {
	if k == kindWSL {
		return locales.HostAddDetailWSL()
	}
	return locales.HostAddDetailSSH()
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
	return locales.HostAddHostPlaceholder()
}
