package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallArchiveCommand tests that the app's install streams an
// archive it fetched to a host, which installs it with the script's
// --archive: the host no longer downloads the archive itself, and the
// command's standard input carries it. The host still fetches the
// install script, which no release embeds.
func TestInstallArchiveCommand(t *testing.T) {
	t.Setenv("KERO_SSH", exe(t))
	log := filepath.Join(t.TempDir(), "ssh.log")
	t.Setenv("KERO_TEST_SHELL", "sh")
	t.Setenv("KERO_TEST_SSH_LOG", log)

	a := Addr{Scheme: "ssh", Host: "devbox"}
	cmd, err := InstallArchiveCommand(a, "2.1.0", true)
	if err != nil {
		t.Fatal(err)
	}
	last := cmd.Args[len(cmd.Args)-1]
	// The script streams the archive on standard input, writes it to a
	// temp file, and installs with install-server.sh --archive FILE.
	if !strings.Contains(last, "cat >") || !strings.Contains(last, "--archive ") || !strings.Contains(last, "--version ") || !strings.Contains(last, "2.1.0") {
		t.Errorf("install does not stream an archive: %s", last)
	}
	// The host no longer downloads the archive itself: it fetches only
	// the install script, which runs --archive.
	if strings.Contains(last, "kero-server-2.1.0-linux-amd64.tar.gz") {
		t.Errorf("the host still downloads the archive: %s", last)
	}
	// A version that is none is refused before a command is made.
	if _, err := InstallArchiveCommand(a, "../../x", true); err == nil {
		t.Error("took a version that is none")
	}
}

// TestInstallArchiveCommandWindows tests that a Windows host reached over
// SSH, which has no archive of the app's, installs with the script's
// download, as it did.
func TestInstallArchiveCommandWindows(t *testing.T) {
	t.Setenv("KERO_SSH", exe(t))
	log := filepath.Join(t.TempDir(), "ssh.log")
	t.Setenv("KERO_TEST_SHELL", "powershell")
	t.Setenv("KERO_TEST_SSH_LOG", log)

	a := Addr{Scheme: "ssh", Host: "devbox"}
	// The connection a dial finds tells the host runs Windows.
	if _, err := Dial(t.Context(), a, Options{}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("dial: %v", err)
	}
	if _, err := InstallArchiveCommand(a, "2.1.0", true); err == nil {
		t.Error("a Windows host takes an archive of the app's")
	}
}

// exe is the test's executable, the ssh the tests fake.
func exe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

// TestParsePlatform tests that uname's answer maps to the goos and goarch
// Kero's server build is made for, and that a host of another platform is
// refused.
func TestParsePlatform(t *testing.T) {
	for _, tt := range []struct {
		out    string
		goos   string
		goarch string
	}{
		{"Linux\nx86_64", "linux", "amd64"},
		{"Linux\naarch64", "linux", "arm64"},
		{"Darwin\narm64", "darwin", "arm64"},
		{"Darwin\nx86_64", "darwin", "amd64"},
	} {
		goos, goarch, err := parsePlatform(tt.out)
		if err != nil {
			t.Fatalf("parsePlatform(%q): %v", tt.out, err)
		}
		if goos != tt.goos || goarch != tt.goarch {
			t.Errorf("parsePlatform(%q) = %s/%s, want %s/%s", tt.out, goos, goarch, tt.goos, tt.goarch)
		}
	}
	if _, _, err := parsePlatform("FreeBSD\nx86_64"); err == nil {
		t.Error("took a FreeBSD host")
	}
	if _, _, err := parsePlatform("Linux\nmips"); err == nil {
		t.Error("took a mips host")
	}
	if _, _, err := parsePlatform("Linux"); err == nil {
		t.Error("took uname's empty answer")
	}
}

// TestPlatform tests that a host reached over SSH tells its platform with
// uname, through the test's fake ssh.
func TestPlatform(t *testing.T) {
	t.Setenv("KERO_SSH", exe(t))
	log := filepath.Join(t.TempDir(), "ssh.log")
	t.Setenv("KERO_TEST_SHELL", "sh")
	t.Setenv("KERO_TEST_SSH_LOG", log)

	a := Addr{Scheme: "ssh", Host: "devbox"}
	goos, goarch, err := Platform(a, true)
	if err != nil {
		t.Fatal(err)
	}
	if goos != "linux" || goarch != "amd64" {
		t.Errorf("Platform = %s/%s, want linux/amd64", goos, goarch)
	}
}
