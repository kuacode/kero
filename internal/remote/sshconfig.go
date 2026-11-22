package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
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
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var got []string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// OpenSSH lets a keyword's argument follow a space or an "=", or
		// both; everything after the first of those is the argument.
		i := strings.IndexAny(line, " \t=")
		if i < 0 {
			continue
		}
		if !strings.EqualFold(line[:i], "Host") {
			continue
		}
		// A line with a wild card on it is a pattern line, not a machine:
		// `Host * !office gateway` says nothing about which machine's name
		// the others are, so take none of them.
		if strings.ContainsAny(line[i:], "*?") {
			continue
		}
		for _, alias := range strings.Fields(strings.TrimLeft(line[i:], " \t=")) {
			// A pattern is not a machine's name either: the negation
			// OpenSSH takes as "not this one", and the tilde, which is not
			// a character of a host name. "local" is the app's own word for
			// this machine, which no host may take.
			alias = strings.TrimRight(alias, "=")
			if alias == "" || strings.ContainsAny(alias, "?!~") || strings.EqualFold(alias, "local") {
				continue
			}
			if slices.Contains(got, alias) {
				continue
			}
			got = append(got, alias)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return got, nil
}
