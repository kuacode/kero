package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
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

	// distroIcons mark a linux host by its distribution, as the user asked
	// for Debian and Ubuntu to read apart: /etc/os-release's ID names the
	// distro, its kerod tells it, and one we have no mark for keeps the
	// linux mark.
	distroIcons = map[string]*ui.SVG{
		"debian":              glyph("si-debian"),
		"ubuntu":              glyph("si-ubuntu"),
		"arch":                glyph("si-archlinux"),
		"archlinux":           glyph("si-archlinux"),
		"fedora":              glyph("si-fedora"),
		"linuxmint":           glyph("si-linuxmint"),
		"mint":                glyph("si-linuxmint"),
		"centos":              glyph("si-centos"),
		"opensuse":            glyph("si-opensuse"),
		"opensuse-leap":       glyph("si-opensuse"),
		"opensuse-tumbleweed": glyph("si-opensuse"),
		"manjaro":             glyph("si-manjaro"),
		"kali":                glyph("si-kalilinux"),
		"kalilinux":           glyph("si-kalilinux"),
		"alpine":              glyph("si-alpinelinux"),
		"alpinelinux":         glyph("si-alpinelinux"),
	}
)

// osBrands are the colors of the marks of operating systems and
// distributions, as their own: the logo of Linux, Apple and Windows, and
// of the distributions Kero names, as Simple Icons serves them.  Light
// then dark; the dark ones for the marks that are black on light, which
// would be lost on the dark.
var osBrands = map[string][2]ui.Color{
	// Operating systems.
	"linux":   {ui.Hex("#c9a227"), ui.Hex("#fcc624")},
	"darwin":  {ui.Hex("#1a1a1c"), ui.Hex("#e6e6ea")},
	"windows": {ui.Hex("#0078d4"), ui.Hex("#5ab4f5")},
	// Distributions of Linux.
	"debian":              {ui.Hex("#a81d33"), ui.Hex("#e8526a")},
	"ubuntu":              {ui.Hex("#e95420"), ui.Hex("#ff8a50")},
	"arch":                {ui.Hex("#1793d1"), ui.Hex("#4fb8ec")},
	"archlinux":           {ui.Hex("#1793d1"), ui.Hex("#4fb8ec")},
	"fedora":              {ui.Hex("#51a2da"), ui.Hex("#7cc0ef")},
	"linuxmint":           {ui.Hex("#86be43"), ui.Hex("#a8d86c")},
	"mint":                {ui.Hex("#86be43"), ui.Hex("#a8d86c")},
	"centos":              {ui.Hex("#262577"), ui.Hex("#8a8ad8")},
	"opensuse":            {ui.Hex("#73ba25"), ui.Hex("#9cd85c")},
	"opensuse-leap":       {ui.Hex("#73ba25"), ui.Hex("#9cd85c")},
	"opensuse-tumbleweed": {ui.Hex("#73ba25"), ui.Hex("#9cd85c")},
	"manjaro":             {ui.Hex("#35bfa4"), ui.Hex("#5fdcc4")},
	"kali":                {ui.Hex("#557c94"), ui.Hex("#8fb4cc")},
	"kalilinux":           {ui.Hex("#557c94"), ui.Hex("#8fb4cc")},
	"alpine":              {ui.Hex("#0d597f"), ui.Hex("#4aa8d8")},
	"alpinelinux":         {ui.Hex("#0d597f"), ui.Hex("#4aa8d8")},
}

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
	// The header is two lines now: the identity (icon, name, state) and,
	// under it, what the host is (address, system) beside its quick
	// actions. The whole block folds and opens the menu on a right click,
	// as the single row did.
	hdr := ui.Column(c.Key("host-"+name)).Margin(top, 0, rowGap, 0).Padding(4, rowInset, 6, inset).Gap(3).Radius(rowRadius)
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
		// Identity: the mark, the name, the state capsule, and the fold.
		ui.Row(c).Height(20).Gap(hdrGap).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, a.hostOriginIcon(h)).Size(hostGlyphW, hostGlyphW).TextColor(a.hostOriginColor(pal, h)).Shrink(0)
			ui.Text(c, name).FontSize(13).FontWeight(650).TextColor(pal.text).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1).
				Margin(0, 0, 0, treeGap-hdrGap)
			// A host with no session has nothing to count, and a bare zero
			// reads as part of the name: it is left out until there is one.
			if a.set.SidebarCount && count > 0 {
				countChip(c, pal, count, treeGap-hdrGap)
			}
			// Update and restart ask for a choice, so they read on the
			// identity line as buttons. The connection state word reads on
			// the second line, with the address in its hover.
			a.hostStateAction(c, pal, h)
			chev := ui.Icon(c, chevronIcon).Size(11, 11).TextColor(pal.textFaint).Shrink(0).Margin(0, 0, 0, treeGap-hdrGap)
			turn := chev.AnimateTo("turn", 90*(1-b2f(collapsed)), ui.Snappy)
			chev.Rotate(turn).Opacity(chev.Animate("shown", b2f(hovered || collapsed), 120*time.Millisecond))
			// Opening a project is the one action every host offers, so it
			// closes out the identity line at its far end, always reachable
			// even when the second line is folded away from view. A spring
			// pushes it against the sidebar edge, past the fold arrow.
			ui.Box(c).Grow(1)
			if h.remote() {
				a.hostQuickButton(c, pal, "reconnect", locales.HostReconnect(), reloadIcon, func() { a.reconnectHost(h) })
				a.hostQuickButton(c, pal, "edit", locales.HostEdit(), editIcon, func() { a.editHost(h) })
			}
			if h.conn == connReady {
				a.hostQuickButton(c, pal, "add", locales.ActionOpenProject(), plusIcon, func() { a.openProjectOn(h) })
			}
		})
		// The second line keeps the connection state word, the round trip
		// and load. Update/restart read on the identity line; the address
		// and system live in the state word's hover. The whole line is one
		// muted gray: it reads as a line of facts, not colored signals.
		ui.Row(c).Height(16).Gap(hdrGap).AlignItems(ui.Center).Children(func() {
			a.hostState(c, pal, h)
			ui.Box(c).Grow(1)
			if h.hasRTT() {
				ui.Text(c, "RTT:"+rttText(h.rtt)).FontSize(10.5).TextColor(pal.textMuted).FontFeatures("tnum").SingleLine().Shrink(0).
					Tooltip(a.statsTip(h))
			}
			if a.set.ShowSystemLoad {
				a.hostLoad(c, pal, h)
			}
		})
	})
}

