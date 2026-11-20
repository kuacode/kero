package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"

	"kero/internal/proto"
	"kero/internal/remote"
	"kero/internal/shell"
	"kero/internal/update"

	"kero/locales"
)

var (
	hostIcon = glyph("f7-desktopcomputer")

	// osIcons mark a host by its operating system, the logo of the OS its
	// kerod runs: the host's tip names the OS too, so the mark stays a
	// mark and not another line of text. Unknown, or a host that has not
	// answered yet (h.info.OS empty), keeps the computer.
	osIcons = map[string]*ui.SVG{
		"linux":   glyph("si-linux"),
		"darwin":  glyph("si-apple"),
		"windows": glyph("si-windows11"),
	}
)

// The sidebar heads a remote host, then its projects under it (see
// groupView). A host folds on a click of its header, and one with no
// sessions shows nowhere unless something there wants the user
// (hostNeedsRow), or HideEmptyHosts keeps it out.

// hostNeedsRow reports whether a host has a row of its own in the
// sidebar, besides its groups: while it can't be reached, wants updating
// first, or is busy with what the user asked, as installing Kero.
func (a *App) hostNeedsRow(h *host) bool {
	return h.busy != "" || h.conn == connFailed || h.conn == connVersion
}

// hostHeader is a host's name, with how the app stands with it: a dot
// while it connects or failed to, a chip when Kero there wants updating.
// It heads its projects, and hides them on a click. This machine's says
// "This Computer" and offers its sessions and projects, not a host's
// settings or removal.
func (a *App) hostHeader(c *ui.Context, pal *palette, h *host, top float32, count int) {
	name := h.name
	if !h.remote() {
		name = locales.HostThisComputer()
	}
	// Its icon starts at the sidebar's inset and its name right after it,
	// the anchor of the tree, a hair more in the dark where the chosen
	// row keeps a pixel of edge.
	inset := float32(rowInset)
	if pal.dark {
		inset++
	}
	hdr := ui.Row(c.Key("host-"+name)).Height(30).Margin(top, 0, rowGap, 0).Padding(0, rowInset, 0, inset).Gap(treeGap).AlignItems(ui.Center).Radius(rowRadius)
	hovered := hdr.Hovered()
	collapsed := a.prefs.Collapsed[a.hostFoldKey(h)]
	pointerTooltip(c, hdr, "tip-host-"+name, a.hostTip(h))
	hdr.ContextMenu(func(m *ui.Menu) {
		label := locales.SidebarCollapse()
		if collapsed {
			label = locales.SidebarExpand()
		}
		if m.Item(label).Chosen() {
			a.toggleHost(h)
		}
		if h.remote() {
			a.hostMenu(m, h)
		} else {
			m.Separator()
			if m.Item(locales.ActionNewSession()).Chosen() {
				a.newSessionOn(h, "")
			}
			if m.Item(locales.ActionOpenProject()).Chosen() {
				a.openProjectOn(h)
			}
			m.Separator()
			if m.Item(locales.HostSettings()).Chosen() {
				a.openSettings("hosts")
			}
		}
	})
	if hdr.Clicked() {
		a.toggleHost(h)
	}
	hdr.Children(func() {
		ui.Icon(c, a.hostOriginIcon(h)).Size(hostGlyphW, hostGlyphW).TextColor(pal.hostColor(h)).Shrink(0)
		ui.Text(c, name).FontSize(13).FontWeight(650).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if a.set.SidebarCount {
			ui.Textf(c, "%d", count).FontSize(12.5).TextColor(pal.textFaint).FontFeatures("tnum").Margin(2, 0, 0, 0)
		}
		// The fold of a host's projects is read right after its count, as
		// it is a property of the label, not of the row's far end.
		chev := ui.Icon(c, chevronIcon).Size(11, 11).TextColor(pal.textFaint)
		turn := chev.AnimateTo("turn", 90*(1-b2f(collapsed)), ui.Snappy)
		chev.Rotate(turn).Opacity(chev.Animate("shown", b2f(hovered || collapsed), 120*time.Millisecond))
		ui.Spacer(c)
		if h.busy == "update" && h.progress > 0 {
			// The update's progress, in place of the load's gauges: a
			// bar as far as the archive has come, fetched or sent.
			ui.Box(c).Size(46, 5).Radius(2.5).Background(pal.chip).Shrink(0).Children(func() {
				w := float32(46 * h.progress / 100)
				if w < 2 {
					w = 2
				}
				ui.Box(c).Size(w, 5).Radius(2.5).Background(pal.hostColor(h))
			})
		} else if a.set.ShowSystemLoad {
			a.hostLoad(c, pal, h)
		}
		a.hostState(c, pal, h)
		// Opening a project is always offered, on the row's far end.
		if h.conn == connReady {
			b := ui.ButtonBase(c.Key("add")).Size(20, 20).Radius(5).Center().TextColor(pal.textMuted).
				Label(locales.ActionOpenProject()).Tooltip(locales.ActionOpenProject())
			if b.Hovered() {
				b.Background(pal.rowHover).TextColor(pal.text)
			}
			b.Children(func() { ui.Icon(c, plusIcon).Size(13, 13) })
			if b.Clicked() {
				a.openProjectOn(h)
			}
		}
	})
}

