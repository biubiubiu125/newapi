# 安全审查报告

## 已修�?
- epay 充�?notify 不再�?ack 后处理�?- epay/epusdt notify 增加 provider、method、amount、currency 校验�?- epay 订阅 return_url 不再可信完成订单�?- epusdt 回调币种解析不再�?token `USDT` 当作订单计价币种�?- Compose 不再硬编�?PostgreSQL/Redis 默认密码，并强制 `SESSION_SECRET`�?- Compose 支持限流环境变量透传，默认值不变�?
## 已验�?
- 管理员登录成功，root 接口需�?session + `New-Api-User` 头�?- 普通用户注�?登录成功�?- epay 错误签名返回 `fail`�?- epay 金额篡改返回 `fail`�?- epusdt 金额篡改返回 `fail`�?- return_url 不改订单状态�?- epusdt 回调 epay 订单返回 `fail`�?- epay 回调 epusdt 订单返回 `fail`�?- 重复支付回调未重复到账、未重复生成佣金�?- 重复提现打款被拒绝�?- PostgreSQL/Redis 未映射公网端口，只在 compose 网络内暴露�?
## 未完成或仍有风险

- 未完成真实外�?epay/epusdt 小额支付�?- 未完成浏览器�?CSRF、Cookie Secure/SameSite、前�?bundle 密钥扫描�?- 未完�?default/classic 模板完整截图验证�?- 10,000 注册压测出现 17.62% 客户端超时，属于上线阻塞�?- 测试机存�?`epusdt-mock` 测试容器，生产前必须删除�?- 压测期间 `.env` 临时放宽限流，生产前必须恢复�?- 日志包含完整测试订单号和部分 SQL 慢查询信息，生产前应确认日志脱敏策略�?
结论：支付回调核心安全问题已修复并通过签名合法测试回调验证，但整体仍未达到生产上线安全标准�?
