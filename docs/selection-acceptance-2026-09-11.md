# 划词能力扩展验收：原型阶段记录

> 本文保留实现前的原型验收与失败证据；文中的“尚未实现”仅指当时状态。后续产品实现、权限/签名修复与验证结果见 [实施记录](implementation-plan.md)。跨应用完整验收仍不能据此标为通过。

日期：2026-09-11。环境：macOS 15.6.1，3 块屏幕。范围为隔离的原生交互原型与现有产品浮窗回归，产品划词功能尚未实现，不能把原型按钮回调当作 Pi 发送链路已通过。

## 1. 当前结论

已证实原生文本框与可编辑 WKWebView 能完成“系统鼠标拖选 → 自动出现工具条 → 点击取出同一份选中文字”，并可保留来源焦点与选区。原生文本框在三块屏幕的定位已通过。

**完整验收未通过。** 只读网页的程序拖选前置条件、Chrome/VS Code 的辅助功能焦点读取，以及全屏下 Esc 收起仍存在阻塞。未将这些场景降级为“已支持”，也没有通过剪贴板、模拟复制或 OCR 绕过。

## 2. 用例矩阵

| 场景 | 结果 | 证据与范围 |
|---|---|---|
| 原生 NSTextView，屏幕 0 | 19/19 通过 | 系统 CG 事件拖选、鼠标抬起监听、自动工具条、对象捕获、点击、焦点/选区保留、外部点击、Esc、双击、键盘全选及开关等 |
| 可编辑 WKWebView，屏幕 0 | 19/19 通过 | 同上；实际鼠标操作，不再依赖 JS 直接设置选区 |
| 原生 NSTextView，屏幕 1、2 | 各 19/19 通过 | 工具条处于对应屏幕可用区域内，来源选区与焦点保留 |
| 原生 NSTextView，全屏 | 19/20 通过 | 工具条在活动全屏 Space，点击捕获成功；Esc 收起未通过。Space 检查不等同于逐像素遮挡检查 |
| 只读 WKWebView | 未通过，不能宣称已支持 | 拖选后 DOM 对照也未得到预期选区，普通选区属性返回 `-25212`；文本标记兼容路径未解决鼠标用例。键盘全选可取到文本，不等于鼠标划词可用 |
| 真实 Google Chrome，独立临时配置 | 阻塞于前置条件 | 合成页面已打开，当前进程已激活，但 `AXFocusedUIElement` 返回 `-25212`；没有进入完整拖选与按钮用例，不能据此断言 Chrome 不支持 AX |
| 真实 VS Code 1.127.0，独立临时配置 | 阻塞于前置条件 | 截图可见 19 字符被选中，焦点元素查询仍返回 `-25212`；已尝试 Electron 文档中的辅助功能开关，没有将失败标成通过 |
| 原生安全输入框 | 4/4 通过 | 使用合成安全输入，实际读不到选中文字且不弹工具条，不涉及真实密码 |
| 全新签名应用身份，无辅助功能授权 | 实测未授权 | 通过 LaunchServices 启动新的 ad-hoc 签名 `.app`，`AXIsProcessTrusted=false`，没有申请权限、修改已有授权或操作 TCC 数据库 |

通过的 19 项检查包括：测试窗口准备、真实探针权限、拖选文本一致、系统鼠标抬起事件送达、自动工具条、来源焦点、对应屏幕、可用区域约束、点击捕获内容、点击后隐藏、来源选区保留、外部点击隐藏、总开关关闭、按钮删空、权限门控、双击选词、键盘选区、已隐藏内容不投递、Esc 隐藏。

总开关、空按钮和过期内容保护验证的是**探针自身**的交互逻辑，不是尚未实现的产品设置。权限门控用例为注入拒绝状态；单独的签名应用用例验证了真实首次未授权状态，未验证系统权限被实时撤销。真实键鼠硬件输入、飞书、PDF、其他浏览器、复杂多行/混合语言与多选区仍未验收。

