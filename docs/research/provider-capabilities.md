# 服务商接口形式与能力差异调研

> 对应 issue #92，服务于 #90（AI harness 地图）。只记录事实，不做决定。
> 调研日期：2026-11-19。来源均为官方文档或官方仓库源码；文档会变，引用前请回到原链接核对。
> 标记：✅ 文档明确支持；⚠️ 有限制（见注释）；❌ 文档明确不支持；❓ 官方文档未说明 / 未核实。

## 一、「接入自定义模型」类应用支持哪些接口形式

结论：**都不只支持 OpenAI 兼容接口**。OpenAI Chat Completions 是所有应用共有的「兜底」格式，但每个应用至少还原生支持 Anthropic Messages，多数还原生支持 Gemini、Ollama 或 OpenAI Responses。

| 应用 | OpenAI Chat Completions | OpenAI Responses | Anthropic Messages（原生） | Gemini（原生） | Ollama（原生） | 来源 |
|---|---|---|---|---|---|---|
| Open WebUI | ✅ 主通道，Anthropic、Gemini、OpenRouter、vLLM 等都按这个格式接 | ✅ 连接上可选 API Type = Responses；另支持 Open Responses 规范 | ⚠️ 对话走 Anthropic 的 OpenAI 兼容端点；原生 Messages 只用于拉模型列表和它自己的 `/api/v1/messages` 透传端点 | ❌（走 OpenAI 兼容） | ✅ 专门的 Ollama 连接 | [1] [2] |
| LobeChat / LobeHub | ✅ `openaiCompatible` 运行时，大多数国内服务商用它 | ✅ 内置 `openaiResponses` 协议 | ✅ `anthropicCompatible` 运行时（Anthropic 用；Moonshot、DeepSeek 也引入了 Anthropic 协议） | ✅ 用 `@google/genai` SDK | ✅ 用 `ollama` SDK | [3] |
| Cherry Studio | ✅ `openai` | ✅ `openai-response` | ✅ `anthropic`、`vertex-anthropic` | ✅ `gemini`、`vertexai` | ✅ `ollama` | [4] [5] |
| Dify | ✅ 官方插件 `openai_api_compatible`（自定义 base URL） | ❓ | ✅ `anthropic` 插件用 `anthropic` Python SDK | ✅ `gemini` 插件 | ✅ `ollama` 插件 | [6] [7] |
| LibreChat | ✅ 自定义端点默认是 OpenAI 兼容 | ❓（本次未核实） | ✅ 自定义端点设 `provider: anthropic` 即走原生 `/v1/messages`，也可接「说 Messages 协议的网关」 | ✅ 内置 `google` 端点 | ❌（经 OpenAI 兼容接） | [8] |

补充：
- Cherry Studio 的自定义服务商文档写的是「任意 OpenAI / Anthropic / Gemini 兼容端点」[4]；源码里的服务商类型枚举还有 `azure-openai`、`aws-bedrock`、`mistral`、`new-api`、`gateway` [5]。
- Open WebUI 自己的文档承认：要用 Anthropic 的 PDF、引用、扩展思考、提示词缓存，得用 pipe function 或 LiteLLM 这类代理，因为它走的 OpenAI 兼容层不支持这些 [2]。
- 反方向的趋势：除 OpenAI 格式外，**Anthropic Messages 格式也正成为第二种「兼容格式」**。DeepSeek [13]、智谱 [17]、通义 [12]、Ollama [21]、LiteLLM [20] 都提供 Anthropic 兼容入口；Kimi 的 Anthropic 端点（`api.moonshot.ai/anthropic`）只在第三方指南里看到，官方文档未核实。

## 二、服务商 / 网关能力矩阵

「流式+工具」指在 `stream: true` 下能收到增量的工具调用（名字、参数分块），而不是只在非流式下支持工具。「流式用量」指流式响应里能拿到 token 数。

