package main

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/internal/remote"
	"kero/internal/settings"
	"kero/internal/tailscale"

	"kero/locales"
)

// hostDraft is the Hosts page's form, which adds a host, or a host's,
// which edits it.
type hostDraft struct {
	name, addr string
	password   string
	err        string
	// editing is the name of the host edited, "" for none.
	editing string
}

// hostsPage is Settings › Hosts: the machines whose sessions the app
// shows beside this one's, which the configuration's host keys keep, as
// `kero hosts` does.
func (a *App) hostsPage(c *ui.Context, pal *palette) {
	settingsGroup(c, pal, locales.SetHosts(), locales.SetHostsDetail(), func() {
		if len(a.hosts) == 1 {
			settingRow(c, pal, locales.SetHostsNone(), locales.SetHostsNoneDetail(), func() {})
		}
		for _, h := range a.hosts[1:] {
			a.hostRow(c, pal, h)
		}
	})
	docLink(c, pal, "hosts-docs", locales.SetHostsDocs(), "https://kero.sh/docs/remote")
	settingsGroup(c, pal, locales.SetHostAdd(), locales.SetHostAddDetail(), func() {
		// The full dialog: a kind picker (SSH, tailnet, WSL), the fields
		// each kind needs and a connection test, shared with the device
		// picker. It opens in this window, not the main one.
		ui.Row(c).Padding(10, 0).Children(func() {
			if button(c, "host-add-open", locales.CommonAdd(), "", true, pal) {
				a.openHostAddInSettings()
			}
		})
	})
	a.listenGroup(c, pal)
	settingsGroup(c, pal, locales.SetHostSettingsSplit(), locales.SetHostSettingsSplitDetail(), nil)
}

// tailnetLook is this machine on the tailnet, as tailscaled last told
// the Hosts page, and when.
type tailnetLook struct {
	self    tailscale.Self
	err     error
	at      time.Time
	looking bool
	copied  time.Time // when the address was copied, for a check mark
}

// listenGroup turns this machine into a host for Kero elsewhere: kerod's
// listener on the tailnet, the configuration's tailnet:// listen, which
// kerod follows as it changes.
func (a *App) listenGroup(c *ui.Context, pal *palette) {
	i := slices.IndexFunc(a.set.Listen, func(l string) bool {
		spec, err := remote.ParseListen(l)
		return err == nil && spec.Kind == "tailnet"
	})
	on := i >= 0
	detail, addr := locales.SetListenDetail(), ""
	if on {
		spec, _ := remote.ParseListen(a.set.Listen[i])
		detail, addr = a.listenAddress(spec.Port)
	}
	settingsGroup(c, pal, locales.SetListenTitle(), "", func() {
		settingRow(c, pal, locales.SetListen(), detail, func() {
			toggle(c.Key("listen"), &on, pal).OnClick(func() {
				s := a.set.Clone()
				s.Listen = slices.DeleteFunc(s.Listen, func(l string) bool {
					spec, err := remote.ParseListen(l)
					return err == nil && spec.Kind == "tailnet"
				})
				if on {
					s.Listen = append(s.Listen, "tailnet://:"+remote.ListenPort())
				}
				a.set = s
			})
		})
		// Only the desktop app updates itself so: a development build, or
		// a package manager's install, can't.
		if mygo.Updater.Enabled() {
			settingRow(c, pal, locales.SetRemoteUpdates(), locales.SetRemoteUpdatesDetail(), func() {
				toggle(c.Key("remote-updates"), &a.set.RemoteUpdates, pal)
			})
		}
		if addr == "" {
			return
		}
		// The address to add elsewhere, in a field of its own to copy.
		copied := time.Since(a.ss.tailnet.copied) < 1500*time.Millisecond
		ui.Row(c).Margin(0, 0, 12, 0).Height(controlH+6).Padding(0, 3, 0, 10).Gap(8).AlignItems(ui.Center).Radius(7).Background(pal.control).Children(func() {
			ui.Text(c, addr).Font("monospace").FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0).Grow(1).Selectable()
			icon, label := copyIcon, locales.HostCopyAddress()
			if copied {
				icon, label = checkIcon, locales.ToastCopied()
			}
			iconButton(c, "copy-listen", icon, label, pal, func() {
				c.WriteClipboard(addr)
				a.ss.tailnet.copied = time.Now()
				go func() {
					time.Sleep(1500 * time.Millisecond)
					a.update(func() {}) // the check mark goes
				}()
			})
		})
	})
}

// listenAddress says where Kero elsewhere finds this machine's listener,
// asking tailscaled for its name now and then, and the address, once it
// is known.
func (a *App) listenAddress(port string) (detail, addr string) {
	t := &a.ss.tailnet
	if !t.looking && time.Since(t.at) > 30*time.Second {
		t.looking = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			self, err := tailscale.Status(ctx)
			cancel()
			a.update(func() { t.self, t.err, t.at, t.looking = self, err, time.Now(), false })
		}()
	}
	switch name := strings.TrimSuffix(t.self.DNSName, "."); {
	case name != "":
		return locales.SetListenOn(), "http://" + net.JoinHostPort(name, port)
	case t.err != nil:
		return locales.SetListenNoTailscale(), ""
	}
	return locales.SetListenDetail(), ""
}

