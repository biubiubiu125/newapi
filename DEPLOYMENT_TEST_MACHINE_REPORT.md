# 测试机部署报告

## 环境

- 主机：`144.24.13.242`
- 系统：Debian aarch64，Linux 6.12.86+deb13-arm64
- Docker：26.1.5+dfsg1
- Docker Compose：2.26.1-4
- CPU：4 vCPU Neoverse-N1
- 内存：23Gi
- `/opt` 可用空间：约 137G

## 目录

- 目标目录：`/opt/newapi-referral-test`
- 初始状态：目录不存在。

## 待执行

- 同步当前源码。
- 在目录内生成 `.env`。
- `docker compose config`。
- `docker compose up -d --build`。
- 健康检查、日志、重启、持久化验证。
