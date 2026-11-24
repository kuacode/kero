package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSONC(t *testing.T) {
	b := []byte(`{
	// my settings
	"fontSize": 15,
	"hosts": [
		{"name": "devbox", "addr": "me@devbox", "password": "s3cret"},
	],
	"some-future-key": true,
}`)
	s, probs := parseJSONC(b)
	if s.FontSize != 15 {
		t.Errorf("fontSize %v", s.FontSize)
	}
	if len(s.Hosts) != 1 || s.Hosts[0].Name != "devbox" || s.Hosts[0].Password != "s3cret" {
		t.Errorf("hosts %+v", s.Hosts)
	}
	if len(probs) != 1 || !strings.Contains(probs[0], "some-future-key") {
		t.Errorf("problems %q", probs)
	}
}

func TestParseJSONCBadValueKeepsRest(t *testing.T) {
	b := []byte(`{"fontSize": "not a number", "cursorBlink": false}`)
	s, probs := parseJSONC(b)
	if s.FontSize != Default().FontSize {
		t.Errorf("fontSize %v, want default", s.FontSize)
	}
	if s.CursorBlink {
		t.Error("cursorBlink was not read")
	}
	if len(probs) == 0 {
		t.Error("a bad value said nothing")
	}
}

func TestDiffJSONOnlyChanges(t *testing.T) {
	s := Default()
	s.FontSize = 15
	b, err := diffJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"fontSize"`) {
		t.Errorf("diff:\n%s", b)
	}
	if strings.Contains(string(b), `"cursorBlink"`) {
		t.Errorf("default in diff:\n%s", b)
	}
}

func TestMergeJSONCKeepsCommentsAndUnknown(t *testing.T) {
	existing := []byte(`{
	// mine
	"fontSize": 14,
	"future": 1,
}`)
	changes := []byte("{\n  \"fontSize\": 16\n}")
	out, err := mergeJSONC(existing, changes)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "// mine") || !strings.Contains(got, `"future"`) {
		t.Errorf("lost a comment or unknown key:\n%s", got)
	}
	if !strings.Contains(got, "16") {
		t.Errorf("new value missing:\n%s", got)
	}
}

func TestMergeJSONCRemovesDefault(t *testing.T) {
	existing := []byte(`{"fontSize": 20}`)
	changes := []byte("{}")
	out, err := mergeJSONC(existing, changes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "fontSize") {
		t.Errorf("returned-to-default stayed:\n%s", out)
	}
}

func TestMergeJSONCKnownAbsentClears(t *testing.T) {
	// diffJSON leaves a setting that has its default again out; mergeJSONC
	// must read that as "take it out of the file".
	existing := []byte(`{"fontSize": 20}`)
	changes, err := diffJSON(Default())
	if err != nil {
		t.Fatal(err)
	}
	out, err := mergeJSONC(existing, changes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "fontSize") {
		t.Errorf("cleared setting stayed:\n%s", out)
	}
}

func TestClearPasswordLeavesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	t.Setenv("KERO_HOME", dir)
	t.Setenv("KERO_CONFIG", path)
	mu.Lock()
	cache = nil
	mu.Unlock()

	s := Default()
	s.Hosts = []Host{{Name: "devbox", Addr: "me@devbox", Password: "s3cret"}}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	cache = nil
	mu.Unlock()
	s = Load()
	s.Hosts[0].Password = ""
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	cache = nil
	mu.Unlock()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("cleared password stayed:\n%s", b)
	}
	if strings.Contains(string(b), "password") {
		t.Errorf("empty password kept:\n%s", b)
	}
}

func TestJSONCRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	t.Setenv("KERO_HOME", dir)
	t.Setenv("KERO_CONFIG", path)
	mu.Lock()
	cache = nil
	mu.Unlock()

	s := Default()
	s.FontSize = 17
	s.Hosts = []Host{{Name: "devbox", Addr: "me@devbox", Password: "p@ss"}}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"password": "p@ss"`) {
		t.Errorf("password not stored:\n%s", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	mu.Lock()
	cache = nil
	mu.Unlock()
	back := Load()
	if back.FontSize != 17 || len(back.Hosts) != 1 || back.Hosts[0].Password != "p@ss" {
		t.Errorf("round trip %+v", back)
	}
}

func TestMigratesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KERO_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("KERO_CONFIG", defaultConfigPath())
	legacy := legacyConfigPath()
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("font-size = 18\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	cache = nil
	mu.Unlock()
	s := Load()
	if s.FontSize != 18 {
		t.Errorf("migrated fontSize %v", s.FontSize)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Errorf("legacy file not renamed: %v", err)
	}
	b, _ := os.ReadFile(Path())
	if !strings.Contains(string(b), `"fontSize": 18`) {
		t.Errorf("config:\n%s", b)
	}
}

func TestConfigPointer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KERO_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Unsetenv("KERO_CONFIG")
	if got := ConfigPath(); got != defaultConfigPath() {
		t.Errorf("default path %s, want %s", got, defaultConfigPath())
	}
	custom := filepath.Join(dir, "my-kero.jsonc")
	if err := SetConfigPath(custom); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPath(); got != custom {
		t.Errorf("path %s, want %s", got, custom)
	}
	if err := SetConfigPath(""); err != nil {
		t.Fatal(err)
	}
	if got := ConfigPath(); got != defaultConfigPath() {
		t.Errorf("path after clear %s", got)
	}
}