## 3. 失败诊断与停止点

| 问题 | 已有证据 | 当前判断 |
|---|---|---|
| 第一轮可编辑网页拖选失败 | 探针把 WKWebView 的 flipped 坐标又反转一次 | 修正测试装置坐标后，19 项通过；不算产品修复 |
| 第一轮原生跨屏定位失败 | 测试窗口初始化后未重新应用目标位置 | 设置实际窗口位置后，屏幕 1/2 各 19 项通过 |
| 第一轮原生键盘全选失败 | 独立测试应用未提供全选菜单动作 | 增加标准测试菜单后通过；不归因于 Pi 或产品浮窗 |
| 只读网页仍失败 | DOM 对照不符合预期选区，部分后续动作可读到文本 | 连续三次针对性调整后停止修改；可疑前提是“当前事件注入已等价于用户真实拖选”，需独立人工复测，不继续猜测 API 兼容性 |
| Chrome/VS Code 焦点不可读 | 内容可见，VS Code 截图确认已有选区，但焦点查询为空 | 处理了过长 Unix socket 路径、隔离 Chrome 焦点、Electron 辅助功能属性及系统焦点/子元素路径；连续三次修正仍未通过后停止修改。可疑前提是“当前隔离启动方式能稳定暴露与日常使用一致的 AX 焦点” |

全屏 Esc 单独保留失败：来源应用也可能响应 Escape 并退出全屏 Space。全局只读事件监听不具备拦截来源应用按键的能力；当前证据不足以认定这是产品可接受行为或明确缺陷所在，不能将“退出全屏后某时刻消失”替代“按 Esc 收起工具条”的验收。

## 4. 复现与证据

原生测试必须串行，测试期间会移动鼠标、改变焦点并输入测试快捷键；仅在专属合成窗口运行，结束后恢复原鼠标位置与之前的前台应用。没有发送真实聊天消息或调用真实模型。

```sh
# 执行原生/可编辑网页/只读网页/安全输入/跨屏/全屏矩阵
sh scripts/check-selection-interaction.sh

# 单独复现真实应用前置条件失败
SELECTION_CASES=chrome:0:0,vscode:0:0 sh scripts/check-selection-interaction.sh

# 单独复现只读网页和全屏阻塞
SELECTION_CASES=web-readonly:0:0,native:0:1 sh scripts/check-selection-interaction.sh

# 新签名应用的真实无权限检查，不弹授权请求
sh scripts/check-selection-permission.sh

# 现有产品浮窗回归，不启动划词原型
sh scripts/check-native-desktop.sh
```

每次脚本为探针建立独立 `work/selection-interaction-*` 目录，保存每个场景的 JSON、stderr、合成选区几何和必要截图。Chrome/VS Code 采用短路径 `/tmp/pcsel-<pid>` 保存独立配置，避免 VS Code 的 Unix socket 路径超过 103 字节。测试不会修改用户常用配置或安装扩展；VS Code 保持限制模式。

`outputs/selection-interaction-results.json` 是最近一次运行的结果，不应误当作全部历史结果。`outputs/selection-acceptance-summary.json` 汇集各场景最近证据路径；完整失败记录仍保留在各次 `work/` 目录。`outputs/selection-permission-results.json` 为无权限结果。忽略目录不进入提交，本文件保留可复现步骤与结论。

最终版探针对原生屏幕 0/1/2、可编辑网页与安全输入进行了独立复测，共 80/80 项通过。证据为 `work/selection-interaction-a0ckqJ/`。这 80 项不包含上表仍未通过的场景，不能替代完整验收结论。

本轮重新构建现有产品成功。首次原生浮窗回归为 58/64 项通过，6 项失败分别是屏幕 1 的两个边缘选屏、屏幕 3 居中、浮窗键盘输入、屏幕 2 的其他应用窗口唤起后焦点、屏幕 2 的其他应用全屏浮窗焦点。证据为 `work/native-acceptance-lh43v70n/result.json`，完整日志为 `outputs/selection-native-regression.log`。产品运行代码未修改，不将这些失败归因于划词代码；保留一次独立复测以判断重复性。

