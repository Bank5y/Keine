# 聊天类应用怎样渲染和导航分支

> 调研日期 2026-11-21。只记录事实和模式，最后一节给出简短建议。
> 上下文怎么组装（祖先链、摘要、缓存）见 `research/branching-dialogue` 分支上的 `docs/research/branching-dialogue.md`，本文不重复，只讲**界面**：入口、标记、导航、岔出点、嵌套、类型。
> 术语按 `GLOSSARY.md`「探索」一节和 ADR 0009：探索对话是由节点组成的树；说到别的应用时用它们自己的叫法（branch、fork、thread、swipe…）。

## 问题

Keine 的探索对话：主节点 + 任意嵌套的子节点 / 分支节点 / 发散节点，每个节点是一段线性对话，从另一个节点的某一轮岔出，可带一段选中的回复片段；一个探索最多 50 个节点；界面在约 1008×630 的弹窗里。现在的草稿是左侧 232px 节点树 + 右侧当前节点对话 + 每条回复下的「开节点」按钮弹出类型菜单。

想知道：现有带分支的聊天应用，
- 分支入口在哪（消息按钮、选中文字、悬停、菜单）；
- 主对话里怎么表示「这里有分支」（行内标记、计数、`< 2/3 >` 切换器、线程回复数）；
- 怎么在分支之间走（树、侧栏、画布、面包屑、标签页、分栏）；
- 分支里怎么表示岔出点 / 继承的上下文；
- 深层嵌套怎么处理；
- 有没有带类型或标签的分支。

来源只用一手资料：官方文档、源码（文件路径 + 提交）、官方 release notes。只在社区帖子/媒体里见到的说法标「未核实」。

源码快照：

| 仓库 | 提交 |
| --- | --- |
| `danny-avila/LibreChat` | `4700c44`（2026-11-20） |
| `open-webui/open-webui` | `0666e59` |
| `lobehub/lobehub`（原 lobe-chat） | `c5d79a3` |
| `earendil-works/pi` | `780bf31` |
| `socketteer/loom` | `91ca920` |
| `rpggio/obsidian-chat-stream` | `54ec1df` |
| `SillyTavern/SillyTavern` | `82b64a4` |

---

## 对比表

| 应用 | 入口 | 主对话里的分支标记 | 导航 | 分支内的岔出点 / 继承 | 嵌套 | 类型 / 标签 |
| --- | --- | --- | --- | --- | --- | --- |
| ChatGPT（未核实） | 编辑消息；消息 ⋯ 菜单「Branch in new chat」 | 编辑后的 `1/2` 箭头（2026 年起时有时无） | 箭头切换；分支是侧栏里的另一个对话 | 新对话标题以「Branch」开头 | 复制式，无层级 | 无 |
| LibreChat | 悬停按钮 fork 图标 → 弹出选项；编辑/重新生成产生兄弟 | 消息脚下 `‹ n / m ›` | 切换器；fork 后是对话列表里的新对话 | 无（新对话沿用原标题） | 切换器逐层；fork 无层级 | 无 |
| Open WebUI | 编辑 / 重新生成 | 消息下 `‹ n/m ›`（可输入页码） | 切换器 + 「Overview」标签页的节点流图，点节点跳过去 | 流图里能看到；对话里无 | 流图任意深 | 无（节点显示模型名） |
| SillyTavern | 每条消息 ⋯ 里「Create branch」「Create checkpoint」；最后一条 AI 消息左右滑 | swipe 计数 `n/m`；有 checkpoint 的消息上一面小旗 | 小旗点开 checkpoint 对话；输入框左侧菜单「Back to parent chat」 | 分支名 `原名 - Branch #n`，checkpoint 名 `Checkpoint #n` | 复制式，只记父对话 | checkpoint 必须起名 |
| TypingMind | 消息 ⋯「Fork chat from here」 | 文档未提 | 新对话 | 文档未提 | 复制式 | 无 |
| LobeChat | 助手消息右键/更多菜单「创建子话题」（开发模式）与「fork」 | 有线程的消息不可删除；会话头部有线程切换弹层 | 右侧 Portal 面板开线程；侧栏话题下列出线程 | 线程顶部是岔出那条消息 + 分隔线写明继承了什么 | 数据有 `parentThreadId`，界面列表是平的 | 线程类型 Continuation / Standalone（含不含上文） |
| pi（TUI `/tree` + p2 web） | 回复下「Branch from here」；用户消息上「Edit」；发送按钮菜单「Send in a new branch」 | 主对话里不标 | 树选择器 / 侧边「Tree」目录面板 | 分支挂在「它岔出的那一轮」下面 | 任意深，压缩缩进 | 每个分支有标签（默认取岔出消息前 200 字），可改名 |
| Loom | 生成 N 个子节点；拆分节点 | 阅读视图可显示当前节点的子节点 | 树视图 + 可视化图 + 键盘父/子/兄弟导航；hoist 子树 | 阅读视图就是祖先链拼成的正文 | 任意深，可折叠、hoist | 标签：bookmark / canonical / archived / note / pinned，各有图标和作用域 |
| obsidian-chat-stream | 选中卡片按快捷键生成回复 | 画布上的箭头 | 画布本身 | 入箭头决定上下文；「Preview context」 | 任意深 | 回复卡片紫色 |
| Msty Studio | 消息更多菜单「Edit as Branch」 | 被分叉的消息旁有分支下拉框 | 下拉框切换；mini map；Branch Explorer 并排 diff | 文档未提 | 文档未提 | 分支可加标签 |
| Slack | 消息「Reply in thread」 | 父消息下回复数 | 线程在侧面板打开（未核实，帮助页 429） | 线程顶部是父消息 | 不能嵌套：只能回复「未成线程的消息」 | 无；回复可「同时发到频道」 |

