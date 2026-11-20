# 自带 API 的应用如何处理联网搜索

调研问题见 [#109](https://github.com/Bank5y/Keine/issues/109)。只记事实，不给 Keine 的建议。

调研日期 2026-11-20。源码结论都来自下列提交的浅克隆（链接是固定到该提交的永久链接）：

| 应用 | 仓库 | 提交 |
| --- | --- | --- |
| Open WebUI | open-webui/open-webui | [`0666e59`](https://github.com/open-webui/open-webui/tree/0666e59d26a4d056e0bacee41742486badd71cf8)（CHANGELOG 最新 0.13.4，2026-11-17） |
| LibreChat | danny-avila/LibreChat | [`4700c44`](https://github.com/danny-avila/LibreChat/tree/4700c44eb77c142b4f9f8b87476924f22890eb39) |
| LobeChat / LobeHub | lobehub/lobe-chat | [`5d4975b`](https://github.com/lobehub/lobe-chat/tree/5d4975b805136d9c952089aa650d9ac759bafd55) |
| NextChat | ChatGPTNextWeb/NextChat | [`1d3dc33`](https://github.com/ChatGPTNextWeb/NextChat/tree/1d3dc331512929a4b1d3e82354e72acce33b5c7a) |
| Cherry Studio | CherryHQ/cherry-studio | [`3110175`](https://github.com/CherryHQ/cherry-studio/tree/3110175b8025598bb4460d832b3d1221bb611726) |
| Chatbox（社区版） | chatboxai/chatbox | [`446bcf4`](https://github.com/chatboxai/chatbox/tree/446bcf43e7a89a0411218821de54d87d7f3cfe11) |

标记约定：**[未核实]** 表示没在一手来源里确认；“未找到”表示在上述源码里搜过、没看到，不等于确定不存在。

## 结论速览

| 应用 | 服务商自带搜索 | 应用提供的搜索工具 | 搜索 key 谁提供 | 多用户：自带模型 key 能否用管理员的搜索 | 搜索的计量/限流/开关 | 模型不支持工具调用时 |
| --- | --- | --- | --- | --- | --- | --- |
| Open WebUI | 只能通过模型自定义参数 `tools` 透传 | 有，约 30 种引擎 | 管理员全局配置（环境变量或后台） | 能。搜索配置全局一份，与模型从哪来无关 | 有按用户/组的 `features.web_search` 权限；有并发上限；未找到按用户计量 | legacy 模式：先生成查询、搜索、把结果作为 RAG 上下文注入 |
| LibreChat | 有：OpenAI、Anthropic、Google 端点的 `web_search` 参数 | 有（`webSearch`：搜索 + 抓取 + 重排） | 按类别分：管理员在服务端配了就用服务端的，留空或填 `user_provided` 就由用户自填 | 能。搜索 key 与模型 key 分开解析 | 有 `WEB_SEARCH` 角色权限；未找到对搜索的计量（balance 只记 token） | 未找到专门的非工具调用路径 |
| LobeChat 服务端版 | 有（“Provider Search”/模型内置搜索） | 有（`SEARCH_PROVIDERS` + `CRAWLER_IMPLS`） | 部署者用环境变量配置；用户只能给已启用的渠道排序 | 能。搜索走服务端，用的是部署者的 key | 未找到按用户的计量或限流；受限 API key 需 `model:invoke` 作用域 | 设置里有“搜索辅助模型”（另选一个支持工具调用的模型），运行时如何使用 **[未核实]** |
| NextChat | 未找到 | 只有插件（OpenAPI，内置 DuckDuckGoLite 等）和 MCP | 插件自带认证，用户在客户端配置 | 不适用（无服务端搜索配置） | 无 | 插件只对 OpenAI、Azure、Moonshot、ChatGLM 等服务商显示 |
| Cherry Studio（桌面） | 有（server 路由，按服务商/模型/端点判定） | 有，约 20 种（DuckDuckGo、SearXNG、Exa MCP 等免 key） | 用户自填（单机应用） | 不适用 | 不适用 | 只能走服务商自带搜索；客户端搜索工具要求函数调用。已移除基于提示词的工具调用 |
| Chatbox（社区版） | 未找到主动调用服务商搜索工具的代码 | 有：Chatbox AI（需许可证）、Bing（抓取网页、免 key）、Tavily、BoCha、Querit、DeepSeek、SearXNG | 用户自填；或用 Chatbox AI 许可证走官方通道 | 不适用（客户端应用） | 不适用 | 提示词工程：先让模型输出 `{action, query}` JSON，应用执行搜索，再把结果拼进消息 |

## 1. 服务商自带搜索 vs 应用的搜索工具

### 服务商一侧（都按调用计费，费用记在模型 API key 所属账户）

- **Anthropic**：Messages API 的 `tools` 里加 `web_search`（`web_search_20250305` / `_20260209` / `_20260318`）。“web search costs **$10 USD per 1,000 searches**, plus standard token costs for search-generated content”；出错的搜索不计费；组织管理员可以在 Console 里关掉，关掉后请求返回 400。来源：[Web search tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool)。
- **OpenAI**：Responses API 用 `{ "type": "web_search" }`（旧的 `web_search_preview` 仍可用）；Chat Completions 只能用专门的搜索模型（如 `gpt-5-search-api`），这类模型每次都先搜再答。“Search actions incur a tool call cost”。来源：[Web search guide](https://developers.openai.com/api/docs/guides/tools-web-search)。价格 $10/1k 次，搜索内容 token 按模型价计（preview 用于非推理模型时 $25/1k、内容 token 免费）。来源：[Pricing](https://developers.openai.com/api/docs/pricing)。
- **通义千问（百炼）**：请求里设 `enable_search: true`，默认由模型自己决定搜不搜，`forced_search` 可以强制搜索。按策略收费：turbo 3 元/千次、max 4 元/千次、agent 4 元/千次（北京等地域）；网页内容拼进提示词，按输入 token 计费；没有免费额度。来源：[联网搜索](https://help.aliyun.com/zh/model-studio/web-search)。
- **智谱**：Chat Completions 的 `tools` 里加 `{"type": "web_search"}`；另有独立的 Web Search API，search_std 0.01 元/次、search_pro 0.03 元/次、sogou/quark 0.05 元/次。工具形式是否另外收费，页面没写 **[未核实]**。来源：[智谱 Web Search](https://docs.bigmodel.cn/cn/guide/tools/web-search)。
- **Kimi（月之暗面）**：当前文档提供独立的 `/v1/tools/search`、`/v1/tools/search_pro`、`/v1/tools/fetch` 接口，以及 Responses API 的 `web_search`、Formula API 的官方工具 `web-search`，都按次计费（成功且有结果才计费），模型 token 另算。具体单价在定价页上没有抓到 **[未核实]**；旧文档里的 `$web_search` builtin_function 在当前文档里没找到 **[未核实]**。来源：[use-web-search](https://platform.kimi.com/docs/guide/use-web-search)、[use-official-tools](https://platform.kimi.com/docs/guide/use-official-tools.md)、[websearch 定价](https://platform.kimi.com/docs/pricing/websearch.md)。
- **DeepSeek**：按 LobeChat 文档的说法，DeepSeek 提供独立的搜索/抓取 API，“按成功请求次数单独计费，与模型 token 费用分开”，没单独设 key 时可以复用模型 key。来源是第三方转述（[LobeChat online-search 文档](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/docs/self-hosting/advanced/online-search.zh-CN.mdx)），**[未核实 DeepSeek 官方文档]**。Cherry Studio 和 Chatbox 也都把 DeepSeek 列为搜索提供方。

### 各应用怎么选

- **Open WebUI**：应用工具是主路径。原生函数调用模式下（`function_calling` 不是 `legacy`），只要全局开了 web search、模型开了 `web_search` 能力、用户有权限，就给模型注入内置工具 `search_web` / `fetch_url`（[utils/tools.py#L864-L871](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/utils/tools.py#L864-L871)）。服务商自带搜索只能靠模型自定义参数 `tools` 透传，CHANGELOG 写着“A model with a custom parameter named tools, such as one that turns on a provider's native web search, now keeps its built-in tools”（[CHANGELOG#L662](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/CHANGELOG.md#L662)）。引擎列表见 [retrieval/web/](https://github.com/open-webui/open-webui/tree/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/retrieval/web)（SearXNG、Tavily、Exa、Jina、Brave、Google PSE、Bing、Bocha、Sougou、DuckDuckGo、Perplexity、Ollama Cloud、external 等）。
- **LibreChat**：两者都支持，各自独立。服务商搜索是端点参数 `web_search`：OpenAI 端点追加 `{type: 'web_search'}`（[openai/llm.ts#L1019](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/packages/api/src/endpoints/openai/llm.ts#L1019)），Anthropic 端点追加 `web_search_20250305`（[anthropic/llm.ts#L433](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/packages/api/src/endpoints/anthropic/llm.ts#L433)），Google 同理；管理员可以用 `dropParams` 去掉它。应用搜索是 `librechat.yaml` 的 `webSearch`，三类服务分开配：搜索（Serper、SearXNG、Tavily、Keenable）、抓取（Firecrawl、Tavily、Keenable）、重排（Jina、Cohere）（[librechat.example.yaml#L1635](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/librechat.example.yaml#L1635)）。
- **LobeChat**：同一时刻只走一条路，由 `resolveSearchDecision` 决定（[model-bank/src/utils.ts#L26-L49](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/packages/model-bank/src/utils.ts#L26-L49)）。模型或服务商的搜索实现是 `internal`（如 Perplexity、Jina）时总走模型搜索；是 `params`（OpenAI、Anthropic、Google、Qwen、Hunyuan 等）时，只有用户打开 `useModelBuiltinSearch` 才走模型搜索，否则用应用的搜索工具。用户文档把“Provider Search”标成“界面提示开启后可能出现非预期行为，不推荐使用”（[web-search.zh-CN.mdx](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/docs/usage/agent/web-search.zh-CN.mdx)）。
- **Cherry Studio**：搜索和抓取各自有 `client`（应用工具）/`server`（服务商自带）/`none` 三种路由。用户的 `modelToolsPreferred` 决定哪一侧优先，优先的一侧不可用时退到另一侧（[shared/utils/provider.ts#L352-L400](https://github.com/CherryHQ/cherry-studio/blob/3110175b8025598bb4460d832b3d1221bb611726/src/shared/utils/provider.ts#L352-L400)）。server 一侧能不能用，查的是 provider-registry 里的服务商 × 模型 × 端点声明。
- **Chatbox**：只有应用一侧的搜索提供方（[web-search/index.ts](https://github.com/chatboxai/chatbox/blob/446bcf43e7a89a0411218821de54d87d7f3cfe11/src/renderer/packages/web-search/index.ts)）。`web_search` 只是模型能力标签（如 Perplexity sonar 系列；OpenRouter 模型定价里 `web_search` 非 0 时也打上这个标签），没找到主动调用 OpenAI/Anthropic 搜索工具的代码。
- **NextChat**：没有内置搜索。默认插件清单是 Dalle3、ArxivSearch、DuckDuckGoLiteSearch，都是 OpenAPI 插件（[public/plugins.json](https://github.com/ChatGPTNextWeb/NextChat/blob/1d3dc331512929a4b1d3e82354e72acce33b5c7a/public/plugins.json)）；另外支持 MCP（`ENABLE_MCP`，开启时必须同时设 `CODE` 访问密码）。

## 2. 搜索 key 谁提供、费用谁承担

- **单机/客户端应用（Cherry Studio、Chatbox、NextChat）**：用户在本机自填 key，费用自然由用户承担。都有免 key 的选项：
  - Cherry Studio：`duckduckgo`、`fetch` 不需要 key；`searxng` 不需要 key（需要实例地址）；`exa-mcp`、`firecrawl` 的 key 可选。`exa-mcp` 是“搜索关键词”能力的兜底提供方（[webSearchProviders.ts](https://github.com/CherryHQ/cherry-studio/blob/3110175b8025598bb4460d832b3d1221bb611726/src/shared/data/presets/webSearchProviders.ts)、[web-search/src/catalog.ts](https://github.com/CherryHQ/cherry-studio/blob/3110175b8025598bb4460d832b3d1221bb611726/packages/web-search/src/catalog.ts)）。
  - Chatbox：默认提供方是 `build-in`（Chatbox AI 搜索），没有许可证会报 `chatbox_search_license_key_required`，即由 Chatbox 官方通道提供、包含在付费许可里（许可证价格和搜索额度 **[未核实]**）。`bing` 直接抓 `https://www.bing.com/search` 的 HTML，免 key（[bing.ts](https://github.com/chatboxai/chatbox/blob/446bcf43e7a89a0411218821de54d87d7f3cfe11/src/renderer/packages/web-search/bing.ts)）；代码注释说移动端原生 HTTP 抓 Bing“unreliable”。
- **Open WebUI**：只有管理员能配。引擎和 key 是全局配置（环境变量如 `WEB_SEARCH_ENGINE`、`TAVILY_API_KEY`，或后台 Admin Settings > Web Search）（[config.py#L1164-L1338](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/config.py#L1164)），没有用户级的搜索 key，费用由部署者承担。完整镜像里的 DDGS 不需要 key，slim 镜像不带它（CHANGELOG#L1363）。
- **LibreChat**：唯一一个可以逐项选择的。对每个字段，服务端环境变量有值就用服务端的（部署者付费）；为空或者等于 `user_provided`，就由用户在对话框里自填，key 存在用户账户下，“only used for your searches”（[web.ts#L486-L515](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/packages/api/src/web/web.ts#L486-L515)、[Web Search 文档](https://www.librechat.ai/docs/features/web_search)）。Keenable 不需要 key，有 key 只是放宽限流。
- **LobeChat 服务端版**：部署者用 `SEARCH_PROVIDERS`、`CRAWLER_IMPLS` 和各家 key 的环境变量配置（[online-search.zh-CN.mdx](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/docs/self-hosting/advanced/online-search.zh-CN.mdx)），没配时默认是 SearXNG。用户设置里只有 `searchProviders` / `crawlerImpls` 两个有序列表，而且“Ids must be a subset of the server-enabled channels”（[types/user/settings/tool.ts](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/packages/types/src/user/settings/tool.ts)），也就是只能排序、不能填自己的 key。路由代码注释明确写着这个接口“spends server-side search quota”（[routers/tools/search.ts](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/apps/server/src/routers/tools/search.ts)）。LobeHub Cloud 已经预置好搜索服务。
- **服务商自带搜索**：一律记在发起请求的模型 key 所属账户上。用户自带模型 key 时，这笔费用就在用户自己的账户上。

## 3. 多用户自托管：自带模型 key 时能否用管理员的搜索

- **Open WebUI**：能。是否搜索只看全局开关、用户权限和模型能力，不看模型的 key 来自哪里（[middleware.py#L2396-L2405](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/utils/middleware.py#L2396-L2405)）。0.13.x 去掉了“Direct Connections”（用户在个人设置里加的模型连接），改成 Tether：用户自己电脑上的模型由 Open WebUI 经 Tether 中转（CHANGELOG#L12、#L151），所以仍然经过服务端中间件。Tether 模型能不能用管理员的搜索，代码里没看到额外限制，但没有实测 **[未核实]**。
  - 开关：`features.web_search` 是按用户/组的权限（`USER_PERMISSIONS_FEATURES_WEB_SEARCH`），`process_web_search` 里对非管理员做了检查（[routers/retrieval.py#L2646-L2658](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/routers/retrieval.py#L2646-L2658)）；另外还有模型级的 Web Search 能力开关和可选的搜索确认弹窗（`ENABLE_WEB_SEARCH_CONFIRMATION`）。
  - 限流：只有全局并发上限 `WEB_SEARCH_CONCURRENT_REQUESTS`，注释说是给“rate-limited APIs like Brave free tier”用的。没找到按用户计量或配额。
- **LibreChat**：能。模型 key（如 `OPENAI_API_KEY=user_provided`）和搜索 key 是两套独立解析，可以组合成“用户的模型 key + 管理员的搜索 key”。
  - 开关：`WEB_SEARCH` 角色权限（默认 USE=true，[roles.ts#L178](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/packages/data-provider/src/roles.ts#L178)），以及 Agent 能力、model spec 的工具开关。
  - 计量：在 balance/transaction 代码里没找到搜索计费，只记 token。`toolCallLimiter`（每用户每秒 1 次）只挂在 `/agents/tools/:toolId/call` 这个手动调用接口上，不是对话里的搜索（[toolCallLimiter.js](https://github.com/danny-avila/LibreChat/blob/4700c44eb77c142b4f9f8b87476924f22890eb39/api/server/middleware/limiters/toolCallLimiter.js)）。
- **LobeChat 服务端版**：能。用户可以在服务商设置里填自己的模型 key（服务端如何存储 **[未核实]**），而搜索一律走服务端 `SearchService`，用的是部署者的 key。
  - 鉴权：用 LobeHub API key 调用时，受限 key 需要 `model:invoke` 作用域。
  - 计量：没找到按用户的搜索计量或限流。
- **Chatbox Team Sharing**：只是一个共享 OpenAI key 的反向代理（[team-sharing/README-CN.md](https://github.com/chatboxai/chatbox/blob/446bcf43e7a89a0411218821de54d87d7f3cfe11/team-sharing/README-CN.md)），不涉及搜索。

## 4. 模型不支持工具调用时

- **Open WebUI**（`function_calling == 'legacy'`）：走 `chat_web_search_handler`：先调用任务模型生成查询 JSON，解析失败就退回用用户原话当查询；然后执行搜索，再把结果作为 RAG 来源注入（[middleware.py#L1222-L1320](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/utils/middleware.py#L1222-L1320)）。原生模式下跳过这一步，交给模型调用工具（L2403 注释）。
- **Chatbox**：模型没有 `web-browsing` 工具能力时，走 `applyLegacyToolFallback` → `searchByPromptEngineering`：用系统提示词让模型输出 `{action: 'search'|'proceed', query}`，应用用正则提取 JSON，执行搜索，把结果拼进消息再正式生成（[model-calls/tools.ts#L41-L72](https://github.com/chatboxai/chatbox/blob/446bcf43e7a89a0411218821de54d87d7f3cfe11/src/renderer/packages/model-calls/tools.ts#L41-L72)、[chatbox-core legacy-tool-fallback.ts#L90](https://github.com/chatboxai/chatbox/blob/446bcf43e7a89a0411218821de54d87d7f3cfe11/packages/chatbox-core/src/generation/legacy-tool-fallback.ts#L90)）。同时开了知识库时会合并成一次判断（`combinedSearchByPromptEngineering`）。
- **LobeChat**：当前模型不支持函数调用、又没有走模型内置搜索时，界面显示“搜索辅助模型”，提示“当前模型不支持技能调用，需搭配支持技能调用的模型用于联网搜索”。默认是 `DEFAULT_MODEL`（[Search/Controls.tsx#L127-L129](https://github.com/lobehub/lobe-chat/blob/5d4975b805136d9c952089aa650d9ac759bafd55/src/features/ChatInput/ActionBar/Search/Controls.tsx#L127)）。在当前源码里，除了设置读写和 openapi 透传，没找到 `searchFCModel` 的运行时用法，实际是否生效 **[未核实]**。
- **Cherry Studio**：客户端搜索工具要求 `isFunctionCallingModel`，不满足时只剩服务商自带搜索一条路，再不行就是 `none`。aiCore CHANGELOG 记录了已“Remove the prompt-based tool-use plugin end-to-end. Tool use now relies solely on native provider tool calling”（[packages/aiCore/CHANGELOG.md#L152](https://github.com/CherryHQ/cherry-studio/blob/3110175b8025598bb4460d832b3d1221bb611726/packages/aiCore/CHANGELOG.md#L152)）。
- **NextChat**：插件只在 `showPlugins` 返回 true 的服务商（OpenAI、Azure、Moonshot、ChatGLM 等）下显示（[app/utils.ts#L335](https://github.com/ChatGPTNextWeb/NextChat/blob/1d3dc331512929a4b1d3e82354e72acce33b5c7a/app/utils.ts#L335)），没有非工具调用的回退。
- **服务商侧**：通义的 `enable_search` 和 OpenAI Chat Completions 的搜索模型都不需要客户端处理工具调用，由服务商在内部搜完再生成。

## 没覆盖的

- AnythingLLM、Jan、Dify 等其他同类没有调研（AnythingLLM 文档链接 404）。
- Chatbox AI 许可证的搜索额度和价格、Kimi 搜索单价、智谱工具形式的计费，都没在一手来源里拿到。
