# 提现与结算报告

## 状态机

- 佣金：`pending` -> `available` -> `frozen` -> `paid`。
- 提现：`pending` -> `approved` -> `paid`，或 `pending/approved` -> `rejected`，用户可取消 `pending`。
- 提现明细：`frozen` -> `released` 或 `withdrawn`。

## 一致性设计

- 提现申请扣减 `available_amount` 并增加 `frozen_amount`。
- 拒绝提现释放冻结金额回 available。
- 标记打款从 frozen 转 withdrawn。
- 账变写入 `referral_commission_ledgers`，`external_ref_id` 唯一。

## 待测试

- 重复提现申请幂等。
- 并发提现。
- 重复审核、重复拒绝、重复打款。
- 金额一致性 SQL：`scripts/checks/commission_withdrawal_integrity_check.sql`。
