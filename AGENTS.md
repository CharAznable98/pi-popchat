# pi-popchat 工程约定

始终使用简体中文。产品需求见 docs/requirements.md，模块边界见 docs/implementation-plan.md。

- 应用只接 Agent，不调用模型API或读取模型凭证。真实模型测试必须显式启用 PI_POPCHAT_REAL_TEST=1。
- internal/agent 是可替换 Agent 边界；internal/core 拥有会话/队列/持久化；desktop.go 与原生桥接处理 macOS；frontend 只调用类型化应用接口。
- 隐藏/切窗不能停止Agent。所有会话动作绑定发起时的sessionId。响应success不是任务完成；恢复不自动重发。
- 修改并发或恢复必须跑对应行为/race测试。默认验证 sh scripts/test.sh；本机应用 sh scripts/build-app.sh。
- work/、dist/、outputs/是忽略目录。不得向测试报告写入用户凭证或真实私人任务内容。测试用专属临时会话/文件。
- 保留独立评审的失败复现测试。原生桌面验收串行运行，避免不同测试抢占焦点。
