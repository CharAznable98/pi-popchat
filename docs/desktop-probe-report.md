# Go + Wails + React 桌面原型验证

日期：2026-09-09。

## 结论

Go + Wails v3 + React 路线已获用户确认。首轮真实 macOS 原型证明该路线可承接核心窗口行为，不需要为了主窗口和 NSPanel 浮窗直接改用 SwiftUI。

这不是完整产品验收。全屏、多显示器、中文输入法候选框、通知点击和退出流程仍需进一步验证，不能把“编译通过”推导成全部桌面体验通过。

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
| 全局快捷键触发 | 已观察到真实回调 | 日志在 14:44:11 记录 global-shortcut:fired、panel:show、再次 fired 和 panel:shortcut-hide；用户的物理键盘观感反馈尚待确认 |
| 普通中文文本显示 | 部分 | 通过辅助功能设置的中文文字可正常显示；不等于中文 IME 和剪贴板交互已通过 |
| 菜单栏、关闭所有窗口后恢复、主动退出 | 待完整人工验证 | 已实现菜单栏及 close hook；本轮没有形成完整 UI 证据链 |
| 全屏应用、多显示器、通知点击 | 未验证 | 需要专项桌面验收 |

自动化工具最初发送的 Alt+Space 只成为应用内按键，未触发 Carbon 回调，因此没有将这些尝试标记为通过。后续日志出现真实回调才增加触发证据。

自动化在中文粘贴时报告剪贴板读取超时、noWindowsAvailable，并曾提示用户正在改变窗口；这些尚不能归因为 Wails 输入框缺陷。未为了消除工具报错而修改系统权限或输入法设置。

## 实现选择与后续约束

原型通过 Go 创建两个窗口，浮窗设置 `MacWindowClassPanel`、`NonActivating`、`FloatingPanel`，启用 `HideOnEscape`，关闭 `HideOnFocusLost`。调用窗口隐藏不会触碰后台任务。

窗口关闭 hook 取消原始关闭并隐藏窗口；`ApplicationShouldTerminateAfterLastWindowClosed` 为 false。菜单栏提供打开浮窗、打开主窗口和退出入口。这些配置应在下一轮人工验收中确认全部行为。

原型使用 `Call.ByName` 和每 400ms 状态轮询，以减少验证代码。正式产品应采用生成的类型绑定和明确事件设计，不直接复制原型的轮询架构。模拟任务只用于验证生命周期，不是 Agent 实现。

编译观察到 Wails 原生对象的 macOS deployment target 与链接目标不一致警告。当前 macOS 15.6.1 可运行，但没有验证旧系统兼容性。正式发布前必须统一部署目标，明确最低 macOS 版本，并验证签名、公证与打包。

## 原型与复现

源代码分支：`prototype/wails-desktop`。归档提交：`b2ed3bd`。主分支不包含可丢弃原型代码。

在 `/Users/bytedance/self-workspace/pi-popchat` 执行：

```sh
git switch prototype/wails-desktop
sh scripts/run-desktop-probe.sh
```

运行脚本构建并启动 `work/Pi Popchat Probe.app`。已有原型运行时先从其菜单栏退出，以免多实例争用快捷键。原型不保存会话、不连接 Pi 或真实模型，也不读取凭证。

原始本地运行日志位于忽略目录 `work/desktop-probe.log`，本报告记录关键观察，避免把临时构建产物加入主分支。
