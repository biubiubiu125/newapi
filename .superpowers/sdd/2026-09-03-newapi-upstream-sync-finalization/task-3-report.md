# Task 3 report

日期：2026-09-03

状态：已完成本地图片任务产物链路，待与其他同步改动一起提交。

完成内容：
- `TaskPublicAddress` 支持空值；非空时只接受绝对 HTTP(S) 地址，不允许认证信息、查询参数、片段或首尾空白。
- 结果响应新增 `result_url`，优先使用 `TaskPublicAddress`，未配置时回退 `ServerAddress`，无地址时不影响 Bearer 响应。
- 结果 URL 使用 HMAC 能力签名，绑定任务 ID 和固定产物键 `image-result`。
- `/v1/image-tasks/:task_id/result?access=...` 在查询任务前完成验签；仅允许公开图片任务、已结算结果和启用状态的任务所有者。
- 保留原 Bearer 认证、结果过期、文件完整性、清理、结算和下载并发控制。
- 签名结果请求按客户端 IP 限流，并拒绝重复 `access` 参数。

验证：
- 定向 Go 测试：`common`、`model`、`service`、`controller`、`middleware`、`router` 全部通过。
- 根模块编译检查：`go test ./... -run '^$' -count=1` 通过。
- `relaykit` 独立测试：`GOWORK=off go test ./... -count=1` 通过。
- 前端 TaskPublicAddress 测试、typecheck、lint、build 通过；全量前端测试为 201/202 通过，剩余 1 项为未修改的 Sub2API Base URL 基线失败。

边界：
- 当前本地分支没有上游通用 task-plugin/artifact 路由体系，因此没有导入上游完整沙箱 JS 插件系统，也没有伪造通用任务产物接口。
