package daemon

import "golang.org/x/sys/windows"

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
