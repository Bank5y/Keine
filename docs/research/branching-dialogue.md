# 多分支 AI 对话的上下文管理调研

> 对应 issue #93，地图 #90。调研日期 2026-11-19。只记录事实和模式，不做决策。
> 术语按 `GLOSSARY.md`「探索」一节：探索对话由主节点、子节点、分支节点、发散节点组成。下文说「节点」时指一般意义上的树节点，说到 Keine 时才用这些术语。

## 结论速览

1. **存储几乎都是「父指针邻接表」**：每条消息存一个 `parentId`，树由此隐含；另有一个「当前叶子」指针决定界面显示哪条路径。LibreChat、Open WebUI、Loom 都是这样；ChatGPT 的导出格式也是（但非官方文档）。
2. **上下文组装 = 从目标节点沿父指针走到根，再反转**。三个开源项目的实现几乎一模一样，都带环检测。兄弟分支天然不进上下文。
3. **摘要/压缩挂在路径上的某个节点上**，而不是挂在「对话」上：于是摘要自动被该节点下面的所有分支共享，而兄弟分支互不影响。LibreChat、Open WebUI、Loom 各有一版。
4. **分支之间的引用很少见**。现有做法只有两类：把另一个对话的当前路径拍平成一段文本塞进上下文（Open WebUI「Reference Chats」），或者把多个并列回答合成一个（Open WebUI「Merge Responses」）。没有发现「在分支 A 里引用分支 B 某个节点」的结构化机制。
5. **「另开分支」有两种实现**：同一棵树里加兄弟节点（编辑/重新生成），或者把路径复制成一个新对话（ChatGPT「Branch in new chat」、LibreChat fork、SillyTavern branch/checkpoint）。后者复制数据、靠名字或 ID 回指来源。
6. **共享前缀缓存**：OpenAI 与 Anthropic 的提示缓存都是「完整前缀精确匹配」。同一祖先链上的分支，只要系统提示、工具、前面的消息逐字节不变，就能命中同一份缓存；在中间改写/插入摘要会让后面的缓存全部失效。
7. **把对话结果同步回文档**：没找到「多分支对话 → 合并进一份共享文档」的现成产品。最接近的是 LibreChat artifacts：文档内容嵌在某条消息的正文里，编辑就是原地改那条消息，因此每个分支有各自的文档版本。

---

## 1. 存储模型

### 1.1 LibreChat（MongoDB）

来源：`danny-avila/LibreChat` @ `d4e85d2`（2026-11-17）

- `packages/data-schemas/src/schema/message.ts`：每条消息是一行文档，关键字段 `messageId`、`conversationId`、`parentMessageId`、`isCreatedByUser`、`text`、`summary`、`summaryTokenCount`、`tokenCount`。唯一索引 `{ messageId, user, tenantId }`。没有 children 数组，没有路径字段——树完全由 `parentMessageId` 推出，根消息的父是常量 `Constants.NO_PARENT`。
- 编辑和重新生成会在同一父节点下产生新的兄弟消息（`api/app/clients/BaseClient.js` 约 472–490 行：`isEdited` 时以被编辑的回复为 head 重新加载历史并换一个新的 `responseMessageId`）。官方文档也说明「Edits and regenerations create alternate branches」（<https://www.librechat.ai/docs/features/fork>）。

### 1.2 Open WebUI（SQLAlchemy，SQLite/Postgres）

来源：`open-webui/open-webui` @ `0666e59`（2026-11-17）

- 历史上整段对话存在 `chat.chat` 这个 JSON 列里：`history.messages` 是 `{id: message}` 映射，每条有 `parentId` 和 `childrenIds`，外加 `history.currentId` 指向当前显示的叶子（`backend/open_webui/models/chats.py`）。
- 现在另有一张规范化的 `chat_message` 表（迁移 `8452d01d26d7_add_chat_message_table.py`），列包括 `id`、`chat_id`（外键，级联删除）、`role`、`parent_id`、`content`(JSON)、`output`、`model_id`、`usage`、`context_summary`、`created_at` 等（`backend/open_webui/models/chat_messages.py` 165–199 行）。写入时 JSON 和表两边都更新（`models/chats.py` 约 515–520 行 `ChatMessages.upsert_message`）。即：**父指针表 + 一份冗余的整树 JSON**。
- 删除中间节点时把孙节点的 `parentId` 改挂到被删节点的父上（`models/chats.py` 约 1148–1163 行）。

