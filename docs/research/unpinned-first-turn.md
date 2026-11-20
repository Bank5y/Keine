# 未固定的第一轮暂存在哪里：同类产品与开源实现调研

> 对应 issue #98（父 issue #90，依赖 #94，阻塞 #84）。只陈述事实与可选方案，不做决定。
> 调研日期：2026-11-20。开源代码均引用到具体 commit。

## 问题回顾

读者选中文字点「分析」后，探索助手（Go 进程里的工具循环）通过 SSE 流式输出标题、简介、正文。点「固定」时，Keine 要把第一轮**注入了上下文的完整请求消息**（选区、整讲正文、课程信息等，可能几万 token，下称「快照」）和**最终回复**存成探索，后续对话轮复用同一前缀以命中 prompt cache；不固定则丢弃（GLOSSARY「探索」一节、#74 用户故事 12：「不固定的分析不留下任何记录」）。

待定的是固定之前这些消息放在哪里：服务端暂存并给前端一个 ID，还是前端把整段消息提交回来。

## 一、模型服务商自己的做法

### OpenAI Responses API / Conversations API

- **Response 对象默认在服务端保存 30 天**：「Response objects are saved for 30 days by default」；可用 `store: false` 关闭。[conversation-state](https://developers.openai.com/api/docs/guides/conversation-state)
- `previous_response_id` 用服务端保存的 response 串起多轮，客户端不必重发历史。但**之前所有输入 token 仍按输入计费**：「all previous input tokens for responses in the chain are billed as input tokens」。（同上）
- **Conversations API** 是持久对象：「Conversation objects and items in them are not subject to the 30 day TTL」。（同上）
- 也就是说，OpenAI 分了两层：一层是**带 30 天 TTL 的生成结果**（response），另一层是**显式创建的持久会话**（conversation）。这和 Keine 的「分析结果 → 固定成探索」很像。
- **无状态模式下的防篡改**：`store: false`（或组织开了 ZDR）时，推理项带 `encrypted_content`，客户端要在后续请求里原样回传；内容对客户端是不透明的。[reasoning](https://developers.openai.com/api/docs/guides/reasoning) 这就是「状态交给客户端保管，但用服务端密钥加密/签名」的做法。

### ChatGPT 临时聊天

- 临时聊天不进历史；不保存的话离开后无法从历史重新打开；**出于安全原因仍可能保留最多 30 天**；**「保存」后变成普通聊天、出现在历史里**；保持临时期间不用于训练。[Temporary Chat FAQ](https://help.openai.com/en/articles/8914046-temporary-chat-faq)（该页对自动抓取返回 403，以上内容来自 OpenAI 帮助中心检索结果，未能逐字核对）
- 可见 ChatGPT 的「先临时、后保存」由服务端持有内容，保存只是改状态，不需要客户端回传内容。

### Anthropic

- **Messages API 无状态**：「The Messages API is stateless, which means that you always send the full conversational history to the API」，并明确「Earlier conversational turns don't necessarily need to actually originate from Claude. You can use synthetic `assistant` messages.」[working-with-messages](https://platform.claude.com/docs/en/build-with-claude/working-with-messages)
  → 对 Keine 的意义：模型 API 本身不校验历史的真伪，如果 Keine 把客户端提交的消息原样转发，**伪造的 assistant 轮次会被模型当作自己说过的话**。防篡改只能在 Keine 这一层做。
- **Message threads（beta）**：服务端替你保存对话，后续只发新消息；「A thread expires 5 minutes after its last request and is deleted no more than 1 hour after its last use」；内容只放内存、用服务端密钥加密、不落持久存储。[message-threads](https://platform.claude.com/docs/en/build-with-claude/message-threads) 这是服务商一侧「短 TTL 暂存」的例子。
- **Claude 无痕聊天**：不进历史、不用记忆、不参与历史搜索，但「retained for … 30 days (default)」用于安全；企业版可配更长。[Using incognito chats](https://support.claude.com/en/articles/12260368-using-incognito-chats)

### Prompt cache 的约束（决定「快照」必须逐字节一致）

- Anthropic：缓存命中要求「100 percent identical prompt segments」，键是 `tools → system → messages` 的累积哈希；默认 TTL 5 分钟（命中会续期），可选 1 小时；组织/工作区之间隔离。[prompt-caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- OpenAI：「Cache reuse requires the entire rendered prefix to match」；GPT-5.6 及以后最小 1024 token，`prompt_cache_options.ttl` 默认 30 分钟、可选 12 小时；更早的模型 in-memory 约 5–10 分钟空闲、最长 1 小时，`24h` 档最长 24 小时。[prompt-caching](https://developers.openai.com/api/docs/guides/prompt-caching)
- 推论：**服务商的缓存最多存活几分钟到一天**，所以「固定后复用前缀」主要省的是固定后不久的连续对话；隔天再打开探索，缓存多半已经过期，但前缀仍必须逐字节相同才可能命中。#74 要求兼容 OpenAI 接口，具体 TTL 取决于部署者用的服务商。

## 二、开源聊天 UI 的代码

### Vercel ai-chatbot（chat-sdk）— `vercel/chatbot@a8989b5`

- **服务端不信任客户端历史**：普通发送时客户端只发最后一条 `message`（[use-active-chat.tsx#L150-L174](https://github.com/vercel/chatbot/blob/a8989b522fa3672a2ae52f053105e40993b36ca1/hooks/use-active-chat.tsx#L150-L174)）；服务端从数据库加载历史再拼上这条（[route.ts#L116-L171](https://github.com/vercel/chatbot/blob/a8989b522fa3672a2ae52f053105e40993b36ca1/app/(chat)/api/chat/route.ts#L116-L171)）。请求体 schema 限定用户消息只能有 `role: "user"`、文本 ≤2000 字符（[schema.ts](https://github.com/vercel/chatbot/blob/a8989b522fa3672a2ae52f053105e40993b36ca1/app/(chat)/api/chat/schema.ts)）。只有工具审批续跑时客户端才发完整 `messages`，而且服务端**只从中取审批状态**、合并到数据库里的消息上，不采用客户端的消息正文（route.ts#L137-L165）。
- **写库时机**：流开始之前写 chat 行和用户消息（#L126、#L183），流结束后在 `onEnd` 写助手消息（#L349-L389）。中途失败时库里只有用户消息。
- **可续流**：配置了 `REDIS_URL` 时，流 id 写进 Postgres 的 `Stream` 表，SSE 交给 `resumable-stream`（#L406-L420）。

### vercel/resumable-stream — `@770cdeb`

- 生产者在**进程内存**里缓存已输出的分块（[runtime.ts#L133](https://github.com/vercel/resumable-stream/blob/770cdeb4b6e4b3838b294b0565591f3d75930897/src/runtime.ts#L133)），后来的消费者通过 Redis pub/sub 请求补发；Redis 里只放一个 sentinel 键，`EX: 24*60*60`（#L168-L190）。[README「How it works」](https://github.com/vercel/resumable-stream/blob/770cdeb4b6e4b3838b294b0565591f3d75930897/README.md#how-it-works)
- 也就是说，流的内容**并不持久化**，生产者进程一挂就无法续传；它解决的是断线重连和多标签页跟随，不是暂存。

### LibreChat — `danny-avila/LibreChat@d4e85d2`

- **临时聊天就是普通消息加 `expiredAt`**：消息、会话、工具调用等集合都建了 MongoDB TTL 索引 `{ expiredAt: 1 }, { expireAfterSeconds: 0 }`（[message.ts#L337](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/data-schemas/src/schema/message.ts#L337)、[convo.ts#L438](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/data-schemas/src/schema/convo.ts#L438)）。默认保留 30 天（`DEFAULT_RETENTION_HOURS = 24 * 30`），可配置为 1–8760 小时（[tempChatRetention.ts#L8-L18](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/data-schemas/src/utils/tempChatRetention.ts#L8-L18)、[librechat.example.yaml#L448-L449](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/librechat.example.yaml#L448-L449)）。查询时还要额外过滤 `expiredAt > now`（[retention.ts](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/data-schemas/src/utils/retention.ts)）。MongoDB 文档说明 TTL 后台任务每 60 秒跑一次，过期后不保证立即删除，读取时应自行按时间过滤。[TTL Indexes](https://www.mongodb.com/docs/manual/core/index-ttl/)
- **历史从数据库加载**：`loadHistory(conversationId, parentMessageId)` 按父链从库里读（[BaseClient.js#L1254](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/api/app/clients/BaseClient.js#L1254)），客户端只给 id。用户消息在生成前就开始持久化，回复在完成后保存（#L785-L1052、#L1245）。
- **生成任务暂存在 Redis/内存**：RedisJobStore 的默认 TTL 是运行中 20 分钟（崩溃兜底）、完成后 5 分钟、完成后分块立即删除（[RedisJobStore.ts#L1924-L1942](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/api/src/stream/implementations/RedisJobStore.ts#L1924-L1942)）；内存版同样是完成后 5 分钟、卡死 20 分钟（[createStreamServices.ts#L120-L126](https://github.com/danny-avila/LibreChat/blob/d4e85d2205c78413ee27d631d7e3b7313556f21d/packages/api/src/stream/createStreamServices.ts#L120-L126)）。

### Open WebUI — `open-webui/open-webui@0666e59`

- **临时聊天的内容只在浏览器里**：id 前缀是 `temporary:<socket session>:<uuid>`（[chatId.ts](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/src/lib/utils/chatId.ts)）。服务端只在 Redis/内存里记一条「这个临时 chat 属于哪个用户和 socket」，TTL 24 小时（[utils/chat.py#L46-L101](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/backend/open_webui/utils/chat.py#L46-L101)）。
- **「保存临时聊天」由客户端把整段 history 提交回来**：`onSaveTempChat` 调用 `createNewChat`，带上 `history` 和 `messages`（[Chat.svelte#L6159-L6186](https://github.com/open-webui/open-webui/blob/0666e59d26a4d056e0bacee41742486badd71cf8/src/lib/components/chat/Chat.svelte#L6159-L6186)）。这正是 Keine 方案里「前端提交整段消息」的实例。
- 普通补全请求也由前端组装 `messages` 发给 `/api/chat/completions`（Chat.svelte#L5124-L5130）。Open WebUI 的前提是用户可以自由编辑自己的消息，它没把「助手说过什么」当作需要防篡改的事实。

### LobeChat — `lobehub/lobe-chat@1ce7bdc`

- 服务端模式下，生成开始前就在库里**同时建好用户消息和一条内容为 `LOADING_FLAT` 的助手占位消息**（[aiChat.ts#L509-L553](https://github.com/lobehub/lobe-chat/blob/1ce7bdcefb2816508d8ef085ee6b37dc8472fe4a/apps/server/src/routers/lambda/aiChat.ts#L509-L553)），之后再更新占位消息。
- 客户端有 `messageService.updateMessage(id, { content, … })` 这条路径，可以直接改写消息内容（[optimisticUpdate.ts#L174](https://github.com/lobehub/lobe-chat/blob/1ce7bdcefb2816508d8ef085ee6b37dc8472fe4a/src/store/chat/slices/message/actions/optimisticUpdate.ts#L174)）。和 Open WebUI 一样，它默认用户可以改自己会话里的任何消息。

### bolt.new — `stackblitz/bolt.new@eda10b1`

- 服务端完全无状态：`/api/chat` 直接用请求体里的 `messages` 调模型（[api.chat.ts#L12](https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/routes/api.chat.ts#L12)）；历史存在浏览器 IndexedDB `boltHistory`（[db.ts#L10](https://github.com/stackblitz/bolt.new/blob/eda10b121221b30825a4c16eec5da1fd3eb1eb99/app/lib/persistence/db.ts#L10)）。

## 三、阅读 / 编辑器里的「AI 结果先给你看，再决定要不要留」

- **Readwise Reader Ghostreader**：回答显示在聊天里，阅读会话期间记住对话；点回答下方的笔记本图标才会**保存到文档笔记**并同步到 Readwise；清空聊天会丢掉当前对话。开了 Global Ghostreader 后会保留对话历史。[Ghostreader Chat](https://docs.readwise.io/reader/guides/ghostreader/chat) 文档没说关闭 Global 时单文档聊天在服务端保存多久。
- **Tiptap AI**：`insert: false` 时结果只放在编辑器扩展存储 `editor.storage.ai` 里（客户端），`aiAccept` 才写进文档，`aiReject` 恢复原样；协同编辑下，未接受的结果只有请求者自己看得到。[Manage responses](https://tiptap.dev/docs/ai/basic/text-generation/manage-responses)
- **Notion AI**：官方 FAQ 说生成或修改后可以接受、丢弃或重试；「Insert below / Replace」等按钮名称只在第三方教程里见到，未经官方核实。[Notion AI FAQs](https://www.notion.com/help/notion-ai-faqs)
- 这几类产品的「待定结果」都是**最终会写进用户自己文档的文本**，没有「后续对话要逐字节复用的模型上下文」这一层，所以只能作为交互层面的参考。Kindle、Apple Books、Glasp 没找到描述 AI 结果暂存机制的一手资料。

## 四、通用做法

| 做法 | 实例 | 存什么、存在哪 | 清理 |
|---|---|---|---|
| 持久表 + 过期字段 | LibreChat `expiredAt` + Mongo TTL 索引；OpenAI response 默认 30 天 | 完整消息，数据库 | 数据库后台任务；读取时还要按时间过滤 |
| KV 带过期 | Open WebUI 临时 chat 归属（24h）；LibreChat job（5/20 分钟）；resumable-stream sentinel（24h）；Anthropic threads（5 分钟/≤1h） | 元数据或短期内容，Redis/内存 | `EX` 自动过期；进程内存的方案重启即丢 |
| 客户端持有、服务端加密/签名 | OpenAI `encrypted_content`（`store:false`） | 不透明密文，客户端 | 不需要清理 |
| 客户端持有、明文回传 | Open WebUI 保存临时聊天；bolt.new；LobeChat 改消息 | 明文消息，浏览器 | 不需要清理；服务端信任客户端 |
| 服务端只认 id，历史从库读 | Vercel chatbot、LibreChat | 数据库 | 跟随会话的生命周期 |

Postgres（Keine 的元数据库，ADR 0001）没有内置的行级 TTL，需要应用里的定时清理或 pg_cron 之类的扩展；读取时同样应按 `expires_at` 过滤。

## 五、对 Keine 的可选方案与取舍（不做决定）

先列几个 Keine 特有的事实，各方案都要面对：

- 快照里的大头（整讲正文、课程信息）**理论上可以由服务端按 (课程版本, 讲, 选区锚点) 重新生成**：课程每个版本是完整目录（ADR 0005），下架/删除时冻结（ADR 0004）。但前提是拼装逻辑是确定的，并且固定时那个版本的目录还在。课程更新后旧目录是否保留，要看 ADR 0004 以外的情况。
- **不能重新生成的部分**：第一轮工具循环里的工具调用和结果、模型的最终回复（非确定），以及 #94 还没定下来的其他注入内容。这部分要么存服务端，要么由客户端回传。
- Prompt cache 要求前缀逐字节一致，任何重新序列化（JSON 字段顺序、空白、时间戳）都会导致缓存未命中。

### A. 服务端暂存，前端凭 id 固定

- **存储位置**：(A1) Postgres 暂存表（如 `analysis_drafts`，含 `user_id`、`expires_at`）；(A2) Go 进程内存 map 加 TTL；(A3) Redis（Keine 目前没有 Redis，见 compose）。
- **安全**：内容由服务端生成，客户端只拿到一个不可猜的 id，固定时按 `user_id` 校验归属。没有注入或伪造回复的问题。
- **存储成本**：每次分析都要写一份几万 token 的快照（约几十到几百 KB），包括最后没固定的大多数。A1 有写放大；A2 只占内存。
- **清理**：A1 要定时任务删除过期行，查询时也按 `expires_at` 过滤（LibreChat 和 MongoDB 文档都是这样做的）；A2 在进程里 sweep。TTL 可以参考：LibreChat 完成后 5 分钟、Anthropic threads 5 分钟/1 小时、OpenAI response 30 天。对 Keine 来说，悬浮框不关就可能一直不固定，TTL 太短会出现「点了固定却提示已过期」。
- **重启**：A1 不受影响；A2 重启即丢，表现为固定失败，需要提示用户重新分析。
- **多标签页**：每次分析一个 id，互不干扰；同一选区在两个标签页各分析一次就有两份草稿，固定时仍要检查「一段文字最多一个探索」（ADR 0007）。
- **重试**：生成出错重试时新建 id 还是覆盖旧 id 都可以；中途关闭（#74 故事 11）时应立即删除或标记作废。
- **和「不留下任何记录」的关系**：#74 故事 12 说不固定的分析不留记录。A1 会在过期前短暂留下记录（ChatGPT 临时聊天、Claude 无痕也是「不进历史，但最多保留 30 天」）；A2 不落盘，更贴近字面意思。
- 变体 **A′**：只暂存不能重新生成的部分（工具轨迹和回复），快照的大头在固定时重新生成，能明显减少写入量，但要依赖拼装逻辑确定、课程版本还在。

### B. 前端把整段消息提交回来

- **B1 明文回传**（Open WebUI 的做法）：服务端什么都不存，没有清理和重启问题。但正如 Anthropic 文档所说，assistant 轮次可以合成，客户端能伪造回复、在「快照」里塞任意提示词，这些内容会在之后每一轮作为可信前缀发给模型，相当于持久化的提示词注入，还被缓存放大。服务端可以逐段校验（例如重新生成快照的大头再比对），但比对本身就要求重新生成，也就退化成了 A′ 或 C。上传体积也很大（几万 token，每次固定一次）。
- **B2 签名或加密回传**（OpenAI `encrypted_content` 的思路）：分析结束时服务端对「快照 + 回复」算 HMAC（或用 AEAD 加密），连同内容一起交给客户端；固定时验签后再入库。服务端不保存状态，重启也不影响（只要密钥不变），篡改能被发现。代价：下行和上行都要传整段内容；签名要绑定 `user_id`、选区和过期时间，否则可以重放到别处；密钥需要配置和轮换；内容在客户端是明文还是密文要单独决定（快照里有课程全文，对本人可见，问题不大）。

### C. 不暂存，固定时由服务端重新生成快照，回复由客户端提交

- 快照由服务端重建，可信；回复由客户端提交，客户端能伪造的只剩「回复」，影响是自己的探索内容被自己改掉。如果探索文档以后允许用户编辑，这个风险就不算风险了；但回复作为后续轮次的 assistant 前缀，依然是被注入的内容。
- 第一轮的工具调用轨迹没法重建，要么丢弃（后续轮的前缀和第一轮实际请求不一致，第一轮的缓存用不上），要么也让客户端提交（又回到 B1 的信任问题）。

### 各方案对照

| | A1 Postgres 暂存 | A2 内存暂存 | B1 明文回传 | B2 签名回传 | C 重建 + 回传回复 |
|---|---|---|---|---|---|
| 篡改风险 | 无 | 无 | 高 | 无（验签） | 只有回复 |
| 未固定时的写入 | 每次分析都写 | 无落盘 | 无 | 无 | 无 |
| 清理 | 定时任务 + 读时过滤 | 进程内 sweep | 无需 | 无需 | 无需 |
| 重启后能否固定 | 能 | 不能 | 能 | 能 | 能（课程版本还在的话） |
| 网络传输 | 只传 id | 只传 id | 上行整段 | 下行 + 上行整段 | 上行回复 |
| 第一轮前缀逐字节一致 | 是 | 是 | 取决于客户端 | 是 | 取决于重建是否确定 |
| 符合「不留记录」 | 留到过期为止 | 是 | 是 | 是 | 是 |

## 来源

- OpenAI：[Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)、[Reasoning](https://developers.openai.com/api/docs/guides/reasoning)、[Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)、[Temporary Chat FAQ](https://help.openai.com/en/articles/8914046-temporary-chat-faq)
- Anthropic：[Using the Messages API](https://platform.claude.com/docs/en/build-with-claude/working-with-messages)、[Message threads](https://platform.claude.com/docs/en/build-with-claude/message-threads)、[Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)、[Using incognito chats](https://support.claude.com/en/articles/12260368-using-incognito-chats)
- 代码：[vercel/chatbot@a8989b5](https://github.com/vercel/chatbot/tree/a8989b522fa3672a2ae52f053105e40993b36ca1)、[vercel/resumable-stream@770cdeb](https://github.com/vercel/resumable-stream/tree/770cdeb4b6e4b3838b294b0565591f3d75930897)、[LibreChat@d4e85d2](https://github.com/danny-avila/LibreChat/tree/d4e85d2205c78413ee27d631d7e3b7313556f21d)、[open-webui@0666e59](https://github.com/open-webui/open-webui/tree/0666e59d26a4d056e0bacee41742486badd71cf8)、[lobe-chat@1ce7bdc](https://github.com/lobehub/lobe-chat/tree/1ce7bdcefb2816508d8ef085ee6b37dc8472fe4a)、[bolt.new@eda10b1](https://github.com/stackblitz/bolt.new/tree/eda10b121221b30825a4c16eec5da1fd3eb1eb99)
- 其他：[MongoDB TTL Indexes](https://www.mongodb.com/docs/manual/core/index-ttl/)、[Readwise Ghostreader Chat](https://docs.readwise.io/reader/guides/ghostreader/chat)、[Tiptap: Manage responses](https://tiptap.dev/docs/ai/basic/text-generation/manage-responses)、[Notion AI FAQs](https://www.notion.com/help/notion-ai-faqs)
