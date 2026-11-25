package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/melbahja/goph/v2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// This file is Kero's SSH client: it speaks the protocol in Go instead of
// starting ssh(1). The bridge standards the same — the command it runs on
// the host, the mux over stdio — only the transport changes, so a host
// needs no Kero-side knowledge of which client reached it.

// sshDialFn and sshExecFn are the seams tests replace with in-process fakes:
// Go SSH has no ssh(1) executable to point KERO_SSH at. Production uses the
// real functions; a test setting one restores it with t.Cleanup.
var sshDialFn = sshDial
var sshExecFn = sshExec

// HostKeyInfo is what the app needs to ask its user about a host key it has
// not seen before: which host, a fingerprint to check against one the host's
// owner publishes, and the known_hosts entry trusting it writes.
type HostKeyInfo struct {
	Host        string // the address the key belongs to, as "[host]:port" (a bare host on port 22)
	Fingerprint string // SHA-256 fingerprint of the key
	KnownLine   string // the exact line to append to known_hosts should the user trust it
}

// HostKeyAsk is how the app answers a first-seen host key: it asks its user,
// and reports whether they chose to trust the host. It runs on the main
// thread's terms — the transport calls it from a dial, which may be on a
// goroutine — and it returns only once the user has answered.
type HostKeyAsk func(HostKeyInfo) bool

// trustHost appends a trusted host-key line to the user's known_hosts,
// creating ~/.ssh as OpenSSH would. It returns the file it wrote to.
func trustHost(line string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ssh")
	// A private directory, as ssh-keygen makes it: the keys and the hosts
	// trusted are the user's alone.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "known_hosts")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return "", err
	}
	return path, nil
}

// sshClientConfig builds what a Go SSH dial needs for a: the login, the
// keys or password to offer, and the known_hosts check. Password, when
// given, is the only method tried, once, as an app's stored credential;
// without one the client offers the agent and the identity files, and
// never prompts.
func sshClientConfig(a Addr, password string, ask HostKeyAsk) (*ssh.ClientConfig, error) {
	cfg, err := configFor(a.Host)
	if err != nil {
		return nil, err
	}
	host := a.Host
	if cfg.HostName != "" {
		host = cfg.HostName
	}
	login := a.User
	if login == "" {
		login = cfg.User
	}
	if login == "" {
		if u, err := user.Current(); err == nil {
			login = u.Username
		} else {
			login = os.Getenv("USER")
			if login == "" {
				login = os.Getenv("USERNAME")
			}
		}
	}
	port := a.Port
	if port == "" {
		port = cfg.Port
	}
	if port == "" {
		port = "22"
	}
	// Dial is given the address the user wrote, but the host key is checked
	// against the real one an alias resolves to.
	known, err := sshHostKeyCallback(host, ask)
	if err != nil {
		return nil, err
	}
	c := &ssh.ClientConfig{
		User:            login,
		HostKeyCallback: known,
		Timeout:         15 * time.Second,
	}
	if password != "" {
		c.Auth = []ssh.AuthMethod{ssh.Password(password)}
		return c, nil
	}
	var keys []ssh.Signer
	for _, key := range sshIdentityFiles(cfg) {
		b, err := os.ReadFile(key)
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(b)
		if err != nil {
			continue // encrypted keys need a passphrase, which we never ask for
		}
		keys = append(keys, s)
	}
	if len(keys) > 0 {
		c.Auth = append(c.Auth, ssh.PublicKeys(keys...))
	}
	if signers := sshAgentSigners(); signers != nil {
		c.Auth = append(c.Auth, ssh.PublicKeysCallback(signers))
	}
	return c, nil
}

// sshAddr is where the TCP connection goes after an alias is resolved.
func sshAddr(a Addr) (string, error) {
	cfg, err := configFor(a.Host)
	if err != nil {
		return "", err
	}
	host := a.Host
	if cfg.HostName != "" {
		host = cfg.HostName
	}
	port := a.Port
	if port == "" {
		port = cfg.Port
	}
	if port == "" {
		port = "22"
	}
	return net.JoinHostPort(host, port), nil
}

