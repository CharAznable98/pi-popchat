# Pi Popchat

macOS 桌面 Agent 客户端。`⌥Space` 唤起浮窗，主窗口管理历史；Go + Wails v3 + React，一期接入用户本机的 Pi。

应用只负责交互、会话和消息交接：不实现 Agent 循环、不直接调用模型、不读取或管理模型凭证。

## 运行

在 macOS 15 / Apple Silicon 上构建（本机实际验证环境为 macOS 15.6.1）：

```sh
sh scripts/build-app.sh
sh scripts/run-app.sh
```

需要 Go 1.26、Node.js 24、Xcode Command Line Tools，以及用户自行安装配置的 Pi。兼容基线是 Pi 0.84.1；应用会检测 Pi，缺失或配置错误时提供指引。Finder 启动也能发现常见 nvm/Homebrew 安装。

构建产物为 `dist/Pi Popchat.app`，使用本机 ad-hoc 签名，不是已公证的公开发行包。没有内置模型密钥或额外服务。其他 macOS 版本和 Intel 尚未声明支持。

## 使用

- `⌥Space`：在当前活动窗口的显示器唤起；同屏再次按下收起。点击外部不会收起，Esc 收起不会停止任务。
- Enter 发送、Shift+Enter 换行；支持图片粘贴、文件拖入、Agent 命令菜单与模型选择。单个附件上限 32 MB，每条消息的图片总计上限 16 MB。
- 执行中发送默认排队；“立即插入”由 Pi 在可接受时机处理。停止、失败或重启后待发送队列暂停，需主动恢复。
- 浮窗隐藏超过 30 分钟且不在执行/等待回答时，新建浮窗会话。旧会话始终可在主窗口继续。
- 关闭窗口后菜单栏常驻。菜单栏、原生“窗口”菜单可恢复界面；主动退出有运行任务时先确认。

应用数据位于 `~/Library/Application Support/Pi Popchat`：SQLite 管理应用索引、草稿及队列；`sessions/` 是本应用的 Pi 会话；`workspaces/` 是默认工作目录；`attachments/` 保存附件。用户 Pi 的配置和凭证仍由 Pi 自己使用。删除历史不会递归删除用户选择的目录或任务产物。

macOS 通知需在系统设置中允许 Pi Popchat。通知不抢焦点，点击定位触发通知的会话。测试可设置 `PI_POPCHAT_DATA_DIR` 使用隔离数据，正常使用不需要设置。

## 开发与验证

```sh
sh scripts/test.sh
# 仅在允许调用实际模型时启用：
PI_POPCHAT_REAL_TEST=1 go test -race ./internal/agent/pi ./internal/core -count=1 -v
# 先退出正在运行的 Pi Popchat，检查期间不要抢占桌面焦点：
sh scripts/check-desktop.sh
```

`test.sh` 包含前端作者测试、独立验收测试、Go 行为测试与 race 检查；真实模型测试默认跳过。`check-desktop.sh` 使用临时数据和假 Agent，仅操作本应用的真实原生窗口，按 JSON 结果判定成功，不以进程退出码代替验收。

`sh scripts/generate-bindings.sh` 从 Go 服务生成前端调用绑定。依赖固定版本和锁文件随仓库提交；修改服务接口后重新生成绑定。

## 架构与证据

| 位置 | 内容 |
|---|---|
| `internal/agent` | 可替换 Agent 边界及 Pi JSONL 子进程适配 |
| `internal/core` | 会话、流式事件、队列、恢复、SQLite 与附件 |
| `desktop.go` / 原生桥接 | macOS 窗口、快捷键、通知、文件和退出生命周期 |
| `frontend/src` | React 状态协调与独立展示组件 |
| `docs/reviews` | 架构、后端、前端及原生独立评审记录 |

需求见 [requirements](docs/requirements.md)，架构见 [implementation-plan](docs/implementation-plan.md)，协议实测见 [Pi compatibility](docs/pi-compatibility-report.md)。最终验收状态以 [交付与验收报告](docs/delivery-report.md) 为准，独立模拟测试不替代真实模型或操作系统验收。
