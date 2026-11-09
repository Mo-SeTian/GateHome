# GateHome Aurora Next · Vue 3 前端工程模版

基于 **Vue 3 + Vite 6 + Vue Router + Lucide Icons** 构建的 GateHome 现代化极客控制台前端模版。严格遵循 [Aurora Next 极客设计规范](../design-system/gatehouse/AURORA-NEXT-SPEC.md)。

---

## 🚀 快速启动

```bash
# 1. 进入前端目录
cd frontend
npm ci

# 2. 启动本地开发实时预览 (热重载)
npm run dev
# 访问地址: http://localhost:5173

# 3. 生产打包构建
npm run build
# 产物输出至 dist/，版本读取仓库根目录的 VERSION

# 4. 本地预览生产构建
npm run preview
```

---

## 页面与联调状态

当前 Vue 工程使用模拟登录和业务数据，可预览独立登录页及 17 个控制台页面。现有业务功能和逐页验收项见[迁移验收清单](../docs/aurora-next-migration-checklist.md)。订阅管理对应防火墙 IP/CIDR 规则源。

---

## 🎨 设计亮点与交互组件

1. **SpotlightCard 光晕跟随**: 卡片监听鼠标指针坐标，通过 CSS 变量 `--spotlight-x` 和 `--spotlight-y` 产生沉浸式 Obsidian 径向光晕。
2. **全局命令面板 (⌘K / Ctrl+K)**: 随时按下快捷键唤起，支持拼音与关键词全域模糊检索 17 个功能模块与常用快捷指令。
3. **全端自适应响应式侧边栏**: 桌面端紧凑固定；移动端与平板端平滑滑动抽屉（无黑边溢出，具备毛玻璃高斯遮罩）。
4. **轻量通知反馈 (Toast)**: 无论启停路由、同步 DDNS 还是复制链接，均有丝滑浮动气泡提示。
5. **双主题引擎**: 原生支持 Obsidian Tech 深色黑曜石主题与 VisionOS Frosted 浅色磨砂主题。
