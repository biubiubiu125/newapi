# 图片异步任务（`/v1/image-tasks`）运维说明

本文档描述 NewAPI 对外图片异步任务 API 的状态语义、结果生命周期和多节点部署要求。

同步接口 `/v1/images/generations`、`/v1/images/edits` 行为保持不变；异步接口由客户端主动选择。

## 对外接口

| 方法与路径 | 说明 |
| --- | --- |
| `POST /v1/image-tasks/generations` | 创建生图任务，鉴权、选渠道、预扣费完成后立即返回 `202` |
| `POST /v1/image-tasks/edits` | 创建图片编辑任务，同上 |
| `GET /v1/image-tasks/{task_id}` | 查询单个任务状态，不返回图片数据 |
| `GET /v1/image-tasks?ids=a,b` | 批量查询状态，一次最多 100 个 ID |
| `GET /v1/image-tasks/{task_id}/result` | 领取结果，返回原始 OpenAI 图片响应 |
| `POST /v1/image-tasks/{task_id}/ack` | 确认已保存结果，触发延迟清理 |
| `POST /v1/image-tasks/{task_id}/cancel` | 取消尚未开始执行的任务并退款 |

所有接口使用 NewAPI Token 鉴权。查询、结果、ACK 和取消除校验任务归属用户外，还会校验创建任务时使用的 Token ID；不返回渠道、密钥、内部结算字段和 `PrivateData`。

状态、结果、ACK 和取消四个接口按「用户 + 令牌」限流，由 `IMAGE_TASK_ACCESS_RATE_LIMIT`（默认 600 次）和 `IMAGE_TASK_ACCESS_RATE_LIMIT_DURATION`（默认 60 秒）控制，超限返回 `429 rate_limit_exceeded` 并带 `Retry-After`。

创建接口在解析请求体前执行数据库共享准入：按用户限频，并限制所有节点合计的在途创建数和请求体预留字节。对应配置为 `IMAGE_TASK_CREATE_RATE_LIMIT`、`IMAGE_TASK_CREATE_RATE_LIMIT_DURATION`、`IMAGE_TASK_CREATE_MAX_IN_FLIGHT` 和 `IMAGE_TASK_CREATE_MAX_RESERVED_MB`。活跃请求每 2 分钟续期一次；进程异常遗留的准入租约最多保留 10 分钟。

完整的请求与响应结构见 `docs/openapi/relay.json`。

## 错误码

所有 `/v1/image-tasks/*` 错误响应使用统一信封 `{"error":{"code":"...","message":"...","type":"image_task_error"}}`。客户端应按 `code` 分支处理，不要匹配 `message` 文本。

| `code` | 典型状态码 | 含义与处置 |
| --- | --- | --- |
| `invalid_request` | 400 / 413 | 请求参数或体积不合法，不要重试 |
| `idempotency_conflict` | 409 | 幂等键已被不同请求内容或不同令牌占用，**不要重试**，换一个键 |
| `idempotency_in_progress` | 409 | 同一幂等键正在创建中，稍后重试 |
| `insufficient_quota` / `insufficient_token_quota` | 403 | 额度不足 |
| `image_task_unavailable` | 503 | 当前节点未启用图片任务执行，或请求存储容量暂时不可用；带 `Retry-After` 时应按其退避重试 |
| `image_task_failed` | 200（任务状态） | 上游执行失败；对外 `error.message` 固定脱敏为 `image task failed`，内部失败详情不通过公开 API 暴露 |
| `settlement_review` | 200（任务状态） | 上游执行结果未知，或生成已结束但结算需要人工复核；结果不可领取 |
| `refund_pending` | 200（任务状态） | 任务已失败，退款仍在后台处理中；不要按已退款处理 |
| `refund_review` | 200（任务状态） | 任务已失败或取消，但退款需要人工复核；不要按已退款处理 |
| `task_not_found` | 404 | 任务不存在或不属于当前用户与令牌 |
| `result_not_ready` | 409 | 任务尚未 `completed`，轮询后重试 |
| `result_expired` | 410 | 结果**已永久清理或过期**，不要重试 |
| `result_temporarily_unavailable` | 503 | 结果仍在保留期内但本次读取失败（例如共享缓存暂时不可用），**应按 `Retry-After` 重试** |
| `not_cancellable` | 409 | 任务已开始执行或已结束，无法取消 |
| `cancel_refund_in_progress` | 409 | 取消已生效，退款仍在处理，按 `Retry-After` 重试确认 |
| `rate_limit_exceeded` | 429 | 触发限流，按 `Retry-After` 退避 |
| `access_denied` | 403 | 当前令牌、用户、分组或来源 IP 无权执行该请求 |
| `request_conflict` | 409 | 请求与当前资源状态冲突 |
| `internal_error` | 500 | 服务内部错误，可携带请求 ID 联系服务端排查 |

