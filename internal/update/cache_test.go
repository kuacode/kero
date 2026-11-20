package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCached tests that the cache keeps what a fetch verified, and
// refuses what it did not: the cache's entry is read only when its
// signature checks, else it is dropped, so a host never takes an archive
// the cache cannot vouch for.
func TestCached(t *testing.T) {
	old := cacheDir
	SetCacheDir(t.TempDir())
	defer func() { cacheDir = old }()

	archive := []byte("an archive")
	version, goos, goarch := "1.2.3", "linux", "amd64"
	name := Asset(version, goos, goarch)

	// Nothing cached yet.
	if _, _, ok := Cached(version, goos, goarch); ok {
		t.Error("an empty cache has an entry")
	}

	// A cache turned off holds nothing.
	SetCacheDir("")
	if _, _, ok := Cached(version, goos, goarch); ok {
		t.Error("a cache off holds an entry")
	}
	SetCacheDir(t.TempDir())

	// Storing, then reading: the entry's signature is not Kero's, so the
	// cache refuses it, as it would an entry another key signed.
	cache(version, goos, goarch, archive, "not a signature")
	if _, _, ok := Cached(version, goos, goarch); ok {
		t.Error("an entry another key signed is served")
	}
	// The files are there, though the cache refuses them.
	if _, err := os.Stat(filepath.Join(cacheDir, name)); err != nil {
		t.Errorf("the cache does not hold the archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, name+".sig")); err != nil {
		t.Errorf("the cache does not hold the signature: %v", err)
	}

	// A missing signature drops the entry.
	if err := os.Remove(filepath.Join(cacheDir, name+".sig")); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := Cached(version, goos, goarch); ok {
		t.Error("an entry without its signature is served")
	}
}
