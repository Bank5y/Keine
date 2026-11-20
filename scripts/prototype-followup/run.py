#!/usr/bin/env python3
"""PROTOTYPE, throwaway (wayfinder ticket #116「探索对话后续轮的模型配置」).

Fixes one first-round analysis on the Raft lesson (deepseek-flash, thinking off,
the analysis default from #99), then runs three typical follow-up questions and
one sync under four configs (flash/pro x thinking off/on) and writes a blind
side-by-side report. Context order follows #94: system + snapshot -> first reply
-> exploration document -> history. Reuses base.py (copy of the #99 prototype).

    KEINE_ROOT=/path/to/Keine REPEATS=2 python3 scripts/prototype-followup/run.py
    python3 scripts/prototype-followup/run.py report     # rebuild report.html only

Reads DEEPSEEK_API_KEY from $KEINE_ROOT/.scratch/.env, writes $KEINE_ROOT/.scratch/followup-proto/.
"""
import json, os, re, sys, time, random, pathlib, html
os.environ.setdefault("COURSE", "wiki")
sys.path.insert(0, str(pathlib.Path(__file__).parent))
import base as B

OUT = B.ROOT / ".scratch/followup-proto"
REPEATS = int(os.environ.get("REPEATS", "2"))
CONFIGS = [("flash", False), ("flash", True), ("pro", False), ("pro", True)]
MODEL_ID = {"flash": "deepseek-flash", "pro": "deepseek-v4-pro"}
PRICE = {"flash": (0.003, 0.15, 0.6), "pro": (0.022, 0.66, 1.98)}  # USD / 1M: cache hit, miss, output (off-peak)
CASE = ("term", "w4", "任期", "领袖选举")

QUESTIONS = [
  ("deep", "深挖某一句", "为什么一个节点收到任期号更大的消息时，要立刻放弃领袖或候选人身份？如果不这样做，会出什么问题？"),
  ("course", "课程内拓宽", "课程里其他协议，比如 Paxos 和二阶段提交，有没有和任期类似的机制？它们分别怎么处理过时的领袖或协调者？"),
  ("outside", "课程外发散", "etcd 或 Kafka 的 KRaft 这些真实系统里，任期是怎么持久化和使用的？和这一讲讲的有什么出入？"),
]

FOLLOWUP_SYSTEM = """你是 Keine 阅读页里的探索助手。读者已经固定了一条探索，现在在探索对话里继续追问。

上下文依次是：读者第一次分析时的课程上下文快照、第一轮的分析回复、当前的探索文档、此前的对话。

回答读者的提问：
- 直接用 Markdown 回答，可以用小标题和列表；流程、状态或结构用文字难以说清时可以画一张 ```mermaid 图（flowchart TD、sequenceDiagram 或 stateDiagram-v2）。
- 需要课程其他讲的内容时用 read_lesson / list_sections / search_course；本讲全文已经在快照里，不要再读本讲。
- 课程外的事实、真实系统的做法、最新进展用 web_search 查，需要全文时用 web_fetch；回答里可以提到来源名称，但不要写任何链接，来源由系统记录。
- 篇幅按问题而定，通常 200–600 字；不要复述探索文档里已有的内容。
- 语言跟随读者的提问。"""

SYNC_PROMPT = """请把上面这段探索对话里值得保留的内容同步回探索文档。

探索文档的段落：标题、简介、含义、延伸（课程内其他讲的相关内容，有序）、扩展（课程外的相似内容，无序）、参考（只能从下面的来源清单里挑）。
同步写进哪一段由内容决定：深化理解的改写含义；课程内的关联加进延伸；课程外的内容加进扩展；用到的来源加进参考。

只输出修改块，块外不要写任何文字，没有要改的段就不输出那一段：
<exp_edit section="含义" op="replace">改写后的完整含义（Markdown，不写标题、不写链接）</exp_edit>
<exp_edit section="延伸" op="add">- 一条，写明是哪一讲（讲 ID）和关系</exp_edit>
<exp_edit section="扩展" op="add">- 一条</exp_edit>
<exp_edit section="参考" op="add">来源编号，逗号分隔，例如 S1,S3</exp_edit>

来源清单：
{sources}"""

