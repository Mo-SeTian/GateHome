# GateHome beta.11 第二轮审查

> 后续修复：6 项发现已在 `develop` 的 `1.0.0-beta.12` 候选中处理，具体实现与验证见 [修复记录](audit-remediation-1.0.0-beta.12.md)。下文保留 beta.11 审查时的事实。

日期：2026-11-10。基线：`develop`，`0a848707cc4681761b97af681dc6bbe83a355cba`，`1.0.0-beta.11`。

结论：发现 **1 项 P1、2 项 P2、3 项 P3**。应先处理 P1 的配置持久化校验问题和 P2 的 MCP 响应权限问题，再合并发布。已有测试和 CI 均通过，但没有覆盖本轮复现的关键授权组合。

本轮只审查和生成隔离复现证据，不修改业务代码、运行配置、账户或数据库；本机实例继续健康运行。上轮修复记录中的“已完成”应结合本报告的漏项阅读，不能理解为所有组合均已验证。

## 按优先级排列的发现

### R1 · P1：仅首页修改的全局 MCP 授权可以保存，却无法重新启动

位置：`internal/gateway/dns_provider.go:107`；保存侧为 `internal/gateway/mcp_settings.go:52`。

beta.11 的保存侧已接受 `enabled=true`、`sunpanel_write=true`、`read/write/maintenance=false`。但 `validateCredentials` 仍要求启用的全局 MCP 必须至少有旧三项权限之一，遗漏新增的首页修改权限。该校验还被启动、普通配置写入和备份解码共用。

隔离复现：

1. 临时管理员通过 `PUT /api/mcp` 保存上述组合，HTTP 200。
2. 调用 `OpenStorePaths` 重新打开同一临时配置，报“MCP 授权配置无效”。真实启动读取同一路径时会失败，影响整个网关启动。
3. 保持当前运行进程，对原配置执行普通 `PUT /api/config`，返回 HTTP 400。
4. 该状态能编码成加密备份，但 `decodeBackup` 检查拒绝；错误还被包装成“备份中的 DNS 凭据缺失或格式错误”，容易误导排查。

这是 beta.11 新增授权支持后没有同步所有校验入口的回归。上轮测试验证了独立客户端的首页授权和备份，遗漏了**全局兼容 Token 仅首页修改**这一组合。

建议：将授权有效性判断集中为一致的函数，保存、启动、普通配置写入和恢复检查共同使用；增加真实 Handler 保存 → 重新打开 → 配置保存 → 加密备份检查的完整回归。不能通过移除全部 MCP 校验解决。

本机当前持久配置未处于该组合，健康检查 HTTP 200。此复现只使用临时目录，没有让主实例进入不可启动状态。

### R2 · P2：没有查询权限的 MCP Token 可从写入响应读取全部配置

位置：`internal/gateway/mcp.go:256`、`internal/gateway/admin.go:531`。

`gatehome_update_config` 按 write 授权执行，但直接返回普通配置保存 Handler 的完整结果；该 Handler 最后调用 `getConfig`。响应没有根据当前 Token 的 read 权限裁剪。

隔离 MCP 协议复现：

1. 创建 `read=false`、`write=true`、`sunpanel_write=false`、`security_write=false` 的独立客户端。
2. 调用 `gatehome_get_config`，被权限检查拒绝。
3. 使用测试管理员提供的最新 revision，只把 `dashboard` 写回同一个有效值。
4. 调用成功，结果包含与此次写入无关的反代域名、名称等全部配置。测试通过读取临时路由标记确认数据来自响应，未使用真实配置。

该路径在 beta.10 及以前也存在；本轮新增的安全策略权限没有消除它。影响是**脱敏配置的读取权限边界**，没有据此认定原始密码或 Token 泄漏。复现需要有效 write Token、有效分区参数和最新 revision，不是未登录读取。

项目文档明确说明“授予修改或维护不会自动授予查询”，因此不能用“写权限通常隐含读取”忽略该差异。

建议：无 read 权限时，仅返回操作结果、revision 和必要的变更标识；确需返回完整配置时追加 read 校验。增加独立协议测试，覆盖“读工具拒绝、写工具成功但响应没有其他分区”。

### R3 · P2：管理员账户弹窗仍允许键盘操作背景页面

位置：`frontend/src/components/AccountModal.vue:3`、`:4`、`:138`。

账户弹窗是普通 div，声明了 `aria-modal=true`，打开时把焦点移动到当前密码输入，但没有模态焦点边界、背景 inert 或原生 dialog。Tab 经过表单末尾后，会移到背景的侧栏导航。

真实 Chromium 复现：打开“修改管理账户密码”，连续 Tab 9 次；焦点经过 BODY 后依次落到背景的概览、反代、转发、首页、DDNS 链接。截图中弹窗仍打开，侧栏 DDNS 已获得焦点。背景链接还可以通过 Enter 激活，形成被遮挡页面误操作。

这是已存在的账户组件问题；上轮修复的是命令面板的焦点边界，两者不是同一组件。

