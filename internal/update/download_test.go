package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A mirror prefixes the release's URL, with or without its own trailing
// slash, and nothing changes without one.
func TestMirrored(t *testing.T) {
	for _, tc := range []struct{ mirror, url, want string }{
		{"", "https://r/k.tar.gz", "https://r/k.tar.gz"},
		{"https://m", "https://r/k.tar.gz", "https://m/https://r/k.tar.gz"},
		{"https://m/", "https://r/k.tar.gz", "https://m/https://r/k.tar.gz"},
		{"  ", "https://r/k.tar.gz", "https://r/k.tar.gz"},
	} {
		old := Mirror
		Mirror = tc.mirror
		if got := mirrored(tc.url); got != tc.want {
			t.Errorf("mirrored(%q, %q) = %q, want %q", tc.mirror, tc.url, got, tc.want)
		}
		Mirror = old
	}
}

// MirrorFor reads the acceleration setting: off fetches directly, on
// uses the address filled in or the default proxy when it is empty.
func TestMirrorFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		accel   bool
		address string
		want    string
	}{
		{"off ignores an address", false, "https://custom", ""},
		{"on uses the address", true, "https://custom/", "https://custom"},
		{"on falls back when empty", true, "", DefaultDownloadMirror},
		{"on falls back when blank", true, "  ", DefaultDownloadMirror},
	} {
		if got := MirrorFor(tc.accel, tc.address); got != tc.want {
			t.Errorf("%s: MirrorFor(%v, %q) = %q, want %q", tc.name, tc.accel, tc.address, got, tc.want)
		}
	}
}

// contentRange reads the length of the file a partial answer covers, as
// "bytes first-last/total", and none it cannot.
func TestContentRange(t *testing.T) {
	for _, tc := range []struct {
		h    string
		off  int64
		want int64
		ok   bool
	}{
		{"bytes 0-4/10", 0, 10, true},
		{"bytes 5-9/10", 5, 10, true},
		{"bytes 0-4", 0, 5, true},
		// The offset it was asked from: a server that answers from the
		// file's start instead has ignored the Range, and the answer is
		// not the rest of the file.
		{"bytes 4-9/10", 5, 0, false},
		{"bytes 5-9/4", 5, 0, false},
		{"bytes 5-4/10", 5, 0, false},
		{"bytes", 0, 0, false},
		{"", 0, 0, false},
		{"bytes a-b/10", 0, 0, false},
	} {
		if got, ok := contentRange(tc.h, tc.off); ok != tc.ok || got != tc.want {
			t.Errorf("contentRange(%q, %d) = %d, %v", tc.h, tc.off, got, ok)
		}
	}
}

// download resumes a fetch cut short, and retries a link that fails,
// reporting the bytes it got as it goes.
func TestDownloadResumesAndRetries(t *testing.T) {
	body := []byte("0123456789")
	var tries int32
	ranges := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tries, 1)
		ranges <- r.Header.Get("Range")
		switch rng := r.Header.Get("Range"); rng {
		case "":
			// The first answer covers part of the file and then ends.
			w.Header().Set("Content-Range", "bytes 0-4/10")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[:5])
		case "bytes=5-":
			w.Header().Set("Content-Range", "bytes 5-9/10")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[5:])
		default:
			t.Errorf("unexpected Range %q", rng)
		}
	}))
	defer srv.Close()

	var got, totalSeen int64
	b, err := download(context.Background(), srv.URL, maxArchive, func(gotSoFar, total int64) {
		got, totalSeen = gotSoFar, total
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(b) != string(body) {
		t.Errorf("downloaded %q, want %q", b, body)
	}
	if got != int64(len(body)) {
		t.Errorf("the last progress is %d bytes, want %d", got, len(body))
	}
	if totalSeen != int64(len(body)) {
		t.Errorf("the progress's total is %d, want %d", totalSeen, len(body))
	}
	if n := atomic.LoadInt32(&tries); n != 2 {
		t.Errorf("the server was asked %d times, want 2", n)
	}
	// The retry resumes from the bytes it has, not the file's start.
	close(ranges)
	var seen []string
	for r := range ranges {
		seen = append(seen, r)
	}
	if len(seen) != 2 || seen[0] != "" || seen[1] != "bytes=5-" {
		t.Errorf("the fetches' Ranges are %v, want none, bytes=5-", seen)
	}
}

// download takes a link that answers the Range with the whole file, from
// the start, as one that ignores the Range does.
func TestDownloadRestartsWithoutARange(t *testing.T) {
	body := []byte("0123456789")
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Length", "10")
			w.Write(body)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-4/10")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[:5])
	}))
	defer srv.Close()

	b, err := download(context.Background(), srv.URL, maxArchive, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(b) != string(body) {
		t.Errorf("downloaded %q, want %q", b, body)
	}
	if len(ranges) != 2 || ranges[0] != "" || ranges[1] != "bytes=5-" {
		t.Errorf("the fetches' Ranges are %v, want none, bytes=5-", ranges)
	}
}

// download gives up on a link that keeps failing, and on a file past its
// bound, rather than retrying forever.
func TestDownloadGivesUp(t *testing.T) {
	var tries int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tries, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := download(context.Background(), srv.URL, maxArchive, nil); err == nil {
		t.Error("took a link that kept failing")
	}
	if n := atomic.LoadInt32(&tries); n != 3 {
		t.Errorf("the server was asked %d times, want 3", n)
	}

	// Past its bound, which the server claims it is not.
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1")
		w.Write([]byte("abc"))
	}))
	defer big.Close()
	if _, err := download(context.Background(), big.URL, 2, nil); err == nil {
		t.Error("took more than its bound")
	}
}

// download reads the whole of a file the server says it has more of,
// instead of taking the part it got.
func TestDownloadRefusesATruncatedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-4/16")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("short"))
	}))
	defer srv.Close()
	if _, err := download(context.Background(), srv.URL, maxArchive, nil); err == nil {
		t.Error("took a file shorter than the server claimed")
	}
}

// Fetch takes the archive for the releases, and refuses a signature that
// is not Kero's.
func TestFetch(t *testing.T) {
	archive := []byte("an archive")
	dir := t.TempDir()
	name := Asset("1.2.3", "linux", "amd64")
	release := filepath.Join(dir, "v1.2.3")
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, name), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, name+".sig"), []byte("not a signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	releases := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer releases.Close()
	t.Setenv("KERO_RELEASES", releases.URL)

	old := Mirror
	Mirror = ""
	defer func() { Mirror = old }()

	if _, err := Fetch(context.Background(), "1.2.3", "linux", "amd64", nil); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("Fetch's error is %q, want the archive's signature", err)
	}
	if _, err := Fetch(context.Background(), "../../x", "linux", "amd64", nil); err == nil {
		t.Error("took a version that is none")
	}

	// Through a mirror of the releases, which serves them under its own
	// root: the archive's and the signature's URLs both go through it.
	var asked []string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		http.ServeFile(w, r, filepath.Join(release, filepath.Base(r.URL.Path)))
	}))
	defer mirror.Close()
	Mirror = mirror.URL
	if _, err := Fetch(context.Background(), "1.2.3", "linux", "amd64", nil); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("through the mirror, Fetch's error is %q, want the archive's signature", err)
	}
	if len(asked) != 2 || !strings.HasSuffix(asked[0], name) || !strings.HasSuffix(asked[1], name+".sig") {
		t.Errorf("the mirror was asked for %v, want the archive and its signature", asked)
	}
}