| 接口 | 流式 | 工具调用 | 流式+工具 | 结构化输出 | 提示词缓存 | 非流式用量 | 流式用量 |
|---|---|---|---|---|---|---|---|
| OpenAI Chat Completions | ✅ [9] | ✅ 默认允许并行 [9] | ✅ [9] | ✅ `json_schema`（推荐）/ `json_object`（旧）[9] | ✅ 自动，≥1024 token；`cached_tokens` [10] | ✅ | ⚠️ 需 `stream_options.include_usage`，在 `[DONE]` 前多发一个用量块 [9] |
| OpenAI Responses | ✅ 语义事件流 [11] | ✅ | ✅ | ✅ `text.format` | ✅ 自动；`usage.input_tokens_details.cached_tokens` [10] | ✅ | ✅ 在 `response.completed` 事件里（❓官方参考页过长未逐字核实；OpenRouter 的 Responses 实现明确如此 [18]） |
| Anthropic Messages（原生） | ✅ [14] | ✅ | ✅ `input_json_delta` 分块传参数 [14] | ✅ `output_config.format`（json_schema，已稳定）+ 工具 `strict: true` [15] | ✅ `cache_control`，需显式标记（另有顶层自动模式，见 [19]） | ✅ | ✅ `message_start` 带初始用量，`message_delta` 带**累计**用量 [14] |
| Anthropic 的 OpenAI 兼容层 | ✅ | ✅ | ✅ | ⚠️ 只接受 `json_schema` + `strict: true`，`json_object` 返回 400；工具的 `strict` 被忽略 [16] | ❌ 明确不支持 [16] | ✅ 但 `prompt_tokens_details` 恒为空 [16] | ✅ `stream_options` 完全支持 [16] |
| Gemini 的 OpenAI 兼容层（beta） | ✅ [22] | ✅ [22] | ❓ 文档没有流式+工具的示例 | ✅ `response_format` 传 Pydantic/Zod 模型 [22] | ⚠️ 显式缓存需 `extra_body` 传 `cached_content` [22]；用量里是否报命中数 ❓ | ❓ 未展示 | ✅ `include_usage` 示例 [22] |
| DeepSeek | ✅ [13a] | ✅ 含 beta `strict` [13a] | ✅（通义平台上的 DeepSeek 亦然） | ⚠️ 只有 `json_object`，无 `json_schema` [13a] | ✅ 自动；`prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`，`cached_tokens` 同值 [13a] | ✅ | ⚠️ 需 `include_usage`；用量挂在**最后一个内容块**上，不是单独的块 [13a] |
| 通义 / 百炼（DashScope 兼容模式） | ✅ [12a] | ✅ 默认并行 [12b] | ⚠️ 函数调用文档给了流式示例（名字在首块，参数分块）[12b]；但兼容性总页仍写「tools 暂时无法与 stream=True 同时使用」[12a]，两页矛盾 | ⚠️ `json_object` 多数模型；`json_schema`+strict 只限部分新模型；部分模型仅非思考模式可用 [12c] | ✅ 隐式（自动）与显式（`cache_control`）二选一；`cached_tokens`，显式另有 `cache_creation_input_tokens` [12] | ✅ | ✅ `include_usage`，最后一块 `choices` 为空、带 `usage` [12a] |
| Kimi / Moonshot | ✅ [23] | ✅ [23] | ✅ delta 中有 `tool_calls` 片段，需自己拼接 [23] | ✅ `json_object` 和 `json_schema`（strict 默认 true，需符合 MFJS 规范）[23] | ✅ 自动前缀缓存（限部分模型）；`cached_tokens`、`cache_write_tokens` [23] | ✅ | ✅ `include_usage`，额外一块、`choices` 为空 [23] |
| 智谱 GLM | ✅ [24] | ✅ `tool_choice` 只支持 `auto` [24] | ⚠️ 需 `tool_stream: true`，且仅 GLM-4.6 及以后部分型号 [24] | ⚠️ 只有 `json_object` [24] | ✅ `prompt_tokens_details.cached_tokens`（自动；OpenRouter 称智谱为自动缓存 [19]） | ✅ | ✅ **默认**在最后一块返回，无需参数；`chunk_include_usage` 可逐块返回累计值 [24] |
| OpenRouter | ✅ | ✅ 按端点支持，可按 `supported_parameters=tools` 筛 [25] | ✅（文档示例为 Responses 流式）[25] | ✅ `json_schema`，按端点支持，建议 `require_parameters: true` 只路由到支持的端点；流式下输出合法的部分 JSON [26] | ⚠️ 透传各家策略：OpenAI/DeepSeek/Kimi/智谱/Grok 自动，Anthropic/通义显式 `cache_control`，Gemini 两种 [19] | ✅ | ✅ Chat Completions **总是**在 `[DONE]` 前发用量块，无需参数 [18] |
| LiteLLM（代理） | ✅ [27] | ✅ | ✅（未见逐家说明）| ⚠️ `json_schema` 对 OpenAI/Gemini/Anthropic/Bedrock/Ollama 等可用；不支持的模型只能客户端校验 [28] | ⚠️ 统一成 OpenAI 用量格式：`cached_tokens`，Anthropic 另有 `cache_creation_input_tokens`；`cache_control` 会被翻译给 Bedrock/Gemini [29] | ✅ | ❓ 流式文档未说明 `include_usage` |
| Ollama（OpenAI 兼容） | ✅ [21a] | ✅ [21a] | ❓ 文档未单独说明 | ⚠️ `response_format` 支持（JSON mode），文档没写 `json_schema` [21a] | ❌ 不适用（本地推理）；Anthropic 兼容层明确不支持 `cache_control` [21] | ✅ | ✅ `include_usage` [21a] |