建议：与项目其他编辑器一致使用原生 `dialog.showModal()`，或完整补齐焦点循环、背景不可交互和关闭后的焦点恢复；回归覆盖 Tab、Shift+Tab、Escape 与原触发按钮恢复。

### R4 · P3：手机权限卡片实际仍为 38px，并未达到实现记录中的 44px

位置：`frontend/src/components/McpPermissions.vue:75`、`:85`。

手机媒体查询先声明 `height/min-height:44px !important`，但随后默认 `.permission-card` 又以相同优先级声明 `38px !important`，后者覆盖前者。

390px 真实浏览器计算结果：高度 38、min-height 38px，网格两列正确。问题是触控目标优化未实际落地，不将项目的 44px 目标表述为所有 Web 控件的强制标准。

建议：把手机覆盖规则放在默认规则后面，或调整优先级；浏览器回归直接断言最终高度，不能只断言两列或源码存在 44px。

### R5 · P3：显式字段样式迁移遗漏 IPSourceFields 复合组件

位置：`frontend/src/components/IPSourceFields.vue:2`、`:17`、`:27`，对应全局样式为 `frontend/src/styles/form-tokens.css:538`。

全局 label 排除链已改成只应用 `.form-field`，但嵌套使用的 IPSourceFields 没有给网卡、IPv4/IPv6 获取方式和接口列表的五个普通标签补类名，也没有自己的布局规则。

DDNS 编辑器中，普通名称标签计算样式为 flex、11.5px、5px gap，而这些来源字段为 block、13.5px、normal gap。截图可见同一表单内字体、间距不一致；该组件同时用于公网 IP 检测设置。字段仍可操作和保存，没有把样式不一致算作业务数据损坏。

建议：为该复合组件的普通字段声明 `.form-field` 或组件专属等效样式；检查渲染在弹窗内的子组件，不能只扫描含有 dialog 标签的单文件组件。不要重新引入广泛的全局 label 覆盖。

### R6 · P3：证书界面与底层错误仍固定提示 5 分钟，与实际退避不符

位置：`frontend/src/components/CertificateEditor.vue:44`、`internal/gateway/certificates.go:303`；实际策略为 `internal/gateway/jobs.go:285`。

新策略连续失败后为 5 分钟、15 分钟、1 小时。但全局证书设置仍只说明失败后 5 分钟冷却，签发失败的底层错误还明确说“5 分钟后可手动重试”。第二次及后续失败时，这会与任务汇总中的实际剩余时间冲突，并让用户无效点击重试。

本项由确定的字符串与退避分支对照确认；没有为复现它向公网 CA 发起申请。

建议：底层错误移除固定重试时间，由调度层提供实际时间；前端说明采用逐步退避，并展示任务给出的下一次允许时间。

## 本轮验证结果及限制

| 项目 | 结果 |
| --- | --- |
| 当前源码 `CGO_ENABLED=0 go test ./... -count=1` 和 `go vet ./...` | 通过，gateway 约 45 秒。 |
| 前端单测 | 33/33 通过。 |
| 原 beta.11 的 GitHub develop CI | job `129814461997` 已 success，含单元测试、单二进制、浏览器回归和 Docker 构建；[工作流记录](https://github.com/Mo-SeTian/GateHome/actions/runs/42811692758)。 |
| 新增临时协议/持久化复现 | R1、R2 均复现。探针通过表示缺陷仍存在，不表示已修复。 |
| 针对 MCP、可信代理、WAF、DDNS、维护锁的 race 专项 | 通过；首轮 race 含 R1 探针，随后追加的 R2 用普通协议测试确认，未把 R2 表述为 race 检查。 |
| Sun-Panel 嵌套 Go 模块 | `./integration` 和 `./api/api_v1/middleware` 的 race 测试通过，补查账号边界、CRUD 并发和会话跟随账户变化。根模块的 `./...` 不自动包含嵌套模块。 |
| 浏览器 | 17 个空状态页面加载无未捕获异常；实际复现 R3、R4，记录 R5 计算样式和截图。 |
| 本机运行实例 | `/healthz` 返回 200；只读核对持久状态，现有 14 条反代、6 条转发、4 个防火墙保留，R1 的危险组合未激活。 |

本轮不以“没有崩溃”代替逐页所有业务操作验证。没有重新执行全部公网 DNS/CA/WebDAV 操作、大流量压测或真实手机测试。本轮钥匙串读取超时，未重新获取主实例已登录的 status 响应；本机版本的 beta.11 结论沿用上轮升级验收，本轮只独立确认健康和上述持久配置条件。

复现脚本及截图位于忽略目录 `.local/beta11-rereview/`：

- `review_probes_test.go` 与 `overlay.json`：Go 临时覆盖测试，不修改正式测试包。
- `probes-go.log`、`targeted-race.log`、`baseline-go.log`、`frontend-tests.log`。
- `browser-review.mjs`、`browser-review.log`、`browser-result.json`。
- `account-focus-escape.png`、`mcp-mobile-height.png`、`ddns-source-labels.png`。

下一步优先为 R1/R2 建立“期望正确行为”的正式失败回归并修复，再处理账户模态边界和三项前端/文案漏项。本轮不提交业务修复、不改版本或部署新程序。