// hostLoad draws a host's CPU and memory load, as its daemon pushes, as
// two compact gauges in the header: a thin vertical gauge of the used
// part, and its percent, as the reference (sheprd) reads them. The gauge
// and its percent take the host's color, or a warning one when the load
// is high. Nothing while the setting is off or no load came.
func (a *App) hostLoad(c *ui.Context, pal *palette, h *host) {
	// A host that never sent its load, an earlier kerod, shows no gauge:
	// nothing, rather than a load of zero that reads as idle.
	if !h.hasLoad() {
		return
	}
	load := h.load
	mem := -1.0
	if load.MemTotal > 0 {
		mem = float64(load.MemUsed) / float64(load.MemTotal)
	}
	row := ui.Row(c.Key("load-" + h.name)).Shrink(0).Gap(3).AlignItems(ui.Center).Tooltip(a.loadTip(h))
	row.Children(func() {
		cpu := float64(load.CPU)
		a.loadGauge(c, pal, float32(cpu)/100, a.loadColor(pal, pal.hostColor(h), cpu, cpuWarn))
		ui.Text(c, fmt.Sprintf("%d%%", load.CPU)).FontSize(11).TextColor(a.loadColor(pal, pal.textMuted, cpu, cpuWarn)).FontFeatures("tnum").SingleLine().Shrink(0)
		if mem >= 0 {
			a.loadGauge(c, pal, float32(mem), a.loadColor(pal, pal.textMuted, mem*100, memWarn))
			ui.Text(c, fmt.Sprintf("%d%%", int(mem*100))).FontSize(11).TextColor(a.loadColor(pal, pal.textMuted, mem*100, memWarn)).FontFeatures("tnum").SingleLine().Shrink(0)
		}
	})
}

// The load past which a gauge's color warns, then turns critical, as
// sheprd's CPU and memory do.
const (
	cpuWarn = 75.0
	memWarn = 80.0
	loadMax = 90.0
)

// loadColor grades a share of a resource: the given color while it is
// low, the palette's warning past warn, its danger past loadMax.
func (a *App) loadColor(pal *palette, normal ui.Color, value, warn float64) ui.Color {
	switch {
	case value >= loadMax:
		return pal.danger
	case value >= warn:
		return pal.attention
	}
	return normal
}

// loadGauge is the thin vertical gauge of a host's load: a track, and the
// used part filled from the bottom.
func (a *App) loadGauge(c *ui.Context, pal *palette, frac float32, col ui.Color) {
	g := ui.Box(c).Size(3, 10).Shrink(0)
	g.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, pal.rowHover, r.W/2)
		frac = min(1, max(0, frac))
		if frac > 0.01 {
			h := r.H * frac
			p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - h, W: r.W, H: h}, col, r.W/2)
		}
	})
}

// loadTip says what the host's load bars show, or that none did yet.
func (a *App) loadTip(h *host) string {
	load := h.load
	cpu := fmt.Sprintf("%d%%", load.CPU)
	mem := "—"
	if load.MemTotal > 0 {
		mem = fmt.Sprintf("%d%%", 100*load.MemUsed/load.MemTotal)
	}
	return fmt.Sprintf("%s: CPU %s · Mem %s", h.name, cpu, mem)
}

// hostTag names a remote host in its color, after the name of one of its
// groups, faded while the app has lost it.
func (a *App) hostTag(c *ui.Context, pal *palette, h *host) {
	col := pal.hostColor(h)
	tag := ui.Row(c.Key("host")).Height(18).Padding(0, 6).Radius(5).AlignItems(ui.Center).Shrink(0).
		Background(col.Alpha(0.14)).Tooltip(a.hostTip(h))
	if h.conn != connReady {
		tag.Opacity(0.55)
	}
	tag.Children(func() {
		ui.Text(c, h.name).FontSize(11.5).FontWeight(600).TextColor(col).SingleLine()
	})
}

