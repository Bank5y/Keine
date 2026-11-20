#!/usr/bin/env python3
"""PROTOTYPE, throwaway (wayfinder ticket #99「用样例课程跑第一轮分析」).

Runs the first-round analysis against the demo sample course with DeepSeek,
following ADR 0008 and the decisions in #94 (context), #95 (tools), #97
(output tags, step limit, auto-remedy). Not production code: no tests, no
error handling beyond what keeps it running.

    python3 scripts/prototype-analysis/run.py            # full matrix
    python3 scripts/prototype-analysis/run.py coexist    # can deepseek_web_search sit next to function tools?

Reads DEEPSEEK_API_KEY from .scratch/.env. Writes .scratch/analysis-proto/.
"""
import json, os, re, sys, time, pathlib, requests

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / ".scratch/analysis-proto" / (os.environ.get("COURSE", "demo") + os.environ.get("TAG", ""))
for line in (ROOT / ".scratch/.env").read_text().splitlines():
    if "=" in line: k, v = line.split("=", 1); os.environ.setdefault(k, v)
KEY = os.environ["DEEPSEEK_API_KEY"]
BASE = "https://api.deepseek.com"
MODEL = os.environ.get("MODEL", "deepseek-flash")
H = {"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"}
MAX_TOOL_STEPS = 6
REPEATS = int(os.environ.get("REPEATS", "3"))

# ---- sample course: same text as scripts/demo/seed.py agent_pkg() ----------
def para(topic, n):
    base = [f"{topic}不是一个新概念，但在大模型出现之后，它第一次变得真正可用。这一讲我们从一个具体的问题出发，看看它在工程上到底意味着什么。",
            "很多团队在第一次落地时都会遇到同一个坑：把所有逻辑塞进一个提示词里，期待模型自己想清楚。结果是效果不稳定，出了问题也很难排查。",
            "更好的做法是把任务拆成几个职责清晰的步骤，每一步只做一件事，并且有明确的输入和输出。这样不仅更容易调试，也更容易评估每一步的质量。",
            "下面这段代码展示了一个最小的实现。注意我们没有引入任何框架，所有的控制流都是显式的。"]
    return "\n\n".join(base[:n])

CHAPTERS = [
  ("opening", "开篇", [("l00", "开篇词｜为什么 Agent 值得认真对待")]),
  ("intent", "意图识别", [("l01", "01｜用户输入规范化"), ("l02", "02｜三层意图识别"), ("l03", "03｜槽位抽取与追问")]),
  ("plan", "规划与执行", [("l04", "04｜任务拆解：从目标到步骤"), ("l05", "05｜工具调用的边界"), ("l06", "06｜失败重试与回退")]),
  ("memory", "记忆", [("l07", "07｜短期记忆与上下文窗口"), ("l08", "08｜长期记忆的写入策略")]),
]
COURSE = {"title": "Agent 系统设计 40 讲",
          "intro": "从意图识别、任务规划到记忆与评估，系统讲解如何把大模型做成可靠的 Agent 系统。每一讲都配有可运行的最小实现。",
          "author": "陈明远：前大厂搜索推荐负责人，现专注于 Agent 平台建设。"}
def lesson_md(title):
    return (f"# {title}\n\n{para('Agent', 3)}\n\n## 一个最小的例子\n\n{para('Agent', 4).split(chr(10)*2)[3]}\n\n"
            "```go\nfunc run(ctx context.Context, goal string) error {\n\tplan, err := planner.Plan(ctx, goal)\n\tif err != nil {\n\t\treturn err\n\t}\n"
            "\tfor _, step := range plan.Steps {\n\t\tif err := executor.Do(ctx, step); err != nil {\n\t\t\treturn retry(ctx, step, err)\n\t\t}\n\t}\n\treturn nil\n}\n```\n\n"
            "![执行流程](images/flow.png)\n\n## 小结\n\n- 把任务拆成职责清晰的步骤\n- 每一步都有明确的输入和输出\n- 失败时有可预期的回退路径\n\n> 思考题：在你的业务里，哪一步最容易出错？为什么？\n")
