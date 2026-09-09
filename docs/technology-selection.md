# 技术选型记录

状态：用户已确认 **Go + Wails v3 + React**。已使用 Wails v3.0.0-beta.18 和 React 19.2.8 完成首轮桌面行为原型；正式工程及本机签名打包已实现，验收结论见 delivery-report.md。

2026-09-09：用户熟悉 Go，要求优先评估 Go 相关技术栈。维护者熟悉度作为长期维护成本的重要依据，SwiftUI + AppKit 不再作为首推路线。

## 已确定约束

仅 macOS，要求全局快捷键、置顶浮窗、明确的焦点行为、菜单栏常驻、系统通知和双窗口会话展示。客户端需要流式消息、图片和文件输入、必要交互卡片以及历史管理。

一期通过成熟 Pi 能力执行任务。Pi 0.84.1 的 RPC 已完成验证，可作为候选接入方式；选用 SDK 应有明确收益，不能为了界面层重复封装 Agent。

## 桌面框架候选

| 方案 | 主要评估点 |
|---|---|
| Wails v3 + Web 前端（当前推荐候选） | Go 管理会话、消息队列和 Pi RPC 子进程；Web 前端负责聊天界面；官方提供多窗口、系统托盘、全局快捷键及 macOS NSPanel 选项，需锁定版本后实测 |
| SwiftUI + AppKit | NSPanel、窗口焦点和系统集成直接；富文本、代码块和交互聊天组件需要评估实现成本 |
| Tauri + Web 前端 | 聊天界面可复用 Web 生态；特殊 macOS 浮窗行为可能需要 AppKit 桥接 |
| Electron + Web 前端 | Web UI 与 Node 子进程接入方便；需实测常驻资源占用及 macOS 浮窗行为 |

## 决策顺序

先选桌面 UI 与系统集成路线，再确定 Pi 进程生命周期及适配接口，随后确定存储和文件目录，最后确定签名与分发。每项决策记录依据、取舍和验证方式。

用户已同意 Wails v3 验证路线，并指定 React 前端。可丢弃原型位于 `prototype/wails-desktop` 分支，提交 `b2ed3bd`；主分支仅保留需求和验证结论。详细结果见 [桌面原型验证](desktop-probe-report.md)。

用户已人工确认 ⌥Space 唤起正常。2026-09-09，用户确认 Pi 进程与会话生命周期方案：每个执行中的会话拥有独立 Pi RPC 子进程，同一会话的浮窗和主窗口共享该进程；历史会话按需恢复，空闲进程可回收。回收时机及并发资源限制随后设计。

用户随后反馈多显示器定位缺失，并明确按当前活动窗口所在屏幕唤起。原型修复为每次唤起重新读取窗口所在屏幕，使用 Wails SetScreen 定位；3 块屏幕的 11 项原生检查通过。该能力需一小段 AppKit/CoreGraphics 元数据桥接，未更换 Go + Wails + React 技术路线。2026-09-09，用户确认修复后的多显示器体验正常。后台进程方案已确认，见上文。

## Wails 初步核查

核查日期：2026-09-09。下表是初步官方文档/示例核查；随后完成了锁定版本的编译及真实窗口验证，实测覆盖范围以桌面原型报告为准。

| 能力 | 官方材料结论 |
|---|---|
| 主窗口与独立浮窗 | v3 原生支持多窗口；v2 是单窗口模型，不建议为该项目绕过 v2 限制 |
| 菜单栏常驻 | System Tray API 在 macOS 对应菜单栏图标 |
| 全局快捷键 | 官方 global-shortcuts 示例说明支持后台触发，macOS 使用 Carbon hot keys |
| 浮窗与焦点 | 当前文档包含 NSPanel、NonActivating、FloatingPanel、WindowLevel 和 Spaces 配置 |
| Esc/失焦行为 | 有 HideOnEscape 和 HideOnFocusLost；需实测其与输入框、弹出菜单和中文输入法的事件优先级 |