def call(messages, model, thinking, tools=True):
    B.MODEL = MODEL_ID[model]
    return B.stream_step(messages, thinking, tools=tools)

def run_turn(msgs, model, thinking, sources):
    """One run of the tool loop on top of msgs (mutated in memory only). Returns final text and trace."""
    B.SEEN_URLS.clear()
    msgs = list(msgs)
    trace, tool_steps = [], 0
    while True:
        last = tool_steps >= B.MAX_TOOL_STEPS
        if last: msgs.append({"role": "user", "content": "已达查询上限，请直接作答。"})
        st = call(msgs, model, thinking, tools=not last)
        trace.append(st)
        msgs.append(B.assistant_msg(st))
        if not st["calls"]: break
        tool_steps += 1
        for c in st["calls"]:
            try: args = json.loads(c["args"] or "{}")
            except json.JSONDecodeError: args = {}
            c["result"] = B.run_tool(c["name"], args)
            if c["name"] == "web_search":
                for t, u in re.findall(r"^\[(.*?)\]\((https?://[^)\s]+)\)", c["result"], re.M):
                    if u not in [s[1] for s in sources]: sources.append((t, u))
            msgs.append({"role": "tool", "tool_call_id": c["id"], "content": c["result"][:8000]})
    return trace[-1]["content"], trace

def summarize(trace):
    u = [s["usage"] or {} for s in trace]
    inp = sum(x.get("prompt_tokens", 0) for x in u); hit = sum(x.get("prompt_cache_hit_tokens", 0) for x in u)
    out = sum(x.get("completion_tokens", 0) for x in u)
    rsn = sum((x.get("completion_tokens_details") or {}).get("reasoning_tokens", 0) for x in u)
    return {"secs": round(sum(s["secs"] for s in trace), 1),
            "ttft": next((round(s["ttft"], 1) for s in reversed(trace) if s["ttft"]), None),
            "wait": round(sum(s["secs"] for s in trace[:-1]) + (trace[-1]["ttft"] or 0), 1),
            "tools": [f"{c['name']}({c['args']})" for s in trace for c in s["calls"]],
            "in": inp, "hit": hit, "out": out, "reasoning": rsn}

def cost(model, m):
    h, miss, o = PRICE[model]
    return (m["hit"] * h + (m["in"] - m["hit"]) * miss + m["out"] * o) / 1e6

def seed():
    p = OUT / "seed.json"
    if p.exists(): return json.loads(p.read_text())
    for _ in range(3):
        B.MODEL = "deepseek-flash"
        r = B.analyse(CASE, False)
        if r["valid"]["ok"]: break
    v = r["valid"]
    s = {"snapshot": B.first_turn(CASE[1], CASE[2], CASE[3]), "reply": r["steps"][-1]["content"],
         "doc": f"# {v['title']}\n\n> {v['intro']}\n\n## 含义\n\n{v['body']}\n\n## 延伸\n\n（空）\n\n## 扩展\n\n（空）\n\n## 参考\n\n（空）"}
    p.write_text(json.dumps(s, ensure_ascii=False, indent=1))
    return s

def base_msgs(s):
    return [{"role": "system", "content": FOLLOWUP_SYSTEM}, {"role": "user", "content": s["snapshot"]},
            {"role": "assistant", "content": s["reply"]}, {"role": "user", "content": "【当前探索文档】\n\n" + s["doc"]}]