### 1.3 Loom（socketteer/loom，本地 JSON 文件）

来源：`socketteer/loom` @ `main`（最后推送 2024-06-28）

- 「多重宇宙」写作工具：整棵树是嵌套 JSON，每个节点有 `children` 数组，内存里另算一个 `tree_node_dict {id: node}`，节点带 `parent_id`（`model.py` 270–300 行，`util/util_tree.py`）。
- 节点上还能挂 `chapter_id`、`memories`、`summaries`，并且有「继承」语义（见 2.3）。

### 1.4 ChatGPT（只看到导出格式，非官方）

- 导出的 `conversations.json` 里每个对话有 `mapping`（`{id: {message, parent, children}}`）和 `current_node`。重建当前对话的通行做法是从 `current_node` 沿 `parent` 走回根再反转。**OpenAI 没有公开这个格式的文档**，以上来自社区解析（如 <https://community.openai.com/t/decoding-exported-data-by-parsing-conversations-json-and-or-chat-html/403144>），只能作参考。
- 2025-09 上线的「Branch in new chat」从某条消息起复制出一个新对话，原对话不变，新对话在侧栏里是独立条目而不是嵌套在原对话下（官方 release notes 页 <https://help.openai.com/en/articles/6825453> 抓取被拒，此条来自媒体报道，未核对原文）。

### 1.5 SillyTavern（复制式分支）

来源：<https://docs.sillytavern.app/usage/core-concepts/chatfilemanagement/>

- 「Create Branch」把当前聊天截到这条消息复制成新聊天并切过去；「Create Checkpoint」同样复制但不切换、要求起名。checkpoint 通过**聊天文件名**回指父聊天，菜单里有「Back to parent chat」；改名会断链。

### 1.6 在 Postgres 里的常见落地（通用模式，非某产品）

- 邻接表（`parent_id`）+ `WITH RECURSIVE` 递归 CTE 取祖先链：<https://www.postgresql.org/docs/current/queries-with.html>
- 物化路径：`ltree` 扩展，用 `@>`/`<@` 判断祖先/后代，可建 GiST 索引：<https://www.postgresql.org/docs/current/ltree.html>
- 上面三个开源项目都**没有**用物化路径：它们一次把整个对话的消息全取出来，在应用内存里走父指针。对单个对话几十到几百条消息的规模，这样足够。

---

## 2. 上下文组装

### 2.1 沿祖先链

三个项目的核心函数几乎同构——建 `id → message` 映射，从目标节点循环取 `parent`，记录已访问 id 防环，最后反转：

- LibreChat `BaseClient.getMessagesForConversation({ messages, parentMessageId, summary })`（`api/app/clients/BaseClient.js` 1478–1528 行）。`loadHistory(conversationId, parentMessageId)` 先取整个对话的消息再调用它（1254 行起）。
- Open WebUI `get_message_list(messages_map, message_id)`（`backend/open_webui/utils/misc.py` 195–228 行）。
- Loom `node_ancestry(node, node_dict)`（`util/util_tree.py` 158–166 行），另有 `nearest_common_ancestor`（206 行起）。

要点：**兄弟分支、其他子树从不进入上下文**；「当前在哪个分支」由请求里带的目标节点 id（LibreChat 的 `parentMessageId`、Open WebUI 的 `currentId`）决定，而不是由服务端会话状态决定。

### 2.2 摘要 / 压缩挂在节点上

