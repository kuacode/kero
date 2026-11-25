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

// WSL 的远程地址完整显示为 wsl://<发行版>，选择器副标题也一样；已连接的
// 远程在侧栏有一个注明状态的点（tooltip 为 HostStateOnline）。
func TestWslFullAddressAndConnectedState(t *testing.T) {
	a := testApp(t)
	addr, err := remote.ParseAddr("wsl://Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	h := &host{
		name: "Ubuntu", addr: addr, cfg: settings.Host{Name: "Ubuntu", Addr: addr.String()},
		conn: connReady, loaded: true,
		info: proto.HostInfo{OS: "linux"},
	}
	a.hosts = append(a.hosts, h)

	if got := a.hostMeta(h); !strings.HasPrefix(got, "wsl://Ubuntu") {
		t.Errorf("hostMeta = %q, want the full wsl:// address", got)
	}
	if got := a.deviceSubtitle(h); !strings.HasPrefix(got, "wsl://Ubuntu") {
		t.Errorf("deviceSubtitle = %q, want the full wsl:// address", got)
	}

	// hostState draws a dot labeled with the connected state.
	tt := ui.NewTester(func(c *ui.Context) { a.hostState(c, paletteFor(c), h) }, 120, 40)
	tt.Frame()
	if !tt.HasText(locales.HostStateOnline()) {
		t.Errorf("connected host has no state label: %q", tt.Texts())
	}
}
