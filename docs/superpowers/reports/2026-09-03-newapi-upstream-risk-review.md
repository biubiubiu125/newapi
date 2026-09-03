# NewAPI 上游剩余高风险项复核报告

日期：2026-09-03

## 复核基线

- 工作区：`C:\Users\Administrator\codex-1\newapi`
- 本地分支：`main`
- 本地当前提交：`13069333e`（本轮复核起点；后续专题提交另行记录）
- 上游目标：`upstream/main`
- 上游当前提交：`9df450fe5`（2026-09-03）
- 公共祖先：`823e26304`
- 本次复核使用 `git diff 823e26304..upstream/main` 和当前本地调用链；未执行整树 merge、rebase、cherry-pick、reset、push 或生产部署。

本报告只决定哪些高风险项暂不自动替换，以及后续进入专题的准入条件；不把“上游有测试”当成本地兼容性已经证明。

## 当前本地保护边界

当前 `main` 仍以本地内置任务适配器、现有任务表和 `TaskBilling` 结算边界为主，已接入：

- 图片任务创建、幂等、排队、执行、结果、取消、退款、结算和租约；
- 图片结果的 `TaskPublicAddress` 签名访问链路；
- 视频任务的现有 `VideoProxy`、Gemini/Vertex URL 解析和渠道代理；
- wallet 额度、订阅额度、渠道额度、预扣、结算、退款和人工复核；
- OAuth/AuthFlow、外部身份绑定和权限/状态字段；
- 当前新版 `web/` 前端；`web/classic/` 已按废弃代码规则清理。

以下边界不因上游新增文件而自动改变。

## 复核结论

### 1. JS task-plugin、sandbox、插件协议：暂缓

上游 `eb48396d5` 起将内置任务适配器替换为 `pkg/jsplugin`、`plugins/tasks/*` 和插件协议，新增插件运行时、插件管理接口、沙箱、模型别名、插件状态和任务产物存储；同时删除 `relay/channel/task/*` 的多组内置适配器。`9df450fe5` 又把轮询适配器接口改成接收完整 task/HTTP response，并引入插件查询上下文和轮询失败计数。

本地当前仍直接依赖内置适配器接口：

- `FetchTask(baseURL, key, body, proxy)`
- `ParseTaskResult(body)`
- `service/task_polling.go` 直接推进本地 `Task` 状态和结算

直接替换会同时改变任务请求协议、插件执行环境、状态持久化、轮询参数、错误分类和结算触发点，不能作为普通 provider 更新处理。

**决定：暂缓，不导入完整插件系统。**

后续准入条件：

1. 为每个现有平台建立“旧适配器 → 插件”的请求、响应、状态、取消和错误码对照表；
2. 明确插件版本、模型别名、`plugin_state`、任务产物和重试的数据库迁移；
3. 证明 `PreConsumeBilling`、成功结算、失败退款、重复轮询和租约不会被插件回调绕过；
4. 增加开关、灰度和按平台回退到旧适配器的方案；
5. 至少完成 PostgreSQL、Redis、failover、图片任务和退款的集成测试后，才进入独立专题。

### 2. Task Artifact（任务产物）和视频代理：暂缓，保留本地链路

上游新增通用 `TaskArtifact` 存储、访问控制、产物投影和插件驱动的内容请求，并大幅重写 `controller/video_proxy.go`，删除 `controller/video_proxy_gemini.go`。新的代理允许插件根据产物 key 构造请求，还改变了重定向、请求头、超时、状态码和错误码处理。

本地已有的是另一条已验证链路：

- 图片结果通过 `service/task_artifact_access.go` 生成签名；
- `controller/image_task_public.go` 校验任务、所有者、状态、过期和限流；
- 视频仍由 `controller/video_proxy.go` 按渠道类型解析 Gemini、Vertex、OpenAI/Sora 或旧结果 URL；
- 本地任务结算和退款不依赖上游插件产物表。

因此，不能把上游通用 artifact 抽象直接覆盖本地图片结果，也不能删除 Gemini/Vertex 专用解析。视频代理的安全校验、代理配置、Range/缓存行为和回源失败语义需要单独做真实链路测试。

**决定：暂缓；保留现有图片签名结果和视频代理。**

后续只考虑两项独立专题：

- 在不改变现有图片结果 URL 的前提下，为视频增加统一 artifact 描述层；
- 单独抽取视频代理的 SSRF、重定向、响应头和超时修复，并逐渠道回归。

### 3. 密码传输加密：暂缓，不能仅同步前端

上游 `b80d633cf`/`918427d8a` 增加浏览器 RSA-OAEP 密码传输、服务端持久化 RSA 私钥、登录加密 key 接口和可选配置；同时修改登录请求字段、初始化流程、路由和前端依赖。

这不是单纯 DTO 改动，涉及：

- 多实例共享私钥和密钥轮换；
- 旧浏览器、API 客户端和登录失败回退；
- 数据库新增密钥表；
- 配置默认值、首次启动失败和密钥丢失；
- 认证错误信息和缓存行为。

**决定：暂缓，不改变当前登录协议。**

后续准入条件：

1. 明确默认关闭还是默认开启，并提供旧客户端兼容窗口；
2. 完成密钥持久化、备份、轮换和多实例启动测试；
3. 覆盖密码登录、2FA、Turnstile、失败重试和 API 客户端；
4. 有可回滚的数据库迁移和配置回退步骤；
5. 前端只在服务端明确声明能力后发送加密字段。

### 4. OAuth state 和绑定写回：保留本地流程，已完成安全子集核验

