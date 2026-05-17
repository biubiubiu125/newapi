# 订单支付全链路报告

## 订单类型

- 充值订单：`model.TopUp`，创建入口 `RequestEpay`、`RequestEpusdtPay`、Stripe/Creem/Waffo 等支付入口。
- 订阅订单：`model.SubscriptionOrder`，创建入口 `SubscriptionRequestEpay`、`SubscriptionRequestEpusdt`、Stripe/Creem 订阅入口。
- 管理员补单：`AdminCompleteTopUp` -> `ManualCompleteTopUp`。

## 金额与状态

- 订单状态实际值：`pending`、`success`、`failed`、`expired`。
- 支付网关字段：`payment_provider`，如 `epay`、`epusdt`、`stripe`、`creem`、`waffo`。
- 支付方式字段：`payment_method`，epay 为 `alipay/wxpay` 等，epusdt 为 `epusdt:<token>:<network>`。
- 实付金额字段：`paid_amount`。
- 实付币种字段：`paid_currency`。
- 站内额度字段：`amount`，到账额度按 `amount * common.QuotaPerUnit`。
- 返佣快照：`referral_affiliate_id`、`referral_rate`、`referral_base_amount`、`referral_commission_status`。

## 已加固规则

- epay 充值 notify 只在签名、订单号、provider、method、金额、币种、订单状态校验通过后完成到账并返回 `success`。
- epusdt 充值 notify 要求签名、merchant、provider、method、金额、订单计价币种校验。
- 订阅 epay return 不再完成订单，只有 notify 能完成订单。
- 订阅 epay/epusdt notify 使用 `CompleteSubscriptionOrderWithValidation` 校验事实。
- 重复成功回调在 model 层幂等返回，不重复加额度。

## 待测试

- 测试机签名合法 epay/epusdt 回调闭环。
- 并发 100 个相同订单回调、100 个不同订单回调。
- 跨网关回调和跨订单类型回调。
- 真实外部扣款未执行；如无真实商户凭据，只能标记为签名合法模拟闭环。
