# Agent 运行时选型调研

> 对应 issue #91，属于地图 #90「AI harness 地图」。调研日期 2026-11-19。
> 本文只列事实和取舍，不做决定；决定留给 grilling ticket #96。
> 术语按 `GLOSSARY.md`「探索」一节：探索助手、分析（探索对话的第一轮）、探索对话。

## 问题和约束

探索助手需要一个带工具循环的运行时：模型请求工具 → 服务端执行 → 结果回填 → 继续，直到模型给出最终回答，并且整个过程流式推给前端。地图 #90 已定的前提：

- 模型接入以 **OpenAI 兼容接口** 为基线，Anthropic Messages 等是加分项。
- 数据库里存的消息格式、推给前端的流式事件、工具定义都用 **Keine 自己的格式**，运行时只在适配层后面，以后能换。
- 固定时只保存第一轮的请求消息和最终返回消息，不保存中间的工具调用。
- Keine 现状：单个 Go 二进制（`CGO_ENABLED=0`，`alpine` 运行镜像，见 `Dockerfile`）加 Postgres，Docker Compose 部署。仓库里目前没有 SSE / 流式接口。

## 候选一览

| 选项 | 语言 / 形态 | 最新版本（2026-11） | 许可 |
| --- | --- | --- | --- |
| Genkit Go | Go 库，进程内 | go/v1.9.0 | Apache-2.0 |
| Eino（字节 CloudWeGo） | Go 库，进程内 | v0.9.23（2026-11-09），v0.10 alpha 中 | Apache-2.0 |
| ADK Go（Google） | Go 库，进程内 | v2.8.0（2026-11-16） | Apache-2.0 |
| LangChainGo | Go 库，进程内 | v0.1.15（2026-10-05） | MIT |
| 官方 Go 客户端 SDK（`openai-go`、`anthropic-sdk-go`）+ 自写循环 | Go 库，进程内 | openai-go v3.103.0、anthropic-sdk-go v2.11.0 | Apache-2.0 |
| Vercel AI SDK | TypeScript，sidecar 进程 | `ai@7.0.177` | Apache-2.0 |
| OpenAI Agents SDK | TS / Python，sidecar | JS 0.22.0，Python v0.25.0 | MIT |
| Claude Agent SDK | TS / Python，sidecar，内部再拉起 Claude Code 二进制 | TS v0.3.328，Python v0.2.185 | Anthropic 商业条款 |
| LangGraph | Python / TS，sidecar | Python 1.2.23 | MIT |
| Pydantic AI | Python，sidecar | v2.71.0 | MIT |

版本和发布日期来自各仓库 GitHub Releases（`gh api repos/<repo>/releases`，2026-11-19 查询）。

## Go 进程内选项

### Genkit Go

- **服务商**：官方插件目录 `go/plugins/` 里有 `googlegenai`、`vertexai`、`anthropic`、`ollama`、`compat_oai`；`compat_oai` 下又有 `openai`、`deepseek`、`openrouter`、`xai`、`kimi`、`dashscope`、`zai` 等子包，即 OpenAI 兼容接口是一等公民，Anthropic 也有官方 Go 插件。[来源：genkit-ai/genkit 仓库 `go/plugins`]
- **工具循环**：`genkit.Generate()` 自动跑工具循环，`ai.WithMaxTurns()` 限制轮数（默认 50），超限返回 `ai.ErrMaxTurnsExceeded` 和部分结果。[来源：genkit.dev/docs/go/tool-calling]
- **流式 + 工具**：可以同时用。`ai.WithStreaming()` 的回调收到每个 chunk，工具请求和工具结果在同一条流里按 part 类型区分；工具自己也能 `SendChunk` 推进度。[同上]
- **工具定义**：`genkit.DefineTool(g, name, desc, func(ctx *ai.ToolContext, in T) (Out, error))`，入参 schema 从 Go struct 推导；`DefineResumableTool` + `tool.Interrupt()` 支持中断等人确认。[同上]
- **消息历史**：`resp.History()` 返回包括工具请求 / 结果在内的完整 `[]*ai.Message`；`ai.WithMessages(...)` 把历史喂回去。`ai.Message` 是 Genkit 自己的中立格式（role + parts），和 Keine 自有格式之间写一层转换即可。[同上]
- **运维**：纯 Go 库，进同一个二进制。附带的 Developer UI / CLI 是开发期工具，生产不需要。
- **成熟度**：README 标注 Go SDK 为 "Production-ready with full feature support"；Google Firebase 团队维护，`go/` 目录近三个月 100+ 次提交。[来源：genkit-ai/genkit README、commits API]
- **注意**：框架面较大（flows、prompt 模板 dotprompt、RAG、可观测性），只用 `Generate` + 工具的话，大部分用不上；仓库已从 `firebase/genkit` 迁到 `genkit-ai/genkit`，Go 模块路径仍是 `github.com/firebase/genkit/go`。

