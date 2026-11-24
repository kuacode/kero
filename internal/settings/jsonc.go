package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/tailscale/hujson"

	"kero/internal/actions"
	"kero/internal/adblock"
	"kero/internal/remote"
)

// The configuration is JSON with comments (JSONC): an object whose members
// are settings, as Settings names them. It holds what differs from the
// defaults; comments, member order and members kero does not know survive a
// save, so a hand-edited file stays the user's. The Settings window edits it
// in place. The older `key = value` format (config.go) is read once and
// rewritten, so upgrades keep what the user set.

// jsoncHeader opens a configuration kero writes anew.
const jsoncHeader = `// Kero's configuration, in JSON with comments (JSONC).
// It holds what differs from the defaults; edit it here or in Settings, and
// your changes apply at once. ` + "`kero config show`" + ` prints the settings in
// effect. The file may hold credentials, and is readable by you alone.
`

// knownJSONKeys are the settings' names in the file: the JSON names of
// Settings' fields.
var knownJSONKeys = sync.OnceValue(func() map[string]bool {
	known := map[string]bool{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		known[name] = true
	}
	return known
})

// looksJSONC tells a JSONC configuration from the older `key = value` one:
// the first thing in it, past whitespace and comments, is a brace.
func looksJSONC(b []byte) bool {
	for i := 0; i < len(b); {
		switch {
		case b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n':
			i++
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i += 2
		default:
			return b[i] == '{'
		}
	}
	return false
}

