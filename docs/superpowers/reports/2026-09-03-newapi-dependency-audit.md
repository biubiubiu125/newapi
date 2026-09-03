# NewAPI 上游依赖与构建变更审计

日期：2026-09-03

## 审计结论

本轮不整合上游 `go.mod`、`go.sum` 或 `web/bun.lock`。上游新增的 `sobek`、`openai-go`、插件编辑器和密码加密依赖，分别服务于 JS task-plugin/sandbox、插件协议或密码传输加密；这些能力仍处于高风险待复核范围，直接导入会扩大运行时、镜像和部署变化。

当前采用的策略是：

- 已有且不改变本地行为的 GORM、数据库连接、请求校验和构建修复保留；
- 与插件运行时、密码加密、插件编辑器绑定的新增依赖暂缓；
- 不通过 lockfile 整体覆盖吸收无关版本重排；
- 先用当前本地依赖完成 provider、relaykit、service 和前端构建验证。

## 当前验证

- 根模块 `GOWORK=off go test ./... -run '^$' -count=1` 通过；
- `cd relaykit && GOWORK=off go test ./... -count=1` 通过；
- 前端 `bun run typecheck`、`bun run lint`、`bun run build` 通过；
- 未执行生产镜像构建、PostgreSQL/Redis 或生产部署验证。

## 后续准入条件

只有在对应业务专题具备独立迁移、回滚、镜像构建和运行时验证后，才允许引入插件或密码加密依赖；届时仍按最小依赖集提交，不接受整文件覆盖。
