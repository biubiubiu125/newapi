# 支付回调测试报告

## 本地单元测试

- epay return 不完成订阅订单：通过。
- epay 充值回调 provider/method/amount/currency 校验：通过。
- epay 重复完成只到账一次：通过。
- epusdt 充值回调 provider/method/amount/currency 校验：通过。
- 订阅订单回调 provider/method/amount/currency 校验：通过。

执行命令：

```bash
go test ./model ./service ./controller
git diff --check
```

## 脚本

- `scripts/payment/callback_test.mjs`

## 未完成

- 测试机 API 级签名合法 epay/epusdt notify。
- 100 次重复回调。
- 100 并发相同订单回调。
- 100 并发不同订单回调。
- 真实小额支付未执行。
