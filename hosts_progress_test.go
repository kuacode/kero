package main

import (
	"testing"

	"kero/internal/settings"
	"kero/locales"
)

// setProgress turns a fetched or uploaded byte count into a percent for
// the note under a host's header: a real percent once the response names
// a size, never past 100, and unmeasured (-1) before one does.
func TestSetProgress(t *testing.T) {
	a := testApp(t)
	h := a.local()

	a.setProgress(h, 45, 100)
	if h.progress != 45 {
		t.Errorf("45 of 100 reads %d%%, want 45", h.progress)
	}

	a.setProgress(h, 150, 100)
	if h.progress != 100 {
		t.Errorf("a count past the total clamps to %d, want 100", h.progress)
	}

	a.setProgress(h, 40, 0)
	if h.progress != -1 {
		t.Errorf("a fetch with no size yet reads %d, want -1", h.progress)
	}

	// A host that has gone away takes no figure.
	ghost := &host{name: "gone"}
	a.setProgress(ghost, 5, 10)
	if ghost.progress != 0 {
		t.Errorf("a host off the sidebar took a progress of %d", ghost.progress)
	}
}

// setStage names the leg of an update and the address a download comes
// from; clearProgress ends both, along with the figure.
func TestProgressStage(t *testing.T) {
	a := testApp(t)
	h := a.local()
	a.setStage(h, "download", "https://gh-proxy.com/archive.tar.gz")
	if h.progressStage != "download" || h.progressURL != "https://gh-proxy.com/archive.tar.gz" {
		t.Errorf("setStage did not stick: stage=%q url=%q", h.progressStage, h.progressURL)
	}
	a.setProgress(h, 45, 100)
	a.clearProgress(h)
	if h.progress != 0 || h.progressStage != "" || h.progressURL != "" {
		t.Errorf("clearProgress left stage=%q url=%q progress=%d", h.progressStage, h.progressURL, h.progress)
	}
}

// The note under a host's installing or updating header reads the real
// percent once it is known, and that the work started before a size can
// be measured, rather than a misleading zero.
func TestHostProgressDetail(t *testing.T) {
	h := &host{name: "build"}
	h.progress = 45
	if got := hostProgressDetail(h); got != "45%" {
		t.Errorf("progress 45 reads %q, want \"45%%\"", got)
	}

	h.progress = -1
	if got := hostProgressDetail(h); got != locales.HostProgressStarting() {
		t.Errorf("no total reads %q, want %q", got, locales.HostProgressStarting())
	}

	// A download names the address it waits on, beside its figure.
	h.progressStage = "download"
	h.progressURL = "https://gh-proxy.com/archive.tar.gz"
	h.progress = 45
	if got := hostProgressDetail(h); got != locales.HostProgressDownload(h.progressURL, "45%") {
		t.Errorf("download detail reads %q, want the URL and percent", got)
	}
}

// The note's title names the leg in progress, so a download and an upload
// are told apart instead of both reading "Updating".
func TestHostBusyTitle(t *testing.T) {
	h := &host{name: "build", busy: "update"}
	h.progressStage = "download"
	if got := hostBusyTitle(h); got != locales.HostUpdatingDownload(h.name) {
		t.Errorf("download title %q, want %q", got, locales.HostUpdatingDownload(h.name))
	}
	h.progressStage = "upload"
	if got := hostBusyTitle(h); got != locales.HostUpdatingUpload(h.name) {
		t.Errorf("upload title %q, want %q", got, locales.HostUpdatingUpload(h.name))
	}
	h.progressStage = ""
	if got := hostBusyTitle(h); got != locales.HostUpdating(h.name) {
		t.Errorf("plain update title %q, want %q", got, locales.HostUpdating(h.name))
	}
}

// "Download on this machine, then upload" is on by default, so a host
// that cannot reach its releases still installs.
func TestLocalDownloadDefault(t *testing.T) {
	a := testApp(t)
	if !settings.Default().LocalDownload {
		t.Error("local-download is off by default, want on")
	}
	if !a.set.LocalDownload {
		t.Error("the app's setting is off by default, want on")
	}
}