- **LibreChat**：消息上有 `summary`、`summaryTokenCount` 字段；压缩（compaction）时把摘要作为 checkpoint 写在分支的叶子上（`getCompactionAnchor`，`BaseClient.js` 525–560 行）。`getMessagesForConversation` 在 `summary: true` 时走到第一个 checkpoint 就停（1512–1520 行），即「checkpoint 摘要 + 它之后的消息」构成上下文。注释写明 checkpoint 之外的完整行仍会交给需要的客户端（1266–1268 行）。
- **Open WebUI**：`chat_message.context_summary` 列（迁移 `4c5ce3d2f27f`，2026-06-18）。`compact_chat_branch` 从当前叶子取祖先链，把除最后一条外的消息总结成摘要，写到当前节点的 `contextSummary` 上（`utils/context_compaction.py` 161–212 行）；请求时 `_apply_latest_summary_checkpoint` 从后往前找路径上最近一个带摘要的节点，丢弃它之前的消息（309 行起）。自动压缩只在「整轮的 user 消息」作为边界时持久化（116 行注释）。
- **Loom**：`create_summary(root_node, end_node, text)` 把摘要挂在起点节点上并记录终点 `end_id`；`past_summaries(node)` 只返回 `end_id` 在当前祖先链上的摘要（`model.py` 1397–1428 行）——即**摘要覆盖的是一段路径，只对经过这段路径的分支生效**。
- 共同模式：摘要存在树里某个节点上；由于祖先链是共享的，挂在节点 X 上的摘要自动对 X 下面所有分支生效，对 X 的兄弟分支不可见。

### 2.3 继承范围可配置的「记忆」（Loom）

`create_memory(node, text, inheritability)`，`inheritability` 取值 `none`（只对该节点）、`subtree`、`global`、`delayed`（该节点滑出上下文窗口后才生效）（`model.py` 1350–1383 行）。这是唯一见到的「节点级注入内容 + 作用域」显式模型。注意构建函数 `construct_memory` 在代码里是注释掉的，功能完成度存疑。

### 2.4 截断

Loom 用 `context_window_index` 按字符长度从祖先链尾部倒推「第一个完整落在窗口内的节点」（`model.py` 1431–1438 行）；LibreChat 有按 token 计数截断的逻辑（`BaseClient.js` 1530 行起引用 OpenAI cookbook 的计数方法）。

### 2.5 服务端托管状态（OpenAI Responses API）

来源：<https://developers.openai.com/api/docs/guides/conversation-state>

- `previous_response_id` 把回复串成链；Conversations API 提供持久对象。
- 文档**没有**一般性地说明能从任意旧 response id 分叉，只在 WebSocket 模式里提到「a new lane can fork from a response on another lane」。
- 链上所有之前的输入 token 照样按输入计费；response 对象默认存 30 天，`store: false` 可关闭。
- 对需要「自己的消息格式、运行时可替换」的设计（#90 的前提）而言，这意味着树和上下文组装仍要自己做，服务端状态只是可选的优化。

---

## 3. 共享前缀的提示缓存

### OpenAI

来源：<https://developers.openai.com/api/docs/guides/prompt-caching>

- 前缀精确匹配：「Cache reuse requires the entire rendered prefix to match.」断点之前任何改动都会让之后的缓存失效；**编辑较早的消息（而不是追加）也会失效**。
- GPT-5.6 起最短可缓存 1,024 token，默认最少保留 30 分钟，Responses API 可要求 12 小时；更早的模型 `in_memory` 约 5–10 分钟、可选 `24h`。
- 价格（GPT-5.6 起）：读 0.1×（部分模型 0.05×），写 1.25×（30 分钟）/ 2.5×（12 小时）。
- `prompt_cache_key` 在 GPT-5.6 之前影响路由，之后主要用于按用户隔离计费。每分钟约 15 次以上请求同前缀时会溢出到别的机器，命中率下降。
- 改 `model`、`tools`、`reasoning.effort`、`text.format` 等设置都会失效。

### Anthropic

来源：<https://platform.claude.com/docs/en/build-with-claude/prompt-caching>

- 前缀顺序 `tools` → `system` → `messages`；最多 4 个 `cache_control` 断点，也可用顶层自动断点（随对话增长前移）。
- 读取时每个断点最多往回查 20 个位置。
- TTL 默认 5 分钟（命中即免费续期），可选 1 小时；写 1.25× / 2×，读默认 0.1×（部分新模型更低）。
- 最短可缓存长度随模型 512–4,096 token 不等；不够长时静默不缓存。
- 上层变化使下层全部失效（改工具 → 全失效；改系统提示 → system 和 messages 失效）。

