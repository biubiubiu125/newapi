# 补丁摘要

## 修改文件

- `controller/topup.go`：epay notify 改为处理成功后 ack；接入严格回调事实校验。
- `model/topup.go`：新增 `PaymentCallbackValidation` 和 `RechargeEpayWithValidation`；epusdt 回调完成增加严格事实校验。
- `service/epusdt.go`：新增/修正回调金额、订单计价币种、merchant id 解析。
- `controller/topup_epusdt.go`：epusdt notify 校验 merchant、金额、币种。
- `model/subscription.go`：订阅订单完成增加 provider/method/amount/currency 校验。
- `controller/subscription_payment_epay.go`：epay return 只展示，不完成订单；notify 严格校验。
- `controller/subscription_payment_epusdt.go`：epusdt 订阅 notify 严格校验。
- `model/payment_method_guard_test.go`：补支付回调事实、幂等测试。
- `controller/payment_callback_guard_test.go`：补 return_url 不可信测试。
- `docker-compose.yml`：移除硬编码默认密码并强制 `.env`。
- `.env.example`：补 Compose 必需变量示例。
- `scripts/load/*`、`scripts/payment/*`、`scripts/checks/*`：新增压测和一致性检查脚本。

## 是否影响生产配置

是。部署必须提供 `.env` 中的 `POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`SESSION_SECRET`。

## 是否需要数据库迁移

本轮代码修改不新增表字段；依赖现有 AutoMigrate 字段。