### Eino（cloudwego/eino）

- **服务商**：`eino-ext/components/model/` 下有 `openai`、`claude`、`gemini`、`deepseek`、`qwen`、`ark`（火山方舟）、`ollama`、`openrouter`、`qianfan`，以及一组 `agentic*` 新接口实现。`openai` 组件有 `BaseURL` 配置，可接 OpenAI 兼容端点。[来源：cloudwego/eino-ext 仓库]
- **工具循环**：`adk.NewChatModelAgent` + `adk.NewRunner`，"The agent handles the ReAct loop internally"；另有 DeepAgent、多 agent、interrupt/resume。[来源：cloudwego/eino README]
- **流式 + 工具**：框架核心就是流式（`schema.StreamReader`），Runner 以事件迭代器输出，事件里带消息；工具调用在流中拼装。
- **工具定义**：实现 `tool.BaseTool` / `InvokableTool` 接口，或用 `utils.InferTool` 从 struct 推导 schema。
- **消息历史**：`schema.Message`（role、content、tool_calls 等，接近 OpenAI 结构），`schema/serialization.go` 提供序列化。
- **运维**：纯 Go 库。
- **成熟度**：13.7k stars，字节内部使用；仍是 0.x，v0.9.x 约每月一个版本，同时有 v0.10 alpha 线，API 仍在演进（schema 里新增了 `agentic_message`）。文档中文齐全。
- **注意**：组件 / 图编排（compose）抽象层次多，概念量大于 Genkit。

### ADK Go（google/adk-go）

- **服务商**：以 Gemini 为主（`model/gemini`）；`model/openaimodel` 可选 Responses 或 Chat Completions（注释说后者是 "the surface most OpenAI-compatible third-party providers implement"）；`model/anthropicmodel` 走 Anthropic Messages 或 Vertex。两者都标 **EXPERIMENTAL**。[来源：google/adk-go `model/*/doc.go`]
- **工具循环**：LLM agent 自带工具循环、多 agent、A2A。
- **消息历史**：消息用 `google.golang.org/genai` 的 `Content` 类型；会话由 `session.Service` 管理，有内存、Vertex、`session/database`（GORM，文档举例 PostgreSQL / Spanner / SQLite）实现。会话表结构由 ADK 定义，不是我们的格式。[来源：`session/database/service.go`]
- **运维**：纯 Go，但依赖树大（GCP、gRPC、OTel、GORM、A2A），要求 Go ≥ 1.26.10。
- **成熟度**：已到 v2，发布频繁（11 月内 3 个版本）。
- **注意**：消息模型绑定 Gemini 的 `genai.Content`，非 Gemini 适配是实验性的；会话存储倾向 ADK 自己管理，和「存自己的格式」原则有摩擦。

### LangChainGo（tmc/langchaingo）

- 社区项目，支持 OpenAI / Anthropic / Ollama 等，有 agents 包。
- 维护节奏明显放缓：v0.1.14（2025-10）到 v0.1.15（2026-10）隔了一年，近半年约 58 次提交。仍是 0.x。
- 结论：可用，但成熟度和活跃度在 Go 候选里最弱。

### 官方 Go 客户端 SDK + 自写工具循环

