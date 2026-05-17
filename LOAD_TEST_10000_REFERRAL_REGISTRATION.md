# 1 万邀请注册压测报�?
## 脚本

- k6 版本：`scripts/load/referral_10k_registration.js`
- Python 标准库版本：`scripts/load/referral_10k_registration.py`
- SQL 校验：`scripts/checks/referral_integrity_check.sql`

## 实测环境

- 测试机目录：`/opt/newapi-referral-test`
- 测试机无 `k6` / `node`，未安装新系统包；实际使�?Python 标准库并发脚本�?- 运行 ID：`023314`
- 推广员：100
- 每个推广员被邀请用户：100
- 总请求：10,000
- 并发模型：`ThreadPoolExecutor(max_workers=1000)`

## HTTP 指标

- 成功�?,238
- 失败�?,762
- 失败类型：`TimeoutError: timed out`
- 状态码：`200=8238`，`0=1762`
- 总耗时�?87.847s
- 平均响应�?7685.70ms
- p95�?0029.57ms
- p99�?0031.84ms
- 最大响应：30056.37ms

## 数据库一致�?
- `registered=10000`
- `bound=10000`
- `missing_binding=0`
- `duplicate_username_groups=0`
- `wrong_promoter=0`
- `promoters_with_invitees=100`
- `per_promoter_min=100`
- `per_promoter_max=100`

## 结论

数据库最终一致性通过，但 HTTP 压测未达到上线验收标准，因为 17.62% 请求在客户端 30s 超时。不能写成�? 万并发注册完全通过”�?
