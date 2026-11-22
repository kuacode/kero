# 快捷添加主机 — 未完成事项

本文记录 quick-add 对话框（`device_add.go`、`internal/remote/wsl.go`、
`internal/remote/sshconfig.go`）本轮实现留下的边界与下一步。参考对象是
[sheprd](https://github.com/sheprd-labs/sheprd) 的设备管理：它值得学的是
判断力，不是布局。

## 1. SSH 密码 / key passphrase 认证（优先级最高）

- 现状：传输层以 `SSH_ASKPASS_REQUIRE=never` 运行 ssh(1)
  （`internal/remote/transport.go:310`），`sshArgs` 的注释写明
  "It never asks anything"；带密码的地址被 `remote.ParseAddr` 直接拒绝
  （`internal/remote/addr.go:60-66`）。对话框在 `device_add.go:624-628`
  留了带注释的插入点，没有放假控件。
- 方案（oracle 评估结论）：路线 (a) —— `SSH_ASKPASS` +
  `SSH_ASKPASS_REQUIRE=force`。Windows OpenSSH 8.4+ 全面支持（本机
  9.5p2 已验证二进制含相关字符串）。改动面：`transport.go:310` 移除
  never 禁令，按需注入 `SSH_ASKPASS` 环境变量与 askpass 助手脚本，
  约 0.5–1 天。约束：密码不写进地址、不在主进程存明文。
- 备选：路线 (c) —— 只强化 `ErrAuth` 的文案
  （`hosts_view.go:477-479` 已有 `HostAuthHint` 先例），半天以内、零风险，
  可作为 (a) 之前的过渡。
- 明确不做：Skip host key verification 复选框。sheprd 有，我们不抄——
  信任主机密钥的安全决策应留在终端里完成（与现有 HostAuthHint 口径一致）。

## 2. Network 标签已移除

- 现状：quick-add 只剩 SSH 与（Windows 上的）WSL 两种；`kindNetwork`
  连同它的 rail 行、表单文案和测试都已删除。Settings › Hosts 页仍可手输
  `http://`/`https://` 地址，那条路不受影响。
- 移除原因：SSH 有 `~/.ssh/config` 别名、WSL 有 `wsl.exe -l -v`，Network
  没有候选来源，只能给一段说明；用户要求"去掉网络类型的连接"。
- 如果以后恢复：`internal/tailscale` 的 `statusResponse`
  （`tailscale.go:180-191`）目前只解析 `Self`，需解析 localAPI("status")
  的 `Peer`（`Prefixes`/`DNSName`/`Online`），把在线 peer 的 MagicDNS 名
  列为候选。缓存 TTL 参照 `sshCandidateTTL`（30s）。

## 3. 判重只按名字

- 现状：`a.set.Host(h.Name)`（`hosts_settings.go:329`、`device_add.go:615`）
  只看 name 是否重名。
- sheprd 的思路值得抄：同一个目标不管写成 SSH 别名、`user@address` 还是
  `ssh://` 形式，都应认出是同一台机器。kero 不需要 `ssh -G`，对
  `remote.ParseAddr` 的规范化结果做 host+port 比较即可。

## 4. SSH config 解析是刻意窄的

`sshconfig.go` 只读 `~/.ssh/config` 一个文件：不调 ssh.exe、不跟
`Include`、不读 drop-in 目录。这是安全边界——通配 `Include` 不会让对话框
读到 home 之外的文件。代价是 Include 里定义的别名会漏。若要放宽，只应
在 home 目录范围内展开 Include 的 glob。

## 5. 云供应商绿点

sheprd 的 rail 给已配置的云供应商画绿点。kero 没有云供应商概念，本轮
不做；若做，形态应是"该 kind 下已有主机"的小圆点，挂在顶部的 kind
切换控件上。

## 6. 已知环境噪音（与 quick-add 无关）

本分支上全量 `go test .` 有一批 Windows 路径分隔符类的既有失败
（`TestPasteImageAsPath`、`TestHookInstallKeepsSettings`、
`TestSearchMenuOneHighlight` 等）。推送前的 gate 以
[FORKING.md](FORKING.md) 的清单为准：`gofmt -w .`、`go vet ./...`、
`go test ./internal/...`、三平台交叉编译。
