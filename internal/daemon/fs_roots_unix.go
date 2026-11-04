//go:build !windows

package daemon

// fsRootJump is the quick jump of where the host's paths start: its root.
func fsRootJump() string {
	return "/"
}
