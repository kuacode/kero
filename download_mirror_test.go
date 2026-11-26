package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"kero/internal/settings"
	"kero/internal/update"
)

// A mirror named in the settings is what the downloads of releases go
// through, and it is saved with the configuration. Acceleration is on by
// default and an empty address then names the default mirror.
func TestDownloadMirrorSetting(t *testing.T) {
	a := testApp(t)
	a.ss.page = "general"
	a.ss.draft = map[string]string{}
	tt := ui.NewTester(a.settingsView, 820, 3000)
	tt.Frame()

	if !settings.Default().DownloadAccel || !a.set.DownloadAccel {
		t.Fatalf("download acceleration = %v (default %v), want on", a.set.DownloadAccel, settings.Default().DownloadAccel)
	}
	mirror := "https://gh-proxy.com/"
	if err := tt.Click("https://gh-proxy.com"); err != nil {
		t.Fatal(err)
	}
	// The field's inner input is focused; the text typed goes into it.
	tt.Type(mirror)
	for range 3 {
		tt.Frame()
	}
	// The configuration keeps the user's text as it is; SetMirror trims
	// the trailing slash when a download goes through it.
	got := settings.Load().DownloadMirror
	if got != mirror {
		t.Fatalf("saved mirror %q, want %q", got, mirror)
	}
	if a.set.DownloadMirror != got {
		t.Fatalf("app mirror %q, saved %q", a.set.DownloadMirror, got)
	}
}

// Acceleration decides whether a fetch goes through any proxy at all: off
// fetches GitHub directly, on takes the named address or the default one.
func TestMirrorAppliesToFetch(t *testing.T) {
	defer func() { update.SetMirror("") }()
	update.SetMirror(update.MirrorFor(false, "https://gh-proxy.com/"))
	if update.Mirror != "" {
		t.Fatalf("mirror %q with acceleration off, want none", update.Mirror)
	}
	update.SetMirror(update.MirrorFor(true, "https://gh-proxy.com/"))
	if update.Mirror != "https://gh-proxy.com" {
		t.Fatalf("mirror %q, want the trailing slash trimmed", update.Mirror)
	}
	s := settings.Default()
	update.SetMirror(update.MirrorFor(s.DownloadAccel, s.DownloadMirror))
	if update.Mirror != update.DefaultDownloadMirror {
		t.Fatalf("default mirror %q, want %q", update.Mirror, update.DefaultDownloadMirror)
	}
}

// The mirror carries the version lookup too: Latest reads api.github.com,
// which is as unreachable as the downloads are where a mirror helps, so a
// mirror that only carried the archive would leave the install stuck
// before it ever fetched. The archive URL shows the prefixing Latest's
// api.github.com lookup now gets.
func TestMirrorAppliesToLatest(t *testing.T) {
	defer func() { update.SetMirror("") }()
	update.SetMirror("https://gh-proxy.com/")
	if got, want := update.ArchiveURL("1.2.3", "linux", "amd64"),
		"https://gh-proxy.com/https://github.com/kuacode/kero/releases/download/v1.2.3/kero-server-1.2.3-linux-amd64.tar.gz"; got != want {
		t.Fatalf("ArchiveURL = %q, want %q", got, want)
	}
}
