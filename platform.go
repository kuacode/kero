package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// fileManager is the system's file manager, as buttons that show a file
// in it name it.
func fileManager() string {
	switch runtime.GOOS {
	case "darwin":
		return locales.FileManagerFinder()
	case "windows":
		return locales.FileManagerExplorer()
	}
	return locales.FileManagerFiles()
}

// openPath opens a file or folder in the app that opens it.
func openPath(path string) { mygo.Shell.OpenPath(path) }

// revealPath shows a file in the file manager.
func revealPath(path string) { mygo.Shell.ShowItemInFolder(path) }

// openTextFile opens a text file, as the configuration, which has no
// extension to tell its app by, in the text editor.
func openTextFile(path string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("/usr/bin/open", "-t", path).Start()
	case "windows":
		exec.Command("notepad.exe", path).Start()
	default:
		exec.Command("xdg-open", path).Start()
	}
}

// ownControls tells that Kero draws the window controls itself: on
// Windows and Linux the top-right card's header holds minimize, maximize
// and close, beside Kero's own buttons, where the system's, at the
// window's edge, would straddle the card. macOS keeps its traffic lights.
var ownControls = runtime.GOOS != "darwin"

// showControls tells that the window controls show (ownControls): where
// the desktop's title bars have buttons (mygo.Theme.WindowButtons), not
// under a tiling compositor, as Hyprland or Sway, which gives windows none,
// or where the user emptied the button layout, as users of tiling window
// managers on X11 do; nor under KWin on Wayland, whose own title bar has
// them. KERO_WINDOW_CONTROLS=1 or 0 shows or hides them all the same.
// followWindowButtons keeps it in step. Main thread only.
var showControls = ownControls

// followWindowButtons sets showControls, and again as the desktop's button
// layout changes. Once the app is ready.
func followWindowButtons() {
	if !ownControls {
		return
	}
	update := func() { showControls = controlsWanted(os.Getenv, mygo.Theme.WindowButtons()) }
	update()
	mygo.Theme.OnUpdated(update)
}

// controlsWanted tells that Kero should draw the window controls, where
// the desktop's title bars have buttons, unless KERO_WINDOW_CONTROLS says
// otherwise.
func controlsWanted(getenv func(string) string, buttons mygo.WindowButtons) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("KERO_WINDOW_CONTROLS"))) {
	case "1", "true", "yes", "on", "show":
		return true
	case "0", "false", "no", "off", "hide":
		return false
	}
	return !buttons.Empty()
}

// framed sets a window's frame: the title bar hidden, under the traffic
// lights on macOS and without its buttons elsewhere (ownControls), which
// keeps the edges that resize the window.
func framed(o mygo.WindowOptions) mygo.WindowOptions {
	if ownControls {
		o.TitleBarStyle = mygo.TitleBarHidden
		o.HideWindowButtons = true
		// No menu bar over the sidebar and the cards: Alt and F10 show the
		// menus, whose shortcuts work all the same.
		o.AutoHideMenuBar = true
		return o
	}
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TitleBarHeight = topBar
	// The lights' buttons are 16 tall, and MyGo puts them a DIP above the
	// position: centered on barMid, as the sidebar's buttons and the
	// panes' headers are.
	o.TrafficLightPosition = &mygo.Point{X: 18, Y: barMid - 7}
	return o
}

var (
	minimizeIcon = icon(`<path d="M3.5 8h9"/>`)
	maximizeIcon = icon(`<rect x="3.5" y="3.5" width="9" height="9" rx="1.5"/>`)
	restoreIcon  = icon(`<rect x="3.5" y="5.5" width="7" height="7" rx="1.2"/><path d="M5.5 3.5h5.5a1.5 1.5 0 0 1 1.5 1.5v5.5"/>`)
)

// controlsW is the window controls' width: three buttons, two apart.
const controlsW = 3*28 + 2*2

// windowControls are a window's minimize, maximize and close buttons, as
// Kero draws them (ownControls), at the right of a header: the Settings
// window's. The main window's are over its corner (mainControls).
func windowControls(c *ui.Context, pal *palette, w *mygo.Window) {
	if !showControls || w == nil {
		return
	}
	ui.Row(c.Key("window-controls")).Gap(2).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 6).Children(func() {
		controlButtons(c, pal, w)
	})
}

