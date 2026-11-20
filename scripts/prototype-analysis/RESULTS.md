# 第一轮分析原型：结果（PROTOTYPE，不合并）

wayfinder ticket #99「用样例课程跑第一轮分析」。脚本 `run.py` 按 ADR 0008、#94、#95、#97、#113 的约定直接调用 DeepSeek（`deepseek-flash`）跑分析，课程工具在本地执行，`web_search` / `web_fetch` 接 DeepSeek Search / Fetch API。

    COURSE=wiki TAG=-v5 REPEATS=2 python3 scripts/prototype-analysis/run.py

`COURSE=wiki` 用 5 篇中文维基百科条目（拜占庭将军问题、CAP 定理、二阶段提交、Paxos、Raft）组成的小课程，数据只放在本地 `.scratch/analysis-proto/wiki/`，不提交（CC BY-SA）。不设 `COURSE` 时用 demo 样例课程（`scripts/demo/seed.py` 的 Agent 专栏，每讲正文相同）。key 从 `.scratch/.env` 读。

## 五轮

| 轮次 | 课程 / 结构 | 合格 | 发现 |
|---|---|---|---|
| 1 | demo，旧结构（三段 + 来源在概念阐述末尾） | 12/12 | 每次都 `read_lesson` 重读本讲；1 次来源写到 `</exp_body>` 外；链接被改写（raw → blob/main），1 次来自模型记忆 |
| 2 | Raft，固定段名（含义/在本讲中/延伸/来源），加 `web_fetch` | 15/16 | 「任期」「已存储」16/16 按 Raft 语义解释；1 次把提示词里段名后的说明抄进标题；72 条链接 3 条不在搜索结果（2 条签名临时链接被改坏） |
| 3 | #113 新结构：标题/简介/含义（本义、在本讲中两个小标题），无链接，可画 Mermaid | 16/16 | 16 份都画图且是横向；更快（思考关 6–9 s，输入约 6–7k token） |
| 4 | 去掉小标题；本义不讨论歧义；图按需、竖向、最多一张 | 16/16 | 5 份用单独一行的加粗字充当小标题；1 个标题照抄「原文写法」 |
| 5 | 定稿提示词 | 16/16 | 无；7/16 画图，全部竖向、全部渲染成功；含义 500–744 汉字；思考关 6–9 s，思考开 9–17 s |

## 其他实测

- `deepseek_web_search` 和函数工具一起声明：非思考模式 400（client tools can only be used together with `deepseek_web_search` in thinking mode）；思考模式请求通过，但模型不调用它、回答「没有可用的联网搜索工具」。
- 思考模式下工具循环里原样回传 `reasoning_content`，16×5 次运行无 400。只测了一次运行内部，跨轮未测。
- 思考开关质量相当；思考关耗时约一半，正文约 1 s 开始流出（思考开 3–9 s）。