上游 `d7992672a` 的核心修复是 OAuth/微信绑定时只更新绑定列，避免“读取完整用户 → 修改一个字段 → 整体保存”覆盖并发发生的封禁、降权或分组变更。

本地已具备等价且更完整的保护：`oauth.Provider` 暴露 `ProviderUserIDColumn()`；GitHub、Discord、OIDC、LinuxDO 实现列映射；`model.ClaimExternalIdentity`/`ClaimExternalIdentityWithTx` 通过事务和身份 claim 处理并发；custom provider 使用 `user_oauth_bindings`；微信绑定也走外部身份 claim。内置 OAuth 的 `AuthFlow`、legacy flow 和 session/auth-version 轮换保持不变。

已确认的 fallback `user.Update(false)` 只会在无法识别为内置 provider 的极端分支执行；当前注册的内置 provider 和 generic provider 都不会走该分支。因此不整文件替换上游 OAuth 实现，继续保留本地更完整的流程。

验证覆盖：

- `model/user_update_test.go`：绑定列白名单、绑定列更新不覆盖角色/状态/分组；
- `model/external_identity_claim_test.go`：重复 claim、释放 claim 和事务边界；
- `controller/auth_flow_test.go`：AuthFlow 消费和绑定 provider 流程。

**决定：OAuth 安全子集已完成；上游接口命名和整文件替换不再导入。**

### 5. `int32` 退役和 wallet 额度扩大：暂缓，必须先做 schema 专题

上游 `a073f74b3` 将单请求计费和 wallet/top-up 的数值域拆分：单请求继续受限，wallet/top-up 使用 JavaScript 安全整数范围，并在启动时检查 PostgreSQL/MySQL 的 `users` 额度列是否仍为 64 位。

本地已经有额度饱和、严格转换、结算审计和超限测试，但仍有以下本地约束：

- 数据库字段、Go 模型、日志和 API 仍存在 `int`/旧额度域；
- 图片任务、订阅、渠道额度和退款共用现有结算边界；
- 生产环境使用 PostgreSQL，不能假设启动检查等价于迁移完成；
- `int32` 仍大量出现在原子计数、协议字段和测试辅助变量中，不能按文本全局替换。

**决定：暂缓，不导入全局 `int32` 退役和 wallet 域切换。**

后续准入条件：

1. 盘点 PostgreSQL 实际列类型、索引、迁移历史和线上最大值；
2. 明确 wallet、单请求 charge、订阅额度、日志 quota 的数值域；
3. 完成在线迁移、回滚和旧版本读取方案；
4. 覆盖充值并发、重复回调、退款、订阅结算、failover 和缓存同步；
5. 只改业务额度字段，不误改计数器、协议字段和无关 `int32`。

### 6. 依赖和构建大变更：拆分吸收

上游新增 `grafana/sobek`、`openai/openai-go` 等插件/协议运行时依赖，升级 GORM MySQL/PostgreSQL 驱动和 GORM patch 版本，并大幅改动 `web/bun.lock`，其中部分依赖服务于密码加密、插件编辑器、测试框架和沙箱。

**决定：不按 lockfile 或 `go.mod` 整体覆盖。**

可单独评估的低风险项：

- 已有代码实际使用且有兼容性测试的 GORM patch 更新；
- 与当前新版 `web/` 构建直接相关、可独立验证的安全依赖更新。

暂不吸收：

- 只为 JS plugin/sandbox 引入的运行时依赖；
- 只为密码加密协议引入的前端加密依赖；
- 与 Vitest/JSDOM/插件编辑器绑定的整套前端测试依赖；
- 未经当前构建、镜像和部署验证的 lockfile 大片重排。

### 7. 通用任务轮询失败分类和上限：已完成安全子集

已在不改变当前 `TaskPollingAdaptor` 接口和图片任务状态机的前提下，吸收上游轮询可靠性改进：

- 增加 `TaskPrivateData.PollFailures` 和 `TASK_POLL_MAX_FAILURES`，默认 20，设为 0 可关闭连续失败上限；
- 将网络错误、空响应、401/403、429、5xx、无法识别的响应和 parser 错误分类并累计；
- 404/410 立即将任务标记失败并按现有 CAS/退款边界退款；
- 达到连续失败上限后失败并退款；收到合法的非终态响应后归零；
- 同时覆盖视频单任务和 Suno 批量轮询；
- 不引入上游 JS task-plugin、sandbox、插件协议或新的 adaptor 接口。

验证：

- `service` 全量测试通过；
- 连续失败上限、合法响应归零、404/410 立即失败退款、Suno CAS 和退款测试通过；
- 根模块编译检查通过。

**决定：轮询可靠性安全子集已完成；插件化批量协议仍暂缓。**

## 可进入下一轮的顺序

1. 视频代理安全修复：只提取 SSRF、重定向、Range、超时和错误码的独立行为；
2. wallet 64 位 schema 迁移：单独设计数据库迁移和线上回滚；
3. 密码传输加密：最后做兼容窗口、密钥生命周期和多实例专题；
4. JS task-plugin/sandbox：在上述边界稳定后再评估。

## 本轮结论

- 已完成的 usage、reasoning、provider conversion 和 hosted-tool 能力化融合继续保留；
- 上游 task-plugin/sandbox、通用 artifact、视频代理大改、密码加密、全局 `int32` 退役和插件依赖不自动导入；
- OAuth 绑定列写回安全子集和轮询失败分类已在本地行为边界内完成；
- 本轮新增的轮询可靠性代码和配置均未触及数据库 schema、生产部署配置或远端状态。
