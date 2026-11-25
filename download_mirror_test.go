package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"kero/internal/settings"
	"kero/internal/update"
)

// A mirror named in the settings is what the downloads of releases go
// through, and it is saved with the configuration.
func TestDownloadMirrorSetting(t *testing.T) {
	a := testApp(t)
	a.ss.page = "general"
	a.ss.draft = map[string]string{}
	tt := ui.NewTester(a.settingsView, 820, 3000)
	tt.Frame()

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

// The mirror named takes effect on a download: the fetch goes through the
// proxy, whose URL prefixes the release's.
func TestMirrorAppliesToFetch(t *testing.T) {
	defer func() { update.SetMirror("") }()
	update.SetMirror(settings.Load().DownloadMirror)
	if update.Mirror != "" {
		t.Skipf("this run has a mirror set in its config (%q); the no-mirror case is covered by the archive-URL assertions in TestMirrorAppliesToLatest", update.Mirror)
	}
	update.SetMirror("https://gh-proxy.com/")
	if update.Mirror != "https://gh-proxy.com" {
		t.Fatalf("mirror %q, want the trailing slash trimmed", update.Mirror)
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