// hostState says how the app stands with a host, at the end of its row,
// or of its first group's header: a dot while it connects or failed to,
// a chip when Kero there wants updating or kerod restarting.
func (a *App) hostState(c *ui.Context, pal *palette, h *host) {
	switch {
	case h.busy != "":
		ui.Spinner(c).Size(12, 12)
	case h.conn == connConnecting && h.loaded:
		a.hostDot(c, pal, pal.attention, locales.HostReconnecting())
	case h.conn == connConnecting:
		a.hostDot(c, pal, pal.textFaint, locales.StatusConnecting())
	case h.conn == connFailed:
		a.hostDot(c, pal, pal.danger, a.hostProblem(h))
	case h.conn == connVersion || h.behind():
		b := ui.ButtonBase(c.Key("update")).Height(22).Padding(0, 7).Gap(5).Radius(11).AlignItems(ui.Center).
			Label(locales.HostUpdateAction(h.name)).Tooltip(a.hostVersionDetail(h))
		if b.Hovered() {
			b.Background(pal.rowHover)
		}
		b.Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.attention)
			ui.Text(c, locales.CommonUpdate()).FontSize(12).FontWeight(500).TextColor(pal.attention).SingleLine()
		})
		if b.Clicked() {
			a.offerHostUpdate(h)
		}
	case h.stale():
		b := ui.ButtonBase(c.Key("restart")).Height(22).Padding(0, 7).Gap(5).Radius(11).AlignItems(ui.Center).
			Label(locales.HostRestartAction(h.name)).Tooltip(locales.HostStaleTip(h.name, h.daemonApp, h.info.Kero))
		if b.Hovered() {
			b.Background(pal.rowHover)
		}
		b.Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.attention)
			ui.Text(c, locales.CommonRestartService()).FontSize(12).FontWeight(500).TextColor(pal.attention).SingleLine()
		})
		if b.Clicked() {
			a.offerHostRestart(h)
		}
	}
}

// hostDot says how the app stands with a host: a dot, and its words as
// the tooltip.
func (a *App) hostDot(c *ui.Context, pal *palette, col ui.Color, label string) {
	ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0).Margin(0, 4, 0, 0).Label(label).Tooltip(label)
}

// hostTip tells what a host is: its address, and Kero there.
// hostOriginIcon is the mark before a host's name: the operating system's
// logo, as the device is a computer and its OS names it; a host that has
// not told its OS yet keeps the computer, for it may be another platform.
func (a *App) hostOriginIcon(h *host) *ui.SVG {
	if ic, ok := osIcons[h.os()]; ok {
		return ic
	}
	return hostIcon
}

func (a *App) hostTip(h *host) string {
	tip := h.addr.String()
	if i := h.info; i.OS != "" {
		tip += "\n" + locales.HostAbout(i.Kero, i.OS+"/"+i.Arch, i.Hostname, i.User)
	}
	if h.conn == connFailed {
		tip += "\n" + h.err
	}
	return tip
}

// hostProblem words why the app can't reach a host.
func (a *App) hostProblem(h *host) string {
	switch {
	case errors.Is(h.errKind, remote.ErrNotInstalled):
		return locales.HostNotInstalled(h.name)
	case errors.Is(h.errKind, remote.ErrAuth):
		return locales.HostRefused(h.name)
	}
	return locales.HostCantConnect(h.name)
}