// mainControls are the main window's controls, over its top right corner,
// where they stay as the right sidebar slides in and out under them: in a
// header of the sidebar or of a card, they would slide with it, out of the
// window. edge is the right padding of the cards' column, which comes to
// nothing as the sidebar shows, the controls moving with it; the headers
// under them keep room for them (controlsRoom, rightSidebar).
func (a *App) mainControls(c *ui.Context, pal *palette, edge float32) {
	if !showControls || a.win == nil {
		return
	}
	ui.Row(c.Key("main-controls")).Absolute().Right(edge + 10).Top(barMid - 13).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
		controlButtons(c, pal, a.win)
	})
}

// controlsRoom keeps room at the right of the header at the window's top
// right, whose gap is gap, for the window controls over it (mainControls),
// as far as they come over the header: less as the right sidebar comes
// in beside the card, and none once the sidebar is wider than they are.
func (a *App) controlsRoom(c *ui.Context, gap float32) {
	if !showControls || a.win == nil {
		return
	}
	// The gap before the room is in it: it narrows to nothing too.
	w := max(controlsW+6+gap-a.side.barW, 0)
	ui.Box(c.Key("controls-room")).Width(w).Shrink(0).Margin(0, 0, 0, -gap)
}

// underControls tells that the pointer is over the window controls, over
// the header of card, w wide, at the window's top right: as they were the
// header's own, they keep its pane hovered, its buttons shown.
func underControls(card ui.Element, w float32) bool {
	if !showControls {
		return false
	}
	x, y, in := card.PointerPosition()
	return in && y < headerH && x >= w-(controlsW+10)
}

// controlButtons are the window controls' buttons.
func controlButtons(c *ui.Context, pal *palette, w *mygo.Window) {
	iconButton(c, "minimize", minimizeIcon, locales.WindowMinimize(), pal, w.Minimize)
	// The system's maximize button for the pointer, which brings
	// Windows 11's snap layouts as it rests there: no tooltip over them.
	maximized := w.IsMaximized()
	label, svg := locales.WindowMaximize(), maximizeIcon
	if maximized {
		label, svg = locales.WindowRestore(), restoreIcon
	}
	m := ui.ButtonBase(c.Key("maximize")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(label).MaximizeButton()
	if m.Pressed() {
		m.Background(pal.rowHover.Alpha(0.12))
	} else if m.Hovered() {
		m.Background(pal.rowHover).TextColor(pal.text)
	}
	m.Children(func() { ui.Icon(c, svg).Size(16, 16) })
	if m.Clicked() {
		if maximized {
			w.Unmaximize()
		} else {
			w.Maximize()
		}
	}
	b := ui.ButtonBase(c.Key("close-window")).Size(28, 26).Radius(6).Center().TextColor(pal.textMuted).Label(locales.CommonClose()).Tooltip(locales.CommonClose())
	if b.Hovered() || b.Pressed() {
		b.Background(pal.danger).TextColor(ui.RGB(255, 255, 255))
	}
	b.Children(func() { ui.Icon(c, closeIcon).Size(16, 16) })
	if b.Clicked() {
		w.Close()
	}
}

// controlsMode is the style the Settings chose for the window controls:
// builtin in the card's corner, apple as traffic lights at the left with
// the title bar's row laid out as windows', or windows at the title bar's
// right. macOS draws its own lights, so there it is always apple.
func (a *App) controlsMode() string {
	if !ownControls {
		return "apple"
	}
	return a.set.WindowControls
}

// titleBar tells that the window has a title bar of its own: the apple and
// windows styles draw one, which holds the sidebar's actions and owns the
// window's top, so the content moves below it and the sidebar, the footer
// and the right sidebar keep none of the buttons the bar already holds.
func (a *App) titleBar() bool {
	switch a.controlsMode() {
	case "apple", "windows":
		return true
	}
	return false
}

// settingsWindowControls draw the Settings window's controls, which in
// mac and windows styles move to the window itself, not the page's header.
func (a *App) settingsWindowControls(c *ui.Context, pal *palette) {
	switch a.controlsMode() {
	case "apple":
		// At the window's left, as the main window's title bar row does,
		// so both windows put the lights the same distance in. The row is
		// as tall as the main window's title bar buttons, so the lights
		// center on the same line rather than riding high.
		ui.Row(c.Key("window-controls")).Absolute().Left(8).Top(2).Height(26).Gap(6).AlignItems(ui.Center).Shrink(0).DragWindow().Children(func() {
			appleButtons(c, pal, a.setWin)
		})
	case "windows":
		ui.Row(c.Key("window-controls")).Absolute().Right(10).Top(2).Gap(2).AlignItems(ui.Center).Shrink(0).DragWindow().Children(func() {
			controlButtons(c, pal, a.setWin)
		})
	}
}

// mainControlsAny draws the main window's controls, by the style the
// Settings chose: main's own builtin controls over the card's corner, or
// apple's and windows' shared title bar's row, apple's traffic lights at
// the left and windows' controls at the right.
func (a *App) mainControlsAny(c *ui.Context, pal *palette, edge float32) {
	switch a.controlsMode() {
	case "apple", "windows":
		// The title bar's own row, by both styles: the sidebar's actions at
		// the window's left and the footer's buttons at its right, centered
		// on barMid. Apple's traffic lights sit at the window's far left,
		// before the actions, and apple's row ends at Kero's own buttons;
		// windows' own controls close its row instead.
		ui.Row(c.Key("title-actions")).Absolute().Left(8).Top(2).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.controlsMode() == "apple" {
				ui.Row(c.Key("traffic")).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					appleButtons(c, pal, a.win)
				})
				ui.Box(c.Key("lights-sep")).Width(10).Shrink(0)
			}
			// The toggle names what pressing it does, by the sidebar's state.
			sidebarTip := locales.SidebarHide()
			if a.prefs.SidebarHidden {
				sidebarTip = locales.SidebarShow()
			}
			iconButton(c, "hide-sidebar", sidebarIcon, a.tip(sidebarTip, "sidebar.toggle"), pal, a.toggleSidebar)
			iconButton(c, "new-session", plusIcon, a.tip(locales.ActionNewSession(), "session.new"), pal, func() { a.newSession("") })
		})
		ui.Row(c.Key("main-controls")).Absolute().Right(10).Top(2).Gap(2).AlignItems(ui.Center).DragWindow().Children(func() {
			// The footer's buttons join the row, to the left of the window
			// controls, which their place in the footer leaves to the row.
			// They are the title row's own, and stay whether or not the
			// sidebar hides: they no longer live in it.
			iconButton(c, "open-project", folderIcon, a.tip(locales.ActionOpenProject(), "project.open"), pal, a.openProject)
			iconButton(c, "palette", searchIcon, a.tip(locales.ActionGoToSession(), "palette.open"), pal, a.openPalette)
			iconButton(c, "keep-awake", a.keepAwakeIcon(), a.keepAwakeTip(), pal, a.toggleKeepAwake)
			iconButton(c, "settings", gearIcon, a.tip(locales.SettingsTitle(), "settings.open"), pal, func() { a.openSettings("") })
			// The toggle names what pressing it does, by the sidebar's state.
			rightTip := locales.RightSidebarHide()
			if !a.rightShown() {
				rightTip = locales.RightSidebarShow()
			}
			iconButton(c, "right-sidebar", sidebarRightIcon, a.tip(rightTip, "rightSidebar.toggle"), pal, a.toggleRight)
			if a.controlsMode() == "windows" {
				// A breath before the window controls, which read as a group
				// apart from Kero's own buttons.
				ui.Box(c.Key("controls-sep")).Width(10).Shrink(0)
				controlButtons(c, pal, a.win)
			}
		})
	default: // builtin: main's own controls over the card's corner.
		a.mainControls(c, pal, edge)
	}
}

