package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

// Distro is one distribution installed in WSL, as wsl.exe -l -v lists it.
type Distro struct {
	// Name is what goes after wsl:// in a device's address.
	Name string
	// Default marks the distribution wsl.exe starts with no argument.
	Default bool
}

// Distros lists the distributions installed in WSL, for the quick Add a
// Host dialog to offer.  Off Windows there is nothing to list, and no
// error: the answer is empty.
func Distros(ctx context.Context) ([]Distro, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	// wsl.exe writes UTF-16LE behind a BOM when its output is a pipe,
	// whatever the console's code page.  -l -v is the listing that
	// carries the state and the star on the default distribution.
	cmd := exec.CommandContext(ctx, "wsl.exe", "-l", "-v")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("wsl.exe -l -v: exit %d", ee.ExitCode())
		}
		return nil, fmt.Errorf("wsl.exe -l -v: %w", err)
	}
	return ParseWslList(out)
}

// ParseWslList reads what wsl.exe -l -v wrote, already decoded to text:
// its columns are Name, State and Version, told apart by runs of two or
// more spaces, the default distribution marked by a leading "* ".  The
// header is the one line whose last column is not a number, which holds
// whatever language wsl.exe answers in.
func ParseWslList(out []byte) ([]Distro, error) {
	text := decodeWslOutput(out)
	var got []Distro
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		defaulted := false
		if rest, ok := strings.CutPrefix(line, "*"); ok {
			// The star stands where the name would start, with the spaces
			// that follow it: "* Debian" is the one wsl.exe runs with no
			// argument, and its name is Debian.
			defaulted = true
			line = strings.TrimLeft(rest, " \t")
		}
		fields := wslFields(strings.TrimLeft(line, " \t"))
		if len(fields) < 2 {
			continue
		}
		last := fields[len(fields)-1]
		if last == "" || !isDigits(last) {
			continue // the header, or a line with no version column
		}
		name := fields[0]
		if name == "" {
			continue
		}
		got = append(got, Distro{Name: name, Default: defaulted})
	}
	return got, nil
}

// decodeWslOutput turns what wsl.exe wrote into Go text: UTF-16LE behind
// a BOM, UTF-8 behind one, or plain bytes from a build that wrote the
// console's own code page.
func decodeWslOutput(out []byte) string {
	if len(out) >= 2 && binary.LittleEndian.Uint16(out) == 0xfeff {
		return decodeUTF16LE(out[2:])
	}
	if len(out) >= 3 && out[0] == 0xef && out[1] == 0xbb && out[2] == 0xbf {
		return string(out[3:])
	}
	// No BOM. wsl.exe writes UTF-16LE when its output is a pipe, which is
	// the case Distros sees, but an older build wrote the console's own
	// code page: readable bytes, so take them as they are.
	return string(out)
}

// decodeUTF16LE reads the bytes as UTF-16LE code units, dropping a byte
// that a sliced-off BOM would have left over.
func decodeUTF16LE(body []byte) string {
	body = body[:len(body)-len(body)%2]
	u := make([]uint16, 0, len(body)/2)
	for i := 0; i < len(body); i += 2 {
		u = append(u, uint16(body[i])|uint16(body[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// wslFields splits a listing's line on runs of two or more spaces.
func wslFields(line string) []string {
	var fields []string
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			continue
		}
		j := i
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}
		if j-i < 2 {
			i = j - 1
			continue
		}
		fields = append(fields, line[start:i])
		start = j
		i = j - 1
	}
	return append(fields, line[start:])
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
