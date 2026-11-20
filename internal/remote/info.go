package remote

import (
	"os"
	"os/user"
	"runtime"
	"strings"

	"kero/internal/build"
	"kero/internal/proto"
	"kero/internal/update"
)

// LocalInfo describes this machine to a remote client, which peer names.
func LocalInfo(peer string) proto.HostInfo {
	h := proto.HostInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Home: proto.Home(),
		Kero: build.Version(), Version: proto.Version, Server: build.Server, Peer: peer,
		Updates: build.Server && update.Managed(update.InstallDir())}
	h.Hostname, _ = os.Hostname()
	h.Distro = DistroID()
	if u, err := user.Current(); err == nil {
		h.User = u.Username
	}
	return h
}

// DistroID is the Linux distribution /etc/os-release names, as its ID, or
// "" on a non-Linux machine and for a system that tells none: the mark
// before a host's name falls back to the OS's when a distro is unknown.
func DistroID() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