// sshHostKeyCallback checks a host's key against the user's known_hosts.
// A key never seen before is not an error the app fails on: it is shown to
// the user through ask, who trusts it or not, and trusting writes it to
// known_hosts as OpenSSH's first-connection prompt would. A key that is
// known but different is refused outright — that is the sign of a host
// that changed its key, or of one in the middle — and is never asked about.
// The host the key is checked against, host, is what the refusals name.
func sshHostKeyCallback(host string, ask HostKeyAsk) (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".ssh")
	var files []string
	for _, name := range []string{"known_hosts", "known_hosts2"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}
	base, err := knownhosts.New(files...)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err // not a key question: a malformed line or such
		}
		// A key that is on record and different: possibly a man in the
		// middle. Say so and refuse; the app never adds a key over it.
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("%w: %v", ErrHostKeyMismatch, err)
		}
		// A key never seen: this is the first-connection question, asked
		// only when there is someone to ask and something to check.
		fingerprint := ssh.FingerprintSHA256(key)
		// The hostname the handshake checked is already "host:port" (a
		// bare host on port 22); that is how known_hosts names the key.
		name := knownhosts.Normalize(hostname)
		entry := knownhosts.Line([]string{name}, key)
		if ask == nil {
			// No one to ask — a non-interactive dial, or a host key the
			// app has no window over yet: refuse as OpenSSH in batch
			// mode would, rather than trust unheard.
			return fmt.Errorf("%w: %s is not trusted yet", ErrAuth, host)
		}
		if !ask(HostKeyInfo{Host: name, Fingerprint: fingerprint, KnownLine: entry}) {
			return fmt.Errorf("%w: %s is not trusted", ErrAuth, host)
		}
		// Trusted: remember it, then let this and later connections in.
		if _, err := trustHost(entry); err != nil {
			return fmt.Errorf("could not add %s to known_hosts: %w", host, err)
		}
		return nil
	}, nil
}