- `openai/openai-go`（v3）：Chat Completions 和 Responses 都有，流式 + 工具调用可同时用，`ChatCompletionAccumulator` 把流式 chunk（含 tool_calls 增量）累积成完整消息。改 `option.WithBaseURL` 即可接任意 OpenAI 兼容服务。**没有**本地工具循环（仓库里的 `agents_*` 是 OpenAI 托管 Agents API 的客户端，不是本地 runner）。
- `anthropics/anthropic-sdk-go`（v2）：有 beta `toolrunner`——`client.Beta.Messages.NewToolRunner` / `NewToolRunnerStreaming`，`MaxIterations`，工具用 `toolrunner.NewBetaToolFromJSONSchema` 从 struct 生成，并支持自动压缩（compaction）。只适用 Anthropic Messages 接口（含 Bedrock / Vertex / Foundry）。[来源：anthropic-sdk-go `tools.md`]
- **自写循环的工作量**：核心循环本身不大——请求 → 累积流 → 若有 tool_calls 则并发执行工具、追加 tool 消息 → 再请求，直到无工具调用或到达轮数上限。需要自己处理的：各家流式 tool_call 增量的拼装差异、并行工具调用、错误 / 超时 / 取消、轮数和 token 上限、用量统计。若要同时支持 Anthropic Messages，需要第二个适配器（或只走各家的 OpenAI 兼容端点，Anthropic 也提供兼容端点但功能有缺）。
- **优点**：消息格式、流式事件、工具定义天然就是自己的格式，没有第三方抽象；依赖最小（每个服务商一个官方 SDK，甚至可以只用 `net/http`）。
- **缺点**：多服务商适配、重试、可观测性都要自己维护；新模型特性（推理内容、缓存、结构化输出）要自己跟进。

## Sidecar 选项（独立进程）

共同的运维成本：Keine 现在是单个静态 Go 二进制，加 sidecar 意味着多一个 Node 或 Python 容器（或同镜像里多一个运行时和进程管理）、一套依赖锁和安全更新、Go 与 sidecar 之间的内部协议（HTTP/SSE 或 gRPC）、两边的健康检查和日志。Go 服务仍要负责鉴权、额度和持久化，sidecar 只做模型调用和工具循环；但工具若要读 Keine 数据（课程正文、探索文档），要么 sidecar 回调 Go 的内部 API，要么 Go 把上下文一次性塞进请求。

### Vercel AI SDK（`ai`，TypeScript）

- **服务商**：仓库 `packages/` 下有 `openai`、`anthropic`、`google`、`openai-compatible`（`@ai-sdk/openai-compatible` 3.0.93）、`deepseek`、`alibaba`、`moonshotai`、`zai`、`xai`、`mistral`、`amazon-bedrock` 等数十个官方 provider，覆盖面在所有候选里最广。
- **工具循环 / 流式**：`streamText` + `tools` + `stopWhen`（`isStepCount(n)`、`hasToolCall(...)`、`isLoopFinished()`）多步循环，流里有工具输入增量、工具结果、`tool-error` 等事件。[来源：ai-sdk.dev/docs/ai-sdk-core/tools-and-tool-calling]
- **工具定义**：`tool({ description, inputSchema (Zod 或 JSON Schema), execute })`，`dynamicTool` 处理运行时才知道 schema 的工具。
- **消息历史**：`responseMessages`（`ModelMessage[]`）可直接追加到历史；另有面向 UI 的 `UIMessage` 格式。结构清晰，容易转换成自有格式。
- **成熟度**：28k stars，Vercel 维护，发布极频繁（v7）。大版本间有破坏性改动（v4→v5→v6→v7 的消息 / 流协议都改过），升级成本需计入。

### OpenAI Agents SDK（JS / Python）

- **服务商**：推荐 `OpenAIResponsesModel`，也支持 `OpenAIChatCompletionsModel`；OpenAI 兼容端点通过 `set_default_openai_client` / `ModelProvider` 设 `base_url`；非 OpenAI 服务商通过 Any-LLM / LiteLLM（Python）或 AI SDK 适配（JS）接入，官方称 "best-effort, beta"。[来源：openai.github.io/openai-agents-python/models]
- **非 OpenAI 的坑**（官方文档列出）：默认 tracing 上传到 OpenAI，无 key 会 401，需关闭；很多服务商没有 Responses API 要改用 Chat Completions；托管工具和部分 Responses 专有能力不可用；有的需 `include_usage=True` 才报用量。
- **工具 / 历史**：`@function_tool` / `tool()` 定义；`result.to_input_list()` 导出历史，`Session` 抽象可自定义存储。
- **成熟度**：Python 30.7k stars，仍是 0.x，发布频繁。

