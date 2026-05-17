# 前端模板复现报告

## 模板路径

- default：`web/default`
- classic：`web/classic`

## 关键页面

- 注册：default `routes/(auth)/sign-up.tsx`，classic `pages/User`
- 充值/订单：default `routes/console/topup.tsx`、`features/wallet`，classic `pages/TopUp`
- 邀请返佣：default `features/referral`，classic `pages/Referral`
- 管理返佣：default `features/admin-referral`，classic `pages/AdminReferral`
- 订阅：default `features/subscriptions`，classic `pages/Subscription`
- 支付配置：classic `pages/Setting/Payment/*`，default `features/system-settings/integrations/*`

## 当前验证

- 本地 default `pnpm install` 成功。
- 本地 `pnpm run build` 因本机系统 `node.exe Access is denied` 失败。
- Dockerfile 在镜像构建中使用 Bun 构建 default/classic，待测试机验证。

## 待执行

浏览器复现步骤、console、Network 和截图仍需测试机服务启动后完成。