**410 与 503 必须区分对待**：410 表示结果不会再出现，客户端应放弃；503 表示结果记录仍在保留期内、只是本次读不到，客户端重试即可，不要因此丢弃任务。

## 状态语义

| 对外状态 | 内部状态 | 说明 |
| --- | --- | --- |
| `queued` | `NOT_START` / `QUEUED` / `SUBMITTED` | 已受理，尚未开始生成 |
| `running` | `IN_PROGRESS` | 正在生成 |
| `finalizing` | `SUCCESS` + 结算未完成 | 图片已生成，账务尚未结算完毕 |
| `completed` | `SUCCESS` + 结算 `SETTLED` | 唯一可领取结果的状态 |
| `failed` | `FAILURE`，或 `SUCCESS` + 结算 `REVIEW` | 结算需人工审查时同样对外呈现为失败 |
| `cancelling` | 取消后的 `FAILURE` + 退款待完成 | 取消已生效，但额度尚未确认退回 |
| `cancelled` | 取消后的 `FAILURE` + 退款完成 | 只有退款完成后才进入该状态 |

只有生成成功且账务结算完成后才会返回 `completed`，`GET /result` 也只在该状态下放行；`finalizing` 期间领取结果返回 `409 result_not_ready`。

## 结果生命周期

- 结果自结果成功落库时间起保留 12 小时，由 `IMAGE_TASK_RESULT_RETENTION_MINUTES` 控制（默认 `720`）。结算重试或最终结算成功不会延长该时间；即使任务仍处于 `PENDING` 或 `APPLIED`，到期结果也会清理。该值存在 12 小时硬上限，配置更大的值会被压回 720 分钟并在启动日志中告警。
- 过期后 `GET /result` 返回 `410 result_expired`，任务记录、使用日志和结算记录仍然保留。结果记录未过期但本次读不到时返回 `503 result_temporarily_unavailable`，客户端应重试而不是放弃。
- 客户端保存结果后应调用 `ack`。ACK 是幂等的，重复调用不会报错；ACK 后保留 2 分钟缓冲，再由后台清理结果文件或内联的 `b64_json`。清理只影响结果内容。ACK 之后 `result_expires_at` 会收敛到这 2 分钟缓冲的截止时间，而不是继续报 12 小时。
- NewAPI 不会主动下载、转换或重新托管上游返回的图片 URL；`url` 与 `b64_json` 两种响应形式都按上游原样返回。

## 幂等键生命周期

`client_task_id` 与 `Idempotency-Key` 的复用取决于任务是否仍在执行，或成功结果是否仍可领取：

- **执行中的任务永远命中复用**，不受窗口限制。任务最长可以跑到 `TASK_TIMEOUT_MINUTES`（默认 24 小时），如果按时间掐窗口，长任务在窗口外被同键重试会重复创建并重复扣费。
- **终态任务只在成功结果仍可领取时命中复用**。失败任务没有可领取结果，同键重新提交会立即创建一条新任务；成功结果自然过期、清理完成，或 ACK 后两分钟缓冲到期时，同键重新提交也会创建新任务。旧任务记录、日志和结算记录仍然保留，只是不再被幂等复用。
- 创建任务耗尽令牌剩余额度后，客户端仍可用原令牌和相同请求重放同一幂等键，以恢复丢失的 `202` 响应；该令牌提交新键或无幂等键时返回 `403 insufficient_token_quota`。
- 命中已有幂等任务的重放不占用新建任务的创建容量和模型请求限流；未命中、需要新建任务时仍按正常创建链路限流。
- 同一幂等键在窗口内对应不同请求内容或不同令牌时返回 `409 idempotency_conflict`，不要重试。
- 幂等预约行按 `IMAGE_TASK_IDEMPOTENCY_LOCK_RETENTION_HOURS`（默认 720 小时）回收，只清理绑定任务已终态且超期的行，在途任务和未绑定预约不受影响。

