# Popchat 图标

开口对话气泡与右上圆点表示随时弹出的对话。应用与侧栏使用苔绿色底（#3f5c46）和暖白符号（#f9f8f0）；macOS 状态栏使用同一几何形状的透明单色模板，由系统适配深浅外观。

唯一几何源为 scripts/generate-icons.swift。修改后在项目根目录运行 `sh scripts/generate-icons.sh`，生成应用 PNG、全尺寸 iconset / ICNS、44 px 状态栏模板及前端 SVG。构建脚本复制 ICNS，Go 嵌入模板，React 使用 SVG。
