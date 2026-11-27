package main

import (
	"testing"

	"kero/internal/proto"
)

// A host's mark is colored by its own brand: the operating system's, or
// the distribution's for a linux host that told one, light and dark, not
// the color the host's settings give its groups.
func TestHostOriginColor(t *testing.T) {
	a := testApp(t)
	// A linux host of a known distribution takes its brand, both ways.
	for _, c := range []string{"ubuntu", "debian", "fedora", "arch"} {
		h := &host{name: "build", info: proto.HostInfo{OS: "linux", Distro: c}}
		want, ok := osBrands[c]
		if !ok {
			t.Fatalf("no brand for %s", c)
		}
		if got := a.hostOriginColor(&lightPalette, h); got != want[0] {
			t.Errorf("%s: light mark is %v, want its brand %v", c, got, want[0])
		}
		if got := a.hostOriginColor(&darkPalette, h); got != want[1] {
			t.Errorf("%s: dark mark is %v, want its brand %v", c, got, want[1])
		}
	}
	// A host of an OS with a brand takes it.
	for _, c := range []string{"linux", "darwin", "windows"} {
		h := &host{name: "build", info: proto.HostInfo{OS: c}}
		want, ok := osBrands[c]
		if !ok {
			t.Fatalf("no brand for %s", c)
		}
		if got := a.hostOriginColor(&lightPalette, h); got != want[0] {
			t.Errorf("%s: light mark is not its brand", c)
		}
		if got := a.hostOriginColor(&darkPalette, h); got != want[1] {
			t.Errorf("%s: dark mark is not its brand", c)
		}
	}
	// A host that has not told its OS keeps the host's color.
	h := &host{name: "build"}
	if got := a.hostOriginColor(&lightPalette, h); got != lightPalette.hostColor(h) {
		t.Errorf("a host that has not answered does not take its host color")
	}
}

// A pane's program mark takes the program's brand: a coding agent's, a
// script's runtime's, and the text's for the rest, faint once the pane
// ended.  Each brand has a light color and one for the dark.
func TestProgramColor(t *testing.T) {
	agents := []string{"claude", "gemini", "codex", "opencode", "amp", "cursor-agent", "copilot", "qwen", "kimi", "cline", "phi"}
	runtimes := []string{"node", "bun", "deno", "python", "python3", "vim", "nvim", "git", "docker"}
	for _, name := range agents {
		want, ok := programBrands[name]
		if !ok {
			t.Fatalf("no brand for agent %s", name)
		}
		p := &proto.Pane{Agent: name, Status: proto.Idle}
		if got := programColor(&lightPalette, p); got != want[0] {
			t.Errorf("%s: light agent mark is %v, want %v", name, got, want[0])
		}
		if got := programColor(&darkPalette, p); got != want[1] {
			t.Errorf("%s: dark agent mark is %v, want %v", name, got, want[1])
		}
	}
	for _, name := range runtimes {
		want, ok := programBrands[name]
		if !ok {
			t.Fatalf("no brand for program %s", name)
		}
		p := &proto.Pane{Process: name, Status: proto.Idle}
		if got := programColor(&lightPalette, p); got != want[0] {
			t.Errorf("%s: light program mark is %v, want %v", name, got, want[0])
		}
		if got := programColor(&darkPalette, p); got != want[1] {
			t.Errorf("%s: dark program mark is %v, want %v", name, got, want[1])
		}
	}
	// An agent without a brand takes the text's, a program without one
	// the muted text's.
	p := &proto.Pane{Agent: "a-stranger", Status: proto.Idle}
	if got := programColor(&lightPalette, p); got != lightPalette.text {
		t.Errorf("an agent without a brand does not take the text's color")
	}
	p = &proto.Pane{Process: "a-stranger", Status: proto.Idle}
	if got := programColor(&lightPalette, p); got != lightPalette.textMuted {
		t.Errorf("a program without a brand does not take the muted text's")
	}
	// An ended pane is faint, whatever it ran.
	p = &proto.Pane{Agent: "claude", Status: proto.Exited}
	if got := programColor(&lightPalette, p); got != lightPalette.textFaint {
		t.Errorf("an ended pane does not go faint")
	}
}
