# 安全审查报告

## 已修

- epay 充值 notify 不再先 ack 后处理。
- epay/epusdt notify 增加 provider、method、amount、currency 校验。
- epay 订阅 return 不再可信完成订单。
- Compose 不再硬编码数据库/Redis 默认密码，并强制 `SESSION_SECRET`。

## 已有防护

- 管理接口使用 `AdminAuth`。
- 普通用户返佣接口使用 `UserAuth`。
- 邀请绑定 `invitee_user_id` 唯一，防止覆盖绑定。
- 佣金唯一键防止同订单重复佣金。
- 提现 idempotency key 和账变 external ref 唯一。

## 待验证

- CSRF/Cookie 安全属性。
- 普通用户访问管理员返佣/提现接口。
- 推广员访问其他推广员数据。
- 回调伪造签名、金额/币种/网关/订单类型篡改。
- 前端 bundle 是否无密钥。
- 数据库/Redis 未公网暴露。
- 日志密钥脱敏。
