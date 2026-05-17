# newapi 上线前审计报告

## 基线

- 审查目录：`C:\Users\biubiubiu\codex工作空间\newapi-full-audit-20260517-194656\newapi`
- 基线 commit：`84082b83c35a236e24473eed93e8744270d14e6a`
- 审查分支：`audit/full-production-readiness`
- 初始状态：`git status --short` 为空；记录文件见 `AUDIT_INITIAL_GIT_STATUS.txt`
- 测试机目标目录：`/opt/newapi-referral-test`

## 代码地图

- 后端入口：`main.go`
- API 路由：`router/api-router.go`
- Web 资源路由：`router/web-router.go`
- 注册入口：`POST /api/user/register` -> `controller.Register`
- 登录入口：`POST /api/user/login` -> `controller.Login`
- 邀请落地页：`GET /api/r/:code`、`GET /r/:code` -> `controller.ReferralLanding`
- 邀请绑定：`controller.Register` -> `ReferralService.BindInviteeByCodeWithTx`
- 充值订单创建：`POST /api/user/pay` -> `controller.RequestEpay`
- epay 充值 notify：`POST|GET /api/user/epay/notify` -> `controller.EpayNotify`
- epusdt 充值创建：`POST /api/user/epusdt/pay` -> `controller.RequestEpusdtPay`
- epusdt 充值 notify：`POST|GET /api/user/epusdt/notify` -> `controller.EpusdtTopUpNotify`
- 订阅订单创建：`POST /api/subscription/epay/pay`、`POST /api/subscription/epusdt/pay`
- 订阅 notify：`/api/subscription/epay/notify`、`/api/subscription/epusdt/notify`
- 返佣用户 API：`/api/user/referral/*`
- 返佣管理 API：`/api/user/admin/referral/*`
- 提现审核/打款：`ApproveReferralWithdrawal`、`RejectReferralWithdrawal`、`MarkReferralWithdrawalPaid`

## 主要表

- `users`：用户、角色、余额、登录身份。
- `top_ups`：充值订单，含 `trade_no` 唯一键、`payment_provider`、`payment_method`、`paid_amount`、`paid_currency`、返佣快照字段。
- `subscription_orders`：订阅订单，含支付与返佣快照字段。
- `referral_affiliates`：推广员，`user_id`、`invite_code` 唯一。
- `referral_bindings`：邀请绑定，`invitee_user_id` 唯一。
- `referral_commissions`：佣金，`source_type + source_trade_no` 唯一。
- `referral_commission_accounts`：推广员资金账户。
- `referral_withdrawals`：提现申请，`idempotency_key` 唯一。
- `referral_commission_ledgers`：账变，`external_ref_id` 唯一。

## 已修复问题

问题编号：AUD-P1A-001  
严重等级：High  
优先级：P1-A  
模块：支付回调  
文件：`controller/subscription_payment_epay.go`  
函数 / 路由：`SubscriptionEpayReturn` / `/api/subscription/epay/return`  
问题描述：浏览器 return_url 曾可触发订阅订单完成。  
影响：攻击者可伪造浏览器跳转绕过可信 notify。  
修复方案：return_url 只做展示跳转，不再修改订单、订阅和返佣状态。  
测试用例：`TestSubscriptionEpayReturnDoesNotCompleteOrder`。  
验证结果：`go test ./controller` 通过。  
是否已完成：是。  
是否仍为上线阻塞项：否。

问题编号：AUD-P1A-002  
严重等级：High  
优先级：P1-A  
模块：充值支付回调  
文件：`controller/topup.go`、`model/topup.go`  
函数 / 路由：`EpayNotify` / `/api/user/epay/notify`  
问题描述：epay notify 在签名通过后先返回 `success`，随后才做订单、网关、金额、币种和到账处理。  
影响：网关认为回调已成功，但订单可能未到账或攻击请求未被正确反馈。  
修复方案：新增 `RechargeEpayWithValidation`，在事务内校验 provider、method、paid_amount、paid_currency、状态并完成订单状态与额度到账；controller 仅在处理成功后返回 `success`。  
测试用例：`TestRechargeEpayWithValidation_*`。  
验证结果：`go test ./model ./service ./controller` 通过。  
是否已完成：是。  
是否仍为上线阻塞项：否。

问题编号：AUD-P1A-003  
严重等级：High  
优先级：P1-A  
模块：跨网关/金额回调防护  
文件：`model/topup.go`、`model/subscription.go`、`controller/*epay*`、`controller/*epusdt*`、`service/epusdt.go`  
函数 / 路由：epay/epusdt 充值和订阅 notify  
问题描述：部分回调完成路径缺少严格金额、币种、支付方式校验；epusdt 解析可能把 token `USDT` 当作订单计价币种。  
影响：存在金额/币种/网关篡改风险。  
修复方案：新增 `PaymentCallbackValidation.RequirePaymentFacts`；epay/epusdt notify 显式要求金额与币种事实；epusdt 订单计价币种只从 `settlement_currency/fiat_currency/order_currency` 或无 token 场景的 `currency` 读取。  
测试用例：`TestCompleteSubscriptionOrder_RejectsMismatchedCallbackFacts`、`TestRechargeEpusdtWithValidation_RejectsMismatchedCallbackFacts`。  
验证结果：`go test ./model ./service ./controller` 通过。  
是否已完成：是。  
是否仍为上线阻塞项：否。

问题编号：AUD-P0-001  
严重等级：High  
优先级：P0  
模块：部署安全  
文件：`docker-compose.yml`、`.env.example`  
问题描述：Compose 默认硬编码 PostgreSQL/Redis 密码，`SESSION_SECRET` 未强制设置。  
影响：上线默认配置存在高危弱口令和会话密钥风险。  
修复方案：Compose 改为必须从 `.env` 提供 `POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`SESSION_SECRET`，并支持隔离 `CONTAINER_PREFIX` 和 `APP_PORT`。  
测试用例：测试机 `docker compose config` 待执行。  
验证结果：本机 Docker 不可用；远端待回归。  
是否已完成：代码修复完成，部署验证待完成。  
是否仍为上线阻塞项：测试机验证前仍阻塞。

## 当前验证

- `go test ./model ./service ./controller`：通过。
- `git diff --check`：通过。
- `go test ./...`：未完全通过，根包因 `web/classic/dist` 缺失无法 embed；其他包通过。
- 本地 default 前端：`pnpm install` 成功，`pnpm run build` 因本机系统 `node.exe Access is denied` 失败。
- 本机 Docker：未安装或不在 PATH。
- 测试机环境：Docker 26.1.5、Compose 2.26.1 可用；`/opt/newapi-referral-test` 初始不存在。

## 上线结论

当前结论：未达到上线运行标准。原因是测试机部署、epay/epusdt 签名合法闭环、1 万注册压测、混合订单支付返佣压测和前端模板浏览器验证仍未完成。