### 对分支对话意味着什么（模式，非建议）

- 同一祖先链上的多个分支，请求前缀相同（系统提示 + 探索背景 + 共同祖先消息），天然适合前缀缓存；两个服务商都按前缀哈希，分支越晚分叉，可复用的部分越长。
- 把稳定内容（工具定义、系统提示、原文与探索文档）放在最前，把每个分支不同的部分放在最后，是两家文档共同暗示的排布。
- 2.2 里「在路径某处插入摘要」等于改写了前缀，会让该点之后的缓存失效；摘要节点之后的请求要重新写缓存。
- 缓存寿命以分钟计（OpenAI 新模型 30 分钟起），用户隔一段时间回到旧分支时大概率不命中。

---

## 4. 分支之间的引用

- **Open WebUI「Reference Chats」**：输入框菜单里可以把另一个对话作为附件引用（`src/lib/components/chat/MessageInput/InputMenu.svelte` 486 行）。后端检查读权限后，取该对话 `currentId` 的祖先链，拍平成 `#### User / #### Assistant` 的 Markdown 文本，当作一份文档塞进检索上下文（`backend/open_webui/retrieval/utils.py` 1488–1525 行）。只能引用**整个对话的当前路径**，不能指定某个分支或节点。
- **Open WebUI「Merge Responses」**：多模型并列回答时，可把同一轮的多个回答合成一个，结果存在消息的 `merged` 字段上（`src/lib/components/chat/Messages/MultiResponseMessages.svelte` 210–218、408–421 行）。这是「同一父节点下的兄弟合并」，不是跨分支任意引用。
- **LibreChat fork 选项**（<https://www.librechat.ai/docs/features/fork>，实现 `api/server/utils/import/fork.js`）：复制到新对话时可选只要直达路径（`DIRECT_PATH`，复用 `getMessagesForConversation`）、路径加沿途分出的兄弟分支（`INCLUDE_BRANCHES`）、或到目标层级为止的所有节点（`TARGET_LEVEL`，默认，按层 BFS）；还可「Start fork here」把目标消息变成新对话的起点（`splitAtTargetLevel` 把目标层所有消息的父改为根）。这是「复制」，不是「引用」：新对话与原对话之后互不影响。
- **Loom** 有 `nearest_common_ancestor`，用于界面上的树操作，不用于把另一分支的内容放进上下文。
- 没有发现哪个产品让模型在一个分支里**按节点引用另一个分支**并保持链接。

---

## 5. 把对话结果同步回文档

- **LibreChat artifacts**：artifact 是嵌在助手消息正文里的 `:::artifact … :::` 块（`packages/api/src/artifacts/update.ts`）。用户编辑 artifact 走 `POST /api/messages/artifact/:messageId`，在**那条消息**里定位第 N 个 artifact 并原地替换文本（`api/server/routes/messages.js` 436–546 行）。所以「文档」属于某个节点；不同分支各有各的版本，没有合并回一份共享文档的机制。
- **ChatGPT Canvas**（2024-10 发布，<https://openai.com/index/introducing-canvas/>，页面抓取被拒，未核对原文）：对话旁边的独立文档，模型可做局部修改，可回到之前版本。据媒体报道，新的默认模型已不再提供 Canvas，改为回答内的写作块——未能在官方页面核实。
- 未发现任何产品做「多个分支的结论 → 有差异确认地合并进同一份文档」。这一块（#90「Not yet specified」第一条）没有可直接借鉴的现成实现。

---

## 不确定 / 未核实

- ChatGPT 的存储与分支细节只来自导出格式和媒体报道；官方 release notes 与 Canvas 页面 403，未读到原文。
- Claude.ai 的编辑分支未调研（无公开一手资料）。
- Loom 最后更新于 2024-06，`construct_memory` 被注释掉，记忆继承可能未完整生效。
- OpenAI / Anthropic 的价格、TTL、最短长度按 2026-11 文档记录，随模型变化频繁。