独立复测为 57/64 项通过：上述 6 项再次失败，另增加屏幕 2 居中失败。证据为 `work/native-acceptance-n7zia8pu/result.json`，日志为 `outputs/selection-native-regression-repeat.log`。因此不能把首轮失败简单解释为一次性焦点干扰；现有浮窗也未满足本轮原生验收门槛。没有擅自修改产品逻辑进行补救，保留失败复现供后续诊断。

## 5. 产品接入门槛

当前可进入原生控件范围的实现讨论，但不能承诺完整跨应用划词。至少应先完成只读网页和 Chrome 的独立人工拖选复测，区分事件注入、来源焦点和取词 API 三个问题；再确定飞书等首批支持应用，逐一通过实际使用场景。

独立人工复测页为 `scripts/fixtures/selection-manual.html`：用真实 Chrome 打开，拖选首行，检查页面显示的 DOM 选区与鼠标抬起计数。该页不显示工具条，仅用于验证“真实鼠标已建立正确选区”这一前提；页面包含合成英文、中文/多行及可编辑对照，不联网或投递内容。

### 人工复测后的进展

用户已反馈“复测页正常”。这确认了人工操作下页面选区显示正常；尚不能作为 AX 读取、工具条或 Pi 发送通过的证据。

随后重跑 Chrome 单场景仍在焦点查询处失败。针对性只读诊断发现：系统焦点对象所属 PID 为日常 Chrome 进程，与本次隔离 Chrome 的目标 PID 不同；主线程查询也为空。目标进程的 `AXEnhancedUserInterface` 返回 `-25208`，`AXManualAccessibility` 返回 `-25205`。因此此前对隔离进程的查询不能代表日常 Chrome 的真实取词能力，需区分多实例焦点归属与浏览器本身的 AX 能力。诊断证据保存在 `work/selection-interaction-GKiOkS/chrome-0-0.ready.json.focus.json`；临时诊断代码已移除。

新增最小只读检查 `scripts/read-selection-fixture.m`，仅扫描已打开且窗口标题匹配复测页的 Chrome 窗口，读取网页区域的选区，输出是否与合成英文一致及长度，不输出选中文字。不打开 URL、不激活窗口、不注入按键、不操作剪贴板、不修改授权：

```sh
clang -fobjc-arc -framework AppKit -framework ApplicationServices scripts/read-selection-fixture.m -o work/read-selection-fixture
work/read-selection-fixture
```

第一次运行该检查未定位到标题匹配的窗口（`fixtureWindowFound=false`），因此尚未验证选区读取。需要用户在 Chrome 保持复测页打开并选中英文首行后再执行；这个结果不证明窗口不存在或 Chrome 不支持取词。

浏览器工具的 URL 安全策略拒绝自动打开本地 `file://` 复测页，未换通道自动导航。上述检查只读取用户已打开的匹配窗口，与被拒绝的自动导航分开；后续需用户手动保留该测试页面。

默认开启总开关不代表默认拥有系统权限。需要设计首次授权说明及缺失权限状态；安全输入或不支持取词时的降级行为仍需按需求决策确认。按钮点击后的独立新会话、共享草稿保留、Pi 投递及复用现有浮窗，还需要产品实现后的行为/race 与原生端到端验收。

参考：[Electron 辅助功能文档](https://github.com/electron/electron/blob/main/docs/tutorial/accessibility.md)、[VS Code 独立配置 CLI](https://code.visualstudio.com/docs/configure/command-line)、[Chromium 独立配置](https://www.chromium.org/developers/creating-and-using-profiles/)、[Apple 事件监听说明](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/EventOverview/MonitoringEvents/MonitoringEvents.html)。公开文档仅支持机制说明，表中的通过/失败均来自本机测试。
