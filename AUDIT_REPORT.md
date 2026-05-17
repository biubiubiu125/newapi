# newapi 上线前审查报�?
## 基线

- 本地工作区：`C:\Users\biubiubiu\codex工作空间\newapi-full-audit-20260517-194656\newapi`
- 初始 commit：`84082b83c35a236e24473eed93e8744270d14e6a`
- 审查分支：`audit/full-production-readiness`
- 初始状态：`git status --short` 为空，见 `AUDIT_INITIAL_GIT_STATUS.txt`
- 测试机目录：`/opt/newapi-referral-test`
- 最终部署提交：`c25bf6d6c68d11ba31c7bf6ee83086190b5ad755`

## 代码地图

- 后端入口：`main.go`
- API 路由：`router/api-router.go`
- 用户注册：`POST /api/user/register` -> `controller.Register`
- 邀请落地：`GET /api/r/:code` / `GET /r/:code` -> `controller.ReferralLanding`
- 邀请绑定：`controller.Register` -> `ReferralService.BindInviteeByCodeWithTx`
- epay 充值订单：`POST /api/user/pay` -> `controller.RequestEpay`
- epay 充�?notify：`POST|GET /api/user/epay/notify` -> `controller.EpayNotify`
- epusdt 充值订单：`POST /api/user/epusdt/pay` -> `controller.RequestEpusdtPay`
- epusdt 充�?notify：`POST|GET /api/user/epusdt/notify` -> `controller.EpusdtTopUpNotify`
- 订阅订单：`POST /api/subscription/epay/pay`、`POST /api/subscription/epusdt/pay`
- 订阅 notify：`/api/subscription/epay/notify`、`/api/subscription/epusdt/notify`
- 返佣用户 API：`/api/user/referral/*`
- 返佣管理 API：`/api/user/admin/referral/*`
- 提现审核/打款：`ApproveReferralWithdrawal`、`RejectReferralWithdrawal`、`MarkReferralWithdrawalPaid`

## 主要�?
- `users`：用户、角色、余额、登录身份�?- `top_ups`：充值订单，�?`trade_no` 唯一、`payment_provider`、`payment_method`、`paid_amount`、`paid_currency`、返佣快照字段�?- `subscription_orders`：订阅订单，含支付与返佣快照字段�?- `referral_affiliates`：推广员，`user_id`、`invite_code` 唯一�?- `referral_bindings`：邀请绑定，`invitee_user_id` 唯一�?- `referral_commissions`：佣金，`source_type + source_trade_no` 唯一�?- `referral_commission_accounts`：推广员资金账户�?- `referral_withdrawals`：提现申请，`idempotency_key` 唯一�?- `referral_commission_ledgers`：账变，`external_ref_id` 唯一�?
## 修复摘要

- 修复 epay 订阅 return_url 可直接完成订单的问题；return 现在只做展示跳转�?- 修复 epay notify 签名通过后先返回 `success` 的问题；现在完成订单校验、到账和事务后才返回成功�?- 新增 `PaymentCallbackValidation`，对 provider、payment method、paid amount、paid currency 做强校验�?- 修复 epusdt 币种解析，避免把 token `USDT` 当作订单计价币种�?- 强化 epay/epusdt 充值和订阅回调的跨网关防护�?- Docker Compose 改为必须�?`.env` 提供 PostgreSQL、Redis、SESSION_SECRET，并支持隔离 `CONTAINER_PREFIX`�?- Docker Compose 增加限流环境变量透传，默认值不变，允许测试环境显式覆盖�?- 新增支付回调、金�?网关校验、return_url 不到账单元测试�?- 新增压测脚本�?SQL 校验脚本�?
## 本地验证

- `go test ./model ./service ./controller`：通过�?- `go vet ./model ./service ./controller ./router ./middleware`：通过�?- `git diff --check`：通过�?- `go test ./...`：根包因本地未预构建 `web/classic/dist` 失败；其他包通过。远�?Docker 构建已覆�?frontend build + Go embed 路径�?- 本地前端：`pnpm install` 成功；`pnpm run build` 因本机系�?`node.exe Access is denied` 失败，不判定为源码构建通过�?
## 测试机验�?
- Docker Compose 源码镜像构建成功�?- app/postgres/redis 启动成功�?- `GET /api/status` 返回 `success=true`、`setup=true`�?- 管理�?`ceshi` 登录成功，role 100，邮�?`cehsi@ceshi.com`�?- epay 签名合法测试回调闭环通过�?- epusdt 通过 compose 内网 mock 创建订单，签名合法测试回调闭环通过�?- return_url 不改订单状态�?- 跨网关回调攻击被拒绝�?- 邀请注册、支付触发佣金、结算、提现审核、打款主链路通过�?- 混合订单支付与返佣压测通过�?,000 单，epay/epusdt �?500，成�?1,000，佣�?1,000，无重复/丢失�?- 10,000 邀请注册压测：数据库最终一致性通过，但 HTTP 成功率未达标�?,238 成功�?,762 客户端超时�?
## 上线结论

未达到上线运行标准�?
阻塞项：

- 10,000 并发注册 HTTP 成功率未达标，存�?17.62% 客户�?30s 超时�?- epay/epusdt 未完成真实外部小额扣款，只完成签名合法模拟回调闭环�?- 前端默认模板、新模板、classic 模板未完成浏览器截图级全链路验证�?
