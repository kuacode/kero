package main

import (
	"testing"

	"kero/locales"
)

// The restart a host's header offers is of the service it runs as: this
// machine's is restarted as the service it is, the app asking it itself,
// while another host's is restarted over the connection to it. Each host
// takes its own path.
func TestSidebarHostRestartRouting(t *testing.T) {
	a := testApp(t)
	local := a.local()
	local.conn = connReady
	local.daemonApp = "2.0.0"
	local.daemonFP = "old"

	// This machine: the app restarts the service itself.
	a.offerHostRestart(local)
	if local.conn != connConnecting {
		t.Fatalf("this machine's restart is not of the service, conn=%v", local.conn)
	}

	// Another host: the app asks it over the connection.
	build := withHost(t, a, "build")
	build.daemonApp = "2.0.0"
	build.info.Kero = "2.1.0"
	a.confirm = nil
	a.offerHostRestart(build)
	if a.confirm == nil {
		t.Fatalf("a remote host's restart is not asked first")
	}
	if a.confirm.action != locales.CommonRestartService() {
		t.Errorf("ask is not of a service restart, action=%q", a.confirm.action)
	}
}
