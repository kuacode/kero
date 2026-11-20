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
	update.SetMirror(settings.Load().DownloadMirror)
	if update.Mirror != "" {
		t.Fatalf("mirror %q with no setting", update.Mirror)
	}
	update.SetMirror("https://gh-proxy.com/")
	defer func() { update.SetMirror("") }()
	if update.Mirror != "https://gh-proxy.com" {
		t.Fatalf("mirror %q, want the trailing slash trimmed", update.Mirror)
	}
}
