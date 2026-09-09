# Go + Wails + React 桌面原型验证

日期：2026-09-09。

## 结论

Go + Wails v3 + React 路线已获用户确认。首轮真实 macOS 原型证明该路线可承接核心窗口行为，不需要为了主窗口和 NSPanel 浮窗直接改用 SwiftUI。

这不是完整产品验收。后续多显示器问题已复现并修正，详见下方补充；全屏、中文输入法候选框、通知点击和退出流程仍需进一步验证。

## 版本与环境

| 项目 | 固定版本/实际环境 |
|---|---|
| Wails Go module | v3.0.0-beta.18 |
| Wails commit | 42a313c1c0da18c437d5486fe2c06fa2848765c4 |
| @wailsio/runtime | 3.0.0-beta.18 |
| React / React DOM | 19.2.8 |
| TypeScript / Vite | 7.0.2 / 8.2.2 |
| Go / Node.js | 1.26.5 / 24.15.0 |
| macOS / CPU | 15.6.1 / Apple Silicon |

版本从 Go module registry 和 npm registry 核实后固定，Go 与 npm 均保存依赖锁定结果。没有安装全局 Wails CLI。使用系统 WebView，原型未加入独立 Node 中间服务。

## 实测记录

| 项目 | 结果 | 证据与限制 |
|---|---|---|
| React TypeScript 构建 | 通过 | 类型检查及 Vite production build 成功 |
| Go / AppKit 构建 | 通过 | go build 成功，本地 ad-hoc .app 可运行 |
| React → Go 状态读取 | 通过 | 两窗口实时显示同一 Go 模拟任务的 ticks、running 和窗口状态 |
| 主窗口和浮窗并存 | 通过 | 主窗口为 NSWindow，浮窗为 NSPanel；两窗口均可见 |
| 浮窗获得输入焦点 | 通过 | Show + Focus 后 textarea 为焦点元素，panelFocused=true |
| Esc 收起浮窗 | 通过 | 操作后 panelVisible=false，主窗口仍可使用 |
| 隐藏不终止后台任务 | 通过 | Esc 后 running=true、ticks 继续增长，30 秒后正常结束 |
| 点击外部不隐藏 | 通过 | 切到 Finder，panelFocused=false 且 panelVisible=true |
| 转入主窗口 | 通过 | 点击浮窗按钮后主窗口出现，日志记录 panel:transfer-to-main |
| 全局快捷键注册 | 通过 | Alt+Space 注册成功，无冲突错误 |
| 全局快捷键触发 | 通过（用户人工确认） | 日志记录 global-shortcut:fired 与浮窗显示/隐藏；用户于 2026-09-09 明确确认物理键盘按 ⌥Space 后浮窗正常出现 |
| 普通中文文本显示 | 部分 | 通过辅助功能设置的中文文字可正常显示；不等于中文 IME 和剪贴板交互已通过 |
| 菜单栏、关闭所有窗口后恢复、主动退出 | 待完整人工验证 | 已实现菜单栏及 close hook；本轮没有形成完整 UI 证据链 |
| 多显示器定位 | 已修复并通过原生检查 | 3 块屏幕共 11 项检查通过，规则按当前活动窗口选屏 |
| 全屏应用、通知点击 | 未验证 | 需要专项桌面验收 |

自动化工具最初发送的 Alt+Space 只成为应用内按键，未触发 Carbon 回调，因此没有将这些尝试标记为通过。后续日志出现真实回调才增加触发证据。

随后用户通过物理键盘完成验证并回复“正常”，全局快捷键唤起的待确认项已关闭。此确认不扩大为全屏、多显示器、中文输入法或通知均已通过。

自动化在中文粘贴时报告剪贴板读取超时、noWindowsAvailable，并曾提示用户正在改变窗口；这些尚不能归因为 Wails 输入框缺陷。未为了消除工具报错而修改系统权限或输入法设置。

## 实现选择与后续约束

