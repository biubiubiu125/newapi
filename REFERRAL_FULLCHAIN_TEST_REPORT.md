# 邀请返佣全链路测试报告

## 正向链路

- 推广员：`p22898`，user id 8，invite code `4WNPVCPN`�?- 被邀请人：`a22898`、`b22898`�?- epay 订单：`USR9NOANDbhv1779022899`，签名合�?notify 成功�?- epusdt 订单：`EPU10dmzwDd1779022899`，签名合�?notify 成功�?- 结算任务：`POST /api/user/admin/referral/settlements/run`，`status=completed`，`scanned_count=2`，`settled_count=2`�?- 推广员汇总：`bound_user_count=2`，`paid_user_count=2`，`available_amount=0.146 CNY`�?
## 数据库证�?
- 两条成功订单均生成佣金�?- 佣金合计 `0.146 CNY`�?- 重复回调未重复生成佣金�?- 混合压测�?1,000 单生�?1,000 条佣金，重复佣金 0，丢失佣�?0�?
结论：邀请注册、支付触发佣金、待结算到可提现主链路通过。真实外部扣款未完成�?
