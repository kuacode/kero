package main

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"kero/locales"
)

// The palette finds commands by their keywords and English names, in a
// language whose titles have neither.
func TestPaletteKeywords(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	a.openPalette()
	tt.Frame()
	for query, want := range map[string]string{
		"browser":  locales.In("zh-Hans").ActionNewBrowser(),
		"adblock":  locales.In("zh-Hans").PaletteSettingsPage(locales.In("zh-Hans").PageBrowser()),
		"maximize": locales.In("zh-Hans").ActionZoomPane(),
	} {
		a.pal.query = ""
		tt.Frame()
		tt.Type(query)
		tt.Frame()
		if !tt.HasText(want) {
			t.Errorf("%q finds no %q; texts %q", query, want, tt.Texts())
		}
	}
	// Keywords match by their starts, not as letters strewn through them.
	if keywords("bsr", actionWords["pane.browser"]) >= 0 {
		t.Error("keywords matched letters in order")
	}
}

// An address, or name=address, typed in the palette offers a host to add,
// where a bare word does not, and choosing it adds the host and connects.
func TestPaletteAddHost(t *testing.T) {
	a := testApp(t)
	a.openPalette()
	for _, q := range []string{"me@build", "ssh://me@build", "dbox=192.168.1.5", "build=me@build"} {
		a.pal.query = q
		if it := a.paletteItems(); !slices.ContainsFunc(it, func(x paletteItem) bool { return x.title == locales.PaletteAddHost() }) {
			t.Errorf("%q: no Add Host in the palette", q)
		}
	}
	// A bare word is a command, not a host.
	a.pal.query = "git"
	if it := a.paletteItems(); slices.ContainsFunc(it, func(x paletteItem) bool { return x.title == locales.PaletteAddHost() }) {
		t.Error("a bare word offered a host to add")
	}
	// Choosing it adds the host and connects at once.
	a.pal.query = "me@build"
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if h := a.set.Host("build"); h == nil {
		t.Errorf("no host added: %v", a.set.Hosts)
	}
	// The app follows the configuration's hosts when its window shows
	// them; the tester has none, so the hosts follow it here.
	a.syncHosts()
	added := a.hostNamed("build")
	if added == nil {
		t.Fatal("added host not in the app")
	}
	if !a.deviceShown(added) {
		t.Error("added host not shown")
	}
}

// The palette offers the modes of Keep computer awake, found by their
// English names in any language, but for the one it is in.
func TestPaletteKeepAwake(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	zh := locales.In("zh-Hans")
	title := func(mode string) string { return zh.AwakeTitle() + ": " + awakeModeIn(zh, mode) }
	offers := func(mode string) *paletteItem {
		for _, it := range a.paletteItems() {
			if it.title == title(mode) {
				return &it
			}
		}
		return nil
	}
	a.openPalette()
	tt.Frame()
	tt.Type("while agents")
	tt.Frame()
	if !tt.HasText(title(awakeAgents)) {
		t.Errorf("no %q found; texts %q", title(awakeAgents), tt.Texts())
	}
	if offers(awakeOff) != nil {
		t.Error("the palette offers the mode it is in")
	}
	offers(awakeAgents).run()
	if a.set.KeepAwake != awakeAgents {
		t.Errorf("the mode is %q, want %q", a.set.KeepAwake, awakeAgents)
	}
	if offers(awakeAgents) != nil || offers(awakeOff) == nil {
		t.Error("the palette's modes did not follow the choice")
	}
}

// The wheel scrolls the palette's list away from the one chosen.
func TestPaletteScrolls(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 900)
	a.openPalette()
	tt.Frame()
	r, ok := tt.Find(a.paletteItems()[3].title)
	if !ok {
		t.Fatalf("no commands shown; texts %q", tt.Texts())
	}
	tt.Scroll(r.X+20, r.Y, 0, 300)
	tt.Frame()
	tt.Frame()
	if y := a.pal.scroll.Y; y < 100 {
		t.Errorf("the wheel scrolled the palette to %g, want 300", y)
	}
}

// Return presses the action of a confirmation, though Cancel comes first.
func TestConfirmReturnRunsAction(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	ran := false
	a.confirm = &confirmation{
		open:   true,
		title:  "Close this pane?",
		detail: "claude is still running in it.",
		action: "Close Pane",
		run:    func() { ran = true },
	}
	tt.Frame()
	if !tt.Focused("Close Pane") {
		t.Fatal("the action button does not have the focus")
	}
	tt.Key(0, ui.KeyEnter)
	if !ran || a.confirm != nil {
		t.Fatalf("Return ran the action %v and left the confirmation %v", ran, a.confirm)
	}
}

// Escape cancels a confirmation.
func TestConfirmEscapeCancels(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	ran := false
	a.confirm = &confirmation{
		open:   true,
		title:  "Close this pane?",
		detail: "claude is still running in it.",
		action: "Close Pane",
		run:    func() { ran = true },
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if ran || a.confirm != nil {
		t.Fatalf("Escape ran the action %v and left the confirmation %v", ran, a.confirm)
	}
}
