package main

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"kero/internal/remote"
	"kero/locales"
)

// menuIconOf makes the menu icon of an SVG, once, sharing it among the
// items that show it, as mygo's menu icons are made once. The icon is a
// template one: the menu shows it in the color of an item's IconColor,
// as the sidebar shows the mark, tinted by the host's own color.
func menuIconOf(s *ui.SVG) *mygo.MenuIcon {
	if s == nil {
		return nil
	}
	if v, ok := menuIconCache.Load(s); ok {
		return v.(*mygo.MenuIcon)
	}
	mi, err := mygo.NewMenuIconSVG(s, true)
	if err != nil {
		return nil
	}
	menuIconCache.Store(s, mi)
	return mi
}

// menuIconCache holds the menu icons the app's menus show, by the SVG
// each is of, so an icon is drawn once.
var menuIconCache sync.Map // *ui.SVG -> *mygo.MenuIcon

// downloadIcon is the tray of the picker's re-download action. No other
// control needs one, so it lives beside the helpers that use it.
var downloadIcon = glyph("lucide-download")

// deviceFilterButton is the sidebar footer's picker of the hosts shown: a
// control across the foot's whole width that names the hosts shown, or All,
// and their count, and opens a panel of them, as a device picker shows a
// device: a header, an All row, one row per host, and the way to add one. A
// check marks each host shown; picking one hides it from the sidebar, and
// picking it again shows it. A sidebar that shows some but not every host
// wears its picker in the accent, so the filter says at a glance that the
// list below is not the whole of it.
func (a *App) deviceFilterButton(c *ui.Context, pal *palette) {
	// The picker's rows are drawn in the theme of this scope, which is the
	// window's, save for how a row the pointer is on looks: see pickerTheme.
	ui.Themed(c, pickerTheme(c, pal)).FillWidth().Shrink(0).Background(ui.Transparent).
		Children(func() { a.deviceFilterBar(c, pal) })
}

// pickerTheme is the window's theme as the host picker takes it. mygo paints
// the row the pointer or the arrows are on with the theme's Accent, and
// writes its text, its check and its subtitle in AccentText: in a picker
// where a check already marks what is shown, a solid bar of the accent reads
// for a chosen state, and the row the picker opens on, being the last one
// above the button, is the one the pointer is on. So here the highlight is a
// wash, with the text's own color on it, and the Border is what shows
// through the panel's separator, which a borderless window leaves invisible.
func pickerTheme(c *ui.Context, pal *palette) *ui.Theme {
	t := *c.Theme()
	t.Accent = pal.accent.Alpha(0.1)
	if pal.dark {
		t.Accent = pal.accent.Alpha(0.16)
	}
	t.AccentText = pal.text
	t.Border = pal.text.Alpha(0.1)
	return &t
}

