# 前端与桌面服务契约

以 `internal/core/types.go`、`desktop.go` 和生成的 `frontend/bindings` 为可执行契约。前端使用生成的 `main.Desktop` 绑定，不自行拼接 Wails 方法名。

## 服务与同步

- `Snapshot(view)`：取得 `main` 或 `panel` 的完整快照。
- `Action(view, action, payload)`：执行操作并返回快照；失败拒绝 Promise，保留用户输入。
- `SaveAttachment(name, mime, base64)`：保存附件，返回结构化引用与图片预览。

`popchat:changed` 是失效通知；前端先订阅再加载，在事件或窗口重新聚焦时刷新，按 `version` 丢弃旧快照，普通会话不持续轮询。每个窗口最多一个在途快照请求，连续失效通知合并为一次后续刷新，刷新启动间隔至少 50ms；未变化的历史 Markdown 不重复解析。主窗口与浮窗各自选择会话，同一会话共享消息、运行进程、队列和草稿。

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


2026-10-27：`sessions` 仅为历史摘要，不包含消息、队列、模型、命令、草稿附件或交互对象；只有 `current` 包含完整展示数据。快照拥有独立的可变切片，不能通过返回值修改后端状态。

`panelRevision/panelPending` 为桌面浮窗展示状态，不持久化。原生窗口显示与隐藏先执行，会话恢复、隐藏时间保存和 Agent 准备在后台进行。`popchat:panel-opening` 原生 DOM 事件立即标记本次恢复；相同 revision 从 pending 变为 ready 后不得倒退，较早 revision 的结果不得解除当前输入保护。恢复期间只通过已有快照刷新通道重查状态，完成后停止；`send/new/select/transfer/respond` 在后端同时拒绝恢复中的浮窗操作，`hide` 不等待草稿保存或会话锁。

数学公式由 Markdown 语法节点识别，使用随应用打包的 KaTeX 渲染，禁止受信任命令。反斜杠定界符在 CommonMark 转义前解析；代码中的公式示例不转换。错误和未闭合公式保留原始源码，消息原文与持久化格式不变。


发送成功的 `Action` 快照带 `submission` 原会话回执：`sessionId/draft/draftRevision/draftAttachments/duplicate`。回执在提交的同一 Engine 锁内生成，覆盖正常发送、排队和按消息 ID 去重；不依赖当前窗口已切换到哪个会话。去重成功不意味着清空草稿或版本加一，前端不得据此推断 CAS 基线；存在后续编辑时保留其既有基线。缺失或错会话回执保留输入并报错。隐藏先启动绑定原 ID 的后台保存，同会话正在发送时等待发送结束，其他会话的发送不阻止保存。
