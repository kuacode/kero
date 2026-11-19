package main

import (
	"github.com/egoist/mygo/ui"
	"kero/locales"
)

// deviceFilterButton is the sidebar footer's picker of the hosts shown: an
// icon button that opens the list, whose tooltip names the hosts shown, or
// All. A check marks each host shown; picking one hides it from the
// sidebar, and picking it again shows it. A dot on the icon marks a
// sidebar that shows some but not every host.
func (a *App) deviceFilterButton(c *ui.Context, pal *palette) {
	label := a.deviceFilterLabel()
	b := ui.ButtonBase(c.Key("device-filter")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).
		Label(label).Tooltip(locales.SidebarDeviceFilter() + " · " + label)
	if b.Pressed() {
		b.Background(pal.rowHover.Alpha(0.12))
	} else if b.Hovered() {
		b.Background(pal.rowHover)
		b.TextColor(pal.text)
	}
	b.Children(func() {
		ui.Icon(c, deviceIcon).Size(16, 16)
		if !a.deviceAll() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.attention).Absolute().Right(4).Top(3).Shrink(0)
		}
	})
	b.Menu(func(m *ui.Menu) {
		for _, h := range a.hosts {
			if !h.remote() && !h.loaded {
				continue
			}
			name := a.hostName(h)
			if m.Item(name).Checked(a.deviceShown(h)).ID("device-" + hostFilterKey(h)).Chosen() {
				a.toggleDevice(h)
			}
		}
		// Managing the hosts themselves, add and remove: the same page the
		// settings offer, where a host is added, changed or removed.
		m.Separator()
		if m.Item(locales.HostSettings()).Chosen() {
			a.openSettings("hosts")
		}
	})
}

// deviceAll reports whether the sidebar shows every host.
func (a *App) deviceAll() bool { return len(a.prefs.DeviceFilter) == 0 }

// deviceFilterLabel names the hosts the sidebar shows, for the picker's
// tooltip: All Hosts, a host's own name when it is the only one, or how
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

// hostName is the name a host's header and lists show it by: its own, or
// This Computer.
func (a *App) hostName(h *host) string {
	if h.remote() {
		return h.name
	}
	return locales.HostThisComputer()
}