### 值得注意的差异（事实）

1. **流式用量的拿法有四种**：要显式开 `include_usage` 后多一个空 `choices` 块（OpenAI、通义、Kimi、Gemini、Ollama）；挂在最后一个内容块上（DeepSeek）；默认就有（智谱、OpenRouter）；事件里带累计值（Anthropic `message_delta`、OpenAI Responses `response.completed`）。
2. **缓存命中字段名不统一**：`prompt_tokens_details.cached_tokens`（OpenAI CC、通义、Kimi、智谱、OpenRouter、LiteLLM）、`input_tokens_details.cached_tokens`（Responses）、`cache_read_input_tokens` / `cache_creation_input_tokens`（Anthropic 格式）、`prompt_cache_hit_tokens`（DeepSeek 自有字段，同时也给 `cached_tokens`）。写入缓存的 token 只有部分服务商报告（Anthropic、通义显式、Kimi、OpenRouter）。
3. **缓存方式分两类**：自动前缀缓存（OpenAI、DeepSeek、Kimi、智谱、Gemini 隐式、通义隐式）和显式标记（Anthropic、通义显式、Gemini 显式）。**走 Anthropic 的 OpenAI 兼容层拿不到缓存**，Anthropic 官方也说这层「主要用于测试和比较，不建议生产使用」[16]。
4. **结构化输出**：严格 JSON Schema 只有 OpenAI、Anthropic、Kimi、Gemini 和部分通义模型；DeepSeek、智谱只有 `json_object`（只保证是合法 JSON，不保证结构）。
5. **流式+工具**：OpenAI、Anthropic、DeepSeek、Kimi 默认就能；智谱要额外参数且限型号；通义文档自相矛盾；Gemini 兼容层和 Ollama 兼容层文档未说明。
6. **思考 / 推理**（附带观察）：Anthropic 兼容层不返回思考内容 [16]；DeepSeek 的 Anthropic 兼容层忽略 `budget_tokens` [13]。

## 未知与待核实

- Gemini OpenAI 兼容层：流式+工具调用、非流式 usage、缓存命中数是否出现在 usage 中。
- Ollama OpenAI 兼容层：流式工具调用、`json_schema`。
- LiteLLM：流式下 `include_usage` 的跨服务商行为。
- 通义：兼容性总页与函数调用页关于「tools + stream」的矛盾，需要实测。
- Kimi 的 Anthropic 兼容端点：只在第三方指南看到。
- Dify、LibreChat 是否接 OpenAI Responses API。
- 各家 Anthropic 兼容层（智谱、通义、Ollama）对工具、`cache_control`、用量字段的细节支持度（DeepSeek 已有明细 [13]）。

## 来源