// controlsRoomAny keeps room at the header's right only in builtin style,
// when the controls sit over it; apple and windows draw their own row.
func (a *App) controlsRoomAny(c *ui.Context, gap float32) {
	if a.controlsMode() == "builtin" {
		a.controlsRoom(c, gap)
	}
}

// underControlsAny tells that the pointer is over the window controls, in
// builtin style only, when they sit over the card's header.
func (a *App) underControlsAny(card ui.Element, w float32) bool {
	if a.controlsMode() != "builtin" {
		return false
	}
	return underControls(card, w)
}

// appleButtons are Mac-style red, yellow and green window controls.
func appleButtons(c *ui.Context, pal *palette, w *mygo.Window) {
	traffic := func(key, label string, color ui.Color, click func()) {
		b := ui.ButtonBase(c.Key(key)).Size(14, 14).Radius(7).Center().Background(color).Label(label).Tooltip(label)
		if b.Clicked() {
			click()
		}
	}
	traffic("close-window", locales.CommonClose(), ui.RGB(255, 95, 86), w.Close)
	traffic("minimize", locales.WindowMinimize(), ui.RGB(255, 189, 46), w.Minimize)
	traffic("maximize", locales.WindowMaximize(), ui.RGB(39, 201, 63), func() {
		if w.IsMaximized() {
			w.Unmaximize()
		} else {
			w.Maximize()
		}
	})
}
