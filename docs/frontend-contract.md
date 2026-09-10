# 前端与桌面服务契约

以 `internal/core/types.go`、`desktop.go` 和生成的 `frontend/bindings` 为可执行契约。前端使用生成的 `main.Desktop` 绑定，不自行拼接 Wails 方法名。

## 服务与同步

- `Snapshot(view)`：取得 `main` 或 `panel` 的完整快照。
- `Action(view, action, payload)`：执行操作并返回快照；失败拒绝 Promise，保留用户输入。
- `SaveAttachment(name, mime, base64)`：保存附件，返回结构化引用与图片预览。

`popchat:changed` 是失效通知；前端先订阅再加载，在事件或窗口重新聚焦时刷新，按 `version` 丢弃旧快照，不持续轮询。主窗口与浮窗各自选择会话，同一会话共享消息、运行进程、队列和草稿。

快照包含 `version/currentId/current/sessions/settings/environment/error`。会话包含草稿文字、`draftAttachments/draftRevision`、`messages/queue/queuePaused`、交互、工作目录、Agent 模型与命令、`provider/model` 及可见正文搜索索引。模型的唯一键为 provider + id。日期为 RFC3339。

## 操作约束

所有针对展示会话的异步操作携带发起时的 `sessionId`，不能在等待结束后读取另一个会话的输入。历史操作以 `id` 指定对象。

| 操作 | 关键载荷 |
|---|---|
| new / select | cwd（可选） / id |
| send | sessionId, text, attachments, clientMessageId（稳定重试 ID） |
| draft | sessionId, text, attachments, expectedDraftRevision；冲突保留本地输入 |
| stop / pauseQueue / resumeQueue | sessionId |
| insert / removeQueued | sessionId, messageId |
| rename / pin / delete | id，以及 title / pinned；删除前确认 |
| respond | sessionId, requestId, value, cancelled |
| model | sessionId, provider, id |
| refreshAgent / settings | 当前会话；shortcut / piPath |
| chooseDirectory | sessionId；仅空会话允许修改 |
| openFile / revealFile / openURL | path / url；相对路径先按展示会话 cwd 解析 |
| transfer / hide / showMain / quit | 窗口操作 |

状态为 idle、starting、running、retrying、waiting、interrupted、failed、stopped。状态说明只表示 Agent 执行情况，不断言用户业务目标已经达到。待发送队列由后端管理；前端不实现第二份投递调度器。

附件先持久化再提交；异步上传结果仍保存到原会话。输入法 composition 或键码 229 期间 Enter/Esc 不触发发送或隐藏。Markdown 不执行原始 HTML，链接仅交给已校验的 HTTP(S) 或本地文件打开功能。


2026-09-10：`current.draftOnly` 表示共享未发送草稿，仍有稳定 ID 和正常输入能力，但不出现在 `sessions` 中。`new` 复用唯一草稿；首次 `send` 携带 `expectedDraftRevision`，原子转为正式会话，ID 不变。`managedWorkspace` 由后端判定；删除只有用户明确选择时传 `removeWorkspace: true`，后端仍重新校验目录所有权和引用。