def main():
    OUT.mkdir(parents=True, exist_ok=True)
    s = seed()
    rp = OUT / "results.json"
    results = json.loads(rp.read_text()) if rp.exists() else []
    done = {(r["q"], r["model"], r["thinking"], r["rep"]) for r in results}
    for model, thinking in CONFIGS:
        for rep in range(REPEATS):
            for qid, _, q in QUESTIONS:
                if (qid, model, thinking, rep) in done: continue
                print(f"{qid} {model} thinking={thinking} #{rep+1} …", flush=True)
                sources = []
                try:
                    msgs = base_msgs(s) + [{"role": "user", "content": q}]
                    text, trace = run_turn(msgs, model, thinking, sources)
                    res = {"q": qid, "model": model, "thinking": thinking, "rep": rep, "text": text, "m": summarize(trace), "sources": sources}
                    if qid == "outside":  # sync this dialogue back (no tools, its own run)
                        src = "\n".join(f"S{i+1}. {t} — {u}" for i, (t, u) in enumerate(sources)) or "（本次对话没有来源）"
                        smsgs = msgs + [{"role": "assistant", "content": text}, {"role": "user", "content": SYNC_PROMPT.format(sources=src)}]
                        st = call(smsgs, model, thinking, tools=False)
                        sres = {"q": "sync", "model": model, "thinking": thinking, "rep": rep, "text": st["content"], "m": summarize([st]), "sources": sources}
                except Exception as e:
                    print("  error", e); continue
                results.append(res)
                if qid == "outside": results.append(sres)
                rp.write_text(json.dumps(results, ensure_ascii=False, indent=1))
    report(s, results)

