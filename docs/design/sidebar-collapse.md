# Kero 左边栏折叠改造设计方案（增量增强版）

> 设计原则：**纯增量、不破坏内置两态样式**。新增一个设置项 `SidebarRail`（默认 **off**）：off 时走原有两态路径，现状行为与样式不变；on 时折叠落到新增的图标栏（Rail）档，作为可选增强。

参考 `herdr-gpui`（`penso/herdr-gpui`，Rust + GPUI）的 `SidebarMode` 三态设计。

效果图见同目录 [sidebar-collapse-rail.svg](./sidebar-collapse-rail.svg)、[sidebar-rail-mark.svg](./sidebar-rail-mark.svg)。

## 1. 问题诊断（为何加 Rail）

Kero 当前左边栏是**两态**：

| 状态 | 字段 | 表现 |
|---|---|---|
| 展开 | `SidebarHidden = false` | 完整会话/项目/主机列表，宽 `SidebarWidth`（默认 268，200–440） |
| 隐藏 | `SidebarHidden = true` | 整栏收成 **0 宽**，会话列表完全不可见、不可导航 |

根因：折叠后**会话列表彻底消失**。唯一入口是 pane header 左上角的 `show-sidebar` 按钮（`panes.go:515`、`view.go:1081`），按钮本身还会随卡片滑动（`chrome.buttonsLeft`，`view.go:49`）。用户在折叠态既扫不到会话状态、也点不到某个会话——这是"不好"的核心，不是动画或宽度问题。

## 2. 目标形态：新增第三档 Rail

引入中间的 **Rail 档**（图标栏）。默认折叠目标由设置项决定，见 §6。

| 模式 | 宽度 | 内容 | 何时出现 |
|---|---|---|---|
| `Expanded` | `SidebarWidth`（268，200–440） | 完整会话/项目/主机列表 | 始终（现状，不变） |
| `Rail`（**新增**） | 固定 **64px** | 压缩图标栏：每项目一个标记盒 + 每会话一个状态点 | 仅当 `SidebarRail` on 且折叠 |
| `Hidden` | 0 | 整栏消失 | `SidebarRail` off 时折叠（现状）；或 on 时显式 ⌘B/zen |

herdr 的关键设计（照搬）：
- **切换在 Expanded⇄Rail 之间走**，Rail 永远保留导航（`rail.rs:103` toggle 即布尔翻转）。
- **Rail 每个标记可点、可导航**，tooltip 补足窄栏放不下的文字（`rail.rs:8-10`）。
- **动画"先滑出，rail 再补位"**（`slide.rs:4`）：面板先滑走，滑完 rail 才占位；终端只在展开时/滑完后各 resize 一次。
- **Rail 为窗口让路**：`RAIL_WIDTH.min(window_width - 240)`，窗口太窄宁可不显示 rail 也不留边框（`rail.rs:67`）。

## 3. Rail 结构（对应 sidebar-rail-mark.svg）

- **项目标记盒**：固定宽度小盒，包住项目首字母/图标；选中项目的盒是唯一带高亮描边的（herdr `rail.rs:6-7`）。
- **会话状态点**：盒下方一列圆点，绿=运行、黄=需关注（Waiting）、灰=空闲/退出；点击直接选中该会话。
- **tooltip**：hover 补足会话名 / 分支 / 状态 / 时间。
- **底部新建按钮**：常驻 `+`（新建会话）。

## 4. 动画（沿用现有 `sidebarMotion`）

现状 `view.go:109` 用 `AnimateTo("sidebar", ...)` 在 0↔1 补间，`chromeFor` 按 progress 算 `sideW`。三态改造：

- **Expanded→Rail**：宽 268→64 补间；内容列保持内部宽 268、`ClipX` 裁切（同右栏 `view.go:163` 手法），收完 rail 内容才出现。
- **Rail→Expanded**：反向。
- **→Hidden**（⌘B/zen，或 Rail off 的折叠）：面板滑到 0。
- **resize 语义**：终端/卡片宽度只在"到达新态"时改一次，补间过程不改，避免每帧 reflow 抖动。现 `a.side.rowW` 已按 `chrome.sideW` 单次 round，保留即可。

## 5. 交互

| 操作 | 结果 |
|---|---|
| header 的 `sidebar.toggle` | Expanded ⇄ Rail（Rail on）／ Expanded ⇄ Hidden（Rail off，现状） |
| `⌘/Ctrl+B` | 同 toggle，或 on 时显式落到 Hidden |
| 点 Rail 项目标记 | 展开该项目（Expanded），选中最近会话 |
| 点 Rail 会话状态点 | 直接选中该会话 |
| hover Rail 标记 | tooltip：会话名 / 分支 / 状态 / 时间 |
| 拖 resize 边缘（Expanded 态） | 调 `SidebarWidth`（不变） |
| 窗口 < 64+240 | rail 自动隐藏（herdr `rail.rs:67`） |

