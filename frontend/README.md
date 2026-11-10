# GateHome Aurora Next · Vue 3 控制台

基于 **Vue 3 + Vite 6 + Vue Router + Lucide Icons** 构建的 GateHome 控制台。遵循 [Aurora Next 设计规范](../design-system/gatehouse/AURORA-NEXT-SPEC.md)，登录、账户及全部 17 页已完成真实 API 迁移，版本为 `1.0.0`。

---

## 🚀 快速启动

```bash
# 1. 进入前端目录
cd frontend
npm ci

# 2. 启动本地开发预览，按实际端口指定 Go 管理端
GATEHOME_API_TARGET=http://127.0.0.1:16666 npm run dev
# 访问 http://127.0.0.1:5173

# 3. 生产打包构建
npm run build
# 产物输出至 dist/，版本读取仓库根目录的 VERSION

# 4. 本地预览生产构建
GATEHOME_API_TARGET=http://127.0.0.1:16666 npm run preview
```

在仓库根目录运行 `make build`，会构建 Aurora Next、SunPanel 和原版组件，再将资源嵌入 Go 程序。启动生成的 `dist/gatehouse` 后，管理根入口 `/` 跳转 `/console/`，例如本机 `http://127.0.0.1:17666/`。原版入口改为 `/legacy/`，设置页提供回退链接，两套界面沿用相同账户和配置；运行二进制不需要 Node.js、Nginx 或源码目录。

`frontend/embed.go` 中的 `//go:embed all:dist` 相对于该 Go 包，嵌入的就是 `frontend/dist`。从干净源码执行 Go 构建或测试前，先运行 `make frontend`；`make build`、`make test`、`make linux` 和 Docker 构建已包含此步骤。直接运行 `npm run build` 只更新前端产物，需重新编译 Go 才会更新二进制内的界面。

两款字体使用固定版本的 Fontsource 包并随 Vite 产物嵌入，浏览器不访问 Google Fonts；许可证位于 `public/licenses/`，运行时可从 `/console/licenses/` 下的对应文件读取。页面保留 Go 管理端的严格 CSP，不需要放开内联脚本或 `eval`。

浏览器回归需要先在仓库根目录运行 `make build`，再在本目录运行 `npx playwright install chromium` 和 `npm run test:browser`。测试会启动独立临时目录和随机回环端口，生成仅供测试的账号，通过普通 HTTP 域名验证新增、保存、并发冲突、发现服务、流量跳转和命令面板键盘行为；结束后停止程序并删除临时数据，不使用本机主服务的配置和密码。可用 `CHROME_PATH` 指定已安装的 Chrome，`GATEHOME_TEST_BINARY` 指定相对仓库根目录的测试程序路径。

---

## 页面与联调状态

登录、账户管理及全部 17 个业务页面已接入 Go API。反向代理包括分组、路由、服务发现、健康检查、图片和独立访问账户；端口转发支持 TCP、UDP、双协议及真实监听状态。防火墙支持有序 IP 组、订阅引用、WAF 例外、HTTP 规则、限速和自动冻结；IP 名单支持按规则冻结、解冻、搜索分页及真实到期倒计时。配置编辑提交原始 `revision`，冲突时保留草稿，保存时保留未编辑字段及既有密码。

概览保留原版全部 14 个组件：资源与网卡遥测、访问/拦截、趋势、省份/IP、地图、代理服务、监听和活动。组件显示、拖拽/键盘顺序、采集网卡和地图省份保存至既有 `dashboard` 配置；显式空列表表示隐藏全部，旧配置的空值仍显示默认组件。地图沿用离线 IP 归属估算，不表示精确位置。

订阅显示服务器真实下载和缓存状态，更新任务入队后继续查询执行结果。日志和审计使用后端快照分页；访问统计基于保留日志，流量统计独立保存最近 30 天的 HTTP 请求体与响应体字节。日志/访问统计使用 `group_id/host`，流量使用路由 ID；查询失败保留并标明上次结果。

动态域名支持 Cloudflare 独立账户组、IPv4/IPv6 检测与接口切换、DNS 解析查询和实际同步任务。SSL 证书支持 DNS-01、普通/SAN/泛域名、独立凭据及测试/正式环境隔离；检查、申请和续期结果读取后台任务状态。Token 留空保留，只有明确勾选清除才删除；模拟测试不代表公网签发。

浏览器首页沿用 SunPanel 独立账号、数据库和原生反代导入功能，提供服务设置、内外网入口和实际嵌入预览。AI 管理支持 MCP 总开关、独立客户端 Token、权限/到期/撤销、首页账号绑定和真实工具调试；新 Token 只在本页内存显示，默认遮盖，离页清理。设置支持真实来源限制、出站代理及写入式密码、日志配额、账户管理和重启。

备份与恢复支持真实容量预检、加密 ZIP 下载、本地/远程恢复预检、WebDAV 连接与备份、每日/每周时区计划及远程文件列表；密码仅写入，不回显。更新支持正式/Beta 检查、真实下载与安装进度、上传 ZIP 预检、更新前备份和历史回滚。应用恢复、更新或回滚前需明确确认，维护重启后重新登录核对实际版本。ZIP 未包含历史流量，完整迁机需要四个持久化目录副本。

Vite 开发预览通过同源路径转发 `/api/`、`/sunpanel/`、`/mcp` 和地图资源 `/china-outline.svg`；嵌入部署由同一个 Go 管理服务直接处理这些路径。浏览器负责管理会话 Cookie；MCP 调试使用独立 Bearer 认证，失败不结束管理会话。逐页验收范围见[迁移验收清单](../docs/aurora-next-migration-checklist.md)，数据升级、构建与本机运行见[1.0.0 交接说明](../docs/aurora-next-1.0.0-handover.md)。订阅管理对应防火墙 IP/CIDR 规则源。

备份与更新上传沿用后端大小限制（更新 ZIP 128 MiB、备份内容 64 MiB）。如使用 Nginx 预览生产构建，将 `client_max_body_size` 设为至少 `130m`，并为 `/api/` 设置足够的读取超时（本机使用 `180s`），避免有效更新包被代理提前拒绝。

---

## 🎨 设计亮点与交互组件

1. **SpotlightCard 光晕跟随**: 卡片监听鼠标指针坐标，通过 CSS 变量 `--spotlight-x` 和 `--spotlight-y` 产生沉浸式 Obsidian 径向光晕。
2. **全局命令面板 (⌘K / Ctrl+K)**: 随时按下快捷键唤起，支持关键词检索 17 个功能模块与常用快捷指令。
3. **全端自适应响应式侧边栏**: 桌面端紧凑固定；移动端与平板端平滑滑动抽屉（无黑边溢出，具备毛玻璃高斯遮罩）。
4. **轻量通知反馈 (Toast)**: 无论启停路由、同步 DDNS 还是复制链接，均有丝滑浮动气泡提示。
5. **双主题引擎**: 原生支持 Obsidian Tech 深色黑曜石主题与 VisionOS Frosted 浅色磨砂主题。
