# GateHome 集成说明

上游：https://github.com/hslr-s/sun-panel
版本：v1.3.0
提交：31fd128633feed4ac8af4d9fb2c7e92696aad20c
许可：MIT，原始版权及许可保留于 LICENSE。

本目录保存完整上游源码及本地适配；不提交 node_modules、前端构建产物、运行配置、数据库或上传文件。上游开发用 .env 文件由显式构建设置取代。

适配范围：前端及 API 使用 /sunpanel/ 前缀；GateHome 将前端嵌入主程序，以同一程序的独立子进程运行后端；SQLite 使用纯 Go 驱动支持 Linux amd64/arm64；关闭上游命令行解析及敏感诊断输出；数据库、上传、缓存与语言文件限定在独立运行目录。GateHome 管理端口启停与监听，Sun-Panel 保留独立账号及自身导入导出。

构建：在仓库根目录运行 make build 或 make linux，需要 Go、Node.js 22 和 npm。sunpanel/build.sh 固定 pnpm 8.15.9，使用上游锁文件构建。

运行数据：Docker /sunpanel；Linux /etc/gatehome/sunpanel；源码开发 .local/runtime/sunpanel。不得将运行数据写入本源码目录。

MCP 管理：GateHome 管理端口提供统一的 Streamable HTTP `/mcp`，API Token 及查询、修改、维护权限在 GateHome 左侧独立的“AI 管理”页中控制。首页工具限定到管理员绑定的一个启用账号；本目录的 `service/integration/mcp.go` 提供分组、网站、偏好及联动操作，复用网页导入去重和关联更新逻辑。父子进程间的临时认证通过继承管道传递，不写入配置或命令参数；内部接口不暴露到首页入口。账号密码与登录令牌不返回给 MCP 客户端。接入说明见 [MCP 文档](../docs/MCP.md)。

网站联动：添加/编辑项目弹窗通过 Naive UI 原生折叠区、可搜索选择器、提示和按钮，从 GateHome 选择已启用的反代项，一次性填入名称、默认网址与内网地址。管理端嵌入及独立首页端口均提供 GET /api/sunpanel/routes，要求有效 GateHome 管理会话，仅返回书签所需字段，不导出凭据。默认网址按协议和监听端口生成，外网映射端口不同时由用户修改后保存。

网络模式：原生菜单提供自动、内网和默认网址。自动模式由浏览器在点击时发送无凭据 HEAD 请求，1.5 秒内收到响应即选内网，失败或超时回退默认网址。新窗口在点击事件内预先打开以保留用户操作授权，检测结果不缓存。模式保存在当前浏览器，网站数据与备份格式不变。运行 npm run test:network 验证选择和超时逻辑。

在线图标兼容图标库名称及 HTTP(S) 图片 URL；“图片”模式也支持远程图片本地化。`/sunpanel/api/file/importImg` 由 GateHome 接入层处理，先校验 Sun-Panel 登录，再按当前出站代理下载公网图片（内网直连），校验并缩放后调用原有上传接口，文件与归属记录都保存在 Sun-Panel 自身存储中。管理端与独立端口使用同一流程，不向远程图片服务转发登录凭据。