// hostQuickButton is one of the header's always-visible actions: a small
// quiet button that lifts under the pointer. Its label carries the tip.
func (a *App) hostQuickButton(c *ui.Context, pal *palette, key, tip string, svg *ui.SVG, onClick func()) {
	b := ui.ButtonBase(c.Key(key)).Size(19, 19).Radius(5).Center().Shrink(0).
		Label(tip).Tooltip(tip).OnClick(onClick)
	fg := pal.textFaint
	if b.Hovered() {
		fg = pal.text
		b.Background(pal.rowHover)
	}
	b.TextColor(fg).Children(func() { ui.Icon(c, svg).Size(12, 12).TextColor(fg) })
}

// hostLoad draws a host's CPU and memory load, as its daemon pushes, as
// two compact figures in the second line, as the reference (sheprd) reads
// them. The figures take the line's muted gray like its other facts:
// nothing changes color as the load rises. Nothing while the setting is
// off or no load came.
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
	row := ui.Row(c.Key("load-" + h.name)).Shrink(0).Gap(4).AlignItems(ui.Center).Tooltip(a.statsTip(h))
	row.Children(func() {
		ui.Text(c, fmt.Sprintf("CPU:%d%%", load.CPU)).FontSize(10.5).TextColor(pal.textMuted).FontFeatures("tnum").SingleLine().Shrink(0)
		if mem >= 0 {
			ui.Text(c, fmt.Sprintf("MEM:%d%%", int(mem*100))).FontSize(10.5).TextColor(pal.textMuted).FontFeatures("tnum").SingleLine().Shrink(0)
		}
	})
}

// statsTip is the one hint for the figures on the row: it adds what they
// leave out — the jitter behind the latency and the memory share — without
// repeating the latency and CPU already read inline. Both the round trip and
// the load carry it, so one hover reads the whole picture.
func (a *App) statsTip(h *host) string {
	var parts []string
	if h.rttSamples > 1 {
		parts = append(parts, locales.HostJitter(rttText(h.rttVar)))
	}
	if h.hasLoad() {
		parts = append(parts, loadDetail(h))
	}
	return strings.Join(parts, " · ")
}

