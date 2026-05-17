# 订单支付全链路报�?
## 入口与状�?
- 充�?epay：`POST /api/user/pay`，notify `POST|GET /api/user/epay/notify`�?- 充�?epusdt：`POST /api/user/epusdt/pay`，notify `POST|GET /api/user/epusdt/notify`�?- 订阅 epay：`POST /api/subscription/epay/pay`，notify `/api/subscription/epay/notify`，return `/api/subscription/epay/return`�?- 订阅 epusdt：`POST /api/subscription/epusdt/pay`，notify `/api/subscription/epusdt/notify`�?- 充值订单表：`top_ups`�?- 订阅订单表：`subscription_orders`�?- 订单状态以代码常量为准，本次实测覆�?`pending -> success`，以及跨网关攻击保持 `pending`�?
## 金额与额�?
- 充值订�?`Amount` 表示站内额度对应的展示充�?amount�?- `Money` / `PaidAmount` 表示真实支付金额快照�?- `PaidCurrency` 表示真实支付币种快照�?- 默认换算：`10` 充�?amount -> `73.00 CNY`，由 `Price=7.3` 计算�?- 支付成功�?quota 按订单快照到账，不信任前端回调传入额度�?
## 已修复内�?
- return_url 不再作为到账依据�?- epay notify 完成签名、订单号、provider、method、amount、currency、状态校验后才返�?`success`�?- epusdt notify 校验签名、merchant id、provider、method、amount、currency、状态�?- 回调到账使用数据库事务和行锁，重复回调幂等�?- 佣金生成依赖已成功订单，�?`source_type + source_trade_no` 唯一�?
## 测试机证�?
- epay 订单：`USR9NOANDbhv1779022899`，合�?notify 返回 `success`，订单成功�?- epay 重复 notify：重�?3 次返�?`success`，无重复到账/佣金�?- epay 金额篡改：`73.00 -> 72.99` 返回 `fail`�?- epusdt 订单：`EPU10dmzwDd1779022899`，合�?notify 返回 `ok`，订单成功�?- epusdt 重复 notify：重�?3 次返�?`ok`，无重复到账/佣金�?- epusdt 金额篡改：`73.00 -> 73.01` 返回 `fail`�?- return_url：测试订�?`USR11NOzCzbll1779022995` return 后仍�?`pending`�?- 跨网关：epusdt 回调 epay 订单返回 `fail`；epay 回调 epusdt 订单返回 `fail`�?
结论：签名合法模拟回调下订单支付、到账、账本、返佣触发、幂等和跨网关防护通过。未完成真实外部扣款�?
