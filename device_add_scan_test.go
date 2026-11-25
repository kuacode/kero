package main

import (
	"context"
	"runtime"
	"testing"
	"time"

	"kero/internal/remote"
)

// TestDeviceAddWslScanReal 在装了 WSL 的机器上，真正调用 remote.Distros()——
// 不预填、不走 mock——确认它能拿到发行版。UI 渲染路径由 TestDeviceAddWslDropdown
// 覆盖；异步回调包装（hostAddFound → a.update）在测试模式下被 a.win==nil 丢弃，
// 这里只验证扫描和解析这段真实逻辑。
func TestDeviceAddWslScanReal(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("只在 Windows 上跑")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ds, err := remote.Distros(ctx)
	if err != nil {
		t.Fatalf("remote.Distros() 失败：%v", err)
	}
	if len(ds) == 0 {
		t.Fatal("remote.Distros() 拿到 0 个发行版；wsl.exe 输出可能没被正确解析")
	}
	// 至少有一个默认发行版，hostAddFound 才能自动选上。
	var hasDefault bool
	for _, d := range ds {
		t.Logf("发行版：%s 默认=%v", d.Name, d.Default)
		if d.Default {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Error("没有一个发行版标为 Default，hostAddFound 无法自动选默认值")
	}
}