LESSONS = {lid: {"title": lt, "chapter": ct, "md": lesson_md(lt)} for _, ct, ls in CHAPTERS for lid, lt in ls}
if os.environ.get("COURSE") == "wiki":  # 5 zh.wikipedia articles as a mini course (CC BY-SA), local only
    W = ROOT / ".scratch/analysis-proto/wiki"
    CHAPTERS = [("basics", "基础问题", [("w0", "01｜拜占庭将军问题"), ("w1", "02｜CAP 定理")]),
                ("protocols", "共识协议", [("w2", "03｜二阶段提交"), ("w3", "04｜Paxos 算法"), ("w4", "05｜Raft")])]
    COURSE = {"title": "分布式共识入门", "intro": "从拜占庭将军问题和 CAP 定理出发，讲清二阶段提交、Paxos 与 Raft 的设计取舍。",
              "author": "示例作者：分布式存储工程师。"}
    LESSONS = {lid: {"title": lt, "chapter": ct, "md": (W / f"{lid[1]}.md").read_text()} for _, ct, ls in CHAPTERS for lid, lt in ls}
TOC = "\n".join(f"- {ct}\n" + "\n".join(f"  - [{lid}] {lt}" for lid, lt in ls) for _, ct, ls in CHAPTERS)

def sections(md):
    out, cur = [], None
    for line in md.splitlines():
        if re.match(r"#{2,4} ", line): cur = line.lstrip("#").strip(); out.append([cur, []])
        elif cur: out[-1][1].append(line)
    return {t: "\n".join(b).strip() for t, b in out}

# ---- tools (Keine's own, executed here) ------------------------------------
TOOLS = [
  {"type": "function", "function": {"name": "read_lesson", "description": "读本课程某一讲的正文，可只读一个小节。讲 ID 见课程目录。",
    "parameters": {"type": "object", "properties": {"lesson_id": {"type": "string"}, "section": {"type": "string", "description": "小节标题，可选"}}, "required": ["lesson_id"]}}},
  {"type": "function", "function": {"name": "list_sections", "description": "列出某一讲的小节目录。",
    "parameters": {"type": "object", "properties": {"lesson_id": {"type": "string"}}, "required": ["lesson_id"]}}},
  {"type": "function", "function": {"name": "search_course", "description": "在本课程所有讲里按关键词查找，返回带位置（讲 › 小节）的片段。",
    "parameters": {"type": "object", "properties": {"keyword": {"type": "string"}}, "required": ["keyword"]}}},
  {"type": "function", "function": {"name": "web_fetch", "description": "读取一个网页的正文（Markdown）。只能读本次搜索结果里出现过的链接。",
    "parameters": {"type": "object", "properties": {"url": {"type": "string"}}, "required": ["url"]}}},
  {"type": "function", "function": {"name": "web_search", "description": "联网搜索，返回标题、链接和摘要。术语和概念默认应该查。",
    "parameters": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}}},
]

SEEN_URLS = set()
def norm(u): return u.split("#")[0].rstrip("/")
def run_tool(name, args):
    if name == "read_lesson":
        l = LESSONS.get(args.get("lesson_id"))
        if not l: return "没有这一讲"
        if args.get("section"): return sections(l["md"]).get(args["section"], "没有这个小节")
        return l["md"]
    if name == "list_sections":
        l = LESSONS.get(args.get("lesson_id"))
        return "\n".join(sections(l["md"])) if l else "没有这一讲"
    if name == "search_course":
        kw, hits = args.get("keyword", ""), []
        for lid, l in LESSONS.items():
            for sec, body in [("（开头）", l["md"].split("\n## ")[0])] + list(sections(l["md"]).items()):
                i = body.find(kw)
                if i >= 0: hits.append(f"{l['title']} › {sec}: …{body[max(0,i-40):i+60]}…")
        return "\n".join(hits[:10]) or "没有找到"
    if name == "web_search":
        r = requests.post(f"{BASE}/v1/tools/search", headers=H, json={"query": args.get("query", "")[:128], "count": 5}, timeout=30)
        if r.status_code != 200: return f"搜索失败：HTTP {r.status_code} {r.text[:200]}"
        res = r.json().get("results", [])
        SEEN_URLS.update(norm(x.get("url", "")) for x in res)
        return "\n\n".join(f"[{x.get('title')}]({x.get('url')})\n{(x.get('summary') or x.get('snippet') or '')[:400]}" for x in res) or "没有结果"
    if name == "web_fetch":
        if norm(args.get("url", "")) not in SEEN_URLS: return "只能读取本次搜索结果里出现过的链接"
        r = requests.post(f"{BASE}/v1/tools/fetch", headers=H, json={"url": args["url"]}, timeout=60)
        if r.status_code != 200: return f"读取失败：HTTP {r.status_code} {r.text[:200]}"
        return (r.json().get("content") or "")[:6000]
    return "未知工具"