## 取消范围

当前上游协议没有取消接口，因此只有尚未开始执行的任务可以取消并安全退款。已取得执行租约或已提交上游的任务返回 `409 not_cancellable`，避免出现"上游已生成、NewAPI 已退款"的错配。

取消标记与退款完成是两个阶段：退款未完成时状态为 `cancelling`；退款主库事务完成后才返回 `cancelled`；事务失败并进入人工复核时返回 `failed` 和 `refund_review`。余额、Token 额度、用量计数、任务退款标记与结算 outbox 在同一个主库事务中提交，不会把部分退款暴露为完成。

## 多节点部署

图片任务的请求体需要在执行时读取。执行阶段的调度按 `storage_node` 做节点亲和；任务一旦提交上游成功（或使用 base64 便携请求体），就会解除节点绑定，任意节点都可以接管轮询与结算。待结算任务不做节点过滤。

**每个节点都必须配置稳定且唯一的 `NODE_NAME`。** 未显式配置时回退到主机名，容器重建会导致节点名变化。仓库根目录 `docker-compose.yml` 已提供默认值。

三种可选部署模式：

| 模式 | 配置 | 说明 |
| --- | --- | --- |
| 单节点 | 保持默认 | 仍需固定 `NODE_NAME`。默认不做结果文件外置，`b64_json` 一律内联数据库 |
| 单节点 + 结果文件外置 | `IMAGE_TASK_FILE_CACHE_SHARED=true` 且 `IMAGE_TASK_FILE_CACHE_SHARED_TRUSTED=true` | 单机本地缓存目录即视为受信共享。需要绕开 `IMAGE_TASK_RESULT_INLINE_MAX_MB` 内联上限时使用 |
| 多节点 + 受信共享存储 | `IMAGE_TASK_FILE_CACHE_SHARED=true` 且 `IMAGE_TASK_FILE_CACHE_SHARED_TRUSTED=true` | 所有节点挂载同一缓存目录。只有该模式下大体积 `b64_json` 结果才会外置为文件；其它模式一律内联数据库 |
| 多节点无共享存储 | 保持 `IMAGE_TASK_LOCAL_FILE_CACHE_AFFINITY=true` | 按创建节点亲和调度；或将其设为 `false` 改用 base64 便携请求体，此时请求体不得超过 `IMAGE_TASK_REQUEST_BODY_BASE64_MAX_MB`（默认 16 MB） |

风险提示：

- 多节点使用**相同** `NODE_NAME` 但没有共享存储时，任务可能被没有请求体文件的节点取走，导致执行失败并退款（不会丢失额度，但会随机失败）。
- 开启共享文件缓存后，磁盘容量预留会通过共享目录中的短锁和预留标记跨节点协调，并在每次预留前按真实文件重新统计；节点异常遗留的预留标记会在 15 分钟后回收，避免单节点进程计数导致共享目录超卖。
- 节点永久下线或改名后，其名下未提交上游的任务由孤儿兜底处理。判定同时要求两个条件：任务绑定的 `storage_node` 已经不在节点心跳列表中（确认该节点消失），且逾期超过 `IMAGE_TASK_ORPHAN_FAIL_SECONDS`（默认 1800 秒）。满足后任务失败并退款。只是排队积压、归属节点仍在心跳的任务不会被清理；未绑定节点和便携任务任何节点都能调度，也不走这一档。心跳数据不可用时（例如实例表被清空）该档整体停用，避免凭时间误判。已提交上游但超过 `TASK_TIMEOUT_MINUTES` 仍无确定结果的任务转为 `settlement_review`，保留上游任务 ID 和预扣额度，不会自动退款。将 `IMAGE_TASK_ORPHAN_FAIL_SECONDS` 设为 `0` 可关闭未提交任务的孤儿退款兜底。
- 待结算任务不做节点过滤，任意节点都能接管结算。改造前创建的历史任务如果没有留存计费证据，且请求体文件在已消失的节点上，跨节点结算会转为结算人工审查（额度保留，不会错误扣费或丢账）。
- 未开启受信共享缓存时，`b64_json` 结果内联进数据库。单条结果超过 `IMAGE_TASK_RESULT_INLINE_MAX_MB`（默认 32 MB）时任务会转为结算人工审查状态，而不是反复重试后误退款。使用 MySQL 时请确认 `max_allowed_packet` 大于该上限。

