# 快捷键按鼠标选屏与浮窗菜单入口追溯

2026-09-10。分支：`feat/panel-mouse-screen`。

## 已确认范围

快捷键按触发时鼠标所在屏幕选屏，在该屏可用区域居中。浮窗同屏可见时再次按快捷键收起；跨屏时移动过去并聚焦。移动鼠标本身不移动窗口。应用顶部菜单入口本次不改变。

## 菜单入口历史

在任务“调研并细化 pi-agent 桌面插件”（任务 ID `01a08431-2365-7033-b1bc-3e5276a7fd12`）中，用户于 2026-09-09 明确要求：“状态栏上移除打开浮窗的按钮”。

提交 `10633b5` 最初同时注册了应用菜单和状态栏菜单的“打开浮窗”。随后提交 `cddc753` 从 `tray.SetMenu(menu)` 使用的状态栏菜单删除了这一项，保留“打开主窗口”和“退出 Pi Popchat”。当前代码延续了这次删除；并非删除后重新加回。

| 入口 | 当前调用链 | 本次处理 |
|---|---|---|
| 全局快捷键（默认 ⌥Space） | `setShortcut` → `GlobalShortcut.Register(key, d.togglePanel)` → `mouseScreen` → 同屏 `hidePanel` / 其他情况 `showPanelOn` | 改为鼠标选屏 |
| 应用顶部“窗口 → 打开浮窗” | `app.Menu.Set(appMenu)` → `windowMenu` 点击 → `showPanel` → `activeWindowScreen` → `showPanelOn` | 保留活动窗口选屏，只打开 |
| 右上角状态栏图标 | `tray.SetMenu(menu)` → 打开主窗口 / 退出 | 已无打开浮窗入口，保持现状 |
| Dock 再打开 / 第二实例启动 | reopen hook / single-instance callback → `showMain` | 保持现状 |

先前回复将应用菜单和状态栏菜单统称“菜单栏”，导致范围混淆。历史记录中的删除要求限定在状态栏，因此不能据此断言应用顶部入口属于漏删。

## 实现

原生桥接使用 `NSEvent.mouseLocation` 和 `NSScreen.frame`，两者同为 AppKit 全局点坐标，避免混用像素与点或翻转纵轴。选屏使用完整屏幕区域，包含菜单栏和 Dock；窗口定位继续使用 Wails `SetScreen`，在目标 `visibleFrame` 居中。选屏无匹配时回退到 `NSScreen.mainScreen`、首屏；无屏幕返回 nil。

目标屏幕在窗口显示及应用激活之前捕获一次；原有 `panelActivationScreen` 保留该目标，完成激活时不重新读取鼠标。隐藏、会话绑定、Agent 执行逻辑不变。

原生探针增加独立单实例 ID，避免测试转而唤起正在运行的用户实例。测试使用临时数据目录和模拟 Agent，串行运行，结束恢复鼠标位置。构建及原生验收脚本可共同使用 `PI_POPCHAT_BUILD_DIR` 指定测试产物目录。

## 验证

默认验证 `sh scripts/test.sh` 通过：前端纯逻辑 4 项、React 24 项，前端生产构建及 Go `-race` 检查通过。本机生产构建与签名校验通过。

原生验收使用 `PI_POPCHAT_BUILD_DIR=work/mouse-screen-build sh scripts/check-native-desktop.sh`，三块显示器共 64 项检查。新增 27 项覆盖各屏四角及中心选屏、前台主窗口位于另一屏时的鼠标屏幕居中、鼠标移动不自动搬动浮窗、跨屏快捷键移动并聚焦，以及同屏收起。原有输入、应用菜单按活动窗口定位、跨应用唤起、三屏其他应用原生全屏及关闭/Dock 回归继续保留。

首轮 62/64 通过，两项普通外部窗口唤起后的焦点检查失败，目标显示器均正确。其中一项的前台 PID 经只读进程核对属于其他应用，存在外部焦点干扰；另一项显示测试主窗口持有焦点，原因未确定。保留首轮原始结果（已移除临时会话目录），不将重跑通过当作已修复该不稳定现象。

相同构建、没有修改代码，第二轮 64/64 通过。结果见 `../validation/mouse-screen-native.json`；首轮记录见 `../validation/mouse-screen-native-first.json`。

此次测试直接调用已注册给全局快捷键的 `togglePanel` 回调，并操作真实 AppKit 窗口与鼠标；没有重新验证物理 ⌥Space 的系统按键投递。未覆盖显示器热拔插、所有显示缩放组合或输入法候选交互。运行中的正式应用没有被替换或重启，开发版位于 `work/mouse-screen-build/Pi Popchat.app`。