# ---- prompt and context (#94 sources, #95 positioning, #97 format) ---------
SYSTEM_V2 = None  # v2 (含义/在本讲中/延伸/来源) lives in git history
SYSTEM = """你是 Keine 阅读页里的探索助手。读者在课程正文里选中了一段文字，请求「分析」。

分析是你整理、总结、搜索资料之后的答复：课程内容、网络资料和你自己的知识都是来源。遇到术语和概念，默认应该用 web_search 查网络资料，需要读某个结果全文时用 web_fetch。本讲全文已经在下面的上下文里，不要再用 read_lesson 读本讲；需要课程其他讲的内容时才用 read_lesson / list_sections / search_course。需要查的时候直接调用工具，不要先写正文。

最终答复的格式（只有这一种形式被读者看到）：
<exp_title>术语</exp_title>
<exp_intro>术语简介</exp_intro>
<exp_body>
含义（Markdown，不写任何标题）
</exp_body>

要求：
- 术语：选中的是术语时就是这个术语，原文写法放在括号里，例如「任期（term）」；选中的是句子时，归纳成这句话讲的核心概念的名称。20 字以内。
- 术语简介：1–2 句，80 字以内，纯文本，不含标题或列表。
- 含义：不写任何标题，也不要用单独一行的加粗文字充当小标题；先后两部分直接用段落自然衔接：
  - 先直接解释这个概念本身的标准含义，就事论事；不要讨论字面意思、译名或它可能被误解成什么。
  - 再结合这一讲的上下文解释它在这里具体指什么、在这一讲的论述或机制里扮演什么角色。
  - 合计 300–700 字，可以用列表。
- 图：只有流程、状态变化或结构关系用文字难以说清时才画，一份最多一张；用 ```mermaid 代码块，只能是 flowchart TD（竖向）、sequenceDiagram 或 stateDiagram-v2，节点文字用中文并加引号。
- 这一次只写含义：不写与其他讲的关系、课程外的相似内容，也不列参考文献或任何链接。
- 语言跟随这一讲正文的语言。
- 标签外不要写任何文字。"""

def first_turn(lid, selection, section, anchor=None):
    l = LESSONS[lid]
    anchor = anchor or selection
    assert anchor in l["md"], anchor
    full = l["md"].replace(anchor, anchor.replace(selection, f"<selected>{selection}</selected>", 1), 1)
    return f"""## 课程信息
课程：{COURSE['title']}
简介：{COURSE['intro']}
作者：{COURSE['author']}

## 课程目录
{TOC}

## 选中的原文
位置：{l['chapter']} › {l['title']} › {section}
「{selection}」

## 本讲全文（选中处用 <selected> 标出）
{full}"""

CASES_WIKI = [
  ("term", "w4", "任期", "领袖选举"),
  ("term2", "w4", "已存储", "领袖死机", "每个已存储指令"),
  ("sentence", "w4", "Raft每个服务器的超时期限是随机的，这降低伺服务同时竞选的几率，也降低因两个竞选人得票都不过半而选举失败的几率。", "领袖选举"),
  ("cross", "w4", "集群内的节点都对选举出的领袖采取信任，因此Raft不是一种拜占庭容错算法。", "（开头）"),
]
CASES = [
  ("term", "l05", "控制流", "一个最小的例子"),
  ("sentence", "l05", "很多团队在第一次落地时都会遇到同一个坑：把所有逻辑塞进一个提示词里，期待模型自己想清楚。", "（开头）"),
]

