package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ConfigAliases lists the Host aliases in the user's own ~/.ssh/config,
// for the quick Add a Host dialog to offer.  It reads that one file and
// nothing else: no ssh.exe, no FollowSymlink, no drop-in directories, so
// a wild-card Include line cannot make it reach past the file.  A
// missing file is not an error; the answer is simply empty.
func ConfigAliases() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return configAliasesFile(filepath.Join(home, ".ssh", "config"))
}

// configAliasesFile reads one OpenSSH client configuration for the
// aliases of its Host lines, keeping the order they are written in and
// dropping repeats.
func configAliasesFile(path string) ([]string, error) {
	hosts, err := readSSHConfig(path)
	if err != nil {
		return nil, err
	}
	var got []string
	for _, h := range hosts {
		if strings.ContainsAny(h.pattern, "*?") {
			continue
		}
		for _, alias := range strings.Fields(h.pattern) {
			alias = strings.TrimRight(alias, "=")
			if alias == "" || strings.ContainsAny(alias, "?!~") || strings.EqualFold(alias, "local") {
				continue
			}
			if !slices.Contains(got, alias) {
				got = append(got, alias)
			}
		}
	}
	return got, nil
}

// sshHost is one Host block of ~/.ssh/config: its patterns and the fields
// of this app's interest, in the order OpenSSH reads them, first value
// winning.
type sshHost struct {
	pattern      string
	hostname     string
	user         string
	port         string
	identityFile []string
}

// sshConfig is the parts of ~/.ssh/config that change how a client dials:
// the real host behind an alias, the login, the port, the keys to try.
type sshConfig struct {
	HostName     string
	User         string
	Port         string
	IdentityFile []string
}

// configFor reads the Host settings that apply to name, from ~/.ssh/config.
// Only the file itself is read: no Include, so a Host line cannot reach
// past it. OpenSSH's first-value-wins rule and the "*" pattern are honored.
// A missing file, or no matching Host, is not an error: the answer is empty
// and the caller uses the address as given.
func configFor(name string) (sshConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return sshConfig{}, err
	}
	hosts, err := readSSHConfig(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return sshConfig{}, err
	}
	var out sshConfig
	seen := map[string]bool{}
	for _, h := range hosts {
		if !hostMatches(h.pattern, name) {
			continue
		}
		if h.hostname != "" && !seen["hostname"] {
			out.HostName, seen["hostname"] = h.hostname, true
		}
		if h.user != "" && !seen["user"] {
			out.User, seen["user"] = h.user, true
		}
		if h.port != "" && !seen["port"] {
			out.Port, seen["port"] = h.port, true
		}
		for _, id := range h.identityFile {
			if !slices.Contains(out.IdentityFile, id) {
				out.IdentityFile = append(out.IdentityFile, id)
			}
		}
	}
	return out, nil
}

// hostMatches tells whether an OpenSSH Host pattern names a host, as
// ssh_config(5) does: a comma or space separated list, "*" and "?" wild
// cards, and "!" negations. A pattern of only negations never matches.
func hostMatches(pattern, name string) bool {
	matched, negated := false, false
	for _, p := range strings.FieldsFunc(pattern, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "!") {
			if wildcardMatch(strings.TrimPrefix(p, "!"), name) {
				return false
			}
			negated = true
			continue
		}
		if wildcardMatch(p, name) {
			matched = true
		}
	}
	return matched && !negated
}

// wildcardMatch matches an OpenSSH Host pattern's "*" and "?" against a
// name; other characters are literal.
func wildcardMatch(pattern, name string) bool {
	for pattern != "" {
		switch pattern[0] {
		case '*':
			pattern = pattern[1:]
			if pattern == "" {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if wildcardMatch(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '?':
			if name == "" {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		default:
			if name == "" || pattern[0] != name[0] {
				return false
			}
			pattern, name = pattern[1:], name[1:]
		}
	}
	return name == ""
}

// readSSHConfig reads one OpenSSH client configuration, splitting it into
// Host blocks. Keywords before the first Host are kept in a block with an
// empty pattern, which matches nothing: OpenSSH applies such globals to
// every host, but the app does not use any of them.
func readSSHConfig(path string) ([]sshHost, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []sshHost
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// A keyword's argument follows a space or an "=", or both.
		i := strings.IndexAny(line, " \t=")
		if i < 0 {
			continue
		}
		key := strings.ToLower(line[:i])
		val := strings.Trim(strings.TrimLeft(line[i:], " \t="), `"'`)
		if key == "host" {
			out = append(out, sshHost{pattern: val})
			continue
		}
		if len(out) == 0 {
			continue
		}
		h := &out[len(out)-1]
		switch key {
		case "hostname":
			if h.hostname == "" {
				h.hostname = val
			}
		case "user":
			if h.user == "" {
				h.user = val
			}
		case "port":
			if h.port == "" && validConfigPort(val) {
				h.port = val
			}
		case "identityfile":
			h.identityFile = append(h.identityFile, expandHome(val))
		}
	}
	return out, sc.Err()
}

func validConfigPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

// expandHome expands a leading "~/" of an IdentityFile with the user's
// home, as OpenSSH does; other paths are left alone.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
