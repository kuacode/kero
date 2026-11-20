package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Mirror prefixes the URLs a download of a release fetches from, as a
// proxy of the releases (gh-proxy.com and the like): its value is the
// proxy's, with or without a trailing slash, else nothing. The app sets
// it from its settings before it downloads. The releases' latest version
// (api.github.com) is never mirrored.
var Mirror string

// SetMirror takes a proxy of the releases (gh-proxy.com and the like) as
// the user's settings name one, trimming any trailing slash. An empty
// value turns the mirror off.
func SetMirror(mirror string) { Mirror = strings.TrimRight(strings.TrimSpace(mirror), "/") }

// mirrored is a release URL through the mirror, else itself: a proxy of
// the releases (gh-proxy and the like) serves them under its own root,
// the release's whole URL after it.
func mirrored(u string) string {
	m := strings.TrimRight(strings.TrimSpace(Mirror), "/")
	if m == "" {
		return u
	}
	return m + "/" + u
}

// progress is a writer that calls on with the bytes read so far.
type progress struct{ on func(int64) }

func (p progress) Write(b []byte) (int, error) {
	if p.on != nil {
		p.on(int64(len(b)))
	}
	return len(b), nil
}

// download fetches a URL into memory, resuming a fetch cut short (Range)
// and retrying a link that failed, calling onProgress with the bytes
// received as it goes. max bounds it.
func download(ctx context.Context, url string, max int64, onProgress func(int64)) ([]byte, error) {
	const tries = 3
	var buf bytes.Buffer
	var total int64
	for attempt := 1; ; attempt++ {
		off := int64(buf.Len())
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		if off > 0 {
			// Resuming: the rest of the file, from where it stopped.
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if attempt < tries && ctx.Err() == nil {
				time.Sleep(backoff(attempt))
				continue
			}
			return nil, err
		}
		switch resp.StatusCode {
		case http.StatusPartialContent:
			// A partial answer says where it starts and how long the
			// file is: the Range it served, as bytes from-N/total.
			n, ok := contentRange(resp.Header.Get("Content-Range"), off)
			if !ok {
				resp.Body.Close()
				return nil, fmt.Errorf("%s: %s", url, resp.Status)
			}
			total = n
		case http.StatusOK:
			if off > 0 {
				// The server ignored the Range: start over.
				buf.Reset()
				off, total = 0, 0
			}
			if resp.ContentLength >= 0 {
				total = resp.ContentLength
			}
		default:
			resp.Body.Close()
			if attempt < tries && ctx.Err() == nil && resp.StatusCode >= 500 {
				time.Sleep(backoff(attempt))
				continue
			}
			return nil, fmt.Errorf("%s: %s", url, resp.Status)
		}
		p := progress{onProgress}
		n, err := io.Copy(io.MultiWriter(&buf, p), io.LimitReader(resp.Body, max+1-off))
		resp.Body.Close()
		off += n
		if onProgress != nil {
			onProgress(off)
		}
		if int64(buf.Len()) > max {
			return nil, fmt.Errorf("%s: too large", url)
		}
		if err != nil {
			if attempt < tries && ctx.Err() == nil {
				time.Sleep(backoff(attempt))
				continue
			}
			return nil, err
		}
		if total > 0 && off < total {
			// The body ended before the file did: fetch the rest.
			if attempt < tries && ctx.Err() == nil {
				time.Sleep(backoff(attempt))
				continue
			}
			return nil, fmt.Errorf("%s: %d of %d bytes", url, off, total)
		}
		return buf.Bytes(), nil
	}
}

// contentRange is the length of the file a partial answer covers, as
// "bytes first-last/total", the total it gives, else how far the bytes it
// covers run.
func contentRange(h string, off int64) (int64, bool) {
	f := strings.Fields(h)
	if len(f) != 2 || !strings.EqualFold(f[0], "bytes") {
		return 0, false
	}
	r := strings.TrimPrefix(f[1], "=")
	from, tot, hasTotal := strings.Cut(r, "/")
	first, last, ok := span(from)
	if !ok {
		return 0, false
	}
	if !hasTotal {
		// No total: the answer's own run, which is the file's, as a
		// single answer covers it all.
		return last + 1, first == off
	}
	n, err := strconv.ParseInt(tot, 10, 64)
	if err != nil || n < 0 || n < last+1 {
		return 0, false
	}
	// A server that ignored the Range answers from the file's start.
	if first != off {
		return 0, false
	}
	return n, true
}

// span is a Range's first and last byte, "first-last" or "first".
func span(s string) (first, last int64, ok bool) {
	lo, hi, cut := strings.Cut(s, "-")
	if !cut || lo == "" {
		return 0, 0, false
	}
	f, err := strconv.ParseInt(lo, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if hi == "" {
		return f, f, true
	}
	l, err := strconv.ParseInt(hi, 10, 64)
	if err != nil || l < f {
		return 0, 0, false
	}
	return f, l, true
}

// backoff is the pause before a retry, growing with the attempt.
func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * 400 * time.Millisecond
}

// Fetch is the verified archive of a version for a platform, downloaded
// through the mirror with resume and retry, reporting the bytes received
// to onProgress as it goes.
func Fetch(ctx context.Context, version, goos, goarch string, onProgress func(int64)) ([]byte, error) {
	archive, _, err := FetchWithSig(ctx, version, goos, goarch, onProgress)
	return archive, err
}

// FetchWithSig is Fetch, with the signature the archive is signed with:
// to upload the archive to a host that could not fetch it itself, the
// host checks the signature it came with.
func FetchWithSig(ctx context.Context, version, goos, goarch string, onProgress func(int64)) ([]byte, string, error) {
	version = strings.TrimPrefix(version, "v")
	if !ValidVersion(version) {
		return nil, "", fmt.Errorf("%q is not a version of Kero", version)
	}
	name := Asset(version, goos, goarch)
	archive, err := download(ctx, mirrored(URL(version, name)), maxArchive, onProgress)
	if err != nil {
		return nil, "", fmt.Errorf("downloading Kero %s: %w", version, err)
	}
	sig, err := download(ctx, mirrored(URL(version, name+".sig")), 4096, nil)
	if err != nil {
		return nil, "", fmt.Errorf("downloading the signature of Kero %s: %w", version, err)
	}
	if err := Verify(archive, version, goos, goarch, string(sig)); err != nil {
		return nil, "", err
	}
	return archive, string(sig), nil
}