# ---- model call (one streamed step) ----------------------------------------
def stream_step(messages, thinking, tools=True):
    body = {"model": MODEL, "messages": messages, "stream": True, "stream_options": {"include_usage": True},
            "thinking": {"type": "enabled" if thinking else "disabled"}}
    if tools: body["tools"] = TOOLS
    t0, first = time.time(), None
    content, reasoning, calls, usage, finish = "", "", {}, None, None
    with requests.post(f"{BASE}/chat/completions", headers=H, json=body, stream=True, timeout=300) as r:
        if r.status_code != 200: raise RuntimeError(f"HTTP {r.status_code}: {r.text[:500]}")
        for raw in r.iter_lines(decode_unicode=True):
            if not raw or not raw.startswith("data: "): continue
            d = raw[6:]
            if d == "[DONE]": break
            ch = json.loads(d)
            if ch.get("usage"): usage = ch["usage"]
            for c in ch.get("choices", []):
                dl = c.get("delta") or {}
                if dl.get("content"):
                    first = first or time.time() - t0; content += dl["content"]
                if dl.get("reasoning_content"): reasoning += dl["reasoning_content"]
                for tc in dl.get("tool_calls") or []:
                    e = calls.setdefault(tc.get("index", 0), {"id": "", "name": "", "args": ""})
                    e["id"] = tc.get("id") or e["id"]
                    fn = tc.get("function") or {}
                    if fn.get("name") and not e["name"]: e["name"] = fn["name"]
                    e["args"] += fn.get("arguments") or ""
                finish = c.get("finish_reason") or finish
    return {"content": content, "reasoning": reasoning, "calls": [calls[i] for i in sorted(calls)],
            "usage": usage, "finish": finish, "secs": time.time() - t0, "ttft": first}

def assistant_msg(step):
    m = {"role": "assistant", "content": step["content"] or None}
    if step["reasoning"] or step["calls"]: m["reasoning_content"] = step["reasoning"]  # DeepSeek thinking mode needs it back
    if step["calls"]:
        m["tool_calls"] = [{"id": c["id"], "type": "function", "function": {"name": c["name"], "arguments": c["args"]}} for c in step["calls"]]
    return m

# ---- validation (#97 §6) ---------------------------------------------------
TAG = re.compile(r"<exp_title>(.*?)</exp_title>\s*<exp_intro>(.*?)</exp_intro>\s*<exp_body>(.*?)</exp_body>", re.S)
def validate(text):
    m = TAG.search(text)
    if not m: return {"ok": False, "why": "no complete tag block"}
    t, i, b = (x.strip() for x in m.groups())
    outside = (text[:m.start()] + text[m.end():]).strip()
    probs = []
    if not t: probs.append("empty title")
    if not i: probs.append("empty intro")
    if re.search(r"^\s*(#|[-*] |\d+\. )", i, re.M): probs.append("intro has heading/list")
    if len(t) > 100: probs.append("title > 100")
    if len(i) > 300: probs.append("intro > 300")
    heads = re.findall(r"^## (.+?)\s*$", b, re.M)
    if re.search(r"^#{1,6} ", b, re.M): probs.append("headings in body")
    if re.search(r"^\*\*[^*\n]{2,20}\*\*[：:]?\s*$", b, re.M): probs.append("bold pseudo-heading")
    if re.search(r"flowchart\s+(LR|RL)|graph\s+(LR|RL)", b): probs.append("horizontal flowchart")
    if b.count("```mermaid") > 1: probs.append("more than one diagram")
    if re.search(r"https?://", b): probs.append("links in body")
    if re.search(r"https?://", outside): probs.append("links outside tags")
    return {"ok": not probs, "why": ", ".join(probs), "heads": heads, "title": t, "intro": i, "body": b, "outside": outside,
            "len": {"title": len(t), "intro": len(i), "body": len(b)}}

def analyse(case, thinking):
    kind, lid, sel, sec, *anchor = case
    SEEN_URLS.clear()
    msgs = [{"role": "system", "content": SYSTEM}, {"role": "user", "content": first_turn(lid, sel, sec, *anchor)}]
    trace, tool_steps, drafts_discarded, remedy = [], 0, 0, False
    while True:
        last = tool_steps >= MAX_TOOL_STEPS
        if tool_steps == MAX_TOOL_STEPS - 1: msgs.append({"role": "user", "content": "还剩 1 次查询。"})
        if last: msgs.append({"role": "user", "content": "已达查询上限，请按格式作答。"})
        st = stream_step(msgs, thinking, tools=not last)
        trace.append(st)
        msgs.append(assistant_msg(st))
        if not st["calls"]: break
        if "<exp_" in st["content"]: drafts_discarded += 1  # would trigger delta reset
        tool_steps += 1
        for c in st["calls"]:
            try: args = json.loads(c["args"] or "{}")
            except json.JSONDecodeError: args = {}
            c["result"] = run_tool(c["name"], args)
            msgs.append({"role": "tool", "tool_call_id": c["id"], "content": c["result"][:8000]})
    v = validate(trace[-1]["content"])
    if not v.get("title") and "no complete" in v["why"]:
        remedy = True
        msgs.append({"role": "user", "content": "上一次答复没有按 <exp_title>/<exp_intro>/<exp_body> 格式输出。请只按格式重新作答，标签外不要写任何文字。"})
        st = stream_step(msgs, thinking, tools=False); trace.append(st); v = validate(st["content"])
    urls = [norm(u) for u in re.findall(r"\]\((https?://[^)\s]+)\)", v.get("body") or "")]
    return {"kind": kind, "selection": sel, "links": len(urls), "links_not_from_search": [u for u in urls if u not in SEEN_URLS], "thinking": thinking, "tool_steps": tool_steps, "drafts_discarded": drafts_discarded,
            "remedy": remedy, "valid": v, "steps": [{k: s[k] for k in ("content", "finish", "secs", "ttft", "usage")} |
            {"reasoning_len": len(s["reasoning"]), "calls": s["calls"]} for s in trace]}

