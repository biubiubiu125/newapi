# 提现结算报告

## 实测链路

- 推广员：`p22898`，user id 8�?- 可提现余额：`0.146 CNY`�?- 提现申请：申�?`0.14 CNY`，状�?`pending`�?- 管理员审核：`POST /api/user/admin/referral/withdrawals/1/approve` 成功，状�?`approved`�?- 管理员打款：`POST /api/user/admin/referral/withdrawals/1/pay` 成功，状�?`paid`，`payment_txn_no=audit_txn_22898`�?- 重复打款：再次调�?pay 返回 `only approved withdrawals can be marked paid`，未重复打款�?
## 账务一致�?
- 提现后账户：`pending=0`，`available=0.006`，`frozen=0`，`withdrawn=0.14`�?- 账本：`commission_accrue=0.146`，`commission_settle available=0.146`，`withdrawal_freeze=-0.14/+0.14`，`withdrawal_paid=-0.14/+0.14`�?
结论：提现申请、冻结、审核、打款和重复打款保护通过�?