- [1] Open WebUI — Connect a Provider：https://docs.openwebui.com/getting-started/quick-start/connect-a-provider.md
- [2] Open WebUI — Anthropic (Claude)：https://docs.openwebui.com/getting-started/quick-start/connect-a-provider/starting-with-anthropic.md
- [3] LobeHub 源码 `packages/model-runtime/src`（`core/protocols/{openaiChatCompletions,openaiResponses,anthropicMessages}`、`providers/{anthropic,google,ollama,moonshot,deepseek,qwen,zhipu}/index.ts`）：https://github.com/lobehub/lobe-chat/tree/main/packages/model-runtime/src
- [4] Cherry Studio 文档 — 服务商：https://cherryai.com.cn/docs/zh-cn/pre-basic/providers/
- [5] Cherry Studio 源码 `src/renderer/types/provider.ts`（`ProviderTypeSchema`）：https://github.com/CherryHQ/cherry-studio/blob/main/src/renderer/types/provider.ts
- [6] Dify 官方插件仓库 `models/`：https://github.com/langgenius/dify-official-plugins/tree/main/models
- [7] Dify 插件 `models/anthropic/models/llm/llm.py`：https://github.com/langgenius/dify-official-plugins/blob/main/models/anthropic/models/llm/llm.py
- [8] LibreChat — Custom Endpoint 配置：https://www.librechat.ai/docs/configuration/librechat_yaml/object_structure/custom_endpoint
- [9] OpenAI — Create chat completion：https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create
- [10] OpenAI — Prompt caching：https://developers.openai.com/api/docs/guides/prompt-caching
- [11] OpenAI — Streaming responses：https://developers.openai.com/api/docs/guides/streaming-responses
- [12] 阿里云百炼 — 上下文缓存：https://help.aliyun.com/zh/model-studio/context-cache
- [12a] 阿里云百炼 — OpenAI 兼容 Chat：https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope
- [12b] 阿里云百炼 — Function Calling：https://help.aliyun.com/zh/model-studio/qwen-function-calling
- [12c] 阿里云百炼 — 结构化输出：https://help.aliyun.com/zh/model-studio/qwen-structured-output
- [13] DeepSeek — Anthropic API：https://api-docs.deepseek.com/guides/anthropic_api
- [13a] DeepSeek — Create Chat Completion：https://api-docs.deepseek.com/api/create-chat-completion
- [14] Anthropic — Streaming messages：https://platform.claude.com/docs/en/build-with-claude/streaming
- [15] Anthropic — Structured outputs：https://platform.claude.com/docs/en/build-with-claude/structured-outputs
- [16] Anthropic — OpenAI SDK compatibility：https://platform.claude.com/docs/en/api/openai-sdk
- [17] 智谱 — Claude API 兼容：https://docs.bigmodel.cn/cn/guide/develop/claude/introduction
- [18] OpenRouter — Streaming：https://openrouter.ai/docs/api/reference/streaming
- [19] OpenRouter — Prompt caching：https://openrouter.ai/docs/guides/best-practices/prompt-caching
- [20] LiteLLM — `/v1/messages`（Anthropic 统一格式）：https://docs.litellm.ai/docs/anthropic_unified
- [21] Ollama — Anthropic compatibility：https://docs.ollama.com/api/anthropic-compatibility
- [21a] Ollama — OpenAI compatibility：https://docs.ollama.com/api/openai-compatibility
- [22] Gemini — OpenAI compatibility：https://aistudio.google.com/docs/openai
- [23] Kimi 开放平台 — Chat API：https://platform.kimi.com/docs/api/chat
- [24] 智谱 — 对话补全 API：https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8
- [25] OpenRouter — Tool calling：https://openrouter.ai/docs/guides/features/tool-calling
- [26] OpenRouter — Structured outputs：https://openrouter.ai/docs/guides/features/structured-outputs
- [27] LiteLLM — Streaming：https://docs.litellm.ai/docs/completion/stream
- [28] LiteLLM — JSON mode / structured outputs：https://docs.litellm.ai/docs/completion/json_mode
- [29] LiteLLM — Prompt caching：https://docs.litellm.ai/docs/completion/prompt_caching
