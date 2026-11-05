package daemon

import (
	"path/filepath"

	"golang.org/x/sys/windows"

	"kero/internal/proto"
)

// fsRoots lists the drives Windows knows, one per set bit of GetLogicalDrives.
func fsRoots() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []string
	for c := 0; c < 26; c++ {
		if mask&(1<<uint(c)) != 0 {
			out = append(out, string(rune('A'+c))+`:\`)
		}
	}
	return out
}

// fsThisPC lists This PC: the drives, one per entry, each a folder of none:
// the picker's Root quick jump shows them, to open a project on one.
func fsThisPC() proto.FSList {
	out := proto.FSList{Dir: proto.ThisPC, Home: proto.Home(), ThisPC: true, Entries: []proto.FSEntry{}}
	for _, r := range fsRoots() {
		out.Entries = append(out.Entries, proto.FSEntry{Name: filepath.Clean(r), Dir: true})
	}
	return out
}