// sshIdentityFiles are the keys a Go client tries, in order: those of an
// IdentityFile in ~/.ssh/config, then OpenSSH's defaults.
func sshIdentityFiles(cfg sshConfig) []string {
	var out []string
	for _, f := range cfg.IdentityFile {
		if !contains(out, f) {
			out = append(out, f)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			f := filepath.Join(home, ".ssh", name)
			if !contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// sshAgentSigners returns a callback over the user's SSH agent, or nil
// when there is none. On Unix SSH_AUTH_SOCK names a socket; on Windows it
// names a named pipe, which os.OpenFile opens by the same path.
func sshAgentSigners() func() ([]ssh.Signer, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		sock = shellAgent()
	}
	if sock == "" {
		return nil
	}
	var rw io.ReadWriteCloser
	if runtime.GOOS == "windows" {
		f, err := os.OpenFile(sock, os.O_RDWR, 0)
		if err != nil {
			return nil
		}
		rw = f
	} else {
		c, err := net.DialTimeout("unix", sock, 3*time.Second)
		if err != nil {
			return nil
		}
		rw = c
	}
	a := agent.NewClient(rw)
	return func() ([]ssh.Signer, error) {
		s, err := a.Signers()
		if err != nil {
			rw.Close()
		}
		return s, err
	}
}

// sshDial opens a Go SSH connection to a, in ctx. The TCP dial and the
// protocol handshake are goph's; the config it is given is ours, keeping
// this app's alias, known_hosts and authentication policy in one place.
func sshDial(ctx context.Context, a Addr, password string, ask HostKeyAsk) (*ssh.Client, error) {
	conf, err := sshClientConfig(a, password, ask)
	if err != nil {
		return nil, err
	}
	addr, err := sshAddr(a)
	if err != nil {
		return nil, err
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portText, 10, 32)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := &goph.Client{
		User: conf.User,
		Addr: host,
		Port: uint(port),
	}
	if err := goph.Dial(client, conf); err != nil {
		return nil, mapSSHError(err)
	}
	return client.Client, nil
}

// mapSSHError tells an authentication or host-key refusal from a failure
// to reach the host, for the app to say what to do.
func mapSSHError(err error) error {
	if err == nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) || authFailure(err.Error()) {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return err
}

// sshConn is the bytes of an SSH session's remote command: what a mux
// handshake talks over. Its stderr is kept apart for failure() to tell
// why a bridge ended.
type sshConn struct {
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	stderr tail
	waited chan error
	once   sync.Once
}

func (c *sshConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *sshConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *sshConn) Close() error {
	c.once.Do(func() {
		c.stdin.Close()
		select {
		case <-c.waited:
		case <-time.After(2 * time.Second):
		}
		c.sess.Close()
		c.client.Close()
	})
	return nil
}

// failure is why the session ended, in the app's errors. It mirrors
// cmdConn.failure, reading the command's exit status and standard error.
func (c *sshConn) failure() error {
	msg := c.stderr.last()
	var err error
	select {
	case err = <-c.waited:
	case <-time.After(3 * time.Second):
	}
	code := -1
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitStatus()
	}
	switch {
	case code == bridgeNotFound || strings.Contains(msg, "kero is not installed"):
		return fmt.Errorf("%w: %s", ErrNotInstalled, msg)
	case (code == 255 || err != nil) && authFailure(msg+" "+errText(err)):
		if len(msg) > 0 {
			return fmt.Errorf("%w: %s", ErrAuth, msg)
		}
		return fmt.Errorf("%w: %v", ErrAuth, err)
	case code == 1:
		if sh := windowsShell(code, c.stderr.all()); sh != "" {
			return &shellError{sh, err}
		}
	}
	if msg != "" {
		return errors.New(msg)
	}
	if err != nil {
		return fmt.Errorf("%s", err)
	}
	return errors.New("the SSH session ended")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// goSSHTransport runs the bridge on a host over Go's SSH, returning its
// stdio as the mux's transport. It replaces exec.Command(ssh, ...) so no
// external ssh program, askpass helper, or Windows OpenSSH is needed.
func goSSHTransport(ctx context.Context, a Addr, password string, ask HostKeyAsk) (io.ReadWriteCloser, error) {
	client, err := sshDialFn(ctx, a, password, ask)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		stdin.Close()
		sess.Close()
		client.Close()
		return nil, err
	}
	c := &sshConn{client: client, sess: sess, stdin: stdin, stdout: stdout, waited: make(chan error, 1)}
	sess.Stderr = &c.stderr
	if err := sess.Start(bridgeCommand(a, hostShell(a))); err != nil {
		c.Close()
		return nil, err
	}
	go func() { c.waited <- sess.Wait() }()
	// Keep the connection alive when sessions are quiet, as ssh's
	// ServerAliveInterval did. The mux's own pings handle a dead peer.
	go sshKeepAlive(ctx, client, c.waited)
	return c, nil
}

// sshExec runs command on a reached host over Go SSH. stdin is streamed in
// when given; stdout and stderr receive the command's output, collected as
// bytes too when the caller wants them nil. Errors are mapped to the app's
// ErrNotInstalled / ErrAuth / shellError, as a dial's are.
func sshExec(ctx context.Context, a Addr, password, command string, stdin io.Reader, stdout, stderr io.Writer) ([]byte, []byte, error) {
	client, err := sshDialFn(ctx, a, password, nil)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer sess.Close()
	var outBuf, errBuf bytes.Buffer
	sess.Stdin = stdin
	sess.Stdout = io.MultiWriter(&outBuf, writerOr(stdout))
	sess.Stderr = io.MultiWriter(&errBuf, writerOr(stderr))
	err = sess.Run(command)
	if err == nil {
		return outBuf.Bytes(), errBuf.Bytes(), nil
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(errBuf.String())
		switch {
		case ee.ExitStatus() == bridgeNotFound || strings.Contains(msg, "kero is not installed"):
			return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("%w: %s", ErrNotInstalled, msg)
		case authFailure(msg):
			return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("%w: %s", ErrAuth, msg)
		}
		if sh := windowsShell(ee.ExitStatus(), errBuf.String()); sh != "" {
			shells.Store(a.String(), sh)
			return outBuf.Bytes(), errBuf.Bytes(), &shellError{sh, err}
		}
	}
	if msg := strings.TrimSpace(errBuf.String()); msg != "" {
		return outBuf.Bytes(), errBuf.Bytes(), errors.New(msg)
	}
	return outBuf.Bytes(), errBuf.Bytes(), err
}

func writerOr(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// sshRun is sshExec for callers that only want stdout and the error.
func sshRun(ctx context.Context, a Addr, password, command string, stdin io.Reader) ([]byte, error) {
	out, _, err := sshExecFn(ctx, a, password, command, stdin, nil, nil)
	return out, err
}

// sshKeepAlive sends a keepalive every ten seconds and closes the
// connection after three unanswered ones. It ends with ctx or the session.
func sshKeepAlive(ctx context.Context, client *ssh.Client, done <-chan error) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			client.Close()
			return
		case <-done:
			return
		case <-t.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				missed++
				if missed >= 3 {
					client.Close()
					return
				}
			} else {
				missed = 0
			}
		}
	}
}