// loadDetail words the CPU and memory figures the daemon sent.
func loadDetail(h *host) string {
	load := h.load
	mem := "—"
	if load.MemTotal > 0 {
		mem = fmt.Sprintf("%d%%", 100*load.MemUsed/load.MemTotal)
	}
	return fmt.Sprintf("CPU %d%% · Mem %s", load.CPU, mem)
}

// rttText words a round trip: milliseconds under a second, seconds past
// it, and under a millisecond a sign that it is close.
func rttText(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		// 精度到到毫秒，不必 < 1ms
		return "1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", int(math.Round(float64(d)/float64(time.Millisecond))))
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// rttColor grades the smoothed round trip green, yellow, red: good while
// it is quick, the palette's warning once it drags, its danger once far.
func (a *App) rttColor(pal *palette, h *host) ui.Color {
	switch {
	case h.rtt >= rttFar:
		return pal.danger
	case h.rtt >= rttSlow:
		return pal.attention
	}
	return pal.good
}

// rttTip names the round trip and, once a variance came of it, the jitter
// beside it: how much the heartbeat moves, which reads the connection's
// steadiness better than the figure alone.
func (a *App) rttTip(h *host) string {
	if h.rttSamples > 1 {
		return locales.HostLatencyJitter(rttText(h.rtt), rttText(h.rttVar))
	}
	return locales.HostLatency(rttText(h.rtt))
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

// hostStateAction puts the choices that need a decision on the identity
// line: an update or a restart button. Connection states read as plain
// words on the second line instead.
func (a *App) hostStateAction(c *ui.Context, pal *palette, h *host) {
	switch {
	case h.conn == connVersion || h.behind():
		b := ui.ButtonBase(c.Key("update")).Height(18).Padding(0, 6).Gap(5).Radius(9).AlignItems(ui.Center).
			Label(locales.HostUpdateAction(h.name)).Tooltip(a.hostTip(h))
		if b.Hovered() {
			b.Background(pal.rowHover)
		}
		b.Children(func() {
			ui.Text(c, locales.CommonUpdate()).FontSize(11).FontWeight(500).TextColor(pal.attention).SingleLine()
		})
		if b.Clicked() {
			a.offerHostUpdate(h)
		}
	case h.stale():
		b := ui.ButtonBase(c.Key("restart")).Height(18).Padding(0, 6).Gap(5).Radius(9).AlignItems(ui.Center).
			Label(locales.HostRestartAction(h.name)).Tooltip(a.hostTip(h))
		if b.Hovered() {
			b.Background(pal.rowHover)
		}
		b.Children(func() {
			ui.Text(c, locales.CommonRestartService()).FontSize(11).FontWeight(500).TextColor(pal.attention).SingleLine()
		})
		if b.Clicked() {
			a.offerHostRestart(h)
		}
	}
}

// hostState says how the app stands with a host, as a short word at the
// start of the second line. The full address, system and latency live in
// its hover (hostTip), so the line itself stays free of a wsl:// address.
// The word is one muted gray whatever the state: the line reads as facts,
// not as colored signals.
func (a *App) hostState(c *ui.Context, pal *palette, h *host) {
	stateWord := func(label string) {
		ui.Text(c, label).FontSize(10.5).FontWeight(500).TextColor(pal.textMuted).SingleLine().Shrink(0).Tooltip(a.hostTip(h))
	}
	switch {
	case h.conn == connConnecting && h.loaded:
		stateWord(locales.HostReconnecting())
	case h.conn == connConnecting:
		stateWord(locales.StatusConnecting())
	case h.conn == connFailed:
		stateWord(locales.StatusDisconnected())
	case h.conn == connReady:
		stateWord(locales.HostStateOnline())
	}
}

// hostTip tells what a host is: its address, and Kero there.
// hostOriginColor is the color of the mark before a host's name: the
// brand of the distribution or operating system the mark is of, light or
// dark as the appearance, else the host's color for the computer of a
// host that has not told its OS yet, and this machine's muted text.
func (a *App) hostOriginColor(pal *palette, h *host) ui.Color {
	name := ""
	if h.os() == "linux" {
		if _, ok := distroIcons[h.distro()]; ok {
			name = h.distro()
		}
	}
	if name == "" {
		name = h.os()
	}
	if col, ok := osBrands[name]; ok {
		if pal.dark {
			return col[1]
		}
		return col[0]
	}
	if _, ok := osIcons[name]; ok {
		return pal.hostColor(h)
	}
	return pal.hostColor(h)
}

// hostOriginIcon is the mark before a host's name: its distribution's
// logo for a linux host that told one (as the user asked Debian and
// Ubuntu to read apart), else its operating system's, else the
// computer's. A host that has not told its OS yet keeps the computer,
// for it may be another platform.
func (a *App) hostOriginIcon(h *host) *ui.SVG {
	if h.os() == "linux" {
		if ic, ok := distroIcons[h.distro()]; ok {
			return ic
		}
	}
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
	if h.hasRTT() {
		tip += "\n" + a.rttTip(h)
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
			title = hostBusyTitle(h)
			detail = hostProgressDetail(h)
		case h.busy == "update":
			title = hostBusyTitle(h)
			detail = hostProgressDetail(h)
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

// hostProgressDetail is the figure under a host's installing or updating
// note. A real percent reads as "45%"; before the response names a size
// the work has begun but cannot be measured, and the user is told that
// rather than shown a misleading zero. A download also names the address
// it comes from, so a stalled fetch says where it waited.
func hostProgressDetail(h *host) string {
	pct := locales.HostProgressStarting()
	if h.progress >= 0 {
		pct = fmt.Sprintf("%d%%", h.progress)
	}
	if h.progressStage == "download" && h.progressURL != "" {
		return locales.HostProgressDownload(h.progressURL, pct)
	}
	return pct
}

// hostBusyTitle is the note's title while a host is being changed: it
// names the leg in progress, so a download and an upload are told apart.
func hostBusyTitle(h *host) string {
	v := h.busyVersion
	switch {
	case h.busy == "update" && h.progressStage == "download":
		return locales.HostUpdatingDownload(v)
	case h.busy == "update" && h.progressStage == "upload":
		return locales.HostUpdatingUpload(v)
	case h.busy == "update":
		return locales.HostUpdating(h.name)
	case h.busy == "install" && h.progressStage == "download":
		return locales.HostInstallingDownload(v)
	default:
		return locales.HostInstalling(h.name)
	}
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
	if m.Item(locales.HostEdit()).Chosen() {
		a.editHost(h)
	}
	if m.Item(locales.HostSettings()).Chosen() {
		a.openSettings("hosts")
	}
	if m.Item(locales.HostRemove()).Chosen() {
		a.confirm = a.removeHostConfirmation(h)
	}
}

// editHost opens the shared add/edit dialog, filled with this host, so
// every entry point (sidebar menu, picker, settings card) uses one form.
func (a *App) editHost(h *host) {
	a.openHostEdit(h)
}

// editHostInSettings opens the same dialog filled with this host, in the
// Settings window where the card that was clicked lives.
func (a *App) editHostInSettings(h *host) {
	a.openHostEditInSettings(h)
}

// startHostEdit puts a host's row in edit mode, on the Hosts page, without
// opening Settings: editHost opens the window, and tests read the page.
func (a *App) startHostEdit(h *host) {
	a.ss.page = "hosts"
	a.ss.hostEdit = hostDraft{name: h.name, addr: h.cfg.Addr, password: h.cfg.Password, editing: h.name}
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
	// The app fetches the archive itself, where the host could not, and
	// sends it: the host installs it with no download of its own. A host
	// of another platform, or one without the app's path, still installs
	// as it did, with the script that downloads. Should the fetch fail,
	// the host may fetch it: let it.
	platform := h.info.OS + "/" + h.info.Arch
	goos, goarch := h.info.OS, h.info.Arch
	if platform == "" {
		// A host with no kero (ErrNotInstalled) tells its platform with a
		// command of its own, so the app fetches the right archive.
		if gos, garch, perr := remote.Platform(h.addr, true, h.cfg.Password); perr == nil {
			goos, goarch = gos, garch
			platform = gos + "/" + garch
		}
	}
	if a.set.LocalDownload && platform != "" && slices.Contains(update.Platforms, platform) && version != "" {
		update.SetMirror(update.MirrorFor(a.set.DownloadAccel, a.set.DownloadMirror))
		update.SetCacheDir(filepath.Join(proto.Dir(), "update-cache"))
		h.busy = "install"
		h.busyVersion = version
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			download := func(got, total int64) { a.setProgress(h, got, total) }
			archive, _, ferr := update.FetchWithSig(ctx, version, goos, goarch, download)
			if ferr != nil {
				a.installFromScript(h, version)
				return
			}
			cmd, cerr := remote.InstallArchiveCommand(h.addr, version, true, h.cfg.Password)
			if cerr != nil {
				a.installFromScript(h, version)
				return
			}
			cmd.Stdin = bytes.NewReader(archive)
			out, err := cmd.CombinedOutput()
			a.update(func() {
				h.busy = ""
				if !slices.Contains(a.hosts, h) {
					return
				}
				if err != nil {
					a.toast(installError(locales.HostInstallFailed, h, out, err))
					return
				}
				a.toast(locales.HostInstalled(h.name))
				a.reconnectHost(h)
			})
		}()
		return
	}
	a.installFromScript(h, version)
}

// installFromScript installs Kero on a host with the release's script,
// which downloads the archive there, as the app's own install did.
func (a *App) installFromScript(h *host, version string) {
	cmd, err := remote.InstallCommand(h.addr, version, true, h.cfg.Password)
	if err != nil {
		a.toast(err.Error())
		return
	}
	h.busy = "install"
	h.busyVersion = version
	go func() {
		out, err := cmd.CombinedOutput()
		a.update(func() {
			h.busy = ""
			if !slices.Contains(a.hosts, h) {
				return
			}
			if err != nil {
				a.toast(installError(locales.HostInstallFailed, h, out, err))
				return
			}
			a.toast(locales.HostInstalled(h.name))
			a.reconnectHost(h)
		})
	}()
}

// installError is the message an install failed with: the program's last
// line, or the error itself.
func installError(f func(string, string) string, h *host, out []byte, err error) string {
	msg := strings.TrimSpace(string(out))
	if i := strings.LastIndex(msg, "\n"); i >= 0 {
		msg = msg[i+1:]
	}
	if msg == "" {
		msg = err.Error()
	}
	return f(h.name, msg)
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
	h.busyVersion = version
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var err error
		if what == "update" {
			// Two ways to update, and the setting picks which:
			//  - local download, then upload: the app fetches the archive
			//    through the acceleration setting, where the host could not, and sends
			//    the whole thing to the host's kerod, which installs it.
			//  - remote download: the host's kerod fetches it itself.
			platform := h.info.OS + "/" + h.info.Arch
			if a.set.LocalDownload && platform != "" && slices.Contains(update.Platforms, platform) {
				update.SetMirror(update.MirrorFor(a.set.DownloadAccel, a.set.DownloadMirror))
				a.setStage(h, "download", update.ArchiveURL(version, h.info.OS, h.info.Arch))
				download := func(got, total int64) { a.setProgress(h, got, total) }
				archive, sig, ferr := update.FetchWithSig(ctx, version, h.info.OS, h.info.Arch, download)
				if ferr == nil {
					// What the app fetched, the app now sends: the host's
					// kerod replaces its install with it.
					upload := func(sent, total int64) { a.setProgress(h, sent, total) }
					a.setStage(h, "upload", "")
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
			h.busyVersion = ""
			h.progress = 0
			h.progressStage = ""
			h.progressURL = ""
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

// setStage says which leg of a host's update the progress figure is for,
// and, for a download, the address it comes from. Synchronous, like
// setProgress, so a stage set just before a fetch is seen by the note.
func (a *App) setStage(h *host, stage, url string) {
	a.updateSync(func() {
		if slices.Contains(a.hosts, h) {
			h.progressStage = stage
			h.progressURL = url
		}
	})
}

// setProgress tells how far the app's fetch or upload of a host's archive
// has gone, for the note under its header. A total gives a real percent;
// before the response names one the fetch has begun but cannot be measured
// yet, which is -1, the note says the work started without a figure.
func (a *App) setProgress(h *host, got, total int64) {
	pct := -1
	if total > 0 {
		pct = int(100 * got / total)
		if pct > 100 {
			pct = 100
		}
	}
	a.updateSync(func() {
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
