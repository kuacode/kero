package daemon

import (
	"log"
	"os"
	"testing"
	"time"

	"kero/internal/proto"
)

// TestRestoreSessionsEndToEnd saves a daemon's state and restores it in a
// second daemon over the same dir: the session must come back, as the app
// expects after a forced exit of the first daemon (a signal stop, not a
// "shutdown", keeps the sessions for a restore).
func TestRestoreSessionsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	// The first daemon, as a forced exit leaves it: create a session and
	// save the state (as saveState does), but never a "shutdown".
	d1 := &Daemon{dir: dir, log: log.New(os.Stderr, "", 0), panes: map[string]*pane{},
		subs: map[*controlConn]struct{}{}, watchers: map[*controlConn]struct{}{},
		changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d1.viewed.Store(new(string))
	d1.projects = []proto.Project{{ID: "pr", Name: "repo", Dir: t.TempDir()}}
	s, err := d1.create(proto.CreateParams{Project: "pr", Dir: d1.projects[0].Dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(s.Panes) == 0 {
		t.Fatal("no pane in the created session")
	}
	d1.saveState()
	// The second daemon, as the next start is: same dir, restore it.
	d2 := &Daemon{dir: dir, log: log.New(os.Stderr, "", 0), panes: map[string]*pane{},
		subs: map[*controlConn]struct{}{}, watchers: map[*controlConn]struct{}{},
		changeCh: make(chan struct{}, 1), saveCh: make(chan struct{}, 1)}
	d2.viewed.Store(new(string))
	d2.restore()
	if len(d2.sessions) != 1 {
		t.Fatalf("restored %d sessions, want 1", len(d2.sessions))
	}
	got := d2.findSessionLocked(s.ID)
	if got == nil {
		t.Fatalf("session %s not found after restore", s.ID)
	}
	if got.name != s.Name {
		t.Errorf("restored name %q, want %q", got.name, s.Name)
	}
	// Let the restored pane's shell settle, then close it.
	time.Sleep(200 * time.Millisecond)
	for _, p := range got.panes {
		p.term.Close()
	}
}
