package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"kero/internal/actions"
	"kero/internal/proto"

	"kero/locales"
)

// folderPicker lists the folders of a host, to open a project in, as the
// system's dialog does this machine's: kerod there lists them (fs.list).
type folderPicker struct {
	open bool
	host *host
	// list is the folder listed, nil while it comes, and err why it did
	// not; gen tells the listing asked for last.
	list *proto.FSList
	err  string
	gen  int
	// query filters the folders, or, as ~/… or /…, names one to go to.
	query  string
	index  int
	hidden bool
	scroll ui.ScrollState
	reveal bool // the chosen folder scrolls into view, once
}

// pickerItem is a folder the picker offers.
type pickerItem struct {
	name, path string
	repo, up   bool
	thisPC     bool // This PC's drive, listed where the host's paths start
	root       bool // the host's paths' start jump: the root, or This PC
}

// pickerRoots are the quick jumps the host offers: where its paths start —
// This PC on Windows, the root elsewhere — then its home. The client builds
// them from the host itself, not from the listing, so they are the same on
// every view, including This PC, and on hosts whose daemon offers no jumps
// of its own. They are their icons: the picker shows the home by the house,
// the other by the machine, each naming where it points where the pointer
// rests.
func (a *App) pickerRoots() []pickerItem {
	if a.picker.list == nil {
		return nil
	}
	home := a.picker.list.Home
	var out []pickerItem
	if a.picker.host.windows() {
		out = append(out, pickerItem{name: locales.PickerRoot(), path: proto.ThisPC, thisPC: true, root: true})
	} else {
		out = append(out, pickerItem{name: locales.PickerRoot(), path: "/", root: true})
	}
	return append(out, pickerItem{name: locales.PickerHome(), path: home})
}

// openProjectOn lists a host's folders, from its home, to open a project
// in one.
func (a *App) openProjectOn(h *host) {
	if h.cl == nil {
		a.toast(locales.HostNotConnected(h.name))
		return
	}
	a.picker = folderPicker{open: true, host: h}
	a.pickerGo("~")
}

// pickerGo lists a folder of the picker's host, or, for This PC on
// Windows, the host's drives.
func (a *App) pickerGo(dir string) {
	p := &a.picker
	p.gen++
	gen, hidden := p.gen, p.hidden
	p.err, p.query, p.index, p.reveal = "", "", 0, true
	cl := p.host.cl
	if cl == nil {
		p.err = locales.HostNotConnected(p.host.name)
		return
	}
	go func() {
		var l proto.FSList
		err := cl.Call("fs.list", proto.FSListParams{Dir: dir, Hidden: hidden, ThisPC: dir == proto.ThisPC}, &l)
		a.update(func() {
			if a.picker.gen != gen {
				return
			}
			if err != nil {
				a.picker.err = err.Error()
				return
			}
			// It shows from its first folder, whatever the last one's scroll.
			a.picker.list, a.picker.reveal = &l, true
		})
	}()
}

// pickerItems are the folders the picker shows: the parent, then those of
// the folder listed that the query matches.
func (a *App) pickerItems() []pickerItem {
	p := &a.picker
	if p.list == nil {
		return nil
	}
	var items []pickerItem
	q := strings.TrimSpace(p.query)
	if q == "" && p.list.Parent != "" {
		items = append(items, pickerItem{name: "..", path: p.list.Parent, up: true})
	}
	type scored struct {
		pickerItem
		score int
	}
	var found []scored
	for _, e := range p.list.Entries {
		if !e.Dir {
			continue
		}
		// A drive of This PC is its own path: the drive, not a folder in it,
		// and shown by its letter, C:\ as C:.
		name, path := e.Name, p.host.joinPath(p.list.Dir, e.Name)
		thisPC := p.list.ThisPC
		if thisPC {
			name = strings.TrimRight(e.Name, `\/`)
			path = e.Name
		}
		sc := fuzzy(q, name)
		if sc < 0 {
			continue
		}
		found = append(found, scored{pickerItem{name: name, path: path, repo: e.Repo, thisPC: thisPC}, sc})
	}
	if q != "" {
		slices.SortStableFunc(found, func(x, y scored) int { return y.score - x.score })
	}
	for _, f := range found {
		items = append(items, f.pickerItem)
	}
	return items
}

