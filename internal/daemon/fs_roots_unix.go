//go:build !windows

package daemon

import "kero/internal/proto"

// fsRootJump is the quick jump of where the host's paths start: its root.
func fsRootJump() string {
	return "/"
}

// fsThisPC lists This PC. A Unix host has no This PC, and the picker never
// asks for one (fsRootJump is the root there), so this stays empty.
func fsThisPC() proto.FSList {
	return proto.FSList{Dir: proto.ThisPC, Home: proto.Home(), ThisPC: true, Entries: []proto.FSEntry{}}
}
