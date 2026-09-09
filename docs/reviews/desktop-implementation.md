# macOS 集成实现记录

日期：2026-09-09。本文是实现者记录，不是独立桌面验收报告。

## 已实现

main.Desktop 暴露 Snapshot、Action、SaveAttachment，并路由到 core。所有发送和草稿以 payload.id 优先绑定发起时会话；发送去重 ID、草稿 CAS 字段及附件持久化走 core，不在桌面层重复存储。

主窗口 NSWindow 与浮窗 NSPanel 共用 Engine。窗口关闭变隐藏；浮窗 HideOnFocusLost=false，Esc 交给 React 处理输入法优先级。每次快捷键唤起按活动窗口几何选择显示器，复用原型实测桥接；调用 PanelShown/PanelHidden 维护超时。转入主窗口定位同一会话。

Wails SingleInstance 在打开数据库前获取单实例锁，第二实例只通知已有实例显示主窗口。菜单栏提供浮窗、主窗口与退出；ShouldQuit 在活跃任务时展示原生确认，取消不退出，确认后 Engine.Close 回收进程。空闲回收定时器随应用关闭停止。

Pi 路径使用适配器 Locate 的 PATH/Homebrew/nvm 查找；版本异步以 --version 获取，不读取凭证，补入 Pi 旁边 Node 所在路径以支持 Finder 启动。初始化后刷新主窗口会话的模型和命令。选择目录使用原生 NSOpenPanel；文件默认打开/在 Finder 显示用参数数组调用 /usr/bin/open，没有 shell 拼接。Web URL 限 HTTP/HTTPS。

通知使用独立 Objective-C UserNotifications 桥接，payload 保存 sessionID，点击显示并选择对应主窗口会话。隐藏状态下才发送，主窗口正在查看对应会话时抑制。授权失败不阻止对话。通知不激活窗口，只有点击回调显示主窗口。

## 编译与打包证据

scripts/build-app.sh 使用锁文件安装前端、执行 TypeScript/Vite 构建、Go 编译、生成 Info.plist，并做本机 ad-hoc 签名及验证。scripts/run-app.sh 可打开已构建应用。

已编译原生 Go/Objective-C 代码，并生成 `dist/Pi Popchat.app`。`codesign --verify --deep --strict` 通过；`file` 确认为 Mach-O arm64；`xcrun vtool -show-build` 确认 minos 15.0、SDK 15.5。部署目标在 Info.plist、CGO 编译和链接统一为 15.0，仅余重复 -lobjc 的链接提示。

本轮没有启动正式应用、操作桌面或占用焦点。要求后续独立验收覆盖实际快捷键、输入焦点、全屏、多屏、通知权限与点击、目录选择、托盘关闭/恢复、退出取消/确认。声明支持范围仅本机 macOS 15.6.1 / Apple Silicon 编译环境；未测试旧 macOS 或 Intel；没有 Apple Developer 签名/公证，不应称为已公证的外部分发包。

旧 Pi Popchat Probe 可能仍占用 Alt+Space，正式 UI 验收前应退出旧原型。