# ---- report ---------------------------------------------------------------
def report(s, results):
    rng = random.Random(116)
    groups = [(q, label, text) for q, label, text in QUESTIONS] + [("sync", "同步（课程外发散那段对话）", "把对话同步回探索文档")]
    secs = []
    for qid, label, qtext in groups:
        rows = [r for r in results if r["q"] == qid]
        cfgs = CONFIGS[:]; rng.shuffle(cfgs)
        cols = []
        for i, (model, thinking) in enumerate(cfgs):
            name = f"{model} · 思考{'开' if thinking else '关'}"
            cards = []
            for r in sorted((r for r in rows if r["model"] == model and r["thinking"] == thinking), key=lambda r: r["rep"]):
                m = r["m"]
                meta = (f"总耗时 {m['secs']}s · 读者等到第一个字 {m['wait']}s · 输入 {m['in']}（缓存 {m['hit']}）· 输出 {m['out']}（推理 {m['reasoning']}）"
                        f" · ≈${cost(model, m):.4f}")
                tools = "".join(f"<li><code>{html.escape(t[:160])}</code></li>" for t in m["tools"]) or "<li>无</li>"
                srcs = "".join(f"<li>S{j+1}. {html.escape(t)}</li>" for j, (t, u) in enumerate(r["sources"])) if qid == "sync" else ""
                cards.append(f"""<div class="card"><div class="rep">第 {r['rep']+1} 次</div><div class="meta">{meta}</div>
<details><summary>工具调用 {len(m['tools'])} 次</summary><ul>{tools}</ul></details>
{f'<details><summary>来源清单</summary><ul>{srcs}</ul></details>' if srcs else ''}
<div class="md" data-md="{html.escape(r['text'])}"></div></div>""")
            cols.append(f"""<div class="col"><h3><span class="blind">方案 {'ABCD'[i]}</span><span class="real">{name}</span></h3>{''.join(cards) or '<p>（未跑）</p>'}</div>""")
        secs.append(f"""<section><h2>{label}</h2><p class="q">{html.escape(qtext)}</p><div class="grid">{''.join(cols)}</div></section>""")
    agg = []
    for model, thinking in CONFIGS:
        rs = [r for r in results if r["model"] == model and r["thinking"] == thinking and r["q"] != "sync"]
        if not rs: continue
        n = len(rs); av = lambda k: sum(r["m"][k] for r in rs) / n
        agg.append(f"<tr><td>{model} · 思考{'开' if thinking else '关'}</td><td>{n}</td><td>{av('secs'):.1f}</td><td>{av('wait'):.1f}</td>"
                   f"<td>{av('in'):.0f}</td><td>{av('out'):.0f}</td><td>{av('reasoning'):.0f}</td><td>{sum(len(r['m']['tools']) for r in rs)/n:.1f}</td>"
                   f"<td>${sum(cost(model, r['m']) for r in rs)/n:.4f}</td></tr>")
    page = f"""<!doctype html><html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>后续轮模型对比</title>
<script src="https://cdn.jsdelivr.net/npm/marked@12.0.2/marked.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10.9.1/dist/mermaid.min.js"></script>
<style>
:root{{--bg:#fbfaf7;--fg:#222;--mut:#777;--card:#fff;--line:#e4e0d8;--acc:#7a4b1f}}
@media (prefers-color-scheme:dark){{:root{{--bg:#1b1a18;--fg:#e8e4dc;--mut:#999;--card:#252321;--line:#3a3733;--acc:#e0a96d}}}}
body{{background:var(--bg);color:var(--fg);font:15px/1.65 system-ui,sans-serif;margin:0;padding:16px}}
h1{{font-size:22px}} h2{{margin-top:40px;border-bottom:1px solid var(--line)}} .q{{color:var(--acc);font-weight:600}}
.grid{{display:grid;grid-template-columns:repeat(4,minmax(280px,1fr));gap:12px;overflow-x:auto}}
.card{{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px;margin-bottom:12px}}
.meta,.rep,details{{color:var(--mut);font-size:12.5px}} .md{{font-size:14px}} .md pre{{overflow-x:auto}}
table{{border-collapse:collapse}} td,th{{border:1px solid var(--line);padding:4px 8px;font-size:13px}}
body:not(.reveal) .real, body.reveal .blind{{display:none}} body:not(.reveal) #agg{{display:none}}
button{{position:sticky;top:8px;z-index:2;padding:6px 12px}}
@media (max-width:700px){{.grid{{grid-template-columns:1fr}}}}
</style></head><body>
<h1>探索对话后续轮的模型配置（PROTOTYPE）</h1>
<p>Raft 一讲，选区「任期」。第一轮固定用 flash · 思考关跑出，四种配置各自接着回答同样的追问，每格 {REPEATS} 次。每节的方案顺序独立打乱。先盲看质量，再点按钮看配置、耗时汇总。价格按 DeepSeek 非高峰价估算。</p>
<button onclick="document.body.classList.toggle('reveal')">显示 / 隐藏配置</button>
<div id="agg"><h2>汇总（不含同步）</h2><table><tr><th>配置</th><th>次数</th><th>平均总耗时 s</th><th>等到第一个字 s</th><th>输入 tok</th><th>输出 tok</th><th>推理 tok</th><th>工具次数</th><th>平均花费</th></tr>{''.join(agg)}</table></div>
<details><summary>固定的第一轮与探索文档</summary><div class="md" data-md="{html.escape(s['doc'])}"></div></details>
{''.join(secs)}
<script>
mermaid.initialize({{startOnLoad:false,securityLevel:'strict'}});
document.querySelectorAll('.md').forEach(el=>{{
  el.innerHTML = marked.parse(el.dataset.md.replace(/<exp_edit section=["“](.*?)["”] op=["“](.*?)["”]>/g,'\\n\\n**【$1 · $2】**\\n\\n').replace(/<\\/exp_edit>/g,'\\n'));
  el.querySelectorAll('code.language-mermaid').forEach(c=>{{const d=document.createElement('pre');d.className='mermaid';d.textContent=c.textContent;c.parentElement.replaceWith(d);}});
}});
mermaid.run({{querySelector:'.mermaid'}}).catch(()=>{{}});
</script></body></html>"""
    (OUT / "report.html").write_text(page)
    print(f"wrote {OUT/'report.html'}")

if __name__ == "__main__":
    if sys.argv[1:] == ["report"]:
        report(json.loads((OUT / "seed.json").read_text()), json.loads((OUT / "results.json").read_text()))
    else: main()
