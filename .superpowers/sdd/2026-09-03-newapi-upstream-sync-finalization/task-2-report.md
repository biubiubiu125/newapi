# Task 2 report

日期：2026-09-03

状态：已完成并已提交到本地分支 `4f968e1d7`。

完成内容：
- 移除 `web/classic/` 的 457 个受 Git 跟踪文件，并移除 classic 的构建、Embed、CI、release、路由和格式化配置引用。
- 保留旧 `theme.frontend` 的数据库迁移/写入兼容，不再提供 classic 运行时切换。
- 新版 `web/` 保持唯一活动前端。

回收站证据：
- 操作前已确认绝对路径为 `C:\Users\Administrator\codex-1\newapi\web\classic`。
- 使用 Windows Recycle Bin API `Microsoft.VisualBasic.FileIO.FileSystem.DeleteDirectory(..., RecycleOption.SendToRecycleBin)`。
- 操作后 `Test-Path` 为 `False`，`git ls-files web/classic` 为 0。

验证：
- `bun run typecheck`：通过。
- `bun run lint`：通过，只有仓库既有 warning。
- `bun run build`：通过。
- classic 活动引用扫描：未发现构建、运行时、CI、release 或路由引用。

剩余兼容引用：
- `theme.frontend` 仅存在于兼容迁移、兼容校验、测试和 OpenAPI 说明中。
- `web/classic` 仅存在于历史报告和项目规则文本中。