---

## 每个应用的要点

### ChatGPT（大部分未核实）

- 「Branch in new chat」在一条较早消息的 ⋯ 菜单里，原对话不变，新对话带着到这条消息为止的上下文——出自 OpenAI 社区论坛上的支持回复，官方 release notes 页面过长未能定位到原条目（[社区标签页](https://community.openai.com/tag/branching/818)），**未核实**。
- 新对话标题以「Branch」开头（媒体报道，[Tom's Guide](https://www.tomsguide.com/ai/chatgpt-just-quietly-rolled-out-a-game-changing-upgrade-heres-why-im-already-obsessed-with-it)），**未核实**。
- 编辑消息后的 `1/2` 版本箭头在 2026 年的网页更新后对部分用户消失，之后又时有时无（[社区帖](https://community.openai.com/t/chatgpt-web-update-removed-message-version-arrows-cannot-access-edited-message-history/1374666)）。官方没有文档。**结论**：ChatGPT 的分支界面是「行内版本切换器 + 复制成新对话」两套并存，且前者不稳定。

### Claude.ai（未核实，从表中略去）

- 找不到 Anthropic 帮助中心描述编辑后版本切换的页面；只有第三方教程说编辑会在消息下出现 `◀ 1/2 ▶`（[claudetopdf](https://claudetopdf.vercel.app/branch-conversations-in-claude)）。**未核实**，不作依据。

### LibreChat

- 入口：悬停在消息上出现的按钮组里有 fork 图标（`packages/chat/src/components/messages/HoverButtons.tsx` 300 行渲染 `<Fork>`），点开是一个弹层：三种复制范围（只要可见路径 / 带相关分支 / 到此为止全部）、「Start fork here」「Leave out files」「Remember」（[官方文档](https://www.librechat.ai/docs/features/fork)；实现 [`Fork.tsx`](https://github.com/danny-avila/LibreChat/blob/4700c44/packages/chat/src/components/messages/Fork.tsx) 148–154、188 行）。勾「Remember」后 fork 图标直接执行不再弹层。
- 分支标记：编辑/重新生成产生的兄弟在消息脚下显示 `‹ n / m ›`，只有兄弟数 > 1 时出现（[`SiblingSwitch.tsx`](https://github.com/danny-avila/LibreChat/blob/4700c44/packages/chat/src/components/messages/SiblingSwitch.tsx) 34–70 行；`MessageFooter.tsx` 68 行）。用户消息右对齐，切换器放在气泡外侧（`MessageFooter.tsx` 63–66 行注释）。
- fork 结果是对话列表里的一个新对话，沿用原标题；没有树视图，也不在新对话里标出来源（文档未提，代码里也没找到树视图组件）。

### Open WebUI

- 分支标记：回复下面 `‹ n/m ›`，中间的数字是可编辑的输入框，能直接跳到第几个（`src/lib/components/chat/Messages/ResponseMessage.svelte` 1048–1085 行，@`0666e59`）。
- 全局视图：「Overview」是聊天旁的一种标签页（`src/lib/components/chat/Tabs/tabs.ts` 的 `TabKind` 含 `'overview'`），内容是用 `@xyflow/svelte` 画的消息树流图，可切换竖排/横排，自动 `fitView` 到当前消息（`src/lib/components/chat/Overview/View.svelte` 31–62 行）。每个节点卡片显示模型名/用户名与一两行正文预览（`Overview/Node.svelte`）。点节点：把主对话切到该消息所在路径并显示它（`src/lib/components/chat/Tabs.svelte` 2084–2095 行 `showMessage(node.data.message, true)`）。
- 即「对话里只放切换器，整体结构交给一张独立的图」。

### SillyTavern

- 入口：每条消息右侧 ⋯ 展开的按钮里有「Create checkpoint」（旗帜图标）和「Create branch」（分叉图标）（`public/index.html` 7425–7426 行，@`82b64a4`；[官方文档](https://docs.sillytavern.app/usage/core-concepts/chatfilemanagement/)）。branch 复制并切过去，checkpoint 复制、要求起名、不切换——文档类比为「在新标签页 / 后台标签页打开链接」。
- 标记：有 checkpoint 的消息上显示一面小旗，悬停显示 checkpoint 名，点击打开该 checkpoint 对话（`index.html` 7429 行；`scripts/bookmarks.js` 311 行 `updateBookmarkDisplay`）。
- 返回：输入框左侧菜单「Back to parent chat」（`index.html` 8151–8153 行；文档）。
- 命名：branch 自动命名为 `原名 - Branch #n`，checkpoint 为 `Checkpoint #n`（`bookmarks.js` 47、187–214 行）。父子关系只靠聊天文件名，改名会断链（文档）。
- swipe：最后一条 AI 消息左右箭头 + `n/m` 计数（`index.html` 7402、7467–7468 行；`script.js` 9138 行）；点计数可打开 swipe 历史选择器，历史 swipe 也能拿来开 branch（`scripts/swipe-picker.js` 注释「supports historical AI messages for inspection and branching」）。

### TypingMind

- 消息旁 ⋯ 菜单「Fork chat from here」，生成一个新对话带着旧上下文，原对话不变（[官方文档](https://docs.typingmind.com/chat-management/fork-chats)）。文档没写新对话的命名和来源标记。

### LobeChat（lobehub）

- 数据：`threads` 表有 `type`、`sourceMessageId`（从哪条消息岔出）、`parentThreadId`（父线程）（[`packages/database/src/schemas/topic.ts`](https://github.com/lobehub/lobehub/blob/c5d79a3/packages/database/src/schemas/topic.ts) 140–170 行）。线程类型 `continuation` / `standalone` / `isolation` / `eval`（`packages/types/src/topic/thread.ts` 8–13 行）。
- 入口：助手消息的更多菜单里，「fork」（复制成独立话题，已正式发布）和「branching」（话题内建线程，中文「创建子话题」，仍只在开发模式下出现）（`src/features/Conversation/hooks/useChatItemContextMenu.tsx` 166–169 行；`locales/zh-CN/common.json` 68 行）。
- 新建线程的面板头部有一个开关「包含话题上下文」，在 Continuation 和 Standalone 之间切换（`src/features/Portal/Thread/Header/New.tsx`；`locales/zh-CN/thread.json` 3 行）——**入口处就让用户选这一枝带多少上下文**。
- 岔出点：线程在右侧 Portal 面板里打开，顶部只渲染主对话里那一条岔出消息，下面一条带分叉图标的分隔线说明继承了什么：「从此消息分叉，已继承至此的主对话上下文」/「从此消息分叉，仅继承上方这条消息」（`src/features/Portal/Thread/Chat/ThreadDivider/index.tsx` 注释与 30–32 行；`locales/zh-CN/chat.json` 3149–3150 行）。
- 主对话里：有线程的消息禁止删除（菜单项置灰并提示原因，`useChatListActionsBar.tsx` 80–87 行）；会话头部有一个线程切换弹层（`src/routes/(main)/agent/features/Conversation/Header/Tags/ThreadSwitcher.tsx`）。侧栏在话题下面列出线程，列表最高约 9 行、超出滚动（`src/routes/(main)/group/_layout/Sidebar/Topic/TopicListContent/ThreadList/index.tsx` 注释）。虽然有 `parentThreadId`，界面列表是平的，没有按父线程缩进。

### pi（`earendil-works/pi`）

两个界面：coding-agent 的终端 `/tree` 选择器，和 p2 web 端的树面板。

- 分支方式三选一：`/tree`（同一会话内换位置，相关的备选放一起）、`/fork`（从较早的用户消息开新会话）、`/clone`（复制当前分支）（`packages/coding-agent/docs/sessions.md` 36–48 行）。离开一个分支时可以把它总结成摘要挂到进入的分支上（同文件 48 行）。
- **TUI 树选择器**（`packages/coding-agent/src/modes/interactive/components/tree-selector.ts`）：
  - 压缩缩进：单子节点的链保持同一缩进，只有父节点有多个孩子时才缩进一级（205–210 行注释）；
  - 当前路径上的条目前面加 `•`（745–746 行），当前分支排在最前（216 行注释）；
  - 分支标签显示为 `[label]`（748 行）；
  - 可折叠子树 `⊟/⊞`（730 行），过滤模式 `default / no-tools / user-only / labeled-only …`（98 行）；
  - 横向视口：树沟槽固定可见，深层时正文向左平移（58–64 行注释）——**深嵌套在窄宽度下的处理办法**。
- **p2 web 树面板**（[`apps/p2/docs/tree.md`](https://github.com/earendil-works/pi/blob/780bf31/apps/p2/docs/tree.md)「Tree panel」节）：
  - 顶栏的分支按钮打开侧面板，形式是转录旁边的**目录（table of contents）**；
  - 一轮一项：用户消息一行，最终回答作为第二行；
  - 当前分支的各轮在顶层、主色；其他分支作为列表**嵌在它岔出的那一轮下面**，每深一级缩进一步、次要色；
  - 指示条标出转录里正在看的那一轮，面板随之滚动；
  - 标签写在每个分支最后一轮旁，主分支读作「Current: …」；
  - 面板只负责移动：编辑和「Branch from here」只在转录里，不在面板里重复（作者明确的决定）；点别的分支的某一轮会先把会话切到那个分支再显示该轮；
  - 侧栏不列分支（「一个会话是一项」，树面板负责切换）。
- 入口（同文件「Where」节）：「Edit」在用户消息旁（悬停出现，触屏常显），「Branch from here」在回答下面；最后一条下面不放，因为那里直接用输入框的「Send in a new branch」（发送按钮的菜单 / Alt+Enter）。
- 标签（「Labels」节）：新分支创建时存一个标签，取岔出消息的前 200 字；可改名，1–200 字；不随后续消息变化。

### Loom（`socketteer/loom`）

- 「多重宇宙」写作工具，两种主视图：阅读模式（把祖先链拼成线性正文 + 树导航条）和树视图（可展开折叠、改拓扑、原地编辑）；另有可视化模式（`j` 切换）（[README](https://github.com/socketteer/loom/blob/91ca920/README.md) Features 与快捷键节）。
- 导航全靠键盘：左右上下 = 父 / 子 / 兄弟，书签前后跳，checkpoint，搜祖先 / 搜全树；`Hoist`（`Alt-h`）把视图限定在某个子树（README「Navigate」）。节点有「visited」状态（`model.py` 275、700 行）。
- 标签带类型：默认 `bookmark`、`canonical`（作用域 ancestry）、`archived`（隐藏）、`note`（隐藏）、`pinned`，每种有图标、快捷键、作用域（node / subtree / ancestry）、是否隐藏 / 只显示（`model.py` 228–262、1566 行起）。这是唯一见到的**给树节点打带图标的类型标签并据此过滤**的实现。

### obsidian-chat-stream

- 整个对话就是 Obsidian Canvas 上的卡片；选中卡片生成回复，回复是连上箭头的紫色卡片；回到更早的卡片再生成就开了新分支；**入箭头决定什么进上下文**，「Preview context」本地预览将发送的内容（[README](https://github.com/rpggio/obsidian-chat-stream/blob/54ec1df/README.md)）。
- 没有「当前分支」概念，空间位置就是结构。

### Msty Studio

- 用户或助手消息的更多菜单里「Edit as Branch」，保存后成新分支、原路径保留；被分叉的消息旁有分支下拉框，可切换路径、给分支加标签、打开 Branch Explorer；mini map 里也有同样的下拉框；Branch Explorer 能把两个分支并排 diff（[Branch Explorer 文档](https://docs.msty.ai/studio/conversations/branch-explorer)、[Responses 文档](https://docs.msty.ai/studio/conversations/responses)）。文档没给 Explorer 的具体布局。

### Slack（经典线程）

- `thread_ts`：「Provide another message's `ts` value to make this message a reply. Avoid using a reply's `ts` value; use its parent instead.」——线程**只有一层**，不能在线程里再开线程；`reply_broadcast` 让回复同时出现在频道里（[chat.postMessage](https://docs.slack.dev/reference/methods/chat.postMessage)；术语见 [Messaging](https://docs.slack.dev/messaging/#threading)）。
- 父消息下显示回复数、线程在侧面板打开、「Threads」汇总视图——帮助中心页面抓取被限流（429），**未核实**。Discord 帮助中心 403，略去。

### 略去

Google AI Studio「Branch from here」、Gemini、Perplexity、Poe：只找到论坛帖或媒体报道，没有官方文档或源码，未纳入对比。

---

## 归纳：五种渲染范式

### 范式 1：行内版本切换器（`‹ n/m ›`）

- **怎么做**：分支是同一父节点下的兄弟；岔点消息下放一个计数切换器，一次只显示一条路径，切换后下方整段对话换掉。
- **谁用**：LibreChat、Open WebUI、SillyTavern swipe、ChatGPT（未核实）、Msty（下拉框版）。
- **适合 Keine 吗**：不适合作主方案。它表达的是「同一问题的几个备选」，兄弟之间互相替代；Keine 的节点是**并存的、带类型的、从某一轮岔出的另一条线**，不是这一轮的替代版本。嵌套时要在多层切换器之间来回点，结构完全看不见。
- **优点**：零额外空间；直觉。**缺点**：看不到全局；没法表达类型；深了就迷路。

### 范式 2：复制成新对话 + 回指

- **怎么做**：从某条消息复制出一个新的顶层对话，放进对话列表；靠标题前缀（「Branch」「- Branch #n」）或元数据回指父对话；有的在原消息上留标记（SillyTavern 小旗）。
- **谁用**：ChatGPT「Branch in new chat」、LibreChat fork、TypingMind、SillyTavern branch/checkpoint、LobeChat fork、pi `/fork`。
- **适合 Keine 吗**：不适合。Keine 的节点属于一个探索、上限 50、要看出树形；拍平进列表就丢了结构。可借鉴的只有 SillyTavern 的**在岔出那条消息上留一个可点的标记**和「Back to parent」。
- **优点**：实现简单，分支彼此独立。**缺点**：结构丢失，深层分支要靠命名找。

### 范式 3：侧面板线程（主对话 + 一个线程面板）

- **怎么做**：主对话不动，点某条消息的入口在旁边面板打开一条线程；面板顶部放岔出的那条消息，下面是线程内容；主对话里在父消息上显示「有线程 / 回复数」。
- **谁用**：Slack（线程只有一层）、LobeChat Portal 线程（带「含不含上文」类型和继承说明分隔线）。
- **适合 Keine 吗**：部分适合。岔出点展示（LobeChat 的「岔出消息 + 说明继承了什么的分隔线」）可以直接借。但它天生只有两栏、一层：1008 宽的弹窗再分两栏对话，每栏不到 500px；嵌套到第三层就没地方放。
- **优点**：始终能看到父对话与岔出点。**缺点**：嵌套差；空间紧。

### 范式 4：大纲树 / 目录面板 + 单栏对话

- **怎么做**：旁边一个缩进列表展示整棵树，选中一项在主区显示它所在的线性对话。pi 的变体最讲究：按「轮」列出，子分支嵌在它岔出的那一轮下面；只在真正分叉处缩进；当前路径加标记；分支有标签；面板只负责导航，岔出动作留在对话里；可折叠、深层时横向平移。
- **谁用**：pi（TUI `/tree`、p2 树面板）、Loom 树视图（加 hoist、类型标签）、LobeChat 侧栏线程列表（平铺版）。
- **适合 Keine 吗**：最适合。50 个节点的缩进列表在 232px 宽、约 560px 高里放得下（折叠后更少）；类型可以用图标 / 颜色 / 前缀放在每行（像 Loom 的标签图标、pi 的 `[label]`）；任意嵌套靠压缩缩进 + 折叠撑住。
- **优点**：结构、当前位置、类型一眼可见；读的区域仍是正常宽度的线性对话。**缺点**：树和对话要靠高亮与滚动联动才不脱节；节点多时标题要短（需要标签）。

### 范式 5：画布 / 节点图

- **怎么做**：每条消息或每个分支是一张卡片，连线表示父子；平移缩放浏览，点卡片进入。
- **谁用**：Open WebUI Overview（作为对话旁的辅助标签页）、obsidian-chat-stream（画布就是对话本身）、Loom 可视化模式、Msty mini map（文档未给细节）。
- **适合 Keine 吗**：作主界面不适合：630 高的弹窗里，卡片里读长回复很吃力，阅读才是 Keine 的主要动作。作**辅助的全景视图**（像 Open WebUI 那样切一个「全貌」标签）可以，但 ≤50 个节点用范式 4 的列表已经看得全，收益有限。
- **优点**：结构最直观，适合比较多个方向。**缺点**：占空间、读文字差、需要布局算法和平移缩放交互。

---

## 对 Keine 的建议（简短）

1. **主结构沿用草稿的范式 4**（左树 + 右单栏对话），按 pi 的树面板打磨：树里每个节点一行（类型图标 + 标签），子节点**挂在它岔出的那一轮下面**而不是只挂在父节点下；只在真正分叉处缩进，可折叠；当前节点与「当前祖先链」高亮；树只负责跳转，「开节点」只在对话里。
2. **类型放在树行和节点头上**：三种类型各一个图标 / 颜色（参照 Loom 的带图标标签）；标签默认取岔出那一轮或选中片段的前若干字（参照 pi 存一次、可改名）。
3. **节点顶部画岔出点**：参照 LobeChat 的 ThreadDivider——先显示父节点岔出的那一轮（折叠成一两行）与选中的片段，再一条分隔线写明「继承到这一轮为止的 X 节点讨论」，并带一串可点的祖先面包屑回到父节点（SillyTavern 的「Back to parent」）。
4. **对话里留行内标记**：某一轮下开过节点时，在这一轮回复下显示一排小标签（类型图标 + 标签，或「3 个节点」），点即跳转——参照 SillyTavern 的小旗和 Slack 的回复数。选中片段开出的节点，可在回复正文里把该片段轻度标出。
5. **不用 `‹ n/m ›` 切换器**表示节点（它表示的是替代版本）；画布全景暂不做，等节点数常态接近上限再考虑。