## 相关环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `UPDATE_TASK` | `true` | 是否运行任务轮询；关闭后图片任务 worker 也不会执行 |
| `IMAGE_TASK_WORKER_ENABLED` | `true` | 是否在本节点运行图片任务 worker |
| `IMAGE_TASK_WORKER_IDLE_SECONDS` | `5` | worker 无可运行任务时的轮询间隔，单位秒 |
| `IMAGE_TASK_WORKER_CONCURRENCY` | `0`（不限） | 全局并发 |
| `IMAGE_TASK_CHANNEL_CONCURRENCY` | `0`（不限） | 单渠道并发 |
| `IMAGE_TASK_BATCH_POLL_SIZE` | `20` | 状态批量轮询大小，上限 100 |
| `IMAGE_TASK_LEASE_SECONDS` | `120` | 执行租约时长 |
| `IMAGE_TASK_RESULT_RETENTION_MINUTES` | `720` | 结果保留时长，硬上限 720 |
| `IMAGE_TASK_ORPHAN_FAIL_SECONDS` | `1800` | 孤儿任务失败退款兜底（需节点心跳确认归属节点消失），`0` 关闭 |
| `IMAGE_TASK_RESULT_INLINE_MAX_MB` | `32` | 内联结果上限，`0` 不限制 |
| `IMAGE_TASK_ACCESS_RATE_LIMIT` | `600` | 状态/结果/ACK/取消接口的单令牌限流次数，`0` 关闭 |
| `IMAGE_TASK_ACCESS_RATE_LIMIT_DURATION` | `60` | 上述限流的窗口秒数 |
| `IMAGE_TASK_CREATE_RATE_LIMIT` | `60` | 创建接口按用户的跨节点共享限流次数，`0` 关闭 |
| `IMAGE_TASK_CREATE_RATE_LIMIT_DURATION` | `60` | 创建限流窗口秒数 |
| `IMAGE_TASK_CREATE_MAX_IN_FLIGHT` | `16` | 所有节点合计的在途创建请求数，`0` 不限制 |
| `IMAGE_TASK_CREATE_MAX_RESERVED_MB` | `1024` | 所有节点合计的创建请求体预留上限，单位 MB，`0` 不限制 |
| `IMAGE_TASK_IDEMPOTENCY_LOCK_RETENTION_HOURS` | `720` | 幂等预约行保留时长，`0` 关闭回收 |
| `IMAGE_TASK_HTTP_RESPONSE_MAX_MB` | `0` | 上游响应体上限，`0` 沿用 `MAX_FILE_DOWNLOAD_MB` |
| `IMAGE_TASK_FILE_CACHE_SHARED` | `false` | 是否使用共享缓存目录 |
| `IMAGE_TASK_FILE_CACHE_SHARED_TRUSTED` | `false` | 共享缓存是否可信，决定结果是否外置为文件 |
| `IMAGE_TASK_LOCAL_FILE_CACHE_AFFINITY` | `true` | 无共享缓存时是否按节点亲和调度 |
| `IMAGE_TASK_REQUEST_BODY_BASE64_MAX_MB` | `16` | base64 便携请求体上限 |
| `TASK_TIMEOUT_MINUTES` | `1440` | 异步任务全量超时，超时失败并退款 |