维护者文档已宣布 v3 Beta，但同时说明尚未达到稳定版。不能把当前文档字段默认视为任意历史 alpha/beta 版本均可用。选定路线后应锁定一个实际发布版本，并核查该版本源码与可执行原型；不直接依赖浮动 master。

建议职责划分：Go 负责会话状态、Pi 子进程及 JSONL 适配、客户端待发送队列和桌面服务；Web 前端负责聊天、输入及历史界面。后台仍调用用户安装的 Pi，不把 Agent 循环搬进 Go。无需因 Pi 使用 Node 而在应用中增加一层 Node 中间服务。

首个验证原型应覆盖：系统快捷键唤起并输入、Esc 隐藏而点击外部不隐藏、主窗口与浮窗并存、关闭窗口后菜单栏常驻，以及多显示器/全屏时的焦点行为。通知点击定位和中文输入法行为也应在产品实现前完成检查。若框架缺口需要原生桥接，再评估桥接的最小范围；不预先假定必须写 Swift 或 Objective-C。

来源：[v3 多窗口](https://v3.wails.io/whats-new/)、[v2 到 v3](https://v3.wails.io/migration/v2-to-v3/)、[系统托盘](https://v3.wails.io/features/menus/systray/)、[全局快捷键示例](https://github.com/wailsapp/wails/tree/master/v3/examples/global-shortcuts)、[窗口选项](https://v3.wails.io/features/windows/options/)、[v3 Beta 公告](https://v3.wails.io/blog/wails-v3-beta/)。

## 已确认：异常中断后的恢复

2026-09-09，用户确认：应用或 Pi 异常退出后保留已落盘历史，将受影响任务标记为中断；重启不自动重发此前正在执行或排队的消息，由用户决定继续或重试，避免重复执行有副作用的工具操作。具体中断检测和队列持久化方式随后设计。用户选择继续不代表工具可以从中断位置恢复。

## 后续推进

已有决策足够进入工程方案与真实对话闭环，不再逐项确认内部目录等实现细节。模块边界、存储建议、投递恢复边界和分阶段验收见 [一期架构与实施计划](implementation-plan.md)。该计划中的新增工程建议不等于已经过实现验证。

## 实施结果

正式产品使用生成的 Wails 类型绑定、事件驱动会话快照、SQLite 本地存储和独立 Pi RPC 进程。React 拆分历史、消息、设置和交互组件；Go 核心不依赖窗口框架。原生导航完成回调由应用显隐意图控制，避免 Wails 初始 Hidden 副本覆盖用户后续隐藏操作。当前交付为 macOS 15 / arm64 本机签名包，尚未公证。

浮窗输入修正：正式浮窗保留 Floating NSPanel，显式唤起采用正常应用激活，不启用 NonActivating。已实测修复系统按键无法进入浮窗的问题；原生键盘、后台激活、多屏及全屏回归通过，见 reviews/panel-input-fix.md。

多屏激活回归修正：唤起前只采集一次活动窗口屏幕，并贯穿定位及应用激活完成过程。当前活动应用为自身时使用 AppKit keyWindow（包含浮窗），其他应用仍读取前台窗口几何信息。显式唤起等待应用激活完成后恢复浮窗焦点，关闭浮窗或主动打开主窗口会取消该待完成意图。见 reviews/active-window-screen-fix.md。

其他应用全屏修正：浮窗使用 NonActivating NSPanel 保留加入外部全屏 Space 的资格，并在用户显式唤起时单独激活应用和交付键盘焦点；二者不能再混为一个配置开关。保留 CanJoinAllSpaces、FullScreenAuxiliary，并增加 macOS 13+ 的 CanJoinAllApplications。上文“禁用 NonActivating”的阶段方案由此替代，详见 reviews/foreign-fullscreen-fix.md。