原型通过 Go 创建两个窗口，浮窗设置 `MacWindowClassPanel`、`NonActivating`、`FloatingPanel`，启用 `HideOnEscape`，关闭 `HideOnFocusLost`。调用窗口隐藏不会触碰后台任务。

窗口关闭 hook 取消原始关闭并隐藏窗口；`ApplicationShouldTerminateAfterLastWindowClosed` 为 false。菜单栏提供打开浮窗、打开主窗口和退出入口。这些配置应在下一轮人工验收中确认全部行为。

原型使用 `Call.ByName` 和每 400ms 状态轮询，以减少验证代码。正式产品应采用生成的类型绑定和明确事件设计，不直接复制原型的轮询架构。模拟任务只用于验证生命周期，不是 Agent 实现。

编译观察到 Wails 原生对象的 macOS deployment target 与链接目标不一致警告。当前 macOS 15.6.1 可运行，但没有验证旧系统兼容性。正式发布前必须统一部署目标，明确最低 macOS 版本，并验证签名、公证与打包。

## 原型与复现

源代码分支：`prototype/wails-desktop`。初始提交：`b2ed3bd`；多显示器修复提交：`043d344`。主分支不包含可丢弃原型代码。

在 `/Users/bytedance/self-workspace/pi-popchat` 执行：

```sh
git switch prototype/wails-desktop
sh scripts/run-desktop-probe.sh
```

运行脚本构建并启动 `work/Pi Popchat Probe.app`。已有原型运行时先从其菜单栏退出，以免多实例争用快捷键。原型不保存会话、不连接 Pi 或真实模型，也不读取凭证。

原始本地运行日志位于忽略目录 `work/desktop-probe.log`，本报告记录关键观察，避免把临时构建产物加入主分支。

## 多显示器修正补充

用户反馈：无论在哪块显示器使用快捷键，浮窗总在同一位置出现。用户随后明确目标规则为“当前活动窗口所在屏幕”，而非鼠标所在屏幕。

原因：原始 `ShowPanel` 仅调用 Show/Focus，窗口复用了创建时的位置。原生检查通过同一唤起函数在 3 块实际屏幕上轮换目标，旧实现的 6 次检查中 4 次失败，屏幕 2、3 的请求都落到屏幕 1。

修正：在显示浮窗前读取前台应用最前面的普通窗口几何，以主要相交面积选择屏幕 ID，再通过 Wails `SetScreen` 在目标屏幕可用区域居中。避免自行换算 Retina 缩放或多屏绝对位置。浮窗已可见但在另一块屏幕时，将它移到目标屏幕；只有同屏时才切换为隐藏。

系统读取封装在原型的 `active_screen_darwin.go`，只使用 AppKit/CoreGraphics 窗口元数据，不读取标题或像素，不引入辅助功能/录屏授权。识别依据为前台应用最靠前的 layer-0 普通窗口；特殊工具浮窗、纯菜单栏应用或桌面没有对应窗口时回退到 AppKit mainScreen，再回退首屏。这些特殊场景不能视为真实 AX 焦点窗口语义已经全面覆盖。

真实回归结果：6 次指定屏幕定位、3 次移动并激活主窗口后重新识别目标、2 次跨屏/同屏快捷键路径，共 **11/11 通过**。这不是只测试坐标算法：检查对象是真实 NSPanel 的 GetScreen 返回值；活动窗口场景还要求窗口几何识别命中，不能依靠 fallback 通过。

检查工具增加了短时间的状态收敛等待，避免将 AppKit 的异步显隐误判为失败。测试期间人工或其他工具抢占焦点会影响结果，因此检查运行期间不并发操作桌面。

可复现命令：切换原型分支后执行 `sh scripts/check-desktop-placement.sh`。红/绿结果归档在 `prototypes/wails-desktop/placement-verification.txt`。少于两块实际显示器时返回未完成状态，不把缺少验证环境报告为通过。修正版程序已在本机替换并启动，等待用户对实际多应用场景的体验反馈。
