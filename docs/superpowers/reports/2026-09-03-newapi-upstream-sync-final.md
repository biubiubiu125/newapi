# NewAPI 上游更新流程收口报告

日期：2026-09-03

## 当前范围

- 工作区：`C:\Users\Administrator\codex-1\newapi`
- 分支：`main`
- 上游：`upstream/main`
- 上游当前提交：`9df450fe54e1a874a5339b7c38a61014217f02c3`
- 上游提交时间：2026-09-03 14:36:18（UTC+08:00）
- 生产服：按用户要求暂缓，不执行生产数据库、Redis、部署和线上冒烟。

## 已完成

1. 清理废弃的 `web/classic/`，并完成新版 `web/` 构建链路验证。
2. 保留并验证图片任务结果签名访问、任务状态、幂等、租约、结算和退款边界。
3. 完成 usage、reasoning、provider conversion 和 hosted-tool capability-scoped 安全子集。
4. 完成 OAuth 绑定列/外部身份 claim 的安全子集核验，不替换本地 AuthFlow、legacy flow 和 custom provider。
5. 完成通用任务轮询失败分类、连续失败上限、404/410 立即失败退款和 Suno 批量轮询适配。
6. 完成上游高风险任务插件、sandbox、通用 artifact、视频代理重构、密码加密、wallet schema 和依赖变更审计。
7. 修正受当前生产校验约束的测试 fixture 与基线断言，避免把无效图片、外部网络和旧默认值误报为业务回归。

## 验证结果

- `service` 全量测试通过；
- provider 定向测试通过；
- `relaykit` 全量测试通过；
- 根模块全量测试通过；
- 前端 typecheck、lint、build 和全量测试通过（43 个测试文件、202 个测试）；
- 补齐 Sub2API Base URL 校验测试、支付回调手工复核语义测试和 relay billing 的渠道 fixture；
- lint 仅保留仓库既有 warning，无 error。

## 明确未执行

- 生产 PostgreSQL/Redis/failover/支付回调/图片任务冒烟；
- 远端 push；
- 生产部署；
- 上游 JS task-plugin/sandbox、通用 artifact、密码加密和 wallet 64 位 schema 的整套导入。

## 交付结论

源码侧可安全完成的上游更新项已在 `main` 完成并分专题提交；剩余项均有明确的业务边界和后续准入条件，不再以“整树同步完成”表述。
