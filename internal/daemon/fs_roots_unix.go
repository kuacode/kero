//go:build !windows

package daemon

import "kero/internal/proto"

// fsThisPC lists This PC. A Unix host has no This PC, and the picker never
// asks for one (its root is /), so this stays empty.
func fsThisPC() proto.FSList {
	return proto.FSList{Dir: proto.ThisPC, Home: proto.Home(), ThisPC: true, Entries: []proto.FSEntry{}}
}
