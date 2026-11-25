package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"kero/internal/proto"
	"kero/internal/remote"
	"kero/internal/settings"
	"kero/locales"
)

// 已连接的远程在第二行行首用简洁文字注明状态，完整地址从行里去掉、
// 放进悬停提示（hostTip）；选择器副标题仍显示 wsl:// 完整地址。
func TestWslFullAddressAndConnectedState(t *testing.T) {
	a := testApp(t)
	addr, err := remote.ParseAddr("wsl://Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	h := &host{
		name: "Ubuntu", addr: addr, cfg: settings.Host{Name: "Ubuntu", Addr: addr.String()},
		conn: connReady, loaded: true,
		info: proto.HostInfo{OS: "linux", Distro: "Ubuntu", Kero: "2.1.36.4"},
	}
	a.hosts = append(a.hosts, h)

	// The full address lives in the hover, not on the line.
	if got := a.hostTip(h); !strings.HasPrefix(got, "wsl://Ubuntu") {
		t.Errorf("hostTip = %q, want the full wsl:// address", got)
	}
	if got := a.deviceSubtitle(h); !strings.HasPrefix(got, "wsl://Ubuntu") {
		t.Errorf("deviceSubtitle = %q, want the full wsl:// address", got)
	}

	// hostState writes the connected state as a short word, with the
	// address in its tooltip, on the second line.
	tt := ui.NewTester(func(c *ui.Context) { a.hostState(c, paletteFor(c), h) }, 120, 40)
	tt.Frame()
	if !tt.HasText(locales.HostStateOnline()) {
		t.Errorf("connected host has no state label: %q", tt.Texts())
	}
}
