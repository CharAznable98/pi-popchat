# pi-popchat

面向 macOS 的桌面 Agent 对话客户端：使用全局快捷键唤起置顶浮窗，在主窗口中管理和继续历史会话。

产品专注于用户交互和接入成熟 Agent。一期接入 Pi，后续考虑 Codex、Claude Code。应用不实现 Agent 循环、不直接对接模型、不管理模型凭证。

当前阶段：需求基线和 Pi 0.84.1 接口验证已完成；已选择 Go + Wails v3 + React，并完成首轮桌面原型。正式产品代码尚未开始。

## 文档

- [需求文档](docs/requirements.md)：已确认需求、交互规则、验收条件及待定细节。
- [Pi 兼容性验证](docs/pi-compatibility-report.md)：17 项接口测试与已知边界。
- [验证结果](docs/pi-compatibility-results.json)：逐项机器可读结果。
- [技术选型记录](docs/technology-selection.md)：决策范围与待评估问题。
- [桌面原型验证](docs/desktop-probe-report.md)：锁定版本、实测结果和原型分支位置。

## 复现接口验证

本机需已安装 Pi 0.84.1 和 Node.js，本次验证使用 Node.js 24.15.0。在项目根目录运行：

```sh
node scripts/verify-pi.mjs
```

脚本调用真实 Pi 子进程及本地模拟模型服务，不连接真实模型服务、不读取真实凭证。测试夹具写入 `work/`，结果写入 `outputs/`。PASS 包括预期不支持能力的负向测试，不能理解为 Pi 原生具备所有产品功能。
