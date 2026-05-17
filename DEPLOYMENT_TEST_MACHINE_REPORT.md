# 测试机部署报�?
## 环境

- 主机：`144.24.13.242`
- 系统：Debian aarch64，Linux `6.12.86+deb13-arm64`
- Docker：`26.1.5+dfsg1`
- Docker Compose：`2.26.1-4`
- CPU�? vCPU Neoverse-N1
- 内存�?3Gi
- `/opt` 可用空间：约 137G 起始，测试后�?131G�?
## 部署

- 目录：`/opt/newapi-referral-test`
- 同步方式：本�?`git archive` 当前审查分支，上传后在隔离目录展开�?- 最终同步提交：`c25bf6d6c68d11ba31c7bf6ee83086190b5ad755`
- `.env`：测试机内随机生�?`POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`SESSION_SECRET`，权�?`600`，未提交 git�?- `docker compose config --quiet`：通过�?- `docker compose up -d --build`：成功�?- 服务：`newapi-referral-test-app`、`newapi-referral-test-postgres`、`newapi-referral-test-redis`�?- 访问地址：`http://144.24.13.242:3000`
- 健康检查：`GET http://127.0.0.1:3000/api/status` 返回 `success=true`、`setup=true`�?
## 管理�?
- username：`ceshi`
- email：`cehsi@ceshi.com`
- role�?00
- 登录：成功�?
## 重启与持久化

- 重启 app/postgres/redis 前计数：`users=11, top_ups=6, referral_commissions=4, referral_withdrawals=1`
- 重启后计数保持一致�?
## 测试辅助

- 拉取并运�?`python:3-alpine` 容器 `epusdt-mock`，仅�?compose 内网模拟 epusdt 创建订单接口�?- �?mock 不代表真实外部扣款�?
## 风险

- Docker Compose 提示 `version` 字段 obsolete，非阻塞，建议后续清理�?- 测试环境为压测临时放宽限流，生产不应直接沿用压测值�?
