# 1 万邀请注册压测报告

## 脚本

- `scripts/load/referral_10k_registration.js`

## 设计

- 默认 100 个推广员，每个推广员 100 个被邀请用户，总量 10,000。
- 用户名前缀：`load_invitee_{promoterIndex}_{userIndex}_{timestamp}`。
- 邮箱：`load_invitee_{promoterIndex}_{userIndex}_{timestamp}@example.test`。
- 邀请码从 `INVITE_CODES` 环境变量传入，逗号分隔。

## 当前状态

尚未执行。不能判定通过。

## 数据库校验

- `scripts/checks/referral_integrity_check.sql`