// hostNote says, under a host's header, what keeps its sessions from
// showing, and what to do about it.
func (a *App) hostNote(c *ui.Context, pal *palette, h *host) {
	ui.Column(c.Key("note-"+h.name)).Margin(0, 0, 6, 0).Padding(8, 10).Gap(6).Radius(8).Background(pal.chip).Children(func() {
		var title, detail string
		switch {
		case h.busy == "install":
			title = locales.HostInstalling(h.name)
		case h.busy == "update":
			title = locales.HostUpdating(h.name)
			if h.progress > 0 {
				detail = fmt.Sprintf("%d%%", h.progress)
			}
		case h.busy == "restart":
			title = locales.HostRestarting(h.name)
		case h.conn == connVersion:
			title, detail = locales.HostVersionTitle(h.name), a.hostVersionDetail(h)
		case errors.Is(h.errKind, remote.ErrNotInstalled):
			// What the host said says it again.
			title = a.hostProblem(h)
		default:
			title, detail = a.hostProblem(h), h.err
			if errors.Is(h.errKind, remote.ErrAuth) && h.addr.Scheme == "ssh" {
				detail += "\n" + locales.HostAuthHint(h.addr.String())
			}
		}
		ui.Text(c, title).FontSize(12.5).FontWeight(600).TextColor(pal.text)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(pal.textMuted).LineHeight(1.35).Selectable()
		}
		if h.busy != "" {
			return
		}
		ui.Row(c).Gap(6).Margin(2, 0, 0, 0).Children(func() {
			switch {
			case h.conn == connVersion:
				if smallButton(c, "update", locales.CommonUpdate(), true, pal) {
					a.offerHostUpdate(h)
				}
			case errors.Is(h.errKind, remote.ErrNotInstalled):
				if smallButton(c, "install", locales.HostInstall(), true, pal) {
					a.installOnHost(h)
				}
			}
			if h.conn == connFailed && smallButton(c, "retry", locales.CommonTryAgain(), !errors.Is(h.errKind, remote.ErrNotInstalled), pal) {
				a.reconnectHost(h)
			}
		})
	})
}

// smallButton is a button for a sidebar's note.
func smallButton(c *ui.Context, key, label string, primary bool, pal *palette) bool {
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 10).Radius(6).AlignItems(ui.Center).Label(label)
	bg, fg := pal.control, pal.text
	if primary {
		bg, fg = pal.accent, ui.Hex("#ffffff")
	}
	if b.Hovered() {
		bg = bg.Mix(pal.text, 0.06)
	}
	b.Background(bg).TextColor(fg).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine()
	})
	return b.Clicked()
}

// hostMenu is a host header's context menu.
func (a *App) hostMenu(m *ui.Menu, h *host) {
	ready := h.conn == connReady
	if m.Item(locales.ActionNewSession()).Disabled(!ready).Chosen() {
		a.newSessionOn(h, "")
	}
	if m.Item(locales.ActionOpenProject()).Disabled(!ready).Chosen() {
		a.openProjectOn(h)
	}
	m.Separator()
	if m.Item(locales.HostReconnect()).Chosen() {
		a.reconnectHost(h)
	}
	if m.Item(locales.HostRestartAction(h.name)).Disabled(h.rc == nil || h.busy != "").Chosen() {
		a.offerHostRestart(h)
	}
	if h.info.Server || h.conn == connVersion {
		if m.Item(locales.HostUpdateAction(h.name)).Disabled(h.rc == nil || h.busy != "" || !released(mygo.App.Version())).Chosen() {
			a.offerHostUpdate(h)
		}
	}
	if errors.Is(h.errKind, remote.ErrNotInstalled) {
		if m.Item(locales.HostInstall()).Disabled(h.busy != "").Chosen() {
			a.installOnHost(h)
		}
	}
	a.colorMenu(m, h)
	m.Separator()
	if m.Item(locales.HostCopyAddress()).Chosen() {
		a.copyText = h.addr.String()
	}
	if m.Item(locales.HostSettings()).Chosen() {
		a.openSettings("hosts")
	}
	if m.Item(locales.HostRemove()).Chosen() {
		a.confirm = a.removeHostConfirmation(h)
	}
}

// reconnectHost connects to a host again, now, with a new connection.
func (a *App) reconnectHost(h *host) {
	a.dropTerms(h)
	if h.cl != nil {
		go h.cl.Close()
		h.cl = nil
	}
	if h.rc != nil {
		go h.rc.Close() // ssh takes a moment to end: never on the main thread
		h.rc = nil
	}
	h.retries, h.err, h.errKind = 0, "", nil
	h.conn = connConnecting
	a.connectHost(h)
}

// removeHostConfirmation asks to remove a host from Settings › Hosts,
// which leaves its sessions running there.
func (a *App) removeHostConfirmation(h *host) *confirmation {
	name := h.name
	return &confirmation{
		open:    true,
		title:   locales.HostRemoveTitle(name),
		detail:  locales.HostRemoveDetail(name),
		action:  locales.CommonRemove(),
		destroy: true,
		run:     func() { a.removeHost(name) },
	}
}

func (a *App) removeHost(name string) {
	s := a.set.Clone()
	var keep = s.Hosts[:0]
	for _, h := range s.Hosts {
		if !strings.EqualFold(h.Name, name) {
			keep = append(keep, h)
		}
	}
	s.Hosts = keep
	a.applySettings(s)
}

