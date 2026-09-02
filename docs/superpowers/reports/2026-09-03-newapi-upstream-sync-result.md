# NewAPI 上游同步结果报告

日期：2026-09-03

## 上游快照

- 本地分支：`main`
- 上游：`upstream/main`
- 上游最新提交：`0ed497f066a68613375124303ef54f220267b334`（2026-09-01）
- 公共祖先：`823e26304a396854ace30b52b98ec497c2dd9c36`
- 上游相对公共祖先新增 70 个提交、691 个变更文件。
- 未执行整树 merge、rebase、cherry-pick、reset、push 或生产部署。

## 已完成

1. 删除废弃的 `web/classic/`，清理活动构建/Embed/CI/release/路由引用，保留 `theme.frontend` 兼容迁移和写入能力。
2. 完成 `TaskPublicAddress` 到图片任务结果 URL 的真实链路：配置校验、HMAC 签名、任务/产物绑定、所有者状态校验、结果过期和限流。
3. 吸收不改变本地业务语义的上游通用修复：
   - SQLite WAL、busy timeout、immediate transaction；
   - Relay 响应头等待超时和溢出保护；
   - 系统任务无变化更新不误判锁丢失；
   - 日志 root 字段脱敏和 quota/rpm/tpm 统计不互相覆盖；
   - PostgreSQL 事务池代理兼容、JSON 字段 string/[]byte 兼容；
   - Token/PrefillGroup PostgreSQL 唯一约束迁移；
   - Relay 无效请求返回 400 且不重试；
   - GORM MySQL/PostgreSQL 依赖升级到上游要求版本。
4. 新增对应的后端、路由、配置和前端回归测试。

## 明确保留

以下不是遗漏，而是按行为级同步规则保留待维护者复核：

- `0ed497f06` 的 hosted-tool、reasoning、provider conversion、usage 和计费完整链路；
- 上游沙箱 JS task-plugin 系统及其插件协议、插件市场、任务存储和视频代理重构；
- 密码传输加密默认语义、OAuth 状态变化、全局 int32 退役等跨边界改动。

这些改动会触及本地图片任务生命周期、渠道故障转移、预扣/结算/退款、权限状态或部署要求，不能用整文件覆盖方式导入。

## 验证结论

- 定向后端测试：通过。
- 根模块编译检查：通过。
- `relaykit` 独立测试：通过。
- 前端 typecheck/lint/build：通过。
- 全量后端仍有既有测试/环境问题：图片结果缓存默认值断言、图片任务测试的 i18n fixture panic，以及 service 测试中的 SQLite 临时表/外部依赖噪声；未将其伪装为全量通过。
- 全量前端 201/202 通过，唯一失败为未修改的 Sub2API Base URL 基线测试。

## 交付状态

- 源码同步：适用项已行为级吸收。
- 本地提交：待本次最终差异审查后提交。
- 远端推送：未执行。
- 生产部署：未执行。