// deviceFilterBar is the picker's face: the mark of a device, what the
// sidebar shows, how many hosts there are at all, and the way the panel
// opens. It is a menu button across its row's whole width.
func (a *App) deviceFilterBar(c *ui.Context, pal *palette) {
	label := a.deviceFilterLabel()
	// The accent says a filter is on: some hosts are hidden below.
	fg, tint, bg := pal.textMuted, pal.textMuted, pal.chip
	if !a.deviceAll() {
		fg, tint = pal.text, pal.accent
		bg = pal.accent.Alpha(0.09)
		if pal.dark {
			bg = pal.accent.Alpha(0.13)
		}
	}
	b := ui.ButtonBase(c.Key("device-filter")).FillWidth().Height(30).Shrink(0).
		Radius(rowRadius).Padding(0, 7).Gap(6).AlignItems(ui.Center).
		Label(label).Tooltip(locales.SidebarDeviceFilter() + " · " + label)
	if b.Pressed() {
		bg = bg.Mix(pal.text, 0.12)
	} else if b.Hovered() {
		bg = bg.Mix(pal.text, 0.06)
		fg = pal.text
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
	paned(b, pal)
	b.Children(func() {
		// The device, in a well of its own, as the hosts' headers hang their
		// mark in one.
		ui.Box(c).Size(20, 20).Radius(6).Shrink(0).Center().Background(tint.Alpha(0.12)).Children(func() {
			ui.Icon(c, deviceIcon).Size(13, 13).TextColor(tint)
		})
		ui.Text(c, label).FontSize(12).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		ui.Box(c).Padding(1, 5).Radius(4).Shrink(0).Center().Background(pal.kbd).Children(func() {
			ui.Text(c, strconv.Itoa(len(a.hosts))).FontSize(10.5).FontWeight(600).
				TextColor(pal.textMuted).SingleLine()
		})
		ui.Icon(c, chevronUpIcon).Size(10, 10).TextColor(pal.textFaint).Shrink(0)
	})
	// The panel hangs above the bar, as wide as the sidebar's card, and no
	// taller than the room above the bar: the bar's bounds as they were last
	// frame, which is what the panel is attached to. A picker of more hosts
	// than the room holds scrolls, and none of it leaves the window; the
	// start, not the end, is lined up, so that a panel wider than the side of
	// the window it stands on is moved into it rather than past its edge.
	bb := b.Bounds()
	pw, ph := float32(0), float32(0)
	if bb.W > 0 {
		pw = min(max(bb.W, 224), 340)
		ph = min(max(bb.Y-8, 140), 420)
	}
	b.PopoverMenuButton(func(m *ui.Menu) {
		a.devicePickerMenu(c, pal, m)
	}, ui.PopoverMenuButtonOptions{
		At: ui.AnchorTopStart, Self: ui.AnchorBottomStart,
		Panel: func(p ui.Element) {
			p.Background(pal.popover).Radius(rowRadius)
			if pw > 0 {
				p.MinWidth(0).Width(pw).MaxWidth(pw)
			}
			if ph > 0 {
				p.MaxHeight(ph)
			}
			floating(p, pal)
		},
		Item: func(item ui.Element) {
			item.Padding(7, 10).Radius(6)
		},
	})
}

// devicePickerMenu builds the panel of the picker: the All row, one row
// per host, and the way to add one. The panel needs no title of its own:
// the button that opens it names what it shows, and a check on each row
// says the rest.
func (a *App) devicePickerMenu(c *ui.Context, pal *palette, m *ui.Menu) {
	// The All row shows every host.
	if m.Item(locales.SidebarDeviceAll()).Checked(a.deviceAll()).ID("device-all").
		Icon(menuIconOf(deviceIcon)).IconColor(pal.textMuted).
		Subtitle(a.deviceSummary()).Chosen() {
		a.showAllDevices()
	}
	// One row per host, marking each host shown.  The trailing text is
	// how far the host is, once a heartbeat came of its connection, as
	// the host's header shows it.
	for _, h := range a.hosts {
		if !h.remote() && !h.loaded {
			continue
		}
		name := a.hostName(h)
		it := m.Item(name).Checked(a.deviceShown(h)).ID("device-" + hostFilterKey(h)).
			Icon(menuIconOf(a.hostOriginIcon(h))).IconColor(a.hostOriginColor(pal, h)).
			Subtitle(a.deviceSubtitle(h))
		// Content replaces the item's label and subtitle in a drawn menu,
		// so devicePickerRow draws them again, with the quick actions.
		// The item keeps its name for the keys, assistive technology and
		// the system's menus.
		host := h // Content runs on each rebuild, past this loop.
		it.Content(func(x ui.MenuItemContent) { a.devicePickerRow(c, pal, x, host) })
		if it.Chosen() {
			a.toggleDevice(h)
		}
	}
	// Adding a host is a quick action of its own: the draft opens as a
	// small dialog right here, rather than dropping the user into
	// Settings, where the rest of the program's configuration would get
	// in the way of one address. The dialog offers the same three kinds
	// of host Settings does, and saves the host the same way.
	m.Separator()
	if m.Item(locales.SetHostAdd()).Icon(menuIconOf(plusIcon)).IconColor(pal.accent).
		Subtitle(locales.DevicePickerAddDetail()).Chosen() {
		a.openHostAdd()
	}
}

// devicePickerRow draws a host's name and subtitle, then the quick
// actions, inside the item's Content. A control in Content takes its own
// presses, so a tap on a button acts on that host without choosing the row
// and hiding the device from the sidebar. The RTT and buttons sit at the
// end of the row, past the text: a picker panel is narrow, so the name and
// subtitle yield first. There is no code here to close the menu after a
// press: the confirmation opens over it, and leaving it up lets the user
// act on several hosts in one visit.
func (a *App) devicePickerRow(c *ui.Context, pal *palette, x ui.MenuItemContent, h *host) {
	fs := x.Context.Theme().FontSize
	ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, a.hostName(h)).FontSize(fs).SingleLine().Ellipsis("…").TextColor(x.Text)
			ui.Text(c, a.deviceSubtitle(h)).FontSize(fs - 1).SingleLine().Ellipsis("…").TextColor(x.Muted)
		})
		if h.hasRTT() {
			ui.Text(c, rttText(h.rtt)).FontSize(fs - 2).SingleLine().Shrink(0).
				FontFeatures("tnum").TextColor(a.rttColor(pal, h))
		}
		name := a.hostName(h)
		ui.Row(c).Gap(2).Shrink(0).AlignItems(ui.Center).Children(func() {
			// A host the app cannot reach has no service to restart; one
			// that is installing or updating is left alone until it ends.
			devicePickerIconButton(c, pal, x, "picker-restart-"+hostFilterKey(h),
				locales.DevicePickerRestartTip(name), reloadIcon,
				h.busy != "" || (h.remote() && h.rc == nil),
				func() { a.offerHostRestart(h) })
			missing := errors.Is(h.errKind, remote.ErrNotInstalled)
			devicePickerIconButton(c, pal, x, "picker-redownload-"+hostFilterKey(h),
				locales.DevicePickerRedownloadTip(name), downloadIcon,
				h.busy != "" || (h.remote() && h.rc == nil && !missing),
				func() {
					switch {
					case !h.remote():
						// This machine's Kero is the app itself: its own
						// updater, as Settings › General offers, fetches it.
						updater.CheckForUpdates()
					case missing:
						a.installOnHost(h) // no Kero there yet: install it
					default:
						a.offerHostUpdate(h)
					}
				})
		})
	})
}

