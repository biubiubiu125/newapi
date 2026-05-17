# 金额与精度报告

## 当前单位

- 站内额度：`quota`，订单字段 `amount` 按 `QuotaPerUnit` 换算到账额度。
- 充值订单真实金额：`TopUp.Money` / `TopUp.PaidAmount`，币种 `TopUp.PaidCurrency`。
- 订阅订单真实金额：`SubscriptionOrder.Money` / `PaidAmount`，币种 `PaidCurrency`。
- epay 当前按 CNY 计价。
- epusdt 当前代码要求订单计价币种为 CNY，链上 token/网络通过 `payment_method` 表达。

## 风险

- `Money`、`PaidAmount`、佣金金额仍使用 `float64` + GORM decimal tag，存在跨数据库/序列化精度风险。
- 汇率和额度比例快照不完整；`QuotaPerUnit` 后台变更是否影响历史订单，需要继续补数据库级快照。
- 返佣基数使用订单创建时 `BuildOrderSnapshot` 的 `ReferralBaseAmount`，但仍需测试后台比例变更不影响历史订单。

## 已修

- epay/epusdt 回调金额按订单保存的 `paid_amount` 严格比对。
- epusdt 不再把 token `USDT` 当作订单计价币种通过校验。

## 待补测试

金额边界：0、0.01、0.1、1、9.99、10、99.99、100、9999.99、高精度 USDT、循环汇率、1%/5%/10%/33.333% 返佣、金额正负 0.01 篡改。