按钮位置：**沿用现状**。展开⇄折叠用现有顶栏（macOS）/标题栏（Windows）的 toggle 按钮，位置不动；隐藏态的 `show-sidebar` 仍在 pane header 左上角。

## 6. 增量实现：默认关闭，不碰旧路径

**唯一的行为开关**是新增设置项 `SidebarRail`（默认 **false**）。折叠落点集中在一处谓词：

```
// view.go
railFold()      = a.set.SidebarRail
foldWidth(winW) = railWidth(64) 当 railFold() 且 winW >= 64+240，否则 0
```

- `SidebarRail == false`（默认）：`foldWidth` 恒为 `0`，`chromeForFold(..., 0, ...)` 与原 `chromeFor` **数值逐字节等价**，列渲染走 else 分支（原样），`leftW` 两写法同义 → 三处调用点全部回到现状，旧行为与样式不变。
- `SidebarRail == true`：折叠时 `chrome.sideW = 64`，列渲染分支渲染 `a.rail(...)`；展开路径复用现有 `sidebar()`。窗口 <64+240 时 `foldWidth` 回落 `0`（herdr 同款让路）。

**实现陷阱（已修）**：列渲染的 Rail 分支必须放在现有 `side.Children(...)` **内部**，不能改成 if/else 把两列都直接挂到 `row`。`side` 容器承载 `ClipX` 与**文件拖放绑定**（`side.DroppedFiles()`/`FileDragOver()`），绕过它会让拖放失效、并连带破坏右栏命中测试（`TestSidebarKeepsTerminalDrops`、`TestRightSidebarMakesRoom`）。改对后两测试恢复通过。

## 7. 迁移清单（已实现）

| 文件 | 改动 | 状态 |
|---|---|---|
| `internal/settings/settings.go` | 新增 `SidebarRail bool`（零值=false，即默认 off）。 | ✅ |
| `internal/settings/config.go` | `boolKey("sidebar-rail", ...)` 注册配置项。 | ✅ |
| `settings_view.go` | 通用页新增 toggle 行（`sidebar-rail`），紧挨 `SidebarCount`。 | ✅ |
| `locales/*.json` + `catalog_gen.go` | 新增 `setSidebarRail`/`setSidebarRailDetail`（en/zh-Hans/ja），`mygo generate` 重新生成。 | ✅ |
| `view.go` | 新增 `railWidth` 常量、`chromeForFold`（`chromeFor` 变其 `foldW=0` 特例）、`railFold`/`foldWidth` 谓词、`rail`/`railGroup`/`railSession`/`firstSessionOf` 渲染器；`view()` 三处调用点接 `foldWidth`，列渲染分支放在 `side.Children` 内。 | ✅ |
| `settings_search.go` | 新设置项加入搜索索引。 | ✅ |
| `view_test.go` | 新增 `TestSidebarRailFoldWidth`（三档宽度）、`TestSidebarRailChrome`（64→268 整像素单调 + 端点）、`TestSidebarRailFoldsToRail`（实机折叠：`chrome.sideW=64` + 项目首字母标记可 `Find`；挂 `KERO_SHOTS` 出 `rail-folded.png`）。 | ✅ |
| `app.go` | 无 prefs 迁移（Rail 是新设置项，默认 off，旧 `SidebarHidden` 语义不变）。 | 无需改 |

验证：`go build ./...` 通过；`go test ./...` 全绿（含 `TestChromeForEquiv` 等价性、Rail 回归、以及未受影响的 `TestRightSidebarMakesRoom`/`TestSidebarKeepsTerminalDrops`）。

## 8. 取舍与风险

- **零默认影响**：`SidebarRail` 默认 off。已用 `chromeForFold(0)`≡`chromeFor` 的数值等价证明 + 全量测试确认未改变任何现有渲染路径。
- **无 prefs 迁移**：Rail 是新设置项，不复用 `SidebarHidden`，老 prefs 语义不变，升级安全。
- **`chrome.buttonsLeft` 联动**：Rail on 时 `sideW=64`，`chromeForFold` 在 `foldW>0` 时把 `hidden` 改为 `railWidth+16`，标题从 rail 右侧起算（`view.go`）；off 时 `sideW=0` 与现状一致。**未实机截图验证按钮定位**。
- **Rail 内容密度**：多 pane、分支、unread 在 64px 里降级为单个程序图标 + tooltip，细节靠 tooltip。
- **窗口极窄**：Rail on 但窗口 <64+240 时 `foldWidth` 回落 `0`（隐藏），避免挤掉终端。
- **toggle 语义**：`toggleSidebar` 未改（仍翻 `SidebarHidden`）。Rail on 时该布尔决定"显示全栏 vs 显示 rail"；Rail off 时维持"显示 vs 隐藏"。⌘B 走同一 action。
