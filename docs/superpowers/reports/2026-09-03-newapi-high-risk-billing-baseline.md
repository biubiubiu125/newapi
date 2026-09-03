# NewAPI 高风险业务链路基线

日期：2026-09-03

## 基线范围

- 工作区：`C:\Users\Administrator\codex-1\newapi`
- 分支：`main`
- 基线提交：`8777fe459`
- 上游快照：`upstream/main` = `9df450fe5`
- 生产服不在本次源码基线范围内；未执行生产数据库、Redis、部署和冒烟。

本基线用于判断后续上游融合是否改变本地行为，不代表已经完整合并上游。

## 普通请求链路

### OpenAI Chat/Responses

1. Controller 解析请求并生成 `RelayInfo`。
2. 根据渠道和模型计算预扣额度。
3. `service.PreConsumeBilling` 创建 `BillingSession`，按 wallet/subscription 偏好预扣。
4. Relay/provider adaptor 完成请求转换、上游调用和流式/非流式响应转换。
5. 根据统一 usage、reasoning、cached tokens 和 hosted-tool usage 计算实际额度。
6. `service.SettleBilling` 完成差额结算；失败时进入回滚/退款路径。
7. `service.LogTaskConsumption` 或普通请求日志更新 user/channel/token 使用量。

Responses 与 Chat 的差异只在请求/响应转换和 usage 事件形态，资金入口仍由 `BillingSession` 统一处理。

### Claude/Gemini

- Claude Messages：保留 system、tool、thinking、流式事件和 Claude usage，再转成统一 OpenAI usage。
- Gemini：保留 thinking config、function call、thought signature、缓存 token 和 Gemini 原始 usage。
- 仅在渠道 capability 明确声明时，将 hosted tool 转换为 Anthropic/Gemini 原生工具；禁用时普通 function tool 不被吞掉。

## 异步图片/视频任务链路

1. Controller 校验任务请求、模型、渠道、幂等键和权限。
2. `service/image_task_creation.go` 在任务创建事务中预扣 wallet 或 subscription，并保存计费快照。
3. `relay/image_task_runner.go` 负责提交、租约、重试、轮询和结果写入。
4. 终态成功时由任务结算逻辑根据保存的 usage/evidence 做补扣或退款差额。
5. 失败、取消、超时或提交不确定时进入退款/复核流程。
6. `service/task_billing.go` 和 `service/image_task_settlement.go` 记录 settlement record、consume/refund log、token/channel/user 使用量。
7. 图片结果通过现有签名访问链路暴露；视频通过现有 `VideoProxy` 和渠道专用 URL 解析。

保护点：

- 任务幂等和 CAS 状态更新；
- 全局/渠道/任务租约；
- 重试不重复预扣、不重复结算、不重复退款；
- 结果清理不能删除仍被结算或公开访问引用的文件；
- 缺失渠道、token 或日志写入失败时进入人工复核，而不是静默吞掉资金错误。

## Reasoning、Tool Call 和 Cache 断言

- 模型后缀只在转换层解析，不改变本地原始模型名和计费模型名。
- provider 不支持 reasoning 时按普通输出回退。
- tool call ID、function response ID 和流式工具事件需在转换中保持可关联。
- cached tokens 和 reasoning tokens 保留 provider 原始字段，并进入统一 usage。
- hosted-tool usage 只进入 usage 归一化，不直接操作余额、订阅或退款。

## 渠道故障转移和重试

- 普通 Relay 通过 `ShouldRetryRelayFailure`、渠道可用性和错误选项决定是否换渠道。
- 任务 Relay 通过 `ShouldRetryTaskRelayFailure`，同时保留锁定渠道、任务幂等和预扣语义。
- 图片任务拥有独立提交不确定、瞬时失败、轮询和结算重试逻辑。
- 转换失败、无效请求和明确权限/参数错误应带 `skip retry`，不能被错误地转成渠道 failover。

## 已执行的基线验证

### 通过

- `cd relaykit && GOWORK=off go test ./... -count=1`
- `GOWORK=off go test ./... -run '^$' -count=1`
- `GOWORK=off go test ./relay/common ./relay/channel/claude ./relay/channel/gemini ./relay/channel/vertex -count=1`
- 前端 `bun run typecheck`
- 前端 `bun run lint`（有既有 warning，无 lint error）
- 前端 `bun run build`
- `git diff --check`

### 已知基线问题

- 前端 `bun run test`：202 个测试中 201 个通过，1 个既有 Sub2API Base URL 测试失败。
- 后端完整运行测试仍受既有图片结果缓存断言、i18n fixture、SQLite 临时表和外部依赖环境影响；本次只把编译和专题测试作为源码交付门槛。
- PostgreSQL、Redis、生产 failover、生产图片任务和支付回调没有在本次基线中执行。

## 基线结论

后续同步必须保持以下不变量：

1. 任何 provider conversion 不能绕过 `BillingSession`。
2. Hosted Tool 不能在未声明能力时改变普通 function tool 行为。
3. Failover 不能重复预扣、结算或退款。
4. 图片任务状态、租约、幂等、结果和公开访问不能由通用插件抽象直接覆盖。
5. 认证、OAuth、权限、wallet schema 和部署依赖必须单独验证，不能整文件替换。
