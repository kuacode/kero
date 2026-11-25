package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// testHostKey makes a host key for the callback tests: what a server would
// offer. A fresh one each call is fine — none of these tests compare two
// fingerprints, only that one is of the key asked about.
func testHostKey(t *testing.T) (ssh.PublicKey, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("could not make a test key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("could not make a public key: %v", err)
	}
	return sshPub, knownhosts.Line([]string{"h"}, sshPub)
}

// TestHostKeyUnknown asks the callback about a host it has never seen. It
// must hand the app what the user needs to check — the host, a fingerprint,
// and the exact line trusting it — and, trusted, write that line.
func TestHostKeyUnknown(t *testing.T) {
	pub, _ := testHostKey(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	cb, err := sshHostKeyCallback("h", func(info HostKeyInfo) bool {
		if info.Host != "[h]:2022" {
			t.Errorf("asked about %q, want %q", info.Host, "[h]:2022")
		}
		// A SHA-256 fingerprint looks like "SHA256:<base64>".
		if !strings.HasPrefix(info.Fingerprint, "SHA256:") {
			t.Errorf("fingerprint %q, want a SHA256: one", info.Fingerprint)
		}
		if ssh.FingerprintSHA256(pub) != info.Fingerprint {
			t.Errorf("fingerprint %q is not the key's %q", info.Fingerprint, ssh.FingerprintSHA256(pub))
		}
		return true // the user trusts the host
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("h:2022", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2022}, pub); err != nil {
		t.Fatalf("a trusted first key: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatalf("known_hosts not written: %v", err)
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "[h]:2022 ") || !strings.Contains(line, pub.Type()) {
		t.Errorf("known_hosts line %q is not the host's key", line)
	}
}

// TestHostKeyUntrusted refuses a first key the user does not trust: nothing
// is written, and the error is the app's ErrAuth.
func TestHostKeyUntrusted(t *testing.T) {
	pub, _ := testHostKey(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cb, err := sshHostKeyCallback("h", func(HostKeyInfo) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("h:2022", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2022}, pub); !errors.Is(err, ErrAuth) {
		t.Errorf("a refused first key: %v, want ErrAuth", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "known_hosts")); !os.IsNotExist(err) {
		t.Errorf("a refused key wrote known_hosts: %v", err)
	}
}

// TestHostKeyNoOneToAsk refuses a first key when there is no one to ask —
// a non-interactive dial has no window over it — rather than trust unheard.
func TestHostKeyNoOneToAsk(t *testing.T) {
	pub, _ := testHostKey(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cb, err := sshHostKeyCallback("h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("h:2022", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2022}, pub); !errors.Is(err, ErrAuth) {
		t.Errorf("an unasked first key: %v, want ErrAuth", err)
	}
}

// TestHostKeyMismatch refuses a key that is known and different: that is
// what a man in the middle looks like, so it is never asked about and
// never added over.
func TestHostKeyMismatch(t *testing.T) {
	pub, _ := testHostKey(t)
	other, _ := testHostKey(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// The host is on record with one key...
	line := knownhosts.Line([]string{"h"}, pub)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	asked := false
	cb, err := sshHostKeyCallback("h", func(HostKeyInfo) bool {
		asked = true
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	// ...and offers another: refused, and the user is not asked.
	if err := cb("h:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, other); !errors.Is(err, ErrHostKeyMismatch) {
		t.Errorf("a changed key: %v, want ErrHostKeyMismatch", err)
	}
	if asked {
		t.Error("a mismatched key was asked about; it must be refused outright")
	}
}

// TestHostKeyNonDefaultPortEntry is the entry a first connection to a host
// on a port other than 22 writes: known_hosts names it "[host]:port", as
// OpenSSH does, so the key is tied to that address and port.
func TestHostKeyNonDefaultPortEntry(t *testing.T) {
	pub, _ := testHostKey(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cb, err := sshHostKeyCallback("h", func(HostKeyInfo) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("h:2022", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2022}, pub); err != nil {
		t.Fatalf("a trusted first key: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatalf("known_hosts not written: %v", err)
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "[h]:2022 ") {
		t.Errorf("known_hosts line %q is not for [h]:2022", line)
	}
	// knownhosts accepts the file we wrote and knows the host by it, so a
	// later connection to the same address is let in without asking again.
	if _, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts")); err != nil {
		t.Errorf("the written known_hosts is not readable: %v", err)
	}
	// The host key the callback checks is the one just trusted, matched
	// against the entry under the address it was written for.
	kh, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := kh("h:2022", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2022}, pub); err != nil {
		t.Errorf("the host just trusted is not known on a reconnect: %v", err)
	}
}
