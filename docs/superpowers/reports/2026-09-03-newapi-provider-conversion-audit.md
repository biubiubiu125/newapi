# NewAPI Provider Conversion 融合审计

日期：2026-09-03

## 审计基线

- 本地分支：`main`
- 本地提交：`8777fe459` 加本轮测试 fixture 修正
- 上游：`upstream/main` = `9df450fe5`
- 公共祖先：`823e26304`
- 审计范围：OpenAI Chat/Responses、Claude、Gemini、Ollama、vLLM、OpenRouter，以及相邻的 Vertex/Zhipu Responses 转换。

本审计不是整树合并记录，而是逐 provider 判断：已融合、保留、暂缓和下一步准入条件。

## Provider 结论

### OpenAI Chat/Responses

已融合：

- Chat/Responses usage、cached tokens 和 reasoning 的统一转换；
- `prompt_cache_key`、penalty 等请求字段保留；
- Responses annotation 与 Chat annotation 的双向转换；
- hosted-tool 使用量进入统一 usage，不直接进入资金结算；
- 请求转换错误带原有 `skip retry` 语义。

保留：

- OpenAI 图片编辑的真实图片校验和 MIME 检测。
- 上游曾有删除图片校验的方向，本地不采用，避免无效或危险文件进入后端。

验证：

- `relay/channel/openai` 全量测试通过；
- `relaykit` 全量测试通过；
- 图片编辑 multipart fixture 已修正为真实 PNG，保留原始文件完整性断言。

### Claude

已融合：

- Claude usage、thinking/reasoning、tool call 和 annotation 的安全转换；
- OpenAI Chat → Claude hosted `web_search` 仅在目标 capability 明确开启时转换；
- 未开启时返回转换错误，不静默丢失语义。

暂缓：

- 上游完整 Claude → OpenAI Responses hosted stream 适配。
- 该路径同时涉及事件顺序、usage、工具计费和 failover，需独立端到端测试。

验证：`relay/channel/claude` 和 `relaykit` 相关测试通过。

### Gemini

已融合：

- 显式零值 `top_p`、`seed`、`max_tokens`；
- `thinking_budget` 严格整数校验；
- function call/response ID；
- Vertex/Gemini function ID 清理；
- `googleSearch`、`codeExecution`、`urlContext` 的 capability-scoped 转换；
- 禁用 hosted interpretation 时保留普通 function declaration。

保留：

- Gemini 原有 thinking、image generation、safety settings 和 stream 语义；
- Vertex 不默认声明 hosted capability，避免把 Gemini-like 协议误判为同等业务能力。

验证：`relay/channel/gemini`、`relay/channel/vertex` 和 `relaykit` 相关测试通过。

### Ollama

已融合：

- reasoning/tool-call context 的安全修复；
- 当前响应 usage 和 `IncludeUsage` 行为保持不变。

暂缓：

- 上游 Claude Messages/OpenAI Responses passthrough。
- 该改动会改变请求协议、响应路径、usage 采集和 stream 生命周期，不能只看“减少转换”就直接替换。

验证：`relay/channel/ollama` 全量测试通过。

### vLLM

审计结论：

- 上游相关变化主要属于 OpenAI-compatible reasoning/tool 语义；
- 当前本地没有独立 vLLM provider 包，不能凭名称套用 OpenAI 或 Ollama 行为；
- 已由通用 relaykit 转换层覆盖的字段继续沿用；
- 未发现可以在不改变本地渠道路由、错误码和 failover 的前提下直接复制的独立实现。

决定：保留当前通用路径，后续仅在出现明确 vLLM 请求/响应样本时补 golden 和 stream 测试。

### OpenRouter

审计结论：

- 没有需要独立复制的 provider-specific 代码；
- 当前通过 `OpenRouterDialect` 保留 OpenRouter 特有 reasoning/cache 透传；
- 不新增 hosted-tool 默认能力，不改变普通 OpenAI-compatible 路径。

验证：根模块编译通过；当前 OpenRouter 包无独立测试文件，后续应补真实协议 golden。

### Zhipu Responses

已融合：

- Responses URL 和请求转换入口；
- 保留现有错误与 channel adaptor 边界。

验证：`relay/channel/zhipu_4v` 测试通过。

## 兼容性不变量

1. 转换层不能直接操作 wallet、subscription、token 或 channel 余额。
2. 转换错误必须保持现有错误码和 `skip retry`，不能误触发 failover。
3. tool call、system、multimodal、stop reason、max tokens 和 stream 事件不能因格式转换被静默丢弃。
4. provider 专用能力必须按目标 channel capability 声明启用。
5. 未能证明 provider 支持时，宁可保留普通 function 或返回明确转换错误，不猜测协议。

## 当前审计结论

- Provider conversion 的安全子集已经完成并独立提交。
- 本次没有发现需要整文件覆盖的低风险 provider 更新。
- Claude Responses hosted stream、Ollama passthrough、真实 vLLM golden 和 OpenRouter golden 仍需独立样本/链路后再融合。
- 本地 provider 测试和 `relaykit` 全量测试通过；生产 provider、failover 和 PostgreSQL/Redis 仍未验证。
