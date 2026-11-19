# 参考实现选型调研：两个模型适配器怎么写，工具循环参考谁

> 对应 issue #102，属于地图 #90「AI harness 地图」。调研日期 2026-11-20。
> 本文只列事实和取舍，不做决定。
> 前提是 ADR 0008（PR #104）：探索助手在 Go 进程内运行，工具循环归 Keine，能替换的只有**模型适配器**。适配器做一次流式调用，向上交出文本增量、完整的工具调用、统一用量（输入、输出、缓存命中、缓存写入）和结束原因。原生适配器只做两个：OpenAI 兼容 Chat Completions 和 Anthropic Messages。
> 在已有两份报告的基础上写：[agent-runtime.md](https://github.com/Bank5y/Keine/blob/research/agent-runtime/docs/research/agent-runtime.md)（候选一览、各框架的形态）和 [provider-capabilities.md](https://github.com/Bank5y/Keine/blob/research/provider-capabilities/docs/research/provider-capabilities.md)（各家流式、用量、缓存字段的差异）。那两份已经讲过的内容这里不再重复。Sidecar 类候选不作为可采用的选项，Vercel AI SDK、OpenAI Agents SDK 和 DeepSeek Harness 只当作循环写法的参考。

## 读过的源码版本

源码都从各仓库默认分支的 tarball 读取（2026-11-20）。版本和发布节奏来自 `gh api repos/<repo>/releases`。

| 仓库 | 读的提交 | 最新发布 | 发布节奏（2026 年） |
| --- | --- | --- | --- |
| `openai/openai-go` | `f17233a150`（11-18） | v3.103.0（11-18） | 9 月 18 个，10 月 29 个，11 月至今 16 个 |
| `anthropics/anthropic-sdk-go` | `65730ae142`（11-17） | v2.11.0（11-17） | 每月 7 到 12 个 |
| `genkit-ai/genkit`（Go 部分，模块 `github.com/firebase/genkit/go`） | `c841c5168a`（11-19） | go/v1.14.0（10-20） | 每月约 1 个（v1.6.1 是 04-06，v1.14.0 是 10-20） |
| `cloudwego/eino` | `4a4b718669`（11-09） | v0.9.23（11-09），另有 v0.10.0-alpha 线 | 约每月 1 个 |
| `cloudwego/eino-ext` | `f11cd68188`（11-19） | `components/model/openai/v0.1.13`、`components/model/claude/v0.1.26`、`libs/acl/openai/v0.1.17` | 各组件单独打 tag |
| `vercel/ai` | `485e2a05eb`（11-19） | `ai@7.0.177`，`@ai-sdk/openai-compatible@3.0.93` | 极频繁 |
| `openai/openai-agents-python` | `91fb508a89`（11-18） | v0.25.0 | 频繁 |
| `deepseek-ai/deepseek-harness` | `2e39e70496`（11-18） | dsh-v0.2.2-rc.5（11-18） | 每周都有 rc 或 alpha |

> 说明：上一份报告写的「Genkit Go v1.9」是 6 月的版本，现在已经到 go/v1.14.0。

---

## 一、适配器内部用什么实现

### 1.1 四种做法都要回答的问题

ADR 0008 要求适配器交出的东西，加上 provider-capabilities 报告列出的各家差异，归结成下面几条标准：

1. **拼装工具调用**：能否从流式增量里拼出完整的工具调用，包括并行调用（多个 `index`），以及兼容端点的怪癖：首块没有 `name`、续块重复发送 `name`、缺 `index`、`index` 为 -1、并行调用复用 id。
2. **统一用量**：缓存命中和缓存写入能否拿到；流式用量怎么拿到（`include_usage` 的独立块、DeepSeek 挂在最后一个内容块上、智谱逐块发累计值、Anthropic 的 `message_delta` 发累计值）。
3. **额外字段和推理内容**：响应里的非标准字段（`reasoning_content`、`reasoning`、`prompt_cache_hit_tokens`、`cache_write_tokens`）能否读到；请求里能否带非标准字段。DeepSeek 的思考模式有硬性要求：带 `tools` 的请求，后续请求**必须原样回传** `reasoning_content`，否则返回 400（[DeepSeek — Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode)）。
4. **取消**：`context.Context` 取消后，请求和流能否立刻停下。
5. **成熟度**：版本号、发布节奏、稳定性承诺、破坏性变更的历史。
6. **依赖体量**：引入后增加的模块和包的数量，以及二进制的大小。

### 1.2 官方 SDK：`openai-go` v3 和 `anthropic-sdk-go` v2

**OpenAI 兼容适配器（`openai-go` v3.103.0）**

- **拼装工具调用**：`ChatCompletionAccumulator.AddChunk`（`streamaccumulator.go`）按 `delta.tool_calls[].index` 归并，`function.name` 和 `function.arguments` 都是**追加拼接**（第 468–469 行 `toolStrings.name.append(...)`、`toolStrings.arguments.append(...)`）。`index == -1` 按 0 处理，注释写明是为兼容「单个工具调用发 -1」的服务商（`streamaccumulator_limits.go` 第 57 行起）。它的问题有三个：
  - 续块如果重复发送完整的 `name`，名字会被拼成两遍。这个怪癖确实存在：Vercel 的 tracker 在注释里专门处理了「continuations repeat a name」，见 2.4。
  - 缺 `index` 的增量在 Go 里反序列化成 0，所有调用会被并到第一个。
  - chunk 的 `id` 中途变化时，`AddChunk` 返回 `false`，整块不累积（第 176–179 行）。调用方必须检查返回值。Genkit 的 `compat_oai` 就没有检查（见 1.3）。
- **用量**：要自己设置 `StreamOptions.IncludeUsage = true`（`chatcompletion.go` 第 3475 行）。累加器把每个 chunk 的 `Usage` **相加**（第 476–486 行），包括 `PromptTokensDetails.CachedTokens`。对「只发一次用量块」的服务商（OpenAI、Kimi、通义、OpenRouter、DeepSeek 的最后一个内容块）结果是对的；对**逐块发累计值**的服务商（智谱开了 `chunk_include_usage`），结果会重复累加。缓存写入（Kimi 的 `cache_write_tokens`、OpenRouter）和 DeepSeek 的 `prompt_cache_hit_tokens` 没有对应的结构体字段，只能从 `JSON.ExtraFields` 里读。
- **额外字段**：响应里每个结构体都有 `JSON.ExtraFields map[string]respjson.Field` 和 `RawJSON()`（README「Response objects」一节；`ChatCompletionChunkChoiceDelta` 定义在 `chatcompletion.go` 第 1136 行起）；请求结构体有 `SetExtraFields(map[string]any)`（`packages/param/param.go` 第 151 行），另外还有 `option.WithJSONSet`。仓库里**没有** `reasoning_content` 字段，累加器也不累积它，要靠调用方自己从 `ExtraFields` 里读出来、再用 `SetExtraFields` 回传（Genkit 就是这么做的）。
- **取消**：请求带 `ctx`；流的错误通过 `stream.Err()` 交出，服务商在流中途发来的 `{"error": …}` 会结束流（`packages/ssestream/ssestream.go` 第 268 行）。默认自动重试 2 次（`internal/requestconfig/requestconfig.go` 第 286 行，README 第 1235 行）。
- **成熟度**：v1.0.0 发布于 2025-05-19，v2.0.0 于 2025-08-07，v3.0.0 于 2025-09-30，之后一年多都是 v3。README 的 Semantic versioning 一节承认「certain backwards-incompatible changes may be released as minor versions」，CHANGELOG 里确实有 minor 版本带破坏性变更的例子（3.28.0 把 `voice` 从 `string` 改成了 union）。代码由 Stainless 从 OpenAPI 生成，几乎每天发版。
- **依赖体量**：自测数据见 1.6。只用 Chat Completions，模块图有 34 个模块，非标准库包 34 个（tidwall gjson/sjson、coder/websocket），但**剥离符号后的二进制有 24 MB**。原因是根包 `openai` 包含整个 API 的类型，不能只编译 Chat Completions 那一部分。

**Anthropic Messages 适配器（`anthropic-sdk-go` v2.11.0）**

- **拼装工具调用**：`Message.Accumulate`（`messageutil.go` 第 25 行起）按 `content_block_start/delta/stop` 的 `index` 拼装。`input_json_delta` 追加到 `Input`；工具调用被截断、拼出的不是合法 JSON 时置为 `{}`（`refreshContentBlockRaw`）。`thinking_delta` 和 `signature_delta` 也会累积，适配器回传思考块需要它们。Anthropic 协议本身用块来区分并行调用，没有 OpenAI 那类怪癖。
- **用量**：注释写明 `message_delta` 里的用量是「cumulative whole-message total, so it overwrites rather than adds」，`cache_creation_input_tokens` 和 `cache_read_input_tokens` 只在字段存在时覆盖（第 44–58 行）。缓存命中和写入都是结构体字段，直接可用。
- **额外字段**：同样由 Stainless 生成，`JSON.ExtraFields`、`RawJSON()`、`SetExtraFields` 和 openai-go 一致。
- **成熟度**：v1.0.0 于 2025-05-22 发布，v2.0.0 于 **2026-10-21** 发布，破坏性变更的原因是「add support for new API version 2026-10-21」（CHANGELOG），也就是大版本跟着 API 版本走。v1 线一直维护到 v1.84.0。
- **依赖体量**：只用 Messages 时，模块图 76 个模块，非标准库包 57 个，二进制 12 MB。多出来的主要是 OpenTelemetry（`otel`、`otel/trace`、`otel/metric`），SDK 内置了 tracing。`go.mod` 里还声明了 AWS、GCP 和 MCP 的依赖，但不 import `bedrock/`、`vertex/` 子包就不会编译进来。

### 1.3 框架的单次模型调用：Genkit Go 的模型插件

只调模型、不跑循环的写法：`genkit.Init(ctx, genkit.WithPlugins(...))` 之后，用 `genkit.LookupModel(g, name).Generate(ctx, req, cb)` 直接调用模型 action（`ai/generate.go` 第 1695 行，内部只走模型中间件，不进工具循环）；也可以给 `genkit.Generate` 传 `ai.WithReturnToolRequests(true)`，让它把工具请求交回调用方（`ai/generate.go` 第 832 行）。

- **OpenAI 兼容（`plugins/compat_oai/generate.go`）**：内部就是 `openai-go` 加 `ChatCompletionAccumulator`（第 672 行），但它在累加器外面补上了官方 SDK 缺的几块，而且每一块都在注释里写明了原因：
  - 自动设置 `IncludeUsage`（第 664 行）。用量不用累加器相加的结果，而是保存最后一个有效的 `usage` 块（第 697–700 行），这样就绕开了「累计值被重复累加」的问题。
  - `reasoning_content`（Kimi、DeepSeek）和 `reasoning`（OpenRouter）从 `ExtraFields` 里读出来（第 716–725 行）。回传时只要消息里有 reasoning 部分，就用 `SetExtraFields` 带回 `reasoning_content`，**即使内容是空的也带**，因为 DeepSeek 思考模式下少了它会拒绝工具调用（第 115–123 行）。
  - 缓存命中先读 `cached_tokens`，为 0 时回退到 DeepSeek 的 `prompt_cache_hit_tokens`；缓存写入从 `prompt_tokens_details.cache_write_tokens` 里读（OpenRouter，第 1062–1078 行）。
  - 网关在流中途报的错（chunk 顶层的 `error`）和 choice 上的 `error` 都会被识别并分类，智谱的四位数错误码不会被当成 HTTP 状态码（第 805–830 行）。
  - 子包 `deepseek`、`kimi`、`dashscope`、`zai`、`openrouter`、`xai` 处理各家的默认值，比如智谱和通义只支持 `tool_choice: auto`（各自的 `README.md`）。
  - **缺点**：只处理 `Choices[0]`；不检查 `AddChunk` 的返回值；工具调用拼装完全沿用 openai-go 累加器的规则（name 追加拼接、缺 index 并到一个）。最要紧的是 **go.mod 锁在 `openai-go v1.8.2`**，比当前的 v3 落后两个大版本，所以 v3 累加器后来加的修正（比如缓存 token 的累加）它都用不上。
- **Anthropic（`plugins/anthropic` + `plugins/internal/anthropic/anthropic.go`）**：基于 `anthropic-sdk-go` **v1.23.0**（没有跟进 v2）。用量的换算规则是 `InputTokens = input + cache_read + cache_creation`，`CachedContentTokens` 和 `CacheWriteTokens` 分开记（第 718–722 行）；流式时对各项取 `max`（第 776–786 行），对累计值和一次性的值都不会出错。
- **统一用量类型**：`ai.GenerationUsage`（`ai/gen.go` 第 186 行）有 `InputTokens`（**包含**缓存部分）、`CachedContentTokens`、`CacheWriteTokens`、`ThoughtsTokens` 和 `Custom`，语义和 ADR 0008 要的「统一用量」几乎一致。
- **成熟度**：README 写明 Go 是「Production-ready」，但「Experimental」API 放在 `genkit/exp`、`ai/exp` 里，并且「may experience breaking changes in minor releases」（`go/README.md` 第 168 行）。所有插件和核心都在**同一个 Go 模块**里，版本一起走。
- **依赖体量**：引入 `compat_oai` 和 `anthropic` 两个插件并 `genkit.Init`，模块图有 170 个模块，非标准库包 133 个（其中 31 个是 OpenTelemetry），二进制 24 MB。

### 1.4 框架的单次模型调用：Eino 的 ChatModel 组件

- **OpenAI 兼容（`eino-ext/components/model/openai` v0.1.13 → `libs/acl/openai` v0.1.17）**：底层**不是** openai-go，而是 `github.com/meguminnnnnnnnn/go-openai`，即 sashabaranov/go-openai 的个人 fork。`libs/acl/openai/go.mod` 的注释写着「temporary solution, switch to github.com/openai/openai-go in the future」。
  - `Stream` 自动设置 `IncludeUsage: true`（`chat_model.go` 第 881 行）。推理内容是结构体字段 `ReasoningContent`，请求和响应双向都支持（第 509、536、820 行），DeepSeek 的回传要求天然满足。缓存命中和写入映射到 `schema.TokenUsage.PromptTokenDetails{CachedTokens, CacheWriteTokens}`（第 1338–1356 行）。请求可以通过 `WithExtraFields` 带额外字段（`option.go` 第 89 行）。
  - 组件只吐出增量 `*schema.Message`。要得到完整的工具调用，得用 eino 核心的 `schema.ConcatMessages`（`eino/schema/message.go` 第 1670 行）：工具调用按 `Index` 归并，`index == nil` 的调用单独保留、不合并；`ID`、`Type`、`Name` 被视为「atomically output in any chunk」，续块重复同名不会拼成两遍，名字不一致就报错（第 1297 行起）。用量对各项取 `max`（第 1762–1787 行）。
- **Anthropic（`components/model/claude` v0.1.26）**：基于 `anthropic-sdk-go` **v1.56.0**。缓存命中和写入都映射了（`claude.go` 第 1406–1433 行），`input_json_delta` 按工具事件逐段交出（第 1588 行）。
- **成熟度**：eino 核心仍是 0.x，同时维护 v0.9 和 v0.10-alpha 两条线；组件是 v0.1.x，单独打 tag；OpenAI 组件依赖一个个人 fork。文档中文齐全，字节内部在用。
- **依赖体量**：两个组件一起引入，模块图 159 个模块，**非标准库包 407 个**，二进制 25 MB。claude 组件引入了 AWS SDK（50 个包）、gRPC 和 google auth（Bedrock 和 Vertex 支持），还带了 `gonja` 模板引擎（eino 核心的 prompt 依赖）。

### 1.5 直接 `net/http` 加 SSE 解析

- **拼装工具调用**：完全自己写，所有怪癖的处理策略由 Keine 决定。现成的参考有三份：Vercel 的 `streaming-tool-call-tracker.ts`（见 2.4，怪癖覆盖最全）、Eino 的 `concatToolCalls`（Go，规则简单）和 openai-go 累加器（Go，对资源上限的防御最细）。
- **用量**：两个协议的字段集合都是固定的，provider-capabilities 报告已经列全，自己定义 struct 就能完整覆盖 `cached_tokens`、`prompt_cache_hit_tokens`、`cache_write_tokens`、`cache_creation_input_tokens`、`cache_read_input_tokens`。累计还是一次性，可以统一用「最后一次有效值或取 max」来处理。
- **额外字段**：天然支持，响应用 `json.RawMessage` 或 `map` 兜底即可。
- **取消**：`http.NewRequestWithContext` 加关闭 body，标准库就够了。重试和退避要自己写。
- **成熟度**：没有升级风险，也没有外部维护者。两个协议有新能力（新的缓存字段、新的块类型）都要自己跟进。
- **依赖体量**：基线是一个只用 `net/http`、`bufio`、`encoding/json` 的程序，6.6 MB，没有第三方依赖。
- **工作量的量级**（参考值，不是估算）：Genkit 的 `compat_oai/generate.go` 是 1190 行（包括消息转换、错误分类、多家怪癖），`internal/anthropic/anthropic.go` 也在这个量级；Vercel 的 tracker 是 534 行 TS。

### 1.6 依赖体量实测

在 `$SCRATCH/bench` 下每种做法建一个最小 `main`，命令是 `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`，与 Keine 的 `Dockerfile` 一致。Go 版本 1.27.2。

| 做法 | 二进制（剥离符号后） | 模块图里的模块 | 编译进来的非标准库包 |
| --- | --- | --- | --- |
| `net/http` + `encoding/json` 基线 | 6.6 MB | 1 | 1（只有 main） |
| `openai-go` v3（只用 Chat Completions 流式 + 累加器） | 24.4 MB | 34 | 34 |
| `anthropic-sdk-go` v2（只用 Messages 流式 + Accumulate） | 12.0 MB | 76 | 57 |
| 两个官方 SDK 一起 | 31.4 MB | 84 | 86 |
| Genkit Go v1.14.0（`compat_oai` + `anthropic` 插件 + `Init`） | 24.0 MB | 170 | 133 |
| Eino 组件（`model/openai` v0.1.13 + `model/claude` v0.1.26） | 25.0 MB | 159 | 407 |

Genkit 的二进制比「两个官方 SDK」小，是因为它锁的是更老的 openai-go v1.8.2 和 anthropic-sdk-go v1.23.0，这两个版本的 API 面更小。

### 1.7 横向对比（适配器）

| | 拼装工具调用（含怪癖） | 缓存命中 / 写入 | 流式用量 | 推理内容收发 | 成熟度 / 升级风险 | 体量 |
| --- | --- | --- | --- | --- | --- | --- |
| `openai-go` v3 | 按 index 归并；name 追加拼接（续块重复 name 会出错）；缺 index 并到一个；id 变化时整块拒收 | 命中 ✅（`cached_tokens`）；DeepSeek 的字段和写入要从 ExtraFields 读 | 要手动开；按块**相加**，遇到累计值会重复计数 | 没有字段，靠 ExtraFields / SetExtraFields 自己处理 | 官方维护，v3 已稳定一年；几乎每天发版；minor 版本偶有破坏性变更 | 24 MB / 34 包 |
| `anthropic-sdk-go` v2 | 协议按块区分，无怪癖；截断时置为 `{}` | ✅ 结构体字段 | ✅ 覆盖累计值 | ✅ thinking / signature | 官方维护；大版本跟 API 版本走（v2 发布于 2026-10-21） | 12 MB / 57 包（含 otel） |
| Genkit `compat_oai` | 沿用 openai-go **v1.8.2** 的累加器；只取 Choices[0] | ✅ 包括 DeepSeek 回退和 OpenRouter 写入 | ✅ 取最后一个用量块 | ✅ 读写都处理，包括 DeepSeek 的空回传 | Go 标为 production-ready，每月一版；内部 SDK 版本落后 | 共 24 MB / 133 包 |
| Genkit `anthropic` | 沿用 anthropic-sdk-go **v1.23.0** | ✅ | ✅ 取 max | ✅ | 同上 | （同上） |
| Eino `model/openai` | 要配合 `schema.ConcatMessages`：按 Index 归并，name 原子化，nil index 单独保留 | ✅ | ✅ 取 max | ✅ 原生字段 | 0.x；底层是个人 fork 的 go-openai，注释写着「临时方案」 | 共 25 MB / 407 包 |
| Eino `model/claude` | anthropic-sdk-go v1.56.0 | ✅ | ✅ | ✅ | 0.x | （同上，含 AWS 和 gRPC） |
| `net/http` 自写 | 由 Keine 决定，可以照搬 Vercel tracker 的规则 | ✅ 自己定义字段 | ✅ 自己处理 | ✅ | 无外部风险，跟进全靠自己 | 6.6 MB 基线 |

### 1.8 取舍（适配器）

1. **Anthropic 适配器**：协议本身规整，几种做法的差别主要在维护成本和体量。官方 SDK 的 `Accumulate` 已经正确处理了累计用量和截断，而且是唯一跟上 v2 / API 2026-10-21 的实现；代价是引入 otel 这一组依赖，以后每次 API 版本升级都伴随一次 SDK 大版本。Genkit 和 Eino 的 Anthropic 插件都还停在 SDK v1。
2. **OpenAI 兼容适配器**：主要风险在怪癖。官方 SDK 的累加器是按 OpenAI 自家的行为写的，对兼容端点有已知的盲区：name 拼接、缺 index、按块相加用量、不处理 reasoning_content。用它的话，这几处多半还得在外面包一层，Genkit `compat_oai` 包的就是这一层。那样算下来，openai-go 真正提供的只剩类型定义和 SSE 解码，却要付出 24 MB 的体积。反过来，自写 `net/http` 能直接照搬 Vercel tracker 的怪癖处理规则，二进制最小，但所有防御（资源上限、错误分类、重试）都要自己写和测。
3. **框架的单次模型调用**：Genkit 和 Eino 把各家怪癖处理得最完整（Genkit 注释详细，Eino 原生支持 reasoning_content），但两者内部的官方 SDK 版本都落后；作为依赖还会带上框架核心的类型（`ai.ModelRequest`、`schema.Message`），适配器里要多做一层双向转换，ADR 0008 已经指出这一层会随框架升级而失效。另外，Eino 的 OpenAI 组件基于个人 fork，在「成熟度」这条硬标准上是一个明确的扣分项。
4. **统一用量的口径要先定**：Genkit 的 `InputTokens` **包含**缓存部分，DeepSeek Harness 的 `TokenUsage.inputTokens` 却是**不含**缓存的（「Counts are DISJOINT」，`packages/llm/llm/src/types.ts` 第 175–195 行，它会从 DeepSeek 的 `prompt_tokens` 里减掉命中的部分）。OpenAI 的 `prompt_tokens` 包含 `cached_tokens`，Anthropic 的 `input_tokens` 不包含 `cache_read` 和 `cache_creation`。不管选哪种实现，Keine 都要先定下自己的口径，额度计量依赖它。
5. **DeepSeek 的 reasoning_content 回传**和「运行中的工具调用只在内存里」（ADR 0008）不冲突，因为一次运行里的子请求都在内存中。但官方文档写的是「all subsequent requests」都要回传。跨轮之后（下一轮探索对话）是否仍然需要回传，需要实测确认，这会影响保存的消息要不要带推理内容。

---

## 二、循环写法参考

ADR 0008 规定由 Keine 负责的部分有：并行工具执行、轮数上限、取消、错误回填给模型、每一步的用量计量，另外还有工具 panic 的兜底。下表按这几项列出各实现的做法和值得读的文件。

### 2.1 `anthropic-sdk-go` beta toolrunner（Go）

文件：`betatoolrunner.go`（1218 行）、`toolrunner/tool.go`。

- **并行**：`executeTools` 用 `errgroup.WithContext`，每个工具调用一个 goroutine，结果按原顺序写进切片，所以回填顺序和模型给出的顺序一致（第 439–470 行）。**没有并发上限**。
- **错误回填**：「tool errors become result content, not Go errors」。找不到工具、参数序列化失败、`Execute` 返回 err，都会变成 `is_error: true` 的 tool_result，内容是 `"Error: ..."`（第 506、606–636 行）。**没有 recover**，工具 panic 会让进程崩掉。
- **轮数上限**：`MaxIterations` 限制 API 调用次数，0 表示不限（第 41–44、696 行）；到达上限时静默结束，不报错。
- **结束原因**：`determineNextStepFromStopReason` 把每一种 stop_reason 显式归到一个分支（第 366–393 行）。`refusal` 时不执行工具（注释写的是「executing them fires side effects the caller never confirmed」）；`max_tokens` 时也不执行工具，因为最后一个调用的参数不完整；不认识的原因按 end_turn 处理。**这一段值得照搬。**
- **取消**：每个工具开始执行前检查 `gctx.Done()`。
- **用量**：没有逐步汇总，要从每次的 `BetaMessage.Usage` 自己取。
- **值得借鉴**：stop_reason 的分类表；按顺序保存结果的 errgroup 写法；「错误转成 tool_result，Go error 只留给取消」的分层。

### 2.2 Genkit Go generate 循环（Go）

文件：`go/ai/generate.go`（3104 行），重点看 `generateWithRequest`（第 473 行起）、`handleToolRequests`（第 1987 行起）、`toolErrorAnswer`（第 1898 行）、`recoverToolPanic` 和 `runToolRecovered`（第 1816–1840 行）、`SumUsage`（第 964 行）。

- **并行**：`handleToolRequests` 给每个工具请求开一个 goroutine，结果通过带缓冲的 channel 收回（第 2012 行），没有并发上限。
- **轮数上限**：`MaxTurns` 默认 50，超出时返回 `ErrMaxTurnsExceeded`，同时用 `failurePartial` 交回一个「可以重新发送」的部分结果（第 555–560、836–838 行）。
- **错误回填**：**默认是工具出错就让整个生成失败**；只有工具返回 `base.ToolFailError`，或者用户挂了 `SoftToolErrors` 中间件，才把 `{"error": msg}` 回填给模型。取消和超时永远不回填（第 1898–1925 行）。工具失败时，整轮连同发起它的模型消息一起丢掉，保证交回的对话可以重发（第 842–848 行的注释）。
- **panic**：`recover` 后返回固定文本 `"tool panicked"`，panic 的值和堆栈只写日志，因为「the panic value can hold anything the tool had in hand, secrets included」（第 1809–1823 行）。
- **取消**：`toolFailureError` 区分调用方取消（`ErrCancelled`，结束原因记为 aborted）和工具自身失败（第 1800 行）。
- **用量**：`SumUsage` 逐字段相加，包括缓存的两个字段。
- **值得借鉴**：panic 文本脱敏；「取消不算工具失败」的区分；超过轮数时交回可重发的部分结果；`GenerationUsage` 的字段设计。

### 2.3 Eino（Go）

文件：`compose/tool_node.go`（并行执行在第 1095–1123 行，配置项在第 190–230 行）、`adk/react.go`、`adk/chatmodel.go`（`MaxIterations` 在第 305 行）、`flow/agent/react/react.go`（`MaxStep`）。

- **并行**：默认并行，`ExecuteSequentially` 可以改成串行。第一个任务在当前 goroutine 上执行，其余各开 goroutine，并 `recover` 成 `safe.NewPanicErr`。注意第一个任务不在 recover 里。
- **轮数上限**：ADK 的 `MaxIterations`，超出返回 `ErrExceedMaxIterations`；图层面还有 `ErrExceedMaxSteps`。
- **错误回填**：`UnknownToolsHandler` 处理模型凭空编出的工具名，`ToolArgumentsHandler` 和 `ArgumentsAliases` 在执行前修正参数，`ToolCallMiddlewares` 包装执行过程。默认工具出错会让图失败，要回填给模型得自己写中间件。
- **值得借鉴**：「未知工具」和「参数修正」两个独立的钩子点。整体抽象（图、Runner、事件迭代器）比 Keine 需要的厚很多。

### 2.4 Vercel AI SDK（TypeScript，只作参考）

文件（`packages/ai/src/generate-text/`）：`stop-condition.ts`（77 行）、`tool-call-concurrency.ts`、`execute-tool-call.ts`、`parse-tool-call.ts`、`to-response-messages.ts`、`sum-token-counts.ts`；`packages/provider-utils/src/streaming-tool-call-tracker.ts`（534 行）；`packages/openai-compatible/src/chat/openai-compatible-chat-language-model.ts`。

- **轮数上限**：`stopWhen` 是一组谓词，比如 `isStepCount(n)`、`hasToolCall(name)`，任意一个为真就停。**默认 `isStepCount(1)`**，也就是不主动开多步（`generate-text.ts` 第 295 行）。
- **并行**：默认所有工具调用并发执行；`toolCallConcurrency` 可以限制并发数，`mapWithConcurrency` 保证结果顺序，abort 后不再启动排队中的调用。
- **错误回填**：工具抛出的异常变成 `tool-error` 部分，转成消息时是 `errorMode: 'text'`，回填给模型（`execute-tool-call.ts` 第 162 行、`to-response-messages.ts` 第 120、207 行）。工具名不存在或参数不合法时会抛 `NoSuchToolError` / `InvalidToolInputError`，可以接一个 `repairToolCall` 函数来修正（`parse-tool-call.ts`）。
- **用量**：每一步有 `usage`，汇总在 `totalUsage`；`inputTokens` 拆成 `noCache`、`cacheRead`、`cacheWrite`（`sum-token-counts.ts`）。
- **流式工具调用的怪癖**（`streaming-tool-call-tracker.ts` 的注释，这是**写 OpenAI 兼容适配器时最值得读的一份**）：续块会重复空名或同名；只有每个调用都带 index 时 index 顺序才可信；并行调用可能复用 id，所以「ID/name pair cannot override index evidence」；同一个 index 上出现不同的 name，说明服务商在复用 index；参数可以解析成 JSON **并不代表已经结束**，必须等流结束再定稿（引用了 issue #13137）。`openai-compatible` 模型另外缓冲了「首块没有 `function.name`」的增量（第 616–670 行）。流结束时如果没有 finish_reason，记为 error。
- **值得借鉴**：tracker 的整套规则；stop condition 作为可组合的谓词；工具并发上限加保序；用量的三段拆分。

### 2.5 OpenAI Agents SDK（Python，只作参考）

文件：`src/agents/run_config.py`（`DEFAULT_MAX_TURNS = 10`）、`run_error_handlers.py`、`run_internal/run_loop.py`、`run_internal/tool_execution.py`、`tool.py`（`default_tool_error_function`，第 1997 行）、`usage.py`（`RequestUsage`、`request_usage_entries`）。

- **错误回填**：工具出错时，默认回填给模型的是固定文本 `"An error occurred while running the tool."`，**不暴露异常内容**；需要细节时自己提供 `failure_error_function`。
- **轮数上限**：超过时抛 `MaxTurnsExceeded`，但可以注册错误处理器，用已有的 `history` 和 `new_items` 生成一个收尾输出（`RunErrorData`）。
- **用量**：`Usage` 除了总数，还保存**逐请求**的 `request_usage_entries`，正好对应 ADR 0008 的「每一步的用量计量」。
- **值得借鉴**：默认不把异常原文回填给模型；超过轮数时「优雅收尾」的钩子；逐请求的用量明细。

### 2.6 DeepSeek Harness（TypeScript，只作参考）

文件：`packages/core/agent-loop/src/agent.ts`（`turn()` 第 333 行起，`step()` 第 438 行起）、`packages/core/tools/src/batch.ts`（285 行）、`packages/core/session/src/repair.ts`、`packages/llm/llm/src/types.ts`（`StreamChunk` 第 496 行、`TokenUsage` 第 175 行、`FinishReason` 第 164 行）、`packages/llm/llm/src/assembler.ts`。

- **适配器接口**：`StreamChunk` 的类型有 `block-start`、`text-delta`、`reasoning-delta`、`tool-call-delta`、`block-end`（带完整的块）、`usage`、`finish`。`FinishReason` 有 `stop`、`tool-calls`、`max-tokens`、`aborted`、`error`。这和 ADR 0008 的适配器边界几乎一一对应，是**现成的接口设计参照**。拼装统一放在 `assembler.ts`（「single canonical assembly algorithm」），适配器只负责翻译。
- **并行**：`batch.ts` 把工具分成「exclusive」和「parallel-safe」两类。exclusive 的调用形成屏障，parallel-safe 的调用进入有上限的滚动池（默认 `DEFAULT_MAX_PARALLEL_TOOL_CALLS = 10`），结果按模型给出的顺序定稿。
- **取消和错误**：abort 后，已经开始的调用等它们跑完，**没有开始的调用补一条合成的错误结果**，保证历史可以重放；step 失败时，`ToolCallRecovery` 给悬而未决的工具调用补上结果（`agent.ts` 第 368–392 行，`repair.ts`）。`max-tokens` 一旦出现就「sticky」，后面的步骤正常结束也不会覆盖这一轮的结束原因。
- **轮数上限**：核心循环里**没有**步数上限（`core/` 和 `llm/` 下搜不到 `maxSteps`、`maxTurns`），它是交互式编码 agent，靠用户中断。
- **用量**：`TokenUsage` 采用「不相交」口径，从 DeepSeek 的 `prompt_tokens` 里减掉缓存命中。
- **值得借鉴**：StreamChunk / FinishReason 的划分；abort 时给没开始的调用补合成结果；exclusive 和 parallel-safe 两类调度（Keine 现在的工具都是只读的，暂时用不上，但要留出口子）。它的整体架构（插件、事件溯源的 session）比 Keine 需要的重得多，不宜照搬。

### 2.7 横向对比（循环）

| | 并行 / 上限 | 轮数上限默认值 | 工具错误默认 | panic | 取消 | 逐步用量 |
| --- | --- | --- | --- | --- | --- | --- |
| anthropic toolrunner | errgroup，无上限，保序 | 0 = 不限，到达后静默结束 | 回填 `is_error` 和原文 | 不处理 | 执行前检查 ctx | 自己取 |
| Genkit | goroutine，无上限 | 50，报错并交回可重发的部分结果 | **让生成失败**（可改成回填） | recover，固定文本 | 和工具失败区分 | `SumUsage` |
| Eino | 默认并行，可串行 | `MaxIterations` / `MaxStep` | 让图失败（用中间件改） | recover（第一个任务除外） | 图层面处理 | 回调 |
| Vercel AI SDK | 默认全部并发，可设上限，保序 | `isStepCount(1)` | 回填 error-text | — | AbortSignal，不再启动排队的调用 | 每步 + `totalUsage` |
| OpenAI Agents | asyncio 并发 | 10，可优雅收尾 | 回填**固定文本** | — | — | `request_usage_entries` |
| DeepSeek Harness | exclusive / parallel-safe，上限 10，保序 | 无 | 回填并合成恢复结果 | — | 没开始的调用补合成错误 | 不相交口径的 TokenUsage |

### 2.8 取舍（循环）

- **错误回填的口径**有三派：回填原文（anthropic toolrunner、Vercel）、回填固定文本（OpenAI Agents）、默认让运行失败（Genkit、Eino）。ADR 0008 的 #90「未定」里，错误回填策略这一条可以直接拿这三派来比。回填原文有泄露内部信息的风险，Genkit 的 panic 文本脱敏和 OpenAI Agents 的默认固定文本都是在防这件事。
- **轮数上限到达时**：静默结束（toolrunner）、报错（Genkit、Eino、OpenAI Agents），或者报错同时交回部分结果、给一个收尾钩子（Genkit、OpenAI Agents）。探索助手的流式事件里有 `error` 和 `done`，选哪种会影响前端看到什么。
- **并发上限**：Go 的两个参考（toolrunner、Genkit）都不限制并发；Vercel 和 DeepSeek Harness 有上限并且保序。ADR 0008 已经有全局并发上限，单次运行内要不要再加一层，可以参考 `mapWithConcurrency` 的保序写法。
- **结束原因**：anthropic toolrunner 的 stop_reason 分类表（refusal 和 max_tokens 都不执行工具）和 DeepSeek Harness 的「max-tokens sticky」是两个容易被忽略的细节。

---

## 如果非要选（仅供参考，属个人意见，请人来判断）

> 以下不是结论，只是按上面的事实给出的一个可能组合，便于讨论。

- **Anthropic 适配器**：倾向用 `anthropic-sdk-go` v2 的 `Messages.NewStreaming` 加 `Message.Accumulate`。协议规整，SDK 已经正确处理了累计用量和截断，而且是唯一跟上 API 2026-10-21 的实现。代价是 otel 依赖，以及大版本跟着 API 版本走。
- **OpenAI 兼容适配器**：在「openai-go 加一层外壳」和「`net/http` 自写」之间。倾向自写：类型和 SSE 解码都不复杂；怪癖规则可以照搬 Vercel 的 `streaming-tool-call-tracker.ts`；用量和 reasoning 的处理可以照搬 Genkit `compat_oai/generate.go` 的注释和逻辑；二进制少 18 MB 左右；也不受 openai-go 累加器的盲区影响。如果更看重「有人替我们跟进 OpenAI 的新字段」，就选 openai-go，但累加器外面的那层（用量取最后一个有效值、name 去重、reasoning_content 收发、检查 `AddChunk` 返回值）还是要写。
- **不推荐**把 Genkit 或 Eino 的模型组件当适配器的内部实现：它们锁的官方 SDK 都落后一个以上大版本，还要多一层框架类型的转换；Eino 的 OpenAI 组件依赖个人 fork。但它们的源码是**写适配器时最好的怪癖清单**。
- **循环**：骨架参考 anthropic toolrunner（errgroup 保序、错误转成 tool_result、stop_reason 分类表），加上 Genkit 的 panic 脱敏、「取消不算工具失败」、超过轮数时交回部分结果，加上 OpenAI Agents 的逐请求用量明细，再加上 DeepSeek Harness 的「abort 时给没开始的调用补合成结果」和 StreamChunk 接口划分。

## 不确定点和待实测

- 通义、智谱、Ollama、LiteLLM 的流式工具调用增量**实际**长什么样（是否重复 name、是否缺 index、首块有没有 name），文档没有写；上面的怪癖清单来自 Vercel 和 Genkit 的源码注释，没有逐家验证。值得用录下来的真实 SSE 做适配器的测试夹具。
- DeepSeek 思考模式要求「all subsequent requests」都回传 `reasoning_content`，跨轮（不只是一次运行内部）是否仍然强制，会影响保存的消息要不要带推理内容。
- 二进制体积是最小程序测出来的，和 Keine 现有的二进制合并后，标准库部分会重叠，实际增量会比表里的数字小（这里没测 Keine 自身的基线，因为构建需要先产出前端 `dist`）。
- `openai-go` 累加器在「chunk id 中途变化」时整块拒收，现实中有没有兼容端点这样做，没有查到证据。
- Genkit 的 `compat_oai` 什么时候升级到 openai-go v3、Eino 什么时候换掉 go-openai 的 fork，都没有公开的计划。

## 来源

- `openai/openai-go`：`streamaccumulator.go`、`streamaccumulator_limits.go`、`chatcompletion.go`、`packages/param/param.go`、`packages/ssestream/ssestream.go`、`internal/requestconfig/requestconfig.go`、`README.md`（Semantic versioning、Response objects）、`CHANGELOG.md`。https://github.com/openai/openai-go/tree/v3.103.0
- `anthropics/anthropic-sdk-go`：`messageutil.go`、`betatoolrunner.go`、`toolrunner/tool.go`、`go.mod`、`CHANGELOG.md`、`MIGRATION.md`。https://github.com/anthropics/anthropic-sdk-go/tree/v2.11.0
- `genkit-ai/genkit`（Go）：`go/ai/generate.go`、`go/ai/gen.go`、`go/ai/option.go`、`go/plugins/compat_oai/generate.go`、`go/plugins/compat_oai/{zai,dashscope}/README.md`、`go/plugins/internal/anthropic/anthropic.go`、`go/go.mod`、`go/README.md`、根目录 `README.md`。https://github.com/genkit-ai/genkit/tree/main/go
- `cloudwego/eino`：`schema/message.go`、`compose/tool_node.go`、`adk/react.go`、`adk/chatmodel.go`、`flow/agent/react/react.go`。https://github.com/cloudwego/eino
- `cloudwego/eino-ext`：`libs/acl/openai/{chat_model.go,option.go,go.mod}`、`components/model/openai/{chatmodel.go,go.mod}`、`components/model/claude/{claude.go,go.mod}`。https://github.com/cloudwego/eino-ext
- `vercel/ai`：`packages/ai/src/generate-text/*`、`packages/provider-utils/src/streaming-tool-call-tracker.ts`、`packages/openai-compatible/src/chat/*`。https://github.com/vercel/ai
- `openai/openai-agents-python`：`src/agents/{run_config.py,run_error_handlers.py,tool.py,usage.py}`、`src/agents/run_internal/{run_loop.py,tool_execution.py}`。https://github.com/openai/openai-agents-python
- `deepseek-ai/deepseek-harness`：`packages/core/agent-loop/{src/agent.ts,src/constants.ts,README.md}`、`packages/core/tools/src/batch.ts`、`packages/core/session/src/repair.ts`、`packages/llm/llm/src/{types.ts,assembler.ts}`、`packages/llm/llm-deepseek/src/messages-api.ts`。https://github.com/deepseek-ai/deepseek-harness
- DeepSeek — Thinking Mode（reasoning_content 回传要求）：https://api-docs.deepseek.com/guides/thinking_mode
- 发布数据：`gh api repos/<repo>/releases` 和 tags，2026-11-20 查询。
- 依赖体量：在本地用 Go 1.27.2 构建最小程序实测（构建脚本不入库）。