// hostRow is a host in Settings › Hosts: how the app stands with it, and
// what the user may do about it.
func (a *App) hostRow(c *ui.Context, pal *palette, h *host) {
	if groupRows++; groupRows > 1 {
		ui.Box(c).Height(1).Shrink(0).Background(pal.text.Alpha(0.07))
	}
	if d := &a.ss.hostEdit; d.editing == h.name {
		a.hostEditor(c, pal, h)
		return
	}
	ui.Column(c.Key("host-"+h.name)).Padding(11, 0).Gap(8).Children(func() {
		// Identity: the mark, the name, a state capsule, and the system
		// there once it is known.
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, a.hostOriginIcon(h)).Size(16, 16).TextColor(pal.textMuted).Shrink(0)
			ui.Text(c, h.name).FontSize(13.5).FontWeight(600).TextColor(pal.text).SingleLine()
			a.hostStateCapsule(c, pal, h)
			ui.Spacer(c)
			if system := a.hostSystem(h); system != "" {
				ui.Text(c, system).FontSize(12).TextColor(pal.textMuted).SingleLine().Ellipsis("…").Shrink(1)
			}
		})
		// Facts about the host that need no interpretation: its address,
		// how far away it is, the Kero there, and how many sessions it
		// holds. A host that cannot be reached says why instead.
		facts := a.hostFacts(h)
		ui.Text(c, facts).Font("monospace").FontSize(12).TextColor(a.hostFactsColor(pal, h)).SingleLine().Ellipsis("…").MinWidth(0).Selectable()
		// Actions, with words rather than bare icons so they can be found
		// at a glance. Removal stays an icon: it is not a everyday action.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if button(c, "edit", locales.HostEdit(), "", false, pal) {
				a.editHostInSettings(h)
			}
			if button(c, "reconnect", locales.HostReconnect(), "", false, pal) {
				a.reconnectHost(h)
			}
			switch {
			case h.busy != "":
			case errorsIsNotInstalled(h):
				if button(c, "install", locales.HostInstall(), "", true, pal) {
					a.installOnHost(h)
				}
			case h.conn == connVersion || h.behind():
				if button(c, "update", locales.HostUpdateAction(h.name), "", true, pal) {
					a.offerHostUpdate(h)
				}
			case h.stale():
				if button(c, "restart", locales.CommonRestartService(), "", true, pal) {
					a.offerHostRestart(h)
				}
			}
			iconButton(c, "remove", closeIcon, locales.HostRemove(), pal, func() { a.ss.confirm = a.removeHostConfirmation(h) })
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, locales.SetHostRemoteControl()).FontSize(12.5).TextColor(pal.text)
				ui.Text(c, locales.SetHostRemoteControlDetail(h.name)).FontSize(12).TextColor(pal.textMuted).LineHeight(1.35)
			})
			// The switch shows the host's setting, which a click writes back.
			on, name := h.cfg.RemoteControl, h.name
			toggle(c.Key("rc"), &on, pal).OnClick(func() {
				s := a.set.Clone()
				if cfg := s.Host(name); cfg != nil {
					cfg.RemoteControl = on
				}
				a.set = s
			})
		})
	})
}

// hostStateCapsule is the host's state as a chip: a colored dot and a short
// word, so a failed host reads as such before the facts under it.
func (a *App) hostStateCapsule(c *ui.Context, pal *palette, h *host) {
	col, word := pal.good, locales.HostStateOnline()
	switch {
	case h.conn == connConnecting && h.loaded:
		col, word = pal.attention, locales.HostReconnecting()
	case h.conn == connConnecting:
		col, word = pal.attention, locales.StatusConnecting()
	case h.conn == connFailed:
		col, word = pal.danger, locales.StatusDisconnected()
	case h.conn == connVersion || h.behind() || h.stale():
		col, word = pal.attention, locales.HostUpdateAction(h.name)
	}
	cap := ui.Row(c).Height(18).Padding(0, 6).Gap(5).Radius(9).AlignItems(ui.Center).Shrink(0).
		Background(col.Alpha(0.12)).Tooltip(a.hostVersionDetail(h))
	cap.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(col).Shrink(0)
		ui.Text(c, word).FontSize(11).FontWeight(500).TextColor(col).SingleLine()
	})
}

// hostSystem is the system a host reported, short enough for a card's head.
func (a *App) hostSystem(h *host) string {
	i := h.info
	switch {
	case i.Distro != "":
		return i.Distro
	case i.OS != "" && i.Arch != "":
		return i.OS + " · " + i.Arch
	case i.OS != "":
		return i.OS
	}
	return ""
}