def coexist():
    body = {"model": MODEL, "messages": [{"role": "user", "content": "搜索一下 2026 年 Agent 框架的新闻，并读一下课程第 5 讲。"}],
            "tools": [{"type": "deepseek_web_search"}] + TOOLS, "thinking": {"type": "disabled"}}
    r = requests.post(f"{BASE}/chat/completions", headers=H, json=body, timeout=120)
    print(r.status_code, r.text[:1500])

def main():
    if sys.argv[1:] == ["coexist"]: return coexist()
    OUT.mkdir(parents=True, exist_ok=True)
    results = []
    for case in (CASES_WIKI if os.environ.get("COURSE") == "wiki" else CASES):
        for thinking in (False, True):
            for n in range(REPEATS):
                print(f"{case[0]} thinking={thinking} #{n+1} …", flush=True)
                try: res = analyse(case, thinking)
                except Exception as e: res = {"kind": case[0], "thinking": thinking, "error": str(e)}
                results.append(res)
                (OUT / "results.json").write_text(json.dumps(results, ensure_ascii=False, indent=1))
    write_report(results)

def write_report(results):
    L = ["# 第一轮分析原型：结果（PROTOTYPE）", "", f"模型 `{MODEL}`，步数上限 {MAX_TOOL_STEPS}，每格 {REPEATS} 次。", "",
         "| 选区 | 思考 | 合格 | 链接/非搜索来源 | 工具步数 | 工具 | 作废草稿 | 补救 | 标签外文字 | 标题/简介/正文字数 | 耗时 s | 首字 s | 输入/缓存/输出 tok |", "|"+"---|"*13]
    for r in results:
        if "error" in r: L.append(f"| {r['kind']} | {r['thinking']} | ERROR {r['error'][:80]} |" + " |"*9); continue
        v, steps = r["valid"], r["steps"]
        tools = ", ".join(c["name"] for s in steps for c in s["calls"])
        u = [s["usage"] or {} for s in steps]
        tok = f"{sum(x.get('prompt_tokens',0) for x in u)}/{sum(x.get('prompt_cache_hit_tokens',0) for x in u)}/{sum(x.get('completion_tokens',0) for x in u)}"
        ln = v.get("len", {})
        L.append(f"| {r['kind']} | {'是' if r['thinking'] else '否'} | {'✅' if v['ok'] else '❌ '+v['why']} | {r.get('links')}/{len(r.get('links_not_from_search',[]))} | {r['tool_steps']} | {tools} | {r['drafts_discarded']} | {'是' if r['remedy'] else ''} | {len(v.get('outside',''))} | {ln.get('title')}/{ln.get('intro')}/{ln.get('body')} | {sum(s['secs'] for s in steps):.0f} | {next((round(s['ttft'],1) for s in reversed(steps) if s['ttft']), '')} | {tok} |")
    for r in results:
        if "error" in r: continue
        v = r["valid"]
        L += ["", "---", "", f"## {r['kind']} · 思考{'开' if r['thinking'] else '关'}", "", f"选区：「{r['selection']}」", ""]
        for s in r["steps"]:
            for c in s["calls"]: L.append(f"- `{c['name']}({c['args']})`")
        if v.get("outside"): L += ["", f"**标签外文字**：{v['outside'][:300]}"]
        L += ["", f"### {v.get('title','(无标题)')}", "", f"> {v.get('intro','')}", "", v.get("body") or ("```\n" + r["steps"][-1]["content"][:2000] + "\n```")]
    (OUT / "report.md").write_text("\n".join(L))
    print(f"wrote {OUT/'report.md'}")

if __name__ == "__main__": main()
