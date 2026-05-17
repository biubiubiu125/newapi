# 支付回调测试报告

## 测试方式

- 测试目录：`/opt/newapi-referral-test`
- 测试脚本：`scripts/payment/callback_test.mjs`、测试机临时 Python 回归脚本�?- epusdt 创建订单通过 compose 内网 `epusdt-mock` 完成�?- 所有回调为签名合法测试回调，未发生真实外部扣款�?
## 结果

- epay 合法 notify：通过�?- epay 重复 notify：通过，未重复到账�?- epay 金额篡改：返�?`fail`�?- epay 错误签名：返�?`fail`�?- epusdt 合法 notify：通过�?- epusdt 重复 notify：通过，未重复到账�?- epusdt 金额篡改：返�?`fail`�?- epusdt merchant id 校验：代码已覆盖，测试配置为匹配 pid�?- return_url：不改订单状态�?- epusdt -> epay 跨网关：返回 `fail`�?- epay -> epusdt 跨网关：返回 `fail`�?
## 证据订单

- `USR9NOANDbhv1779022899`
- `EPU10dmzwDd1779022899`
- `USR11NOzCzbll1779022995`
- `EPU11EQAv611779022995`

结论：签名、金额、币种、支付方式、跨网关�?return_url 信任边界测试通过。真实小额支付未完成�?
