package main

import (
	"strconv"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

// deviceFilterButton is the sidebar footer's picker of the hosts shown: a
// button that names the hosts shown, or All, and their count, and opens a
// panel of them, as a device picker shows a device: a header, an All row,
// one row per host, and the way to add one. A check marks each host
// shown; picking one hides it from the sidebar, and picking it again
// shows it. A dot on the button marks a sidebar that shows some but not
// every host.
func (a *App) deviceFilterButton(c *ui.Context, pal *palette) {
	label := a.deviceFilterLabel()
	b := ui.ButtonBase(c.Key("device-filter")).Height(26).Radius(6).Padding(0, 8).Gap(5).
		AlignItems(ui.Center).Justify(ui.End).TextColor(pal.textMuted).
		Label(label).Tooltip(locales.SidebarDeviceFilter() + " · " + label)
	if b.Pressed() {
		b.Background(pal.rowHover.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(pal.rowHover)
		b.TextColor(pal.text)
	}
	b.Children(func() {
		ui.Icon(c, deviceIcon).Size(16, 16)
		ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine()
		ui.Text(c, strconv.Itoa(len(a.hosts))).FontSize(11).TextColor(pal.textFaint).SingleLine()
		ui.Icon(c, chevronUpIcon).Size(9, 9).TextColor(pal.textFaint)
		if !a.deviceAll() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.attention).Absolute().Right(4).Top(3).Shrink(0)
		}
	})
	b.PopoverMenuButton(func(m *ui.Menu) {
		a.devicePickerMenu(c, pal, m)
	}, ui.PopoverMenuButtonOptions{
		// The button is at the window's foot's end, so the panel opens
		// above it, lined up with its end: the picker hangs from the
		// corner it comes of, as a device picker does.
		At: ui.AnchorTopEnd, Self: ui.AnchorBottomEnd,
		Panel: func(p ui.Element) {
			p.NoShadow().Background(pal.popover).Radius(rowRadius)
			floating(p, pal)
		},
		Item: func(item ui.Element) {
			item.Padding(7, 10).Radius(6)
		},
	})
}

// devicePickerMenu builds the panel of the picker: a header naming what
// it lists, the All row, one row per host, and the way to add one.
func (a *App) devicePickerMenu(c *ui.Context, pal *palette, m *ui.Menu) {
	// The panel's title and the hint of how it is used. The panel is as
	// wide as the sidebar's card, as its children show, and hangs above
	// the button at the card's end.
	m.Item(locales.DevicePickerTitle()).Disabled(true).Content(func(x ui.MenuItemContent) {
		ui.Text(x.Context, locales.DevicePickerTitle()).FontSize(11).FontWeight(600).TextColor(pal.textFaint)
		ui.Text(x.Context, locales.DevicePickerHint()).FontSize(11).TextColor(pal.textFaint).Opacity(0.8)
	})
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
		if h.hasRTT() {
			it = it.Trailing(rttText(h.rtt))
		}
		if it.Chosen() {
			a.toggleDevice(h)
		}
	}
	// Adding a host: the palette takes an address, or name=address, as a
	// host to add, without a trip to Settings.
	m.Separator()
	if m.Item(locales.SetHostAdd()).Icon(menuIconOf(plusIcon)).IconColor(pal.accent).
		Subtitle(locales.DevicePickerAddDetail()).Chosen() {
		a.openPalette()
		a.pal.addingHost = true
	}
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