### Claude Agent SDK（TS / Python）

- **形态**：官方原话 "A library that runs the Claude Code binary"——SDK 会拉起 Claude Code 进程，自带文件 / shell / 搜索等内置工具、权限、hooks、子 agent。[来源：code.claude.com/docs/en/agent-sdk/overview]
- **服务商**：只支持 Claude 模型，经 Anthropic API、Bedrock、Vertex（Agent Platform）、Microsoft Foundry 或 `ANTHROPIC_BASE_URL` 网关。**不能**以 OpenAI 兼容接口为基线。[来源：code.claude.com/docs/en/third-party-integrations]
- **工具 / 技能**：自定义工具走 MCP（含进程内 MCP server）；技能从 `.claude/skills/` 的 `SKILL.md` 文件加载——这是唯一原生「技能」概念的候选。
- **消息历史**：会话默认写本地 `~/.claude/projects/<cwd>/<id>.jsonl`；可挂 `SessionStore` 适配器镜像到自有后端，也有 `getSessionMessages()` 读出。格式是 Claude Code 的 transcript 格式。[来源：code.claude.com/docs/en/agent-sdk/sessions]
- **条款**：受 Anthropic 商业条款约束；不允许第三方产品提供 claude.ai 登录。
- **运维**：sidecar 里还要带 Claude Code 二进制，进程重、启动慢；它面向「能读写文件、跑命令」的编码型 agent，对 Keine 这种只需几种只读工具的场景能力过剩，且内置工具需逐个关掉。

### LangGraph（Python / TS）

- **服务商**：通过 LangChain 的 chat model（`ChatOpenAI` 可设 `base_url`、`ChatAnthropic` 等）。
- **工具循环**：`prebuilt` 的 ReAct agent 或自建图；`stream_mode="messages"` 等可流式输出 token 和工具事件。
- **消息历史**：状态以 checkpoint 存，后端有 `PostgresSaver` / `AsyncPostgresSaver`（`libs/checkpoint-postgres`）、SQLite、内存。[来源：docs.langchain.com/oss/python/langgraph/persistence] checkpoint 是图状态快照，表结构归 LangGraph 管，读回消息要经过图状态。
- **成熟度**：Python 45k stars，已 1.x，发布频繁。生态最大，但抽象层（LangChain 消息、图、checkpoint）也最厚。

### Pydantic AI（Python）

- `models/` 下有 `openai`、`anthropic`、`google`、`bedrock`、`groq`、`mistral`、`ollama`、`openrouter`、`zai` 等；工具用装饰器 + 类型注解定义；`ModelMessagesTypeAdapter` 可把消息历史序列化为 JSON 并恢复。v2.x，21k stars，发布频繁。作为 Python sidecar 是「轻量 + 类型清楚」的一档。

## 横向对比