// installOnHost asks, then installs Kero on a host the app reaches over
// SSH or WSL, as Kero's install script does, and connects.
func (a *App) installOnHost(h *host) {
	a.confirm = &confirmation{
		open:   true,
		title:  locales.HostInstallTitle(h.name),
		detail: locales.HostInstallDetail(h.name),
		action: locales.CommonInstall(),
		run:    func() { a.install(h) },
	}
	if a.win != nil {
		a.win.Focus()
	}
}

func (a *App) install(h *host) {
	version := mygo.App.Version()
	if !released(version) {
		version = ""
	}
	cmd, err := remote.InstallCommand(h.addr, version, true)
	if err != nil {
		a.toast(err.Error())
		return
	}
	h.busy = "install"
	go func() {
		out, err := cmd.CombinedOutput()
		a.update(func() {
			h.busy = ""
			if !slices.Contains(a.hosts, h) {
				return
			}
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if i := strings.LastIndex(msg, "\n"); i >= 0 {
					msg = msg[i+1:]
				}
				if msg == "" {
					msg = err.Error()
				}
				a.toast(locales.HostInstallFailed(h.name, msg))
				return
			}
			a.toast(locales.HostInstalled(h.name))
			a.reconnectHost(h)
		})
	}()
}

// hostVersionDetail says how Kero on a host differs from this app, and
// what updates which.
func (a *App) hostVersionDetail(h *host) string {
	v, app := h.kero(), mygo.App.Version()
	switch {
	case h.ahead():
		return locales.HostNewerDetail(h.name, v, app)
	case !h.info.Server && !h.info.Updates:
		return locales.HostDesktopDetail(h.name, v, app)
	case !h.info.Updates:
		return locales.HostManualDetail(h.name, v, app)
	}
	return locales.HostVersionDetail(h.name, v, app)
}

// offerHostUpdate updates the server build of Kero on a host to this
// app's version, and restarts kerod there into it, asking first, as the
// restart ends the programs in its sessions. A host newer than the app,
// or one that updates otherwise, gets told what to do instead.
func (a *App) offerHostUpdate(h *host) {
	switch {
	case h.ahead():
		updater.CheckForUpdates()
		a.toast(a.hostVersionDetail(h))
		return
	case !h.info.Updates:
		a.toast(a.hostVersionDetail(h))
		return
	case !released(mygo.App.Version()):
		a.toast(locales.HostUpdateDev())
		return
	}
	detail := locales.HostUpdateDetail(h.name)
	if !h.info.Server {
		detail = locales.HostUpdateAppDetail(h.name)
	}
	a.confirm = &confirmation{
		open:    true,
		title:   locales.HostUpdateTitle(h.name, mygo.App.Version()),
		detail:  withRunning(detail, a.busyCount(h)),
		action:  locales.CommonUpdate(),
		destroy: true,
		run:     func() { a.hostRequest(h, "update") },
	}
	if a.win != nil {
		a.win.Focus()
	}
}

// offerHostRestart restarts kerod on a host, asking first while programs
// run in its sessions.
func (a *App) offerHostRestart(h *host) {
	// This machine's kerod is restarted as the service it runs as, which
	// the app asks on its own, with the sessions it ends in the words;
	// another host's over the connection to it.
	if !h.remote() {
		a.offerRestart()
		return
	}
	a.confirm = &confirmation{
		open:    true,
		title:   locales.HostRestartTitle(h.name),
		detail:  withRunning(locales.HostRestartDetail(h.name), a.busyCount(h)),
		action:  locales.CommonRestartService(),
		destroy: true,
		run:     func() { a.hostRequest(h, "restart") },
	}
	if a.win != nil {
		a.win.Focus()
	}
}

