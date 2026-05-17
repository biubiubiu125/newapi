# 邀请返佣全链路测试报告

## 代码链路

- 推广员：`ReferralService.ApplyAffiliate`、`ApproveAffiliate`。
- 邀请链接：`ReferralLanding` -> signed cookie/session。
- 注册绑定：`Register` -> `BindInviteeByCodeWithTx`，同一事务创建用户和绑定关系。
- 支付触发佣金：`ProcessTopUpCommission`、`ProcessSubscriptionCommission`。
- 佣金唯一性：`referral_commissions.source_type + source_trade_no`。
- 待结算到可提现：`RunSettlementBatch` / `SettleDueCommissions`。
- 提现：`CreateWithdrawal` -> 冻结可提现金额。
- 审核/拒绝/打款：`ApproveWithdrawal`、`RejectWithdrawal`、`MarkWithdrawalPaid`。

## 当前结论

代码具备唯一约束、事务和账变表设计；支付回调加固后，返佣触发基础更可信。完整业务闭环仍需测试机 API/DB/日志证据。

## 待执行

- epay 成功充值触发返佣。
- epusdt 成功充值触发返佣。
- 订阅订单是否参与返佣。
- 异常场景：自邀、覆盖邀请、重复回调、失败支付、越权访问、并发提现。
