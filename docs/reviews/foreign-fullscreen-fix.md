# 其他应用全屏时浮窗停留在桌面

## 原因

此前的原生验收只验证本应用自己的主窗口全屏，遗漏独立应用的全屏 Space。为了处理输入焦点而改成普通激活型 NSPanel 后，浮窗无法进入其他应用的全屏空间。浮窗 isVisible 或 isKeyWindow 为真均不能证明它在用户正在看的空间内。

新增独立 AppKit 测试进程进入原生全屏后，旧版本稳定得到 panelActiveSpace=false；增加 CanJoinAllApplications 单独也未解决：窗口属性已生效，浮窗拥有 key 状态，但不在 CGWindowList 的屏幕窗口列表中。

## 实现

保持 NonActivating NSPanel 的空间资格，同时在用户显式唤起时单独激活应用，再赋予浮窗键盘焦点；保留此前激活完成后恢复原屏幕和焦点的处理。普通通知或后台事件不触发该过程。窗口属性还包含 CanJoinAllApplications，Wails beta.18 未导出其名称，因此在项目内按 macOS SDK 的公开枚举值声明。

官方属性说明：[Apple CanJoinAllApplications](https://developer.apple.com/documentation/appkit/nswindow/collectionbehavior-swift.struct/canjoinallapplications)。该标志适用于加入其他应用全屏空间的浮动窗口；项目最低 macOS 15，覆盖其 macOS 13 可用范围。

## 验证方式

独立测试进程不共享应用 bundle 身份，等待 NSWindowDidEnterFullScreenNotification 后调用与快捷键相同的 togglePanel 路径。分别在三块物理显示器执行，并验证：目标屏幕正确、浮窗拥有焦点、浮窗在活动 Space、系统屏幕窗口列表中浮窗排列在仍可见的外部全屏窗口之前。测试随后终止自己的临时进程，等待空间退出动画完成，不读写用户任务。

这比“调用 Show 成功”或仅验证本应用全屏更严格；仍不声称模拟了所有第三方应用或物理快捷键事件。

## 本次结果与限制

- 首轮包含单屏外部全屏场景的 31 项原生检查全部通过。
- 扩展三屏的 37 项检查中，三屏外部全屏的启动、就绪及置顶共 9 项全部通过；另有 3 项普通窗口检查失败，报告捕获前台进程为 Chrome PID 1008 而不是指定测试进程，因此本轮不能记为整套通过。保留原始失败结果，未放宽断言或覆盖为零失败。
- sh scripts/test.sh 通过：28 项前端检查、Go 桌面和核心/适配器竞态检查、前端生产构建；生产应用构建及签名校验通过。
- 重启实际应用后，通过原生 UI 打开浮窗，按键 p 成功进入输入框；随后清除测试字符并验证 Esc 收起，未发送消息。

此前笼统的“全屏通过”结论覆盖范围不足；后续桌面验收必须继续保留外部进程全屏与系统窗口排列断言。