| | OpenAI 兼容 | Anthropic Messages | 流式 + 工具同用 | 历史可导出成自有格式 | 加进 Keine 的运维成本 | 成熟度 / 活跃度 |
| --- | --- | --- | --- | --- | --- | --- |
| Genkit Go | 是（`compat_oai`，多家子包） | 是（官方插件） | 是 | 是，`resp.History()` → 转换 | 低（同一二进制） | Go 标为 production-ready，活跃 |
| Eino | 是（`BaseURL`） | 是（`claude` 组件） | 是（流式为核心） | 是，`schema.Message` → 转换 | 低 | 0.x，活跃，API 仍在变 |
| ADK Go | 是，但 **实验性** | 是，但 **实验性** | 是 | 难，消息是 `genai.Content`，会话由 ADK 管 | 低（依赖重） | v2，活跃，Gemini 优先 |
| LangChainGo | 是 | 是 | 是 | 是 | 低 | 0.x，维护放缓 |
| 官方 SDK + 自写循环 | 是（`openai-go` + BaseURL） | 是（再加 `anthropic-sdk-go`，其 beta toolrunner 可参考） | 是（自己拼装） | 天然是自有格式 | 最低 | 依赖官方 SDK，活跃；循环本身自己维护 |
| Vercel AI SDK | 是（`@ai-sdk/openai-compatible`） | 是 | 是 | 是，`ModelMessage` | 高（Node sidecar） | 活跃，大版本常有破坏性改动 |
| OpenAI Agents SDK | 是（Chat Completions 路径，有坑） | 经 LiteLLM / AI SDK，beta | 是 | 是，`to_input_list()` / Session | 高（Node 或 Python sidecar） | 0.x，活跃 |
| Claude Agent SDK | **否** | 是（仅 Claude） | 是 | 部分，transcript JSONL / `SessionStore` | 最高（sidecar + Claude Code 二进制） | 活跃，商业条款 |
| LangGraph | 是 | 是 | 是 | 部分，经 checkpoint / 图状态 | 高（Python/Node sidecar） | 1.x，活跃，抽象最厚 |
| Pydantic AI | 是 | 是 | 是 | 是，`ModelMessagesTypeAdapter` | 高（Python sidecar） | v2，活跃 |

## 取舍总结（不含决定）

1. **进程内 Go vs sidecar**。Go 进程内保持「单二进制 + Postgres」的部署形态，工具可直接调用 Keine 的内部函数读课程和探索文档；sidecar 能用上 TS / Python 更成熟的生态（尤其 Vercel AI SDK 的服务商覆盖），代价是多一个运行时、内部协议和一套依赖要长期维护，且工具读 Keine 数据要绕回 Go。
2. **框架 vs 自写循环**。探索助手的工具预计是少量只读工具（取原文上下文、取探索文档等），工具循环本身复杂度不高；框架的主要价值在多服务商适配和流式拼装。自写循环让「自有消息格式 / 流式事件 / 工具定义」原则零摩擦；用框架则需要一层双向转换，且框架升级可能改动该层。
3. **OpenAI 兼容基线**。Claude Agent SDK 不满足这一前提（只接 Claude）；ADK Go 和 OpenAI Agents SDK 对非本家模型的支持标为实验 / beta。Genkit Go、Eino、Vercel AI SDK、Pydantic AI、自写循环都把 OpenAI 兼容端点当常规路径。
4. **「以后能换运行时」的边界**。无论选哪个，适配层的接口可以按「输入：自有格式的消息列表 + 自有格式的工具定义；输出：自有格式的流式事件 + 最终消息 + 用量」来划；ADK Go（`genai.Content` + 自管会话）、LangGraph（checkpoint 持有状态）、Claude Agent SDK（transcript 持有状态）会倾向自己持有历史，和这个边界冲突最大；Genkit、Eino、Vercel AI SDK、Pydantic AI、自写循环都是「调用方传入历史、拿回新消息」的无状态用法，最容易放在适配层后面。
5. **成熟度风险**。Genkit Go 和官方 SDK 的维护方最稳定；Eino 活跃但 0.x 且有 alpha 新线；LangChainGo 维护放缓；Vercel AI SDK 活跃但大版本迭代快；各 sidecar SDK 多数仍是 0.x。
6. **技能（skills）**。只有 Claude Agent SDK 原生有 `SKILL.md` 式技能；其他候选里「技能」都要自己实现（例如按需注入的提示词片段 + 一组工具），这和选哪个运行时基本无关。

## 不确定点

- Vercel AI SDK 文档页未直接展示 `fullStream` 的事件类型清单，流式事件细节需要原型验证。
- 各家 OpenAI 兼容端点（DeepSeek、Qwen、Kimi 等）对「流式 + 并行工具调用」的实际兼容程度，属于另一张服务商差异调研 ticket 的范围，这里没有逐一验证。
- Eino 主分支提交数与 alpha 线的关系没有深挖；v0.10 何时稳定未知。
