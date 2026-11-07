# GateHome 使用的 LeiBox 组件

来源：[LeiBox 点阵加载](https://leibox.pantoria.cn/docs/animations/ai-generating)，原生 DOM 版本，2026-11-07 获取。完整下载文件保存在 `ai-generating/`，保留来源声明。目录没有提供可扩展到整个组件库的统一许可证，不据此推断其他参考素材的许可。

组件用于实际请求的等待反馈，不表示完成百分比。`loader.js` 提供元素连接 / 移除、减少动态效果和离开视口时的生命周期；任务状态仍由原有 API 控制。

GateHome 对 `motion-runtime.ts` 的唯一适配是将动态 `<style>` 改为本站外部样式表，保持管理界面原有 `style-src 'self'`。`scripts/build_leibox.mjs` 生成 `web/leibox-ui.js` 与 `web/leibox-loader.css`，样式隔离在组件自己的 Shadow DOM 中，不影响 Sun-Panel。

构建需要 Node.js 22.13 或以上：

```sh
node scripts/build_leibox.mjs
```

其余导航、模态框、分组与菜单沿用 GateHome 的原生 DOM / Web Animations，不新增动画框架。
