# 第四组独立验收：macOS 原生与生命周期

日期：2026-09-09。最终结论：本组发现的阻断问题已关闭，原生自动化验收通过；没有发现仍需阻断交付的确定缺陷。通知物理点击和真实中文输入法候选框操作未验证，不应声称这两项已经人工通过。

本组未参与初版实现，独立审查正式源码与固定版本 Wails v3.0.0-beta.18；编写独立回归和 owned-window 探针。为遵守桌面独占约定，本组没有启动应用或操作 CUA。真实原生及通知探针由主 agent 串行执行，本组最终只读取源码和原始日志核验结果。

## 最终证据

| 证据 | 核验结果 |
| --- | --- |
| `docs/validation/native-desktop-pass-1.json` | 19/19，failed=0；3块真实显示器 |
| `docs/validation/native-desktop-pass-2.json` | 19/19，failed=0；重复运行通过 |
| `work/production-native.log` | 生产构建实际输出19/19，failed=0；逐项pass均为true |
| `docs/validation/notifications.json`、`work/production-notifications.log` | systemDelivered=true、noFocusSteal=true、nativeCallbackTargetsSession=true；physicalNotificationClickObserved=false |
| `native_acceptance_test.go` | 本组独立关闭回调、退出判断、非法文件/URL三组曾以 `go test -race . -run TestNativeAcceptance -count=1` 通过（1.752秒）；本次最终静态复核未重复执行测试 |

19项原生检查覆盖：首次导航前主动隐藏后不复活；两WebView就绪；隔离会话；3屏活动窗口识别和浮窗定位；异屏移动/同屏隐藏；转入主窗口仍为同一会话；两窗口关闭只隐藏；Dock reopen仅主窗口；owned主窗口真实进入全屏、浮窗可见且有键盘焦点、真实退出全屏。全屏以 NSWindow DidEnter/DidExit 事件和实际状态共同判定，没有用固定等待时间冒充转换完成。

## 缺陷关闭复核

### N1 · P2 · 关闭后的异步回调继续访问已关闭数据库：已关闭

初版独立测试先Close再Refresh，复现错误从空变为 `保存失败：sql: database is closed`；关闭后Select还错误返回成功。修复后核心关闭状态拒绝迟到写入，桌面done检查拒绝退出后动作，OnShutdown先关闭done并分离通知handler，再Close engine。独立红测试已转绿。

### N2 · P2 · Dock与晚到导航绕过浮窗生命周期：已关闭

Wails默认Dock reopen会显示全部隐藏窗口；应用现取消该默认事件，只调用showMain。最终实际证据为 `delegate=AppDelegate supported=true hookDelta=1 mainVisible=true panelVisible=false`。

另确认Wails `webview_window_darwin.go` 的run复制初始options，导航完成后异步读取旧Hidden并Show；runtime-ready自身没有Show。正式应用两窗初始均Hidden:true，以mainWanted/panelWanted表达当前意图；取消框架导航默认处理，按当前意图恢复可见性。showMain/hideMain/showPanel/hidePanel/applyVisibility的意图读写和原生Show/Hide已放入同一主线程执行，没有持d.mu等待跨线程InvokeSync。

导航hook不重做PanelShown或启动Agent，只同步可见性；关闭浮窗继续记录隐藏时刻。当前没有配置options.JS/CSS，取消框架导航注入分支不会丢失应用自定义注入。

早期探针曾出现Dock偶发失败和全屏转换同步不足，未将失败误报通过；补齐导航前隐藏场景、事件时间序列与原生全屏完成事件后，两份归档及生产构建日志均通过。旧导航Show路径不能单独解释所有早期panel复活现象，因此不将历史触发原因过度归结于一个未经证明的分支。

### N3 · P2 · 通知失败只有终端日志：已关闭

通知错误现经回调显示系统设置启用指引，普通用户无需查看终端。通知数据携带sessionID，默认动作回调定位该会话；应用退出时分离handler。最终探针证实真实系统通知中心已接收通知，发送未显示窗口抢焦点，同一原生回调入口正确恢复指定会话。

## 架构核验

- 单实例锁在application.New内部同步取得，主程序之后才OpenStore/Core.New，第二实例不会先改写会话状态。
- 窗口关闭hook取消销毁并隐藏，不调用Agent停止；明确退出才进入任务提示与关闭流程。
- 快捷键先注册新组合，成功才注销旧组合；失败保留旧值。活动屏幕按frontmost应用普通窗口最大相交面积选择。
- 构建脚本与Info.plist一致要求macOS15.0；本机架构与ad-hoc签名验证有效，不能将其描述为公证或Intel兼容验证。
- `scripts/check-native-desktop.sh` 每次使用全新报告路径，检查报告存在、suite正确、结果非空、failed=0及全部pass；不依赖AppKit进程退出码0判断通过。

## 明确保留的验证边界

1. 通知证据是**真实OS送达 + 原生回调入口验证**，没有物理点击系统通知横幅；报告明确保存physicalNotificationClickObserved=false。
2. 未物理操作中文输入法候选框；前端组合输入测试不能替代真实IME体验验收。
3. 全屏验收使用应用自身真实NSWindow/NSPanel，确认可见性与键盘焦点；不是任意第三方全屏应用、系统安全界面或像素遮挡的全面保证。
4. 本组的无模型原生探针只验证桌面层，真实Pi/模型能力由独立Agent验收记录覆盖，不将mock桌面任务写成真实模型测试。

上述为验证范围边界，不是已知失败。当前证据支持本组范围内通过；交付说明应保留这些事实。

## 主实现者追加闭环记录：投递收尾

独立评审确认 `advanceIdleQueueLocked` 在同一锁内释放投递占用、检查 idle/暂停/关闭与其他投递后出队，不重复投递。评审追加指出保存失败时应将回滚后的队列显式暂停，主实现者已修复。确定性回归覆盖正常推进、保存失败且恢复后主动继续、停止后不再投递，连续 10 轮 race 通过。此段为主实现者补记，独立判断来源为本组后续只读评审。

## 追加队列修复独立签注

本组最终只读复核 `scheduleLocked`、`advanceIdleQueueLocked`、`finishCommand` 及 `delivery_regression_test.go`：最后一个提交预留释放后，在同一锁内仅当会话idle、队列未暂停、应用未关闭且没有其他提交时才推进；出队后先持久化再启动RPC。保存失败明确设置QueuePaused并发布状态变化，不会留下未提示暂停且没有推进触发的队列。finishCommand已共用该推进入口。

确定性回归现覆盖正常推进、保存失败后明确恢复才发送、停止后不再发送三分支，并检查没有额外prompt。实现者报告该回归10轮race通过；本组读取 `work/release-tests.log`，确认最终Go各包检查及前端18项React测试、生产构建均通过。本组未重复运行或操作桌面。

结论：本组指出的保存失败队列停滞缺口已闭环；未发现本次修复造成提前推进、重复投递或停止后误启动的残留阻断。`docs/delivery-report.md` 对原生19项与真实通知送达/回调证据的说明准确，保留了未物理点击通知、未物理操作IME候选框等边界。
