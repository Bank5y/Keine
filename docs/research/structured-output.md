# 工具循环里的规范化输出与步数上限收尾

> 对应 issue #105，属于地图 #90「AI harness 地图」，结论服务于 #97「分析的输出约束与运行中的界面反馈」。调研日期 2026-11-20。
> 本文只列事实和取舍，不替 Keine 做决定。文末「对 Keine 的可选方案及各自代价」只列选项。

## 背景

探索助手的分析要在**工具循环**里产出三段结构的 Markdown：标题、简短介绍、概念阐述，并且**流式**显示给读者。按 ADR 0008（PR #104，`docs/adr/0008-assistant-loop-in-process.md`），工具循环由 Keine 自己用 Go 写；流式事件只有 Keine 定义的 `delta`、`activity`、`done`、`error` 四种，服务商的原始片段不透传；原生适配器只有 OpenAI 兼容 Chat Completions 和 Anthropic Messages 两个，其他服务商走 OpenAI 兼容端点。#97 目前定的是「Markdown 约定加提示词」。

这份报告要回答 ticket 的 5 个问题：约束手段、最终答案和中间文字怎么区分、结构化输出的流式、校验与失败处理、步数上限收尾。各服务商流式、用量、缓存字段的差异已经在 [provider-capabilities.md](https://github.com/Bank5y/Keine/blob/research/provider-capabilities/docs/research/provider-capabilities.md) 里讲过，这里只补结构化输出相关的部分。

## 读过的来源

源码都从各仓库默认分支读取（2026-11-20），链接都固定到下表的提交。

| 仓库 | 提交 |
| --- | --- |
| `openai/openai-agents-python` | `91fb508a89`（11-18） |
| `pydantic/pydantic-ai` | `0473c164a4`（11-20） |
| `vercel/ai` | `a8f0e9dc55`（11-19） |
| `langchain-ai/langchain` | `204968fbbd`（11-19） |
| `langchain-ai/langgraph` | `58a6e1cc5b`（11-20） |
| `567-labs/instructor` | `0acb3a19c3`（11-18） |
| `genkit-ai/genkit`（Go 部分） | `9a7d0b65f8`（11-19） |
| `anthropics/claude-agent-sdk-python` | `69846cdf82`（11-19） |
| `stackblitz/bolt.new` | `eda10b1212`（2024-12-17，开源版最后一次提交） |

文档类来源（OpenAI、Anthropic、各国内服务商、Claude Code、Vercel 博客）见文末的编号引用。**可信度标记**：文中「官方明确」是指文档或源码里直接写着；「推断」是指根据可见行为或几处事实推出来的，没有官方原话。

---

## 一、约束手段：各自在哪里用、适用条件

### 1.1 五种手段一览

| 手段 | 谁在用（默认或可选） | 保证什么 | 和工具调用同时用 |
| --- | --- | --- | --- |
| **提示词约定**（只靠说明和示例） | Pydantic AI `PromptedOutput` [P1]；OpenAI Deep Research 的报告格式只靠提示词 [O7]；Anthropic 官方的格式引导建议 [A6] | 什么都不保证，靠模型遵守 | 天然兼容 |
| **服务商原生结构化输出**（JSON Schema，受限解码） | OpenAI Structured Outputs [O1]；Anthropic `output_config.format` [A1]；Pydantic AI `NativeOutput` [P1]；LangChain `ProviderStrategy` [L1]；Vercel `Output.object` 在支持的服务商上 [V1]；Kimi、部分通义模型 [C3][C5] | 解码层面保证符合 schema（拒答、`max_tokens` 截断除外） | 同一请求里可以同时带工具：Anthropic 文档写明「Claude 可能先调工具，也可能直接返回 JSON」[A1]；OpenAI 推荐「连接工具用函数调用，回答用户用 `text.format`」[O1]；Gemini 2 之前、Groq 不能和函数工具同用 [P1] |
| **「提交结果」工具**（output tool） | Pydantic AI 默认（Tool Output）[P1]；LangChain `ToolStrategy` [L1]；Claude Agent SDK 的 `StructuredOutput` 工具 [A9]；Vercel 的 Anthropic 适配器在不支持原生格式时用名为 `json` 的工具 [V3] | 工具参数可以开 `strict` 得到受限解码 [O2][A2]；不开就只有事后校验 | 本身就是工具，最自然；但通常要求「强制调用工具」，见 1.3 |
| **XML / 自定义标签分隔** | bolt.new 的 `<boltArtifact>` / `<boltAction>` [B1]；Anthropic 官方推荐用 XML 标签引导格式、去掉开场白 [A6] | 不保证模型一定写标签，但客户端能确定性地切出标签内外 | 天然兼容（标签在普通文本里） |
| **受限解码**（服务商内部实现） | Anthropic：「Structured outputs guarantee schema-compliant responses through constrained decoding」，把 schema 编译成语法 [A1]；Kimi：「Token 级约束解码（CFG）」[C3] | 是上面「原生结构化输出」和 `strict` 工具的底层机制，应用侧不直接接触 | 同上 |

### 1.2 失败率：官方给出的数字很少

- OpenAI 和 Anthropic 都只说原生结构化输出「保证」符合 schema，例外是拒答（`refusal`）和 `max_tokens` 截断 [O1][A1]。Anthropic 还写明了一个不保证的点：`enum` / `const` 字符串的大小写可能不一致，「response completes normally, with no error」[A1]。
- Pydantic AI 对三种模式的评价是：Tool Output「has been shown to work very well」，Prompted Output「is often the least reliable approach as the model is not forced to match the schema」[P1]。没有给数字。
- Vercel 的 v0 团队在官方博客写道，LLM 生成的代码出错率「as often as 10%」，所以他们在流式过程中和流式结束后都做修复 [V5]。
- ChatGPT canvas 发布文章给过「什么时候打开 canvas」的触发准确率（写作 83%、代码 94%），这是训练出来的产品行为，不是格式约束失败率 [O6]。
- 除此之外，各家都没有公开「提示词约定 vs 结构化输出」的失败率对比。**结论：失败率只能自己测。**

### 1.3 和工具调用组合时的几个硬约束

1. **Anthropic 最新一批模型不接受强制工具调用。** Claude Fable 5.2 / 5.1、Mythos 5.2 / 5.1、Opus 5.5、Sonnet 5.5 上，`tool_choice: any` 和 `tool_choice: tool` 直接返回 400；文档给的替代是「`auto` 加 strict 工具」或「structured outputs」；`none` 仍然可用 [A3]。手动 extended thinking 下也不能强制 [A3]。这直接影响「提交结果工具」：
   - LangChain `ToolStrategy` 在有结构化输出工具时把 `tool_choice` 设为 `"any"`（[`factory.py` 第 1477–1478 行][L2]），在这些模型上会 400。
   - Pydantic AI 给这些模型标了 `supports_forced_tool_choice=False`，自动退回 `auto`；模型没调输出工具而是写了文字时，它「retries with a prompt to call a tool」[P2]。
   - Vercel 的 Anthropic 适配器在模型拒绝强制工具时改用原生 `output_config.format`，并发一条 warning（[`anthropic-language-model.ts` 第 618–645 行][V3]）。
2. **强制工具调用会吃掉说明文字和思考。** Anthropic：`tool_choice` 为 `any` / `tool` 时「Claude doesn't emit a natural-language response or explanation before `tool_use` content blocks」，而且「Forcing a tool also skips thinking」[A3]。
3. **Anthropic 的原生 JSON 输出和引用（citations）不兼容**，同时开会返回 400；也不能和 prefill 同用 [A1]。Keine 的 Anthropic 路线用服务商自带的 `web_search` / `web_fetch`（ADR 0008），它们的结果带引用——这一点是否等同于「开启 citations」导致冲突，文档没有直说，需要实测（推断）。
4. **prefill 已经没了。** Claude 4.6 及之后的模型（含 Opus 5.5、Sonnet 5、Fable 5、Mythos 5）在最后一条是 assistant 预填时返回 400；官方给的迁移路径是 structured outputs、系统提示词，或者「direct the model to output within XML tags, use structured outputs, or use tool calling. If the occasional preamble slips through, strip it in post-processing」[A6][A7]。
5. **国内兼容端点支持差异大**（详见 3.3）：DeepSeek 和智谱只有 `json_object`，没有 `json_schema` [C1][C6]；智谱 `tool_choice`「默认 `auto` 且仅支持 `auto`」[C6]；DeepSeek 思考模式下 `required` 和指定工具返回 400 [C2]；通义思考模式不支持 `required` [C7]。也就是说，「强制调用提交结果工具」在多数国内模型上做不到，只能 `auto` 加提示词。
6. **原生结构化输出的 schema 有限制。** OpenAI：所有字段必须 required、`additionalProperties: false`、最多 5000 个属性、10 层嵌套 [O1]。Anthropic：所有 strict schema 合计最多 20 个 strict 工具、24 个可选参数、16 个联合类型参数，超了报「Schema is too complex for compilation」；首次使用某个 schema 要编译语法，缓存 24 小时 [A1]。改 `output_config.format` 会让该对话的提示词缓存失效 [A1]。

---

## 二、最终答案和中间文字怎么区分

### 2.1 各家的判定规则

所有框架都**不看文字内容**，而是看**这一步的响应里有没有工具调用**（以及服务商给的结束原因）：

| 框架 / 产品 | 「最终答案」的判定 | 来源 |
| --- | --- | --- |
| OpenAI Agents SDK | 「produces text output with the desired type, and there are no tool calls」；取最后一条 message 的文字。有 `output_type` 时再用 schema 校验这段文字 | 官方文档 [O3]；源码 [`turn_resolution.py` 第 1202–1208 行][OA1] |
| Pydantic AI（`run` / `iter`） | 输出工具调用成功，或者（允许 `str` 时）一段文字。**纯文字和工具调用同时出现时，文字不算最终答案**：「text it emits alongside a tool call is usually narration before it acts ("Let me look that up…"), not a finished answer. Ending the run on it would silently skip the tool, so plain text never preempts a co-emitted function tool」 | [P1] |
| Pydantic AI（`run_stream`） | **例外**：流式方法「commit the first matching output the instant it streams」，所以允许 `str` 时，模型在调工具前流出的第一段文字会被当成最终答案，后面的工具调用不执行。官方建议需要完整循环时改用 `run_stream_events()` 或 `iter()` | [P1] |
| Vercel AI SDK | 最后一步 `finishReason === 'stop'`，或者结束原因不是 `tool-calls` 且有文字，才解析 `output`；最后一步以 `tool-calls` 结束时访问 `output` 抛 `NoOutputGeneratedError` | 源码 [`generate-text.ts` 第 1893–1913 行][V2]；文档 [V1] |
| Claude Agent SDK | 「Claude continues calling tools and processing results until it produces a response with no tool calls」；有 `output_format` 时以 `StructuredOutput` 工具调用为准 | [A8][A9] |
| Genkit Go | 响应里没有工具请求就返回 | [`generate.go` 第 832–834 行][GK1] |
| OpenAI Responses API（GPT-5.4 / 5.5） | **服务商直接标注**：assistant 消息带 `phase` 字段，`commentary` 表示「intermediate assistant updates, such as preambles before tool calls」，`final_answer` 表示「the completed answer」。手动回放历史时要原样保留，否则「preambles [may] be treated as final answers」 | 官方明确 [O4] |

要点：

- **中间文字和工具调用通常在同一个响应里**：文字块在前，`tool_use` 块在后。Anthropic 官方的例子就是「I'll help you check the current weather…」后面跟一个 `tool_use`，并提醒「Your code should treat these responses like any other assistant-generated text, and not rely on specific formatting conventions」[A3]。Kimi 文档也写明 `finish_reason` 为 `tool_calls` 时 `message.content`「occasionally not empty」，内容通常是解释要调哪些工具 [C4]。
- **所以「这段文字是不是最终答案」要等这一步结束才知道**：文字先流出来，工具调用在后面。Anthropic 的 `stop_reason`（`end_turn` / `tool_use`）、OpenAI 兼容端点的 `finish_reason`（`stop` / `tool_calls`）都在流末尾才到。
- **目前只有 OpenAI Responses API 的 `phase` 能在消息开始时就区分**。Vercel 的 OpenAI 适配器在流式里读取 `phase`（[`openai-responses-language-model.ts` 第 1745 行][V4]）；OpenAI Agents SDK 的 Chat Completions 转换器也认这两个值（`chatcmpl_converter.py` 第 309 行）。Chat Completions、Anthropic Messages 和国内兼容端点都没有这个字段。Keine 走的是 Chat Completions，用不上它。

### 2.2 不让中间文字混进结果的做法

| 做法 | 谁在用 | 代价 |
| --- | --- | --- |
| **以「无工具调用的那一步」为准，丢弃或另行展示其他步的文字** | OpenAI Agents SDK、Vercel、Genkit、Claude Agent SDK 都是这个规则 | 流式时要么把每一步的文字先缓冲到步结束，要么先推出去、步结束后再改判 |
| **答案只从「提交结果工具」的参数里取** | Pydantic AI Tool Output、LangChain ToolStrategy、Claude Agent SDK | 文字永远不会混进结果；但要处理「模型没调这个工具，写了一段文字就结束」的情况（Pydantic AI 会重提示 [P2]） |
| **答案只从约定的标签里取** | bolt.new：标签外是说明文字，标签内是 artifact 和动作，流式解析器按字符确定性切分 [B1] | 模型漏写或写错标签时要兜底 |
| **服务商标注** | OpenAI Responses API 的 `phase` [O4] | 只有 OpenAI Responses API 有 |
| **用强制工具调用压掉说明文字** | Anthropic：`tool_choice` 为 `any` / `tool` 时不输出文字 [A3] | 最新一批 Claude 模型不支持；国内多数模型不支持强制 |
| **提示词要求「不要开场白」并在后处理里剥掉** | Anthropic 官方迁移建议 [A6] | 是概率性的 |

另外，流式框架在**每一步开始时会重置部分解析状态**：Vercel 的 `partialOutputStream` 在 `start-step` 时调用 `resetOutputState()`（[`stream-text.ts` 第 1085–1153 行][V6]）。也就是说，中间步骤的文字如果碰巧能解析成部分输出，会先推给调用方，下一步再清掉。这是 Vercel 的实现细节，说明框架本身也没有在流式中提前判定的办法。

---

## 三、结构化输出的流式

### 3.1 部分 JSON 解析

- **Pydantic AI**：`stream_output()` 对每个快照做「partial validation」，最终输出做完整校验；依赖 Pydantic 的部分校验（pydantic#10748）[P1]。输出函数和输出校验器在流式时会被**调用多次**，用 `ctx.partial_output` 区分，副作用只应在 `partial_output=False` 时执行 [P1]。
- **Vercel AI SDK**：`streamText` 的 `partialOutputStream` 只在「partial json can be parsed」时发布，而且只在值变化时才发（[`stream-text.ts` 第 1233–1249 行][V6]）。
- **Instructor**：`create_partial` 流式返回部分对象，但「we do not support validators since they would not be able to be applied to the streaming response」[I2]。
- **Claude Agent SDK**：结构化结果只在最后的 `ResultMessage.structured_output` 里给；文档建议 UI 里跳过名为 `StructuredOutput` 的工具调用 [A9]。

### 3.2 工具参数的增量流式：OpenAI 和 Anthropic

- **OpenAI**：Responses API 有 `response.function_call_arguments.delta` / `.done` 事件 [O2]；Chat Completions 的 `delta.tool_calls[].function.arguments` 分块到达（provider-capabilities 报告已列）。结构化输出（`text.format`）走 `response.output_text.delta`，官方提醒「Text deltas and response.output_text.done do not guarantee completion」，要看最终响应状态 [O1]。
- **Anthropic**：工具参数通过 `input_json_delta` 下发。**默认是缓冲的**：「the API buffers and validates each parameter value before streaming it back, so nothing prints for a large parameter until Claude has finished generating it」。给工具设 `eager_input_streaming: true`（已稳定，取代 `fine-grained-tool-streaming-2025-05-14` beta 头）后才按生成进度下发，代价是「you might receive partial or invalid JSON」，`max_tokens` 截断会切断参数；官方建议解析失败时把原文包成 `{"INVALID_JSON": "..."}` 作为 `is_error: true` 的 tool_result 还给模型 [A4]。Vercel 的 Anthropic 适配器**默认**给所有函数工具开 `eager_input_streaming`（`toolStreaming` 默认 `true`，[`anthropic-language-model-options.ts` 第 326–334 行][V3]）。
- **含义**：如果「概念阐述」放在提交结果工具的一个字符串参数里，在 Anthropic 上不开 `eager_input_streaming` 时，这个参数会一次性到达，读者看不到逐字流式。

### 3.3 国内兼容端点

| 服务商 | `response_format` | 工具参数是否增量下发 | 强制工具调用 |
| --- | --- | --- | --- |
| DeepSeek | 只有 `text` / `json_object`（「Must be one of `text` or `json_object`」）；JSON 模式下非 JSON 内容会被替换成空格，`content` 可能为空 [C1] | API 参考写「subsequent chunks only carry the function arguments」，即分块下发，但没有明说是按生成进度 [C1] | `strict` 工具是 beta，要用 `https://api.deepseek.com/beta`，思考和非思考模式都支持；思考模式下 `required` 和指定工具返回 400 [C2] |
| Kimi（Moonshot） | `json_object` 和 `json_schema`；`json_schema` 用「Token 级约束解码（CFG）」，前提是 schema 符合 MFJS 规范，`kimi-k3.1-pro`、`kimi-k3` 稳定支持 [C3] | 是：首块带 `id` 和函数名，「only `tool_call.function.arguments` will be output in subsequent chunks」，按 `index` 拼接 [C4] | 文档未说明 `tool_choice` 的取值 [C4] |
| 智谱 GLM | 只列了 `text` / `json_object`（文档正文说「三种」但只列两种，自相矛盾）[C6] | 默认**不**增量；要同时设 `stream: true` 和 `tool_stream: true`，「在不进行缓冲或JSON验证的情况下流式传输工具使用参数」，仅 GLM-4.6 及以后部分型号 [C5][C6] | 「默认 `auto` 且仅支持 `auto`」[C6] |
| 通义（百炼） | `json_object` 和 `json_schema`，`json_schema` 只限部分模型（qwen4-max、Qwen3.7 / 3.8 系列等）；多模态输入会「自动降级为 `json_object`」[C7] | 是：「以数据流的形式分块返回」，函数名在首块，按 `index` 拼接 [C8] | 支持 `auto`、`none`、指定函数；部分模型支持 `required`；思考模式不支持 `required` [C8] |

各家文档都**没有说明 `json_schema` 和 `tools` 同时使用时的行为**（Kimi、通义都写「未提及」），这一点只能实测。

### 3.4 标签流式解析器

bolt.new 的 `StreamingMessageParser`（[`message-parser.ts`][B1]）是最直接的公开实现：

- 按字符扫描：遇到 `<` 时累积可能的标签名，只要还是 `<boltArtifact` 的前缀就**暂不输出**，确定不是标签就原样输出；标签的 `>` 还没到达时设 `earlyBreak`，等下一块再解析。
- 状态按消息 id 保存（`position`、`insideArtifact`、`insideAction`），每次传入完整累积文本，从上次位置继续。
- 进出标签时触发回调（`onArtifactOpen` / `onActionClose` 等），标签外的文字作为说明文字原样渲染，标签内的内容交给 artifact 面板和动作执行器。
- 系统提示词用「ULTRA IMPORTANT: Think first and reply with the artifact that contains all necessary steps」「Do NOT be verbose and DO NOT explain anything unless the user is asking for more information」来压缩标签外的文字（[`prompts.ts` 第 159–161 行][B2]）。
- bolt.new 的开源版**没有工具调用**，所有动作都写在标签里；输出被 `max_tokens` 截断（`finishReason === 'length'`）时追加 `CONTINUE_PROMPT` 续写，最多 2 段（[`api.chat.ts`][B3]，[`constants.ts`][B4]）。

---

## 四、校验与失败处理

### 4.1 各框架是否在循环里校验、怎么重试

| 框架 | 校验位置 | 失败后 | 次数上限 |
| --- | --- | --- | --- |
| Pydantic AI | 输出工具参数和文字输出都在循环里用 Pydantic 校验；可加 `@agent.output_validator`，抛 `ModelRetry` 要求重来 | 把错误作为重试提示发回模型，继续循环 | 输出重试预算**默认 1**，`Agent(retries={'output': N})` 或 `ToolOutput(max_retries=N)` 调整；文字路径用全局预算，用完抛 `UnexpectedModelBehavior('Exceeded maximum output retries (N)')`；工具路径按工具计数 [P1][P3] |
| LangChain `create_agent` + `ToolStrategy` | 结构化输出工具的参数用 schema 校验 | `handle_errors` 默认 `True`：把「`Error: {error}\n Please fix your mistakes.`」作为 ToolMessage 还给模型（[`factory.py` 第 128、650–676 行][L2]）；原始 JSON Schema dict 不校验，`handle_errors` 无效 [L1] | 没有单独的次数上限（源码里没有 `max_retries`），受整体步数限制约束 |
| LangChain `ProviderStrategy` | 服务商原生结构化输出 | 不走 `handle_errors` 的重试分支（`_handle_structured_output_error` 对非 ToolStrategy 直接返回不重试，[`factory.py` 第 650–660 行][L2]） | — |
| Claude Agent SDK | `StructuredOutput` 工具的输入按 JSON Schema（draft-07）校验 | 不符合时 tool_result 带 `is_error: true`，模型再调一次 `StructuredOutput` | 用完返回 `error_max_structured_output_retries`；次数文档没给具体值 [A9] |
| Instructor | 每次响应用 Pydantic 校验 | 带着校验错误重新请求（「Validation Error found … Recall the function correctly, fix the errors」） | `client.create(max_retries=2)` 表示最多 3 次 SDK 调用；和 SDK 自身的网络重试是两回事；可用 `token_budget` 按累计用量停止重试（只限非流式）[I1] |
| OpenAI Agents SDK | `output_type` 非纯文本时，用 schema 校验最后一条 message | **不重试模型**：抛 `ModelBehaviorError`；可注册 `error_handlers={"invalid_final_output": ...}` 返回一个兜底值（同样按 `output_type` 校验），「It does not retry the model call or replay any tool side effects」 | — [O3]，源码 [`turn_resolution.py` 第 1249–1310 行][OA1] |
| Vercel AI SDK | 最后一步解析 `output` | **不重问模型**：抛 `NoObjectGeneratedError`（带 `text`、`usage`、`cause`）[V1]；`repairText` 只是本地修复函数（签名 `({text, error}) => Promise<string \| null>`），且只在已废弃的 `generateObject` / `streamObject` 上 [V7]；`maxRetries` 是网络重试 | — |
| Genkit Go | `jsonHandler.ParseOutput` 解析并按 schema 校验（[`format.go` 第 309–322 行][GK2]） | 返回错误；源码里未见循环内重问（推断） | — |

### 4.2 计费

- 每次重试都是一次完整的模型调用，**输入（整个上下文）和输出都计费**。Instructor 文档专门说明不要用「配置值相乘」估算调用次数，例如「429 → invalid JSON response → 500 → valid JSON response」是 4 次 HTTP 请求、2 次 Instructor 尝试 [I1]。
- Anthropic：拒答时「You'll be billed for the tokens generated」[A1]；`max_tokens` 截断的 JSON 不完整，官方建议提高 `max_tokens` 重试 [A1]。
- Pydantic AI 对截断的处理是**不重试**：Native / Prompted Output 截断成不能解析的 JSON 时抛 `OutputTokenLimitExceeded`，「The model isn't asked to try again, as it would run into the same limit」；输出工具参数被截断时抛 `IncompleteToolCall` [P1]。
- 提示词缓存能降低重试的输入成本，但 Anthropic 改 `tool_choice` 会让缓存的消息块失效（工具定义和系统提示词仍然缓存）[A3]，改 `output_config.format` 让整段对话的缓存失效 [A1]。

### 4.3 已经流式推给用户的内容怎么办

没有任何一个框架提供通用的「撤回已推送内容」机制。各家做法：

| 做法 | 谁 |
| --- | --- |
| **流出即定稿，不再重试** | Pydantic AI `run_stream`：第一个匹配的输出一流出就锁定，`'early'` 语义，工具失败后的重试也不适用 [P1] |
| **让 UI 跳过机制性的调用** | Claude Agent SDK：「If your application shows or logs tool calls, skip the ones named `StructuredOutput`」，同一次查询可能有多次 `StructuredOutput` 调用 [A9] |
| **流式中就地改写** | v0 的「LLM Suspense」：在文本流向用户的过程中改写（例如修正图标导入），「the user never sees an intermediate incorrect state」；流式结束后再跑 autofixer 和 linter [V5] |
| **截断后续写而不是重来** | bolt.new：`finishReason === 'length'` 时追加续写提示，把新流接到旧流后面 [B3] |
| **部分结果只做部分校验，最终再完整校验** | Pydantic AI（`partial_output` 标志）[P1]；Vercel（`partialOutputStream` 只发可解析的部分）[V6] |
| **出错时整体作废** | Genkit：失败时丢掉当前这一轮的输出（包括「a partially streamed model message」），只返回到上一个完整轮次为止的对话 [GK1] |

---

## 五、步数上限收尾

### 5.1 各家默认行为

| 框架 | 上限参数 | 到达上限时 | 有没有「最后再调一次、不给工具」 |
| --- | --- | --- | --- |
| OpenAI Agents SDK | `max_turns`（一次模型调用算一轮） | 抛 `MaxTurnsExceeded`；可注册 `error_handlers={"max_turns": ...}` 返回固定的 `final_output`，并选择是否写进历史 | 没有内置 [O3]，源码 [`run.py` 第 1585–1605 行][OA2] |
| Pydantic AI | `UsageLimits(request_limit=…, tool_calls_limit=…)` | 抛 `UsageLimitExceeded`，「instead of returning a result」 | **官方文档给了做法**：写一个 capability，剩余请求数 ≤ 3 时注入「N model request(s) left in this run: produce your final output now」，在**最后一次请求时把函数工具全部拿掉**（`prepare_tools` 返回 `[]`）[P3] |
| Vercel AI SDK | `stopWhen`（默认 `isStepCount(1)`；`stepCountIs` 是 `isStepCount` 的别名） | 停止；如果最后一步以 `tool-calls` 结束，访问 `output` 抛 `NoOutputGeneratedError` | 没有内置；文档提醒「generating the structured output counts as a step」，要给它留步数 [V1]；`prepareStep` 可以按步改工具和 `toolChoice` [V2] |
| LangChain 旧版 `AgentExecutor` | `max_iterations`、`max_execution_time` | `early_stopping_method="force"`（默认）返回固定字符串「Agent stopped due to iteration limit or time limit.」 | `"generate"`：把中间步骤拼成 scratchpad，加上「I now need to return a final answer based on the previous steps:」再调一次 LLM。**只有已废弃的 `Agent` 类（ReAct 文本解析式）支持**，函数调用式的 `RunnableAgent` 只支持 `force`（[`agent.py` 第 110–135、920–972 行][L3]） |
| LangChain v1 `create_agent` | `ModelCallLimitMiddleware(thread_limit, run_limit, exit_behavior)` | `exit_behavior="end"`（默认）注入一条人造 AI 消息说明超限并结束；`"error"` 抛 `ModelCallLimitExceededError`（[`model_call_limit.py` 第 131–146 行][L4]） | 没有内置 |
| LangGraph | `recursion_limit`（按超步计） | 抛 `GraphRecursionError`；默认值现在是 10007（环境变量 `LANGGRAPH_DEFAULT_RECURSION_LIMIT`，[`_config.py` 第 42–44 行][LG1]） | 旧的预构建 `create_react_agent`（已标 deprecated）用 `RemainingSteps`：剩余步数 < 2 且还有工具调用时，返回「Sorry, need more steps to process this request.」，不抛错（[`chat_agent_executor.py` 第 435–440 行][LG2]） |
| Claude Agent SDK / Claude Code | `max_turns`（每个带工具的轮次算一轮，最终纯文字回答再算一轮）、`max_budget_usd` | 返回 `ResultMessage`，subtype 为 `error_max_turns` / `error_max_budget_usd`，**没有 `result` 文本**；单次 `query()` 会在给出结果后抛错；会话可以恢复后提高上限继续 [A8] | 没有 |
| Genkit Go | `WithMaxTurns`（默认 50） | 返回 `ErrMaxTurnsExceeded`，附带一个「partial」响应：截到最后一个完整轮次的对话，结束原因 `aborted`，「output parsing is skipped」（[`generate.go` 第 405–428、555–560、836–838 行][GK1]） | 没有 |
| bolt.new | 没有工具循环；只限续写段数 `MAX_RESPONSE_SEGMENTS = 2` | 超过后不再续写 | — [B3][B4] |

### 5.2 归纳

- **默认都是报错或停止**。OpenAI Agents SDK、Pydantic AI、Claude Agent SDK、Genkit、LangGraph 都是报错；LangChain v1 和旧 LangGraph 预构建是注入一条固定文字。
- **「再调一次、不给工具」只有两处**：LangChain 已废弃的 `early_stopping_method="generate"`，以及 Pydantic AI 文档里的「Wrapping up before the request limit」示例。后者的做法是**在上限前提前告知、最后一次请求去掉工具**，而不是超限后额外多调一次。
- **去掉工具的方式会影响缓存和兼容性**：Anthropic 支持 `tool_choice: none`，但改 `tool_choice` 会让缓存的消息块失效 [A3]；直接从请求里删掉工具会改变缓存前缀（Pydantic AI 在 Anthropic 文档里也提到过滤工具列表「invalidates Anthropic's prompt cache since the cached prefix includes the tool array」[P2]）。智谱只支持 `tool_choice: auto` [C6]，只能删工具。
- **还有一种情况容易漏**：历史消息里留着未回答的工具调用时，服务商不接受回传。Genkit 因此在超限时把「the tool requests [WithMaxTurns] refused to run」一并丢掉 [GK1]。Keine 若在超限时再调一次，要先给最后一轮的工具调用补上结果，或者整轮丢掉。

---

## 六、OpenAI 和 Anthropic 自己的做法

### 6.1 API 层的官方推荐

**OpenAI**

| 项目 | 内容 | 类型 |
| --- | --- | --- |
| 工具和结构化回答怎么选 | 「If you are connecting the model to tools, functions, data, etc. in your system, then you should use function calling. If you want to structure the model's output when it responds to the user, then you should use a structured `text.format`」[O1] | 官方明确 |
| Structured Outputs vs JSON mode | Structured Outputs 是「the evolution of JSON mode」，「only Structured Outputs ensure schema adherence」，能用就用 [O1] | 官方明确 |
| strict 函数调用 | 「We recommend always enabling strict mode」；要求 `additionalProperties: false`、所有字段 required [O2] | 官方明确 |
| 流式 | 推荐用 SDK 处理结构化输出的流式；文字增量和 `output_text.done` 不保证完成，要看最终状态 [O1]；函数参数通过 `response.function_call_arguments.delta` 增量下发 [O2] | 官方明确 |
| 区分中间说明和最终答案 | Responses API 的 `phase`：`commentary` / `final_answer`，GPT-5.4 / 5.5 的长工具流程里推荐带上，丢掉会导致「preambles to be treated as final answers」[O4] | 官方明确（仅 Responses API） |
| Agents SDK 的 `output_type` | 实现上：`AgentOutputSchema` 默认 `strict_json_schema=True`，把 schema 转成严格模式；Responses 模型把它作为 `text.format`（`type: json_schema`）发给服务商，**每一轮都带着**，同时带工具；最终输出取「无工具调用」那一步最后一条 message 的文字再校验（`agent_output.py`；`openai_responses.py` 第 995–1073、2110–2118 行；`turn_resolution.py` 第 1202–1310 行）[OA1] | 源码 |
| Deep Research 的报告 | API 输出由若干 `web_search_call` 项和一条带 `url_citation` 注释的 `message` 组成；报告格式（章节、图表、引用）在示例里只靠提示词指定，没有提到结构化输出；长报告推荐后台模式 [O7] | 官方明确（格式只靠提示词这一点来自示例，属推断） |

**Anthropic**

| 项目 | 内容 | 类型 |
| --- | --- | --- |
| structured outputs | `output_config.format`（JSON 输出）和 `strict: true`（工具）两项，已稳定，所有模型可用；受限解码；「Reliable: No retries needed for schema violations」；与 citations、prefill 不兼容；语法只作用于模型的直接输出，「not to tool use calls, tool results, or thinking tags」[A1] | 官方明确 |
| 用工具强制 JSON | 旧的通行做法是「定义一个工具 + `tool_choice` 强制」。现在文档的态度是：在支持强制的模型上「combine `tool_choice: {"type": "any"}` with strict tool use」；在不支持强制的新模型上改用 `auto` + strict 工具，或 structured outputs [A3] | 官方明确 |
| prefill | Claude 4.6 及之后不再支持，返回 400；格式控制迁移到 structured outputs，去开场白改用系统提示词、XML 标签、structured outputs 或工具调用，「If the occasional preamble slips through, strip it in post-processing」[A6][A7] | 官方明确 |
| XML 标签与格式引导 | 「Tell Claude what to do instead of what not to do」「Use XML format indicators」（例：把正文写在 `<smoothly_flowing_prose_paragraphs>` 标签里）「Match your prompt style to the output you want」[A6] | 官方明确 |
| 工具调用之间的文字 | 「When using tools, Claude often comments on what it's doing or responds naturally to the user before calling tools」，代码不应依赖它的格式 [A3]；提示词指南写到 Claude Fable 5.1 在 agent 工作中「writes fewer user-facing updates between tool calls」，需要的话要明确要求 [A6] | 官方明确 |
| 细粒度工具流式 | `eager_input_streaming: true`，已稳定；不缓冲、不校验；无效 JSON 的回传格式 [A4] | 官方明确 |
| Claude Agent SDK structured output | `output_format: {type: "json_schema", schema}`；**实现是一个注入的 `StructuredOutput` 工具**：校验失败时 tool_result 带 `is_error: true`，模型重调；用完报 `error_max_structured_output_retries`；结果在 `ResultMessage.structured_output` [A9] | 官方明确 |

### 6.2 他们自己产品里的做法

| 产品 | 公开资料能确认的部分 | 类型 |
| --- | --- | --- |
| **Claude Code 的 artifact** | 「Claude writes the page to an HTML or Markdown file in a temporary directory outside your project, then publishes it」；更新时「Claude edits the underlying file and publishes again to the same URL」[A10]。也就是说，**文档内容走文件和发布工具**，不在聊天文字里；聊天文字只是说明 | 官方明确 |
| **Claude.ai 的 artifact** | 帮助中心把 artifact 描述为在对话旁边打开的独立产出物（文档、幻灯片、设计、仪表盘等），可以模板创建、导出；2026-09-16 之前创建的属于「Legacy artifacts」，不能再新建 [A11]。**怎么在流式中识别 artifact，官方没有公开**。2024 年版本据流传的系统提示词是用 `<antArtifact>` 标签包裹，这属于**非官方来源，可信度低**，这里只作参考 | 帮助中心为官方；实现细节非官方 |
| **Claude Code 的 max_turns** | 达到上限返回 `error_max_turns`，没有最终文本，可恢复会话后继续 [A8] | 官方明确 |
| **ChatGPT canvas** | 官方发布文章：通过后训练让 GPT-4o 学会「何时打开 canvas」「何时做定点修改、何时整体重写」，触发准确率写作 83%、代码 94%；用 o1-preview 蒸馏合成数据 [O6]。工具名 `canmore` 等实现细节只见于社区流传的提示词，**非官方** | 文章为官方；工具形态属推断 |
| **ChatGPT deep research** | API 层见上表；产品里报告的结构怎么约束，没有公开资料 | — |

小结（推断）：两家自己的产品都把「要交付的文档」放在**工具调用或独立的输出通道**里，聊天文字只做说明；两家的 Agent SDK 到上限都是报错，不做强制收尾。

---

## 七、对 Keine 的可选方案及各自代价

以下按「在哪里放三段结构」分。每个方案都要同时回答：中间文字怎么处理、怎么流式、怎么校验、步数上限怎么收尾。

### 方案 A：保留「Markdown 约定加提示词」，按「无工具调用的那一步」判定最终答案

- **做法**：文字一律当候选；步结束时 `finish_reason` / `stop_reason` 显示还有工具调用，就把这一步的文字改判为中间文字；最后一步的文字做结构校验（解析 Markdown 标题层级）。
- **流式**：两种选择。（a）每步先缓冲到结束再推 `delta`，读者在最后一步之前看不到正文，最后一步也要等它结束才能看到——实际上失去流式。（b）先推出去，步结束后撤回或降级为中间说明。ADR 0008 的四种事件里没有「撤回」，要么给 `delta` 加「属于哪一步」的字段、在 `activity` 或新事件里宣布改判，要么改 ADR。
- **校验失败**：可以带着错误再请求一次（Pydantic AI 默认重试 1 次），但已经流出去的正文要处理。
- **代价**：改动最小，所有服务商都能用；但流式和「不混入中间文字」之间必须二选一或扩展事件协议；格式只能事后校验。

### 方案 B：XML 标签包裹（如 `<analysis><title>…</title>…</analysis>`），流式标签解析器切分

- **做法**：参照 bolt.new 的 `StreamingMessageParser`，标签外的文字一律算中间说明（可作为 `activity` 或丢弃），标签内的内容按段推 `delta`。
- **流式**：逐字流式，标签一出现就能确定它是正文，不必等步结束。
- **校验失败**：标签缺失、段落缺失可以确定性地发现；重试同样面临已推内容的问题，但因为能按段识别，可以只补缺的段。
- **代价**：要自己写和测 Go 版流式解析器（处理跨块的半个标签）；模型漏写标签时要兜底（例如整段当作正文或报错）；Anthropic 官方推荐这种写法 [A6]，国内模型的遵守程度要实测。和工具调用、服务商自带搜索都兼容，不依赖任何服务商特性。

### 方案 C：「提交分析」工具（title、intro、body 三个字段），答案只从工具参数取

- **做法**：模型调用 `submit_analysis` 就结束循环；其他文字一律是中间说明。
- **流式**：要按增量解析工具参数里的 JSON 字符串，再转成 `delta`。Anthropic 必须开 `eager_input_streaming`，否则 `body` 一次性到达 [A4]；智谱必须开 `tool_stream` 且只限 GLM-4.6 以后 [C5]；DeepSeek 文档没明说是否按生成进度下发 [C1]。Markdown 写在 JSON 字符串里要转义，增量解析时要处理半个转义序列。
- **校验失败**：最成熟的路径。参数校验失败就把错误作为 `is_error` 的 tool_result 还回去（Claude Agent SDK、LangChain 的做法）。
- **代价**：**不能强制调用**——Opus 5.5、Sonnet 5.5、Fable 5.x 不支持 [A3]，智谱只支持 `auto` [C6]，DeepSeek / 通义思考模式不支持 `required` [C2][C8]，所以必须处理「模型写了一段文字就结束、没调工具」（Pydantic AI 的做法是再提示一次 [P2]）。strict 工具在 DeepSeek 上是 beta。

### 方案 D：服务商原生 JSON Schema 输出

- **代价**：DeepSeek、智谱没有 `json_schema` [C1][C6]，国内只有 Kimi 和部分通义模型有；Anthropic 和 citations 不兼容、和 prefill 不兼容 [A1]，与服务商自带搜索的引用能否共存要实测；改格式会让缓存失效 [A1]；各家都没说明与工具同时用时中间文字怎么受约束。**作为跨服务商的基线不成立**，只能作为个别适配器的增强。

### 方案 E：两段式——工具循环只做研究，最后单独一次「不给工具」的调用来写

- **做法**：循环里的文字都不展示为正文（只推 `activity`）；模型停止调用工具（或接近步数上限）时，再发一次去掉工具或 `tool_choice: none` 的请求，专门写三段结构，这一次逐字流式。
- **好处**：「最终答案」的判定变成确定的（就是这次调用）；步数上限收尾和正常收尾是同一条路径（类似 Pydantic AI 的 wrap-up 示例 [P3]）；可以和 A / B 组合（这次调用里再用标签或 Markdown 约定）。
- **代价**：多一次调用（输入可以命中缓存，但 Anthropic 改 `tool_choice` 让缓存的消息块失效 [A3]，删工具会改缓存前缀 [P2]；智谱只能删工具 [C6]）；如果模型在循环最后一步已经写出了完整答案，这次调用就是重复花费，需要决定是接受上一步的文字还是一律重写；最后一轮如果留有未回答的工具调用，要先补结果或丢弃 [GK1]。

### 步数上限收尾的可选项（与上面任一方案组合）

| 选项 | 先例 | 代价 |
| --- | --- | --- |
| 直接报错（`error` 事件） | OpenAI Agents SDK、Pydantic AI、Claude Agent SDK、Genkit、LangGraph | 读者拿不到任何分析，已花的用量照算 |
| 返回固定文字 | LangChain `force` / `ModelCallLimitMiddleware(exit_behavior="end")`、LangGraph 预构建 | 简单，但不是三段结构，要在界面上单独呈现 |
| 接近上限时提示、最后一次去掉工具 | Pydantic AI 文档示例 [P3] | 要在循环里知道「还剩几步」；去工具的缓存代价同方案 E |
| 超限后再调一次不带工具的模型 | LangChain 已废弃的 `generate` [L3] | 比上限多一次调用；要处理未回答的工具调用 |

---

## 引用

### OpenAI

- [O1] Structured model outputs：https://developers.openai.com/api/docs/guides/structured-outputs
- [O2] Function calling：https://developers.openai.com/api/docs/guides/function-calling
- [O3] OpenAI Agents SDK 文档 — Running agents（final output 判定、`max_turns`、`error_handlers`）：https://github.com/openai/openai-agents-python/blob/91fb508a8931016efa793b033147d08cd20308cf/docs/running_agents.md
- [O4] Reasoning models（`phase` 字段）：https://developers.openai.com/api/docs/guides/reasoning
- [O6] Introducing canvas（2024-10）：https://openai.com/index/introducing-canvas/
- [O7] Deep research / web search（Responses API）：https://developers.openai.com/api/docs/guides/deep-research

### Anthropic

- [A1] Structured outputs：https://platform.claude.com/docs/en/build-with-claude/structured-outputs
- [A2] Strict tool use：https://platform.claude.com/docs/en/agents-and-tools/tool-use/strict-tool-use
- [A3] Define tools（强制工具调用的限制、工具前的说明文字）：https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools
- [A4] Fine-grained tool streaming：https://platform.claude.com/docs/en/agents-and-tools/tool-use/fine-grained-tool-streaming
- [A6] Prompting best practices（格式引导、XML 标签、prefill 迁移）：https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices
- [A7] Claude Opus 5.5 migration guide（prefill 返回 400）：https://platform.claude.com/docs/en/models/opus-5-5/migration-guide
- [A8] Claude Agent SDK — How the agent loop works（turn 定义、`error_max_turns`）：https://code.claude.com/docs/en/agent-sdk/agent-loop
- [A9] Claude Agent SDK — Get structured output from agents：https://code.claude.com/docs/en/agent-sdk/structured-outputs
- [A10] Claude Code — Share session output as artifacts：https://code.claude.com/docs/en/artifacts
- [A11] What are artifacts and how do I use them?：https://support.claude.com/en/articles/17153992-what-are-artifacts-and-how-do-i-use-them

### 国内服务商

- [C1] DeepSeek — JSON Output：https://api-docs.deepseek.com/guides/json_mode ；Chat Completions API 参考：https://api-docs.deepseek.com/api/create-chat-completion/
- [C2] DeepSeek — Tool Calls（strict beta）：https://api-docs.deepseek.com/guides/tool_calls/
- [C3] Kimi — 使用 response_format 控制模型输出格式：https://platform.kimi.com/docs/guide/response_format
- [C4] Kimi — Use Kimi API for Tool Calls：https://platform.kimi.ai/docs/guide/use-kimi-api-to-complete-tool-calls
- [C5] 智谱 — 工具流式输出：https://docs.bigmodel.cn/cn/guide/capabilities/stream-tool
- [C6] 智谱 — 对话补全 API 参考：https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8
- [C7] 阿里云百炼 — 结构化输出：https://help.aliyun.com/zh/model-studio/qwen-structured-output
- [C8] 阿里云百炼 — Function Calling：https://help.aliyun.com/zh/model-studio/qwen-function-calling

### 框架和应用

- [P1] Pydantic AI — Output（输出模式、`end_strategy`、流式、截断、输出校验器）：https://github.com/pydantic/pydantic-ai/blob/0473c164a4d144243d621a85b16670fbe7652054/docs/output.md
- [P2] Pydantic AI — Anthropic（强制工具调用与输出模式）：https://github.com/pydantic/pydantic-ai/blob/0473c164a4d144243d621a85b16670fbe7652054/docs/models/anthropic.md
- [P3] Pydantic AI — Agents（Usage Limits、Wrapping up before the request limit、How output retries are enforced）：https://github.com/pydantic/pydantic-ai/blob/0473c164a4d144243d621a85b16670fbe7652054/docs/agent.md
- [V1] Vercel AI SDK — Generating Structured Data：https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/content/docs/03-ai-sdk-core/10-generating-structured-data.mdx
- [V5] Vercel — How we made v0 an effective coding agent：https://vercel.com/blog/how-we-made-v0-an-effective-coding-agent ；Introducing the v0 composite model family：https://vercel.com/blog/v0-composite-model-family
- [L1] LangChain — `structured_output.py`（`ToolStrategy`、`ProviderStrategy`）：https://github.com/langchain-ai/langchain/blob/204968fbbddad5b6c663f311593990e399ed48cc/libs/langchain_v1/langchain/agents/structured_output.py
- [I1] Instructor — Retry Logic：https://github.com/567-labs/instructor/blob/0acb3a19c3ec6f89c6a204340c52a30ee33fa04b/docs/concepts/retrying.md
- [I2] Instructor — Streaming Partial Responses：https://github.com/567-labs/instructor/blob/0acb3a19c3ec6f89c6a204340c52a30ee33fa04b/docs/concepts/partial.md

### 源码行引用

[OA1]: https://github.com/openai/openai-agents-python/blob/91fb508a8931016efa793b033147d08cd20308cf/src/agents/run_internal/turn_resolution.py#L1202-L1310
[OA2]: https://github.com/openai/openai-agents-python/blob/91fb508a8931016efa793b033147d08cd20308cf/src/agents/run.py#L1585-L1605
[V2]: https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/packages/ai/src/generate-text/generate-text.ts#L1893-L1913
[V3]: https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/packages/anthropic/src/anthropic-language-model.ts#L610-L660
[V4]: https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/packages/openai/src/responses/openai-responses-language-model.ts#L1745
[V6]: https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/packages/ai/src/generate-text/stream-text.ts#L1085-L1249
[V7]: https://github.com/vercel/ai/blob/a8f0e9dc55d73f2237759abd263da6ae674de523/packages/ai/src/generate-object/repair-text.ts
[L2]: https://github.com/langchain-ai/langchain/blob/204968fbbddad5b6c663f311593990e399ed48cc/libs/langchain_v1/langchain/agents/factory.py#L650-L676
[L3]: https://github.com/langchain-ai/langchain/blob/204968fbbddad5b6c663f311593990e399ed48cc/libs/langchain/langchain_classic/agents/agent.py#L920-L972
[L4]: https://github.com/langchain-ai/langchain/blob/204968fbbddad5b6c663f311593990e399ed48cc/libs/langchain_v1/langchain/agents/middleware/model_call_limit.py#L131-L146
[LG1]: https://github.com/langchain-ai/langgraph/blob/58a6e1cc5b7dfc3619fc6e7e7573b040d554649b/libs/langgraph/langgraph/_internal/_config.py#L33-L44
[LG2]: https://github.com/langchain-ai/langgraph/blob/58a6e1cc5b7dfc3619fc6e7e7573b040d554649b/libs/prebuilt/langgraph/prebuilt/chat_agent_executor.py#L435-L440
[GK1]: https://github.com/genkit-ai/genkit/blob/9a7d0b65f8bddeafbbc16678b75a4e36fde42305/go/ai/generate.go#L405-L428
[GK2]: https://github.com/genkit-ai/genkit/blob/9a7d0b65f8bddeafbbc16678b75a4e36fde42305/go/ai/format.go#L309-L322
[B1]: https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/lib/runtime/message-parser.ts
[B2]: https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/lib/.server/llm/prompts.ts#L159-L161
[B3]: https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/routes/api.chat.ts
[B4]: https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/lib/.server/llm/constants.ts

- [OA1] OpenAI Agents SDK `turn_resolution.py`：见上方链接；另 `agent_output.py`（`strict_json_schema=True` 默认）、`models/openai_responses.py` 第 995–1073、2110–2118 行（`text.format`）、`models/chatcmpl_converter.py` 第 309 行（`phase`），同一提交。
- [OA2] OpenAI Agents SDK `run.py`（`MaxTurnsExceeded` 与 `max_turns` 处理器）。
- [V2] Vercel `generate-text.ts`（最终输出解析条件）；`stop-condition.ts`（`isStepCount`、`hasToolCall`）同一目录。
- [V3] Vercel Anthropic 适配器（`structuredOutputMode`、`json` 工具、拒绝强制工具时改用原生格式）；`anthropic-language-model-options.ts` 第 168–178、326–334 行（`toolStreaming` 默认 `true`）。
- [V4] Vercel OpenAI Responses 适配器（流式读取 `phase`）。
- [V6] Vercel `stream-text.ts`（每步重置部分输出、只发可解析的部分 JSON）。
- [V7] Vercel `repair-text.ts`（`RepairTextFunction`）；`generate-object.ts` 第 121 行标注 `@deprecated Use generateText with an output setting instead`。
- [L2] LangChain `factory.py`（`STRUCTURED_OUTPUT_ERROR_TEMPLATE` 第 128 行，`_handle_structured_output_error`，`tool_choice = "any"` 第 1477–1478 行）。
- [L3] LangChain 旧版 `agent.py`（`early_stopping_method` 的 `force` / `generate`；`BaseSingleActionAgent.return_stopped_response` 第 110–135 行只支持 `force`）。
- [L4] LangChain `ModelCallLimitMiddleware`。
- [LG1] LangGraph 默认递归上限；[LG2] 预构建 `create_react_agent` 的 `remaining_steps`。
- [GK1] Genkit Go `generate.go`（`failurePartial` 第 405–428 行、默认 50 轮第 555–560 行、超限判断第 836–838 行）；[GK2] `format.go`（JSON 解析与校验）。
- [B1]–[B4] bolt.new 开源版的流式标签解析器、系统提示词、续写逻辑、常量。