// hostRequest asks a host to update Kero, then restart kerod into it, or
// only to restart it; the app connects again as kerod comes back, its
// sessions with it, or, should it fail, as it stands.
func (a *App) hostRequest(h *host, what string) {
	rc := h.rc
	if rc == nil {
		a.toast(locales.HostNotConnected(h.name))
		return
	}
	h.busy = what
	version := mygo.App.Version()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var err error
		if what == "update" {
			// The app downloads the archive through the mirror, where the
			// host itself could not, and uploads it: the host's kerod
			// replaces its install with what the stream holds. A host of
			// another platform, or an app not of a release, updates as
			// the host does (KindUpdate, which fetches it itself). Should
			// the app's fetch fail, the host may still fetch it: let it.
			platform := h.info.OS + "/" + h.info.Arch
			if platform != "" && slices.Contains(update.Platforms, platform) {
				update.SetMirror(a.set.DownloadMirror)
				download := func(n int64) { a.setProgress(h, n, 0) }
				archive, sig, ferr := update.FetchWithSig(ctx, version, h.info.OS, h.info.Arch, download)
				if ferr == nil {
					// What the app fetched, the app now sends: the host's
					// kerod replaces its install with it.
					upload := func(sent, total int64) { a.setProgress(h, sent, total) }
					err = rc.Upload(ctx, version, h.info.OS, h.info.Arch, sig, archive, upload)
				} else {
					err = rc.Host(ctx, proto.HostRequest{Kind: proto.KindUpdate, App: version})
				}
			} else {
				err = rc.Host(ctx, proto.HostRequest{Kind: proto.KindUpdate, App: version})
			}
		}
		if err == nil {
			err = rc.Host(ctx, proto.HostRequest{Kind: proto.KindRestart})
		}
		a.update(func() {
			h.busy = ""
			h.progress = 0
			if !slices.Contains(a.hosts, h) {
				return
			}
			if err != nil {
				a.toast(fmt.Sprintf("%s: %v", h.name, err))
				// kerod may have stopped all the same: the connection
				// that disconnected waited for this.
				a.reconnect(h)
				return
			}
			if what == "update" {
				a.toast(locales.HostUpdated(h.name, version))
			}
			// Through the bridge, which stays, or through a new
			// connection to the listener of the kerod that follows.
			time.AfterFunc(time.Second, func() { a.update(func() { a.reconnectHost(h) }) })
		})
	}()
}

// setProgress tells how far the update of a host has gone, for its
// header: a fraction of the archive the app fetched (total 0) or sent.
func (a *App) setProgress(h *host, got, total int64) {
	pct := 0
	if total > 0 {
		pct = int(100 * got / total)
	} else if h.loadAt.IsZero() {
		// Fetching: no total until the server says.
		pct = -1
	}
	a.update(func() {
		if slices.Contains(a.hosts, h) {
			h.progress = pct
		}
	})
}

// openRemotePath opens what a path printed in a remote pane names, which
// the host tells: a folder in a new session there, a file in the editor
// of the host, in a pane beside, as nothing of this machine is the host's.
func (a *App) openRemotePath(h *host, id, link string) {
	path := lineSuffix.ReplaceAllString(link, "")
	cl := h.cl
	if cl == nil {
		return
	}
	base := a.workDir(id)
	go func() {
		var st proto.FSStat
		err := cl.Call("fs.stat", proto.FSStatParams{Path: path, Base: base}, &st)
		a.update(func() {
			switch {
			case err != nil:
				a.toast(err.Error())
			case !st.Exists:
				a.toast(locales.HostNoSuchFile(h.name, path))
			case st.Dir:
				a.newSessionIn(h, st.Path)
			default:
				s := a.session(id)
				if s == nil {
					for i := range a.st.Sessions {
						if a.st.Sessions[i].PaneByID(id) != nil {
							s = &a.st.Sessions[i]
						}
					}
				}
				if s == nil {
					return
				}
				// The host's editor, as its shell would run it.
				p := proto.CreateParams{Split: id, Direction: "down", Command: editorCommand(h, st.Path)}
				a.split(s, p)
			}
		})
	}()
}

// withRunning adds to what a restart does how many sessions run programs
// it ends, when any do.
func withRunning(detail string, n int) string {
	if n == 0 {
		return detail
	}
	return detail + " " + locales.RestartRunning(n) + "."
}

// editorCommand edits a file of a host in a pane, with the editor its
// shell names, $VISUAL or $EDITOR, else vi, or on Windows edit.
func editorCommand(h *host, file string) []string {
	if !h.windows() {
		return []string{"sh", "-c", `exec ${VISUAL:-${EDITOR:-vi}} "$1"`, "sh", file}
	}
	return []string{"powershell.exe", "-NoProfile", "-Command",
		`$e = if ($env:VISUAL) { $env:VISUAL } elseif ($env:EDITOR) { $env:EDITOR } else { 'edit' }; Invoke-Expression ($e + ' ' + ` + shell.PSQuote(shell.PSQuote(file)) + `)`}
}