// isPathQuery tells a query that names a folder rather than filters: on
// Windows, ~\…, a share's \\server\… and a drive's C:\… too.
func isPathQuery(q string, windows bool) bool {
	if strings.HasPrefix(q, "/") || q == "~" || strings.HasPrefix(q, "~/") {
		return true
	}
	drive := len(q) >= 3 && q[1] == ':' && (q[2] == '\\' || q[2] == '/') && ('A' <= q[0] && q[0] <= 'Z' || 'a' <= q[0] && q[0] <= 'z')
	return windows && (strings.HasPrefix(q, `~\`) || strings.HasPrefix(q, `\\`) || drive)
}

func (a *App) pickerView(c *ui.Context, pal *palette) {
	p := &a.picker
	if !p.open {
		return
	}
	h := p.host
	items := a.pickerItems()
	p.index = min(max(p.index, 0), max(len(items)-1, 0))
	var goTo, openDir string
	ui.DialogBase(c.Key("picker"), &p.open, func(backdrop, panel ui.Element) {
		backdrop.Background(pal.overlay).Justify(ui.Start).Padding(84, 0, 0, 0)
		floating(panel.Width(600).MaxHeight(500).Radius(14).Background(pal.popover), pal).Clip().
			Label(locales.PickerTitle(h.label()))
		ui.Row(c).Height(42).Shrink(0).Padding(0, 16).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, hostIcon).Size(14, 14).TextColor(pal.hostColor(h))
			// The machine it lists, which any other can take the place of.
			if len(a.hosts) > 1 {
				a.pickerHosts(c, pal, h)
			} else {
				ui.Text(c, locales.PickerTitle(h.label())).FontSize(13).FontWeight(600).TextColor(pal.text).SingleLine().Shrink(0)
			}
			if p.list != nil {
				dir := h.shortDir(p.list.Dir)
				if p.list.ThisPC {
					// This PC is not a folder: its name is the jump's.
					dir = locales.PickerRoot()
				}
				ui.Text(c, dir).FontSize(12.5).TextColor(pal.textFaint).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			}
			ui.Spacer(c)
			label := locales.PickerShowHidden()
			if p.hidden {
				label = locales.PickerHideHidden()
			}
			b := ui.ButtonBase(c.Key("hidden")).Height(22).Padding(0, 8).Radius(6).Label(label)
			if b.Hovered() {
				b.Background(pal.rowHover)
			}
			b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(pal.textMuted).SingleLine() })
			if b.Clicked() && p.list != nil {
				p.hidden = !p.hidden
				goTo = p.list.Dir
			}
		})
		// The quick jumps: the home and the root, wherever the listing is.
		// They are their icons, the home by the house, the root by the
		// machine, each naming where it points on hover.
		if roots := a.pickerRoots(); len(roots) > 0 {
			ui.Row(c.Key("roots")).Height(40).Shrink(0).Padding(0, 16).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				for _, r := range roots {
					b := ui.ButtonBase(c.Key("root-"+r.path)).Size(28, 28).Padding(0).Radius(6).Center().TextColor(pal.textFaint).Tooltip(r.name)
					if b.Hovered() {
						b.Background(pal.rowHover)
						b.TextColor(pal.text)
					}
					if b.Clicked() {
						goTo = r.path
					}
					b.Children(func() {
						svg := homeIcon
						if r.root {
							svg = hostIcon
						}
						ui.Icon(c, svg).Size(16, 16)
					})
				}
			})
		}
		ui.Row(c).Height(44).Shrink(0).Padding(0, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, folderIcon).Size(16, 16).TextColor(pal.textMuted)
			in := ui.TextInputBase(c.Key("q"), &p.query).Grow(1).FontSize(15).Placeholder(locales.PickerPlaceholder()).AutoFocus()
			if in.Changed() {
				p.index, p.reveal = 0, true
			}
			if c.Shortcut(0, ui.KeyDown) || c.Shortcut(ui.Ctrl, ui.KeyN) {
				p.index, p.reveal = min(p.index+1, len(items)-1), true
			}
			if c.Shortcut(0, ui.KeyUp) || c.Shortcut(ui.Ctrl, ui.KeyP) {
				p.index, p.reveal = max(p.index-1, 0), true
			}
			q := strings.TrimSpace(p.query)
			switch {
			case c.Shortcut(ui.Cmd, ui.KeyEnter):
				// Opens the folder chosen, without going in.
				openDir = a.pickerChosen(items)
			case in.Submitted() || c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeyTab):
				switch {
				case isPathQuery(q, h.windows()):
					// A path typed in opens its folder as the project, on
					// any machine: the host expands and cleans it, and the
					// user named where to open, not where to browse.
					openDir = q
				case len(items) > 0:
					goTo = items[p.index].path
				}
			}
		})
		list := ui.Scroll(c.Key("folders")).Grow(1).MinHeight(0).Padding(6).TrackScroll(&p.scroll)
		reveal := p.reveal
		p.reveal = false
		list.Children(func() {
			switch {
			case p.err != "":
				ui.Text(c, p.err).FontSize(13).TextColor(pal.danger).Padding(12, 12).Selectable()
			case p.list == nil:
				ui.Row(c).Padding(12).Children(func() { ui.Spinner(c) })
			case len(items) == 0:
				ui.Text(c, locales.PickerNoFolders()).FontSize(13).TextColor(pal.textFaint).Padding(12, 12)
			}
			for i, it := range items {
				row := ui.Row(c.Key(it.path)).Height(34).Padding(0, 10).Gap(10).AlignItems(ui.Center).Radius(8)
				if i == p.index {
					row.Background(pal.accent.Alpha(0.12))
					if reveal {
						row.ScrollIntoView()
					}
				} else if row.Hovered() {
					row.Background(pal.rowHover)
				}
				if row.Clicked() {
					p.index = i
				}
				if row.DoubleClicked() {
					goTo = it.path
				}
				row.Children(func() {
					glyph := folderIcon
					if it.repo {
						glyph = branchIcon
					}
					ui.Icon(c, glyph).Size(14, 14).TextColor(pal.textFaint)
					ui.Text(c, it.name).FontSize(13.5).TextColor(pal.text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				})
			}
		})
		ui.Row(c).Height(52).Shrink(0).Padding(0, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.PickerHint(actions.Glyphs("CmdOrCtrl+Enter"))).FontSize(12).TextColor(pal.textFaint).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			if button(c, "cancel", locales.CommonCancel(), "esc", false, pal) {
				p.open = false
			}
			if button(c, "open", locales.CommonOpen(), "", true, pal) {
				openDir = a.pickerChosen(items)
			}
		})
	})
	switch {
	case openDir != "":
		p.open = false
		a.openProjectAt(h, openDir)
	case goTo != "":
		a.pickerGo(goTo)
	}
	if !p.open {
		a.picker = folderPicker{}
		if openDir == "" {
			a.focusTerminal()
		}
	}
}

// pickerChosen is the folder Open opens: the one chosen in the list,
// else, as with the parent chosen, the folder listed.
func (a *App) pickerChosen(items []pickerItem) string {
	p := &a.picker
	if len(items) > 0 && !items[p.index].up {
		return items[p.index].path
	}
	if p.list != nil {
		return p.list.Dir
	}
	return ""
}

// pickerHosts is the picker's switch of machines: the one it lists, and
// each other, which a click lists instead.
func (a *App) pickerHosts(c *ui.Context, pal *palette, shown *host) {
	ui.Row(c.Key("hosts")).Gap(2).AlignItems(ui.Center).MinWidth(0).Shrink(1).Children(func() {
		for _, h := range a.hosts {
			label := h.label()
			b := ui.ButtonBase(c.Key("host-"+h.name)).Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center).
				Label(locales.PickerTitle(label))
			fg := pal.textMuted
			switch {
			case h == shown:
				b.Background(pal.rowSelected).InsetShadow(0, 0, 0, 1, pal.rowSelBorder)
				fg = pal.text
			case b.Hovered():
				b.Background(pal.rowHover)
			}
			if h.remote() && h.conn != connReady {
				b.Opacity(0.5)
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(fg).SingleLine()
			})
			if b.Clicked() && h != shown {
				a.openProjectOn(h)
			}
		}
	})
}