// parseJSONC reads a configuration over the defaults, with what it got
// wrong: members kero does not know, values of the wrong type, hosts and
// environment entries it cannot take. The good ones apply regardless.
func parseJSONC(b []byte) (Settings, []string) {
	s := Default()
	std, err := hujson.Standardize(b)
	if err != nil {
		return s, []string{fmt.Sprintf("the configuration is not valid JSONC: %v", err)}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(std, &raw); err != nil {
		return s, []string{fmt.Sprintf("the configuration is not an object of settings: %v", err)}
	}
	known := knownJSONKeys()
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []string
	for _, name := range names {
		if !known[name] {
			problems = append(problems, fmt.Sprintf("no setting %q", name))
			continue
		}
		// Each member on its own, so that one bad value costs only itself.
		one := append(append(append([]byte("{"), mustJSON(name)...), ':'), raw[name]...)
		one = append(one, '}')
		if err := json.Unmarshal(one, &s); err != nil {
			problems = append(problems, name+": "+err.Error())
		}
	}
	s.normalize()
	problems = append(problems, validateJSON(&s)...)
	return s, problems
}

// validateJSON drops the list entries kero cannot take, saying each, and
// keeps the rest: one bad host does not cost the others, as the older
// format's lists read.
func validateJSON(s *Settings) []string {
	var problems []string
	bad := func(what, format string, args ...any) {
		problems = append(problems, what+": "+fmt.Sprintf(format, args...))
	}

	var env []string
	for _, kv := range s.Env {
		if strings.TrimSpace(kv) == "" {
			continue
		}
		if !strings.Contains(kv, "=") {
			bad("env", "%q is not NAME=value", kv)
			continue
		}
		env = append(env, kv)
	}
	s.Env = env

	var features []string
	for _, f := range s.FontFeatures {
		f = strings.TrimSpace(f)
		if !validFeature(f) {
			bad("fontFeatures", "%q is not an OpenType feature", f)
			continue
		}
		features = append(features, f)
	}
	s.FontFeatures = features

	var hosts []Host
	seen := map[string]bool{}
	for _, h := range s.Hosts {
		switch {
		case !remote.ValidName(h.Name):
			bad("hosts", "%q can't name a host", h.Name)
			continue
		case seen[strings.ToLower(h.Name)]:
			bad("hosts", "%q is named twice", h.Name)
			continue
		}
		a, err := remote.ParseAddr(h.Addr)
		if err != nil {
			bad("hosts", "%s: %v", h.Name, err)
			continue
		}
		h.Addr = a.String()
		if h.Color != "" && !containsString(HostColors, h.Color) {
			bad("hosts", "%s: no color %q (%s)", h.Name, h.Color, strings.Join(HostColors, ", "))
			h.Color = ""
		}
		seen[strings.ToLower(h.Name)] = true
		hosts = append(hosts, h)
	}
	s.Hosts = hosts

	var listen []string
	for _, l := range s.Listen {
		if _, err := remote.ParseListen(l); err != nil {
			bad("listen", "%v", err)
			continue
		}
		listen = append(listen, strings.TrimSpace(l))
	}
	s.Listen = listen

	var allow []string
	for _, a := range s.ListenAllow {
		if a = strings.TrimSpace(a); a != "" {
			allow = append(allow, a)
		}
	}
	s.ListenAllow = allow

	for id := range s.Shortcuts {
		if id == "" {
			continue
		}
		if _, ok := actions.Find(id); !ok {
			bad("shortcuts", "no action %q", id)
			delete(s.Shortcuts, id)
		}
	}

	var lists []string
	for _, id := range s.AdLists {
		if id == "" {
			continue
		}
		if _, ok := adblock.Find(id); !ok {
			bad("adLists", "%q is no list", id)
			continue
		}
		if !containsString(lists, id) {
			lists = append(lists, id)
		}
	}
	s.AdLists = lists

	return problems
}

// diffJSON names the settings that differ from the defaults, as the file
// holds them. A setting that has its default again is left out, which
// mergeJSONC reads as "take it out of the file".
func diffJSON(s Settings) ([]byte, error) {
	var ours, def map[string]json.RawMessage
	if err := roundTrip(s, &ours); err != nil {
		return nil, err
	}
	if err := roundTrip(Default(), &def); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for name, v := range ours {
		if d, ok := def[name]; !ok || !bytes.Equal(v, d) {
			out[name] = v
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

// marshalJSONC writes s as a configuration: the changes alone, or every
// setting (`kero config show`).
func marshalJSONC(s Settings, onlyChanges bool) ([]byte, error) {
	var (
		body []byte
		err  error
	)
	if onlyChanges {
		body, err = diffJSON(s)
	} else {
		body, err = json.MarshalIndent(s, "", "  ")
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 2 { // {}
		body = nil
	}
	return append(append([]byte(jsoncHeader), body...), '\n'), nil
}

// mergeJSONC writes the changes into an existing configuration, keeping its
// comments, member order and members kero does not know. A setting that
// returned to its default leaves the file; one that appeared joins it.
func mergeJSONC(existing, changes []byte) ([]byte, error) {
	newAst, err := hujson.Parse(changes)
	if err != nil {
		return nil, err
	}
	newObj, ok := newAst.Value.(*hujson.Object)
	if !ok {
		return marshalJSONC(Settings{}, true)
	}
	oldAst, err := hujson.Parse(existing)
	if err != nil {
		return nil, err
	}
	oldObj, ok := oldAst.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("the configuration is not an object")
	}

	fresh := map[string]hujson.ObjectMember{}
	for _, m := range newObj.Members {
		fresh[memberName(m)] = m
	}
	known := knownJSONKeys()
	kept := make([]hujson.ObjectMember, 0, len(oldObj.Members)+len(fresh))
	for _, m := range oldObj.Members {
		name := memberName(m)
		if nm, ok := fresh[name]; ok {
			// The setting's value changes; the comments and order around
			// it stay.
			v := nm.Value
			v.BeforeExtra = m.Value.BeforeExtra
			v.AfterExtra = m.Value.AfterExtra
			m.Value = v
			delete(fresh, name)
			kept = append(kept, m)
			continue
		}
		if known[name] {
			continue // back to its default: the file says nothing
		}
		kept = append(kept, m) // a member kero does not know stays
	}
	names := make([]string, 0, len(fresh))
	for name := range fresh {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		kept = append(kept, fresh[name])
	}
	oldObj.Members = kept
	oldAst.Format()
	return oldAst.Pack(), nil
}

func roundTrip(s Settings, out *map[string]json.RawMessage) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func memberName(m hujson.ObjectMember) string {
	if lit, ok := m.Name.Value.(hujson.Literal); ok {
		return lit.String()
	}
	return ""
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Show writes the settings in effect as a JSONC document, every setting
// named, as `kero config show` prints it.
func Show(s Settings) string {
	b, err := marshalJSONC(s, false)
	if err != nil {
		return ""
	}
	return string(b)
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
