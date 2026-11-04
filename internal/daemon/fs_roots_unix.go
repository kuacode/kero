//go:build !windows

package daemon

// fsRoots lists the root directory, where a Unix host's paths start.
func fsRoots() []string {
	return []string{"/"}
}