// devicePickerIconButton is one of the picker's small icon buttons. It
// takes the row's highlight colors, so it reads the same on a row the
// pointer is on, and shows no more than a chip under the pointer. A
// disabled button says the host is busy or out of reach.
func devicePickerIconButton(c *ui.Context, pal *palette, x ui.MenuItemContent, key, tip string, svg *ui.SVG, disabled bool, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Size(20, 20).Radius(5).Center().Shrink(0).
		Label(tip).Tooltip(tip).Disabled(disabled).OnClick(onClick)
	fg, bg := x.Muted, ui.Transparent
	switch {
	case disabled:
		fg = x.Muted.Alpha(0.45)
	case b.Pressed():
		bg, fg = pal.rowHover.Alpha(0.12), x.Text
	case b.Hovered():
		bg, fg = pal.rowHover, x.Text
	}
	b.Background(bg).TextColor(fg).Children(func() {
		ui.Icon(c, svg).Size(12, 12).TextColor(fg)
	})
}

// deviceAll reports whether the sidebar shows every host.
func (a *App) deviceAll() bool { return len(a.prefs.DeviceFilter) == 0 }

// showAllDevices shows every host again, undoing a picker that hid some.
func (a *App) showAllDevices() {
	if a.deviceAll() {
		return
	}
	a.prefs.DeviceFilter = nil
	a.savePrefs()
}

// deviceFilterLabel names the hosts the sidebar shows, for the picker's
// button: All Hosts, a host's own name when it is the only one, or how
// many show.
func (a *App) deviceFilterLabel() string {
	shown := 0
	var one *host
	for _, h := range a.hosts {
		if !a.deviceShown(h) {
			continue
		}
		shown++
		one = h
	}
	switch {
	case a.deviceAll():
		return locales.SidebarDeviceAll()
	case shown == 1:
		return a.hostName(one)
	default:
		return locales.SidebarDevicePicked(shown)
	}
}

// deviceSummary is the All row's subtitle: how many hosts there are, and
// how many are connected.
func (a *App) deviceSummary() string {
	total := len(a.hosts)
	connected := 0
	for _, h := range a.hosts {
		if h.conn == connReady || h.conn == connVersion {
			connected++
		}
	}
	return locales.DevicePickerSummary(total, connected)
}

// deviceSubtitle is a host row's subtitle: who it is and how it stands.
// A host's own is This Computer; a remote one, its address. Then its
// state: online, or why it is not.
func (a *App) deviceSubtitle(h *host) string {
	if h.remote() {
		return h.addr.Scheme + " " + h.addr.Host + " · " + a.deviceStatus(h)
	}
	return a.hostName(h) + " · " + a.deviceStatus(h)
}

// deviceStatus is a host's state in the picker: online, or the way it
// stands when it is not.
func (a *App) deviceStatus(h *host) string {
	switch h.conn {
	case connConnecting:
		return locales.StatusConnecting()
	case connFailed:
		return locales.StatusDisconnected()
	default: // connReady, connVersion
		return locales.DeviceOnline()
	}
}

// hostName is the name a host's header and lists show it by: its own, or
// This Computer.
func (a *App) hostName(h *host) string {
	if h.remote() {
		return h.name
	}
	return locales.HostThisComputer()
}