// hostFacts is the card's fact band: the address, the round trip, the Kero
// version, and how many sessions the host holds.
func (a *App) hostFacts(h *host) string {
	parts := []string{h.addr.String()}
	if h.hasRTT() {
		parts = append(parts, rttText(h.rtt))
	}
	if h.info.Kero != "" {
		parts = append(parts, "Kero "+h.info.Kero)
	}
	if n := len(a.sessionsOf(h)); n > 0 {
		parts = append(parts, locales.HostSessions(n))
	}
	return strings.Join(parts, " · ")
}

func (a *App) hostFactsColor(pal *palette, h *host) ui.Color {
	if h.conn == connFailed {
		return pal.danger
	}
	return pal.textMuted
}

// hostEditor is a host's row as the user edits its address and name.
func (a *App) hostEditor(c *ui.Context, pal *palette, h *host) {
	d := &a.ss.hostEdit
	ui.Column(c.Key("edit-"+h.name)).Padding(10, 0).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			save := func() { a.saveHost(h) }
			textField(c, "edit-addr", &d.addr, locales.SetHostAddressPlaceholder(), 0, pal).Font("monospace").OnSubmit(save)
			textField(c, "edit-name", &d.name, locales.SetHostNamePlaceholder(defaultHostName(d.addr)), 130, pal).OnSubmit(save)
			if fieldButton(c, "edit-cancel", locales.CommonCancel(), false, pal) {
				*d = hostDraft{}
			}
			if fieldButton(c, "edit-save", locales.CommonSave(), true, pal) {
				save()
			}
		})
		if h.remote() && h.addr.Scheme == "ssh" {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				save := func() { a.saveHost(h) }
				ui.Text(c, locales.SetHostPassword()).FontSize(12.5).TextColor(pal.textMuted)
				textField(c, "edit-password", &d.password, locales.SetHostPasswordPlaceholder(), 0, pal).
					Font("monospace").Password().OnSubmit(save)
			})
		}
		if d.err != "" {
			ui.Text(c, d.err).FontSize(12).TextColor(pal.danger)
		}
	})
}

// saveHost gives a host the address and name the user edited. A host
// renamed keeps its connection; one of another address connects again.
func (a *App) saveHost(h *host) {
	d := &a.ss.hostEdit
	addr := strings.TrimSpace(d.addr)
	name := strings.TrimSpace(d.name)
	if addr == "" {
		d.err = locales.SetHostNeedsAddress()
		return
	}
	if name == "" {
		name = defaultHostName(addr)
	}
	parsed, err := settings.ParseHost(name + "=" + addr)
	if err != nil {
		d.err = err.Error()
		return
	}
	if o := a.set.Host(parsed.Name); o != nil && !strings.EqualFold(o.Name, h.name) {
		d.err = locales.SetHostNameTaken(parsed.Name)
		return
	}
	if dup := a.set.HostAtAddr(parsed.Addr, h.name); dup != nil {
		d.err = locales.SetHostAddrTaken(dup.Name)
		return
	}
	s := a.set.Clone()
	cfg := s.Host(h.name)
	if cfg == nil { // removed meanwhile
		*d = hostDraft{}
		return
	}
	cfg.Name, cfg.Addr = parsed.Name, parsed.Addr
	cfg.Password = strings.TrimSpace(d.password)
	// syncHosts finds the host by its new name, and keeps it while its
	// address stays.
	h.name = parsed.Name
	a.set = s
	*d = hostDraft{}
}

func errorsIsNotInstalled(h *host) bool { return errors.Is(h.errKind, remote.ErrNotInstalled) }

// hostStatus says how the app stands with a host.
func (a *App) hostStatus(h *host) string {
	switch {
	case h.busy == "install":
		return locales.HostInstalling(h.name)
	case h.busy == "update":
		return locales.HostUpdating(h.name)
	case h.busy == "restart":
		return locales.HostRestarting(h.name)
	case h.conn == connConnecting && h.loaded:
		return locales.HostReconnecting()
	case h.conn == connConnecting:
		return locales.StatusConnecting()
	case h.conn == connFailed && errors.Is(h.errKind, remote.ErrNotInstalled):
		return a.hostProblem(h)
	case h.conn == connFailed:
		return a.hostProblem(h) + ": " + h.err
	case h.conn == connVersion:
		return a.hostVersionDetail(h)
	}
	i := h.info
	s := locales.HostConnected(i.Kero, i.OS+"/"+i.Arch, i.Hostname, i.User)
	if n := len(a.sessionsOf(h)); n > 0 {
		s += " · " + locales.HostSessions(n)
	}
	return s
}

// defaultHostName is the name a host of an address takes when the user
// gives none.
func defaultHostName(addr string) string {
	a, err := remote.ParseAddr(addr)
	if err != nil {
		return "devbox"
	}
	return a.Label()
}
