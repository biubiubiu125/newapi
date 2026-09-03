# NewAPI 上游同步 1-3 项收口复核（Main 线）

- 复核日期：2026-09-04（Asia/Shanghai）
- 本地仓库：`C:\Users\Administrator\codex-1\newapi`
- 本地分支：`main`，当前提交：`01b445be1`
- 上游基线：`upstream/main` = `32c261923a9786c64d2af087327ef057e7bde7e3`
- 同步方式：只做行为级吸收和人工融合；未执行整树 merge、rebase、cherry-pick、push 或生产部署。

## 结论

此前未完成的 1-3 项已在源码同步范围内收口：

1. 已刷新并吸收当前上游可安全融合的增量。
2. 高风险业务链路已完成逐项复核和分类；不再把本地二开与上游更新机械覆盖。
3. 上游相对差异已逐提交复核，剩余项均有明确结论，不再存在“未审阅”项。

`web/classic/` 已移除；当前只保留正在使用的 `web/`，并补充了 setup 状态刷新回归测试。

## 本轮实际修改

- `common/etag.go`、`controller/revalidated_response.go`：吸收 ETag / 304 公共内容缓存校验，保留本地响应字段和业务语义。
- `controller/misc.go`：公告、关于、协议、隐私、Midjourney、首页公共接口接入重新校验响应。
- `.github/workflows/release.yml`：优先从 tag 触发器解析 release 版本，非 tag 构建保留 `git describe` 回退。
- `web/src/routes/__root.tsx`：setup 状态仅在当前页面会话缓存，刷新页面后重新向服务端校验。
- `web/src/routes/__tests__/-root-setup-status.test.ts`：覆盖刷新后不信任 localStorage 的回归场景。

## 高风险链路结论

### 已完成行为级融合

任务轮询上下文、有限失败、超时和退款边界；Provider conversion、reasoning、hosted-tool capability 与 usage billing；OAuth 绑定状态；图片任务的签名访问、租约、幂等、结算、退款和渠道故障转移；充值原子性、钱包额度及并发状态；Responses penalty、Gemini models、Ollama reasoning/tool-call 等兼容修复。

### 已复核但保留本地实现

- JavaScript task-plugin / sandbox / artifact / 视频代理 / 插件市场：上游新增运行时与当前本地任务系统不是同一闭环，不能单点导入。
- 密码登录传输加密：涉及登录、注册、找回、管理员改密、旧客户端兼容、密钥生命周期，暂不改变现有认证协议。
- billing / subscription / wallet / quota：金额、倍率、预扣、退款、订阅优先级和状态语义保留本地实现；通用修复按兼容性单独判断。
- int32 弃用、token / prefill_groups 约束及其他 schema migration：需独立数据库副本、迁移、回滚和 PostgreSQL 验证，不混入本轮。
- Bun、Go、relaykit、Electron 依赖树：不做整体覆盖，只允许独立验证后的安全补丁或构建阻断修复。

上游 `32c261923` 的“插件模型无渠道时返回 503 说明”依赖完整插件运行时，因此没有单独复制死代码或文案。

## 81 个上游提交复核结果

通过 `git log --left-right --cherry-pick`、逐提交 `git show --stat/patch` 和本地实现核对完成复核。由于本地存在多次行为级融合，Git patch-id 不等价不代表功能未吸收。

| 分类 | 处理结论 |
| --- | --- |
| 已有等价实现或行为级融合 | 保留本地实现，不重复覆盖 |
| 本轮直接吸收 | ETag / 304、release tag 版本解析、setup 状态刷新 |
| 已复核、可拆分后续吸收 | 响应头超时、400 校验、JSON/SQLite/PG 兼容、日志、文档、构建上下文、独立安全补丁等 |
| 高风险暂缓并登记决策门 | task-plugin、认证加密、账务/订阅语义、schema/int32、依赖大范围升级及其耦合提交 |
| **复核总状态** | **81 个上游提交全部已逐项分类，无未审阅项** |

逐提交明细以当前上游提交历史为准，关键高风险提交包括：`eb48396d5`、`6c22550ea`、`32c261923`、`aece11d2f`、`73afad588`、`dc4732cfe`、`b80d633cf`、`918427d8a`、`a073f74b3`、`27ff6a876`、`69a41eead`、`2bf0820f4`、`2b6f1dfef`、`67a0585d0`、`0ed497f06`、`bbd97446c`。

## 验证证据

- WSL：`GOWORK=off go test ./... -count=1` 通过。
- Web：`bun run typecheck` 通过。
- Web：`bun run test`，44 个测试文件、203 个测试通过。
- Web：`bun run build` 通过。
- 额外 setup 回归测试通过。
- `git diff --check` 通过。
- 当前工作区只剩本轮 setup 状态修改和测试文件；构建产物未纳入 Git。

## 明确未执行项

按此前“生产服先放一放”的要求，以下不纳入本轮完成范围：

- origin push；
- 生产 PostgreSQL / Redis、支付回调、真实渠道故障转移和真实任务链路冒烟；
- 生产镜像构建、停机窗口、数据库迁移和部署切换。

这些不是源码同步 1-3 项的遗漏，而是独立的发布/生产验证阶段。
