# 混合订单支付与返佣压测报�?
## 脚本

- k6 版本：`scripts/load/payment_referral_mixed_load_test.js`
- Python 标准库版本：`scripts/load/payment_referral_mixed_load_test.py`
- 回调工具：`scripts/payment/callback_test.mjs`
- SQL 校验：`scripts/checks/order_payment_integrity_check.sql`

## 实测结果

- 测试机目录：`/opt/newapi-referral-test`
- 运行 ID：`023746`
- 用户数量�?,000
- 订单数量�?,000
- epay 订单�?00
- epusdt 订单�?00
- 回调模型�?00 workers 并发创建订单并发送签名合法测�?notify�?- 总耗时�?.832s
- 成功�?,000
- 失败�?
- 平均响应�?748.60ms
- p95�?980.27ms
- p99�?491.55ms
- 最大响应：3787.85ms

## 数据库校�?
- `orders=1000`
- `success_orders=1000`
- `commissions=1000`
- `duplicate_commissions=0`
- `missing_commissions=0`

结论：混合订单支付与返佣压测通过。该测试使用签名合法测试回调，不代表真实外部扣款�?
