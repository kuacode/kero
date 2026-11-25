package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"kero/internal/shell"
	"kero/internal/update"
)

// remoteCmd is an install or probe command the app runs. Its ssh form is
// carried out in-process (see gossh.go); its wsl form runs wsl.exe. It
// exposes the fields callers set (Stdin, Stdout, Stderr) and the Run /
// Output / CombinedOutput they use, so neither needs to know the transport.
type remoteCmd struct {
	addr     Addr
	password string
	command  string // ssh: the remote command; wsl: unused
	exec     *exec.Cmd
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (c *remoteCmd) run() error {
	if c.exec != nil {
		c.exec.Stdin, c.exec.Stdout, c.exec.Stderr = c.Stdin, c.Stdout, c.Stderr
		return c.exec.Run()
	}
	_, _, err := sshExecFn(context.Background(), c.addr, c.password, c.command, c.Stdin, c.Stdout, c.Stderr)
	return err
}

func (c *remoteCmd) Run() error { return c.run() }

func (c *remoteCmd) Output() ([]byte, error) {
	if c.exec != nil {
		c.exec.Stdin, c.exec.Stderr = c.Stdin, c.Stderr
		return c.exec.Output()
	}
	out, _, err := sshExecFn(context.Background(), c.addr, c.password, c.command, c.Stdin, nil, c.Stderr)
	return out, err
}

func (c *remoteCmd) CombinedOutput() ([]byte, error) {
	if c.exec != nil {
		c.exec.Stdin = c.Stdin
		return c.exec.CombinedOutput()
	}
	out, errout, err := sshExecFn(context.Background(), c.addr, c.password, c.command, c.Stdin, nil, nil)
	return append(out, errout...), err
}

// InstallCommand is the command that installs the server build of Kero,
// of version ("" the latest), on a host reached over SSH or WSL: the
// release's install script, which puts it in ~/.local, or on Windows in
// %LOCALAPPDATA%\Programs, and runs kerod as a service of the user's. It
// downloads the script whole before it runs it, so that a failed download
// fails the command.
//
// A host is of Windows when a connection found it so (Windows): a dial
// before tells, as the app's has.
func InstallCommand(a Addr, version string, batch bool, password string) (*remoteCmd, error) {
	if version != "" {
		version = strings.TrimPrefix(version, "v")
		if !update.ValidVersion(version) {
			return nil, errors.New(version + " is not a version of Kero")
		}
	}
	switch a.Scheme {
	case "ssh":
		run := "sh -c " + shellQuote(shInstall(version))
		if Windows(a) {
			run = encodedPowerShell(powershellInstall(version))
		}
		return &remoteCmd{addr: a, password: password, command: run}, nil
	case "wsl":
		if runtime.GOOS != "windows" {
			return nil, errors.New("wsl:// addresses are of Windows")
		}
		wa := []string{}
		if a.Host != "" {
			wa = append(wa, "-d", a.Host)
		}
		cmd := exec.Command("wsl.exe", append(wa, "--exec", "sh", "-c", shInstall(version))...)
		hideWindow(cmd)
		return &remoteCmd{exec: cmd}, nil
	default:
		return nil, errors.New("Kero installs itself on a host over SSH or WSL: install it there with the script of its releases")
	}
}

// Platform is the operating system and architecture of a host reached
// over SSH, as uname tells it, for the app to fetch the right archive
// and install it there. A host with no kero, or one a connection has not
// found yet, tells its platform with a command of its own. Windows maps
// its uname.
func Platform(a Addr, batch bool, password string) (string, string, error) {
	switch a.Scheme {
	case "ssh":
		run := "sh -c " + shellQuote(`uname -s; uname -m`)
		out, err := sshRun(context.Background(), a, password, run, nil)
		if err != nil {
			return "", "", err
		}
		return parsePlatform(string(out))
	case "wsl":
		return "linux", "amd64", nil // WSL is Windows' Linux; its arch the app's is, as WSL runs it.
	default:
		return "", "", errors.New("no way to tell the platform of " + a.String())
	}
}

// parsePlatform is uname's answer: "Linux\nx86_64" and the like, as a
// goos and goarch pair. A host of Windows' uname maps as it does.
func parsePlatform(out string) (string, string, error) {
	lines := strings.Fields(out)
	if len(lines) < 2 {
		return "", "", errors.New("uname told nothing of the platform")
	}
	goos := map[string]string{"Linux": "linux", "Darwin": "darwin"}[lines[0]]
	arch := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[lines[1]]
	if goos == "" || arch == "" {
		return "", "", fmt.Errorf("the host is %s %s, which Kero's server build is not made for", lines[0], lines[1])
	}
	return goos, arch, nil
}

// InstallArchiveCommand is the command that installs the server build of
// Kero, of version, on a host reached over SSH or WSL, from an archive the
// app fetched and verified: the archive is written to the host, which
// installs it with install-server.sh --archive, with no download of its
// own. Only sh hosts: Windows has no archive of the app's (install.go's
// InstallCommand still runs the script there, which downloads).
func InstallArchiveCommand(a Addr, version string, batch bool, password string) (*remoteCmd, error) {
	version = strings.TrimPrefix(version, "v")
	if !update.ValidVersion(version) {
		return nil, errors.New(version + " is not a version of Kero")
	}
	switch a.Scheme {
	case "ssh":
		if Windows(a) {
			return nil, errors.New("Kero installs over SSH on Windows with the script of its releases, which downloads")
		}
		// The archive comes on the command's standard input: the app writes
		// it, and the host writes it to a temp file before it installs, as
		// a download cut short could stop it anywhere.
		run := "sh -c " + shellQuote(shInstallArchive(version))
		return &remoteCmd{addr: a, password: password, command: run}, nil
	case "wsl":
		if runtime.GOOS != "windows" {
			return nil, errors.New("wsl:// addresses are of Windows")
		}
		// wsl.exe forwards its standard input to the distro's shell, so
		// the archive the app fetched streams in just as it does over
		// ssh: the distro installs it without a download of its own.
		cmd := exec.Command("wsl.exe", wslInstallArchiveArgs(a, version)...)
		hideWindow(cmd)
		return &remoteCmd{exec: cmd}, nil
	default:
		return nil, errors.New("Kero installs itself on a host over SSH or WSL: install it there with the script of its releases")
	}
}

// wslInstallArchiveArgs is wsl.exe's arguments for an install that streams
// the archive on standard input. It is kept apart from the platform guard
// so the command can be checked where there is no wsl.exe.
func wslInstallArchiveArgs(a Addr, version string) []string {
	args := []string{}
	if a.Host != "" {
		args = append(args, "-d", a.Host)
	}
	return append(args, "--exec", "sh", "-c", shInstallArchive(version))
}

// shInstallArchive is the script of sh that installs Kero from an archive
// the app streams on its standard input: install-server.sh, which it
// writes to a file and runs with --archive, so no download of the host's.
func shInstallArchive(version string) string {
	return `set -e; f="$(mktemp)"; trap 'rm -f "$f"' EXIT; cat >"$f"; ` +
		`url=` + shellQuote(update.InstallScript(version)) + `; s="$(mktemp)"; trap 'rm -f "$s" "$f"' EXIT; ` +
		`if command -v curl >/dev/null 2>&1; then curl -fsSL "$url" -o "$s"; ` +
		`elif command -v wget >/dev/null 2>&1; then wget -qO "$s" "$url"; ` +
		`else echo "installing Kero needs curl or wget" >&2; exit 1; fi; ` +
		`sh "$s" --archive "$f" --version ` + shellQuote(version)
}

// shInstall is the script of sh that installs Kero: install-server.sh, or
// on Windows, whose SSH server may run Git's sh, install-server.ps1.
func shInstall(version string) string {
	args := ""
	if version != "" {
		args = " --version " + version
	}
	return `case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) exec ` + encodedPowerShell(powershellInstall(version)) + `;; esac; ` +
		`set -e; url=` + shellQuote(update.InstallScript(version)) + `; f="$(mktemp)"; trap 'rm -f "$f"' EXIT; ` +
		`if command -v curl >/dev/null 2>&1; then curl -fsSL "$url" -o "$f"; ` +
		`elif command -v wget >/dev/null 2>&1; then wget -qO "$f" "$url"; ` +
		`else echo "installing Kero needs curl or wget" >&2; exit 1; fi; sh "$f"` + args
}

// powershellInstall is the script of PowerShell that installs Kero on
// Windows: install-server.ps1, which it runs from memory, as no policy of
// PowerShell's on scripts then stops it.
func powershellInstall(version string) string {
	args := ""
	if version != "" {
		args = " -Version " + shell.PSQuote(version)
	}
	return `$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue'; ` +
		`[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12; ` +
		`$s = (New-Object Net.WebClient).DownloadString(` + shell.PSQuote(update.InstallScriptPS(version)) + `); ` +
		`& ([scriptblock]::Create($s))` + args
}

// encodedPowerShell is a command, for cmd and PowerShell alike, that runs
// a script of PowerShell: encoded, as neither shell then reads any of it.
func encodedPowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

// FindShell finds whether a host reached over SSH runs commands with a
// shell of Windows, as Windows then reports, for InstallCommand: it runs
// sh there, which they have not. A connection finds it too. It fails as an
// install would.
func FindShell(a Addr, batch bool, password string) error {
	if a.Scheme != "ssh" || Windows(a) {
		return nil
	}
	_, errout, err := sshExecFn(context.Background(), a, password, "sh -c 'exit 0'", os.Stdin, nil, nil)
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		if sh := windowsShell(ee.ExitStatus(), string(errout)); sh != "" {
			shells.Store(a.String(), sh)
			return nil
		}
	}
	if err != nil && strings.TrimSpace(string(errout)) != "" {
		return errors.New(strings.TrimSpace(string(errout)))
	}
	return err
}
