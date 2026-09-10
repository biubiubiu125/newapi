# NewAPI 上游高风险业务链路完整融合收口报告（Main 线）

- 日期：2026-09-05（Asia/Shanghai）
- 工作区：`C:\Users\Administrator\codex-1\newapi`
- 分支：`main`
- 执行方式：行为级人工融合；未执行整树 merge、rebase、cherry-pick、强制覆盖、生产部署或远端 push。

## 已完成

1. **密码传输加密**：后端持久化 RSA-OAEP/SHA-256 密钥和 key id；登录、注册、找回密码、管理员改密统一解密；前端优先 Web Crypto，保留兼容回退和旧字段窗口；日志不记录密码明文或密文。
2. **wallet/quota 64 位**：用户、Token、兑换码、充值快照、渠道余额等钱包额度使用 `int64`，数据库额度列使用 `bigint`；提供 SQLite/ PostgreSQL/ MySQL 方言检查、幂等迁移辅助和人工迁移 SQL。
3. **task-plugin/sandbox**：接入受限 JavaScript runtime、协议校验、HTTP allowlist、超时/取消、并发限制、权限和 kill switch、审计、任务 adaptor、轮询/取消/结果/结算边界；无任意进程、文件和宿主回调入口。
4. **artifact 与视频代理**：接入任务产物投影、签名 URL、任务归属、管理员/公开访问、过期和匿名访问限流；视频链路优先本地产物描述，再回源上游 URL，保留渠道代理、Range、超时、清理和失败语义；补齐了可配置的 S3-compatible 对象存储后端、任务产物引用持久化、流式写入、Range/条件请求和安全对象路径校验。
5. **内置插件和管理前端**：内置任务插件、插件市场/导入/启停/详情/来源校验、usage logs 产物展示及作者信息已接入；刷新所需 Bun 依赖和 lockfile。
6. **`web/classic/`**：活动源码和构建路径已无引用；新版 `web/` 是当前前端入口。

## 验证证据

- WSL `env GOWORK=off go test -mod=mod ./... -count=1`：通过。
- WSL `env GOWORK=off go vet ./...`：通过。
- 重点 race 测试：认证/额度/schema、插件 runtime/adaptor、controller/service/relay/router：通过。
- Web `bun run typecheck`：通过。
- Web `bun run test`：58 个测试文件、337 个测试通过。
- Web `bun run build`：通过。
- Web `bun run lint`：0 errors，仅保留仓库已有 warnings。
- 变更前端源码 `oxfmt --check`：64 个文件通过。
- `git diff --check`：通过。

## 明确边界

- 按请求计费的任务/日志 quota 仍保持兼容性的有界 `int`，进入钱包记账前由 `common.MaxQuota` 约束；钱包余额和数据库 schema 不再使用 32 位承载。
- 默认仍使用上游回源模式；S3-compatible 后端只有在配置完整且显式启用时才生效，配置缺失或非法会安全回退到上游模式。当前验证覆盖了本地 HTTP S3-compatible 测试服务，没有执行生产对象存储供应商的真实写入/读取演练。
- 仓库全量 `bun run format:check` 仍会报告一批与本轮无关的既有 protected-header-safe 格式文件；本轮变更文件已单独通过格式检查，未为此改动无关前端。

## 未执行发布项

- 生产 PostgreSQL/Redis 迁移及回滚演练；
- 真实 provider、支付回调、渠道故障转移和真实任务链路冒烟；
- 镜像构建、部署切换和远端 push；
