"""A stand-in OpenAI-compatible model endpoint for the demo (scripts/demo/run.sh).

POST <url>/chat/completions streams a reply as Server-Sent Events: a short one
to the connection test (a request declaring only connection_check), so it
passes. An analysis first looks up the course (search_course and read_lesson
at once, so the popover shows its progress), then, with the results back,
streams some reasoning and a tagged analysis a few characters at a time; a
follow-up turn of the exploration dialogue gets a Markdown answer instead. The key "bad" gets a 401, to show
the failed-test state.

    python3 scripts/demo/fake_model.py 18999
"""
import http.server, json, re, sys, time

REPLY = ["这是", "演示用的", "模型回复。"]

ANALYSIS = """<exp_title>Agent（智能体）</exp_title>
<exp_intro>能够自主规划步骤、调用工具，并根据执行结果调整下一步行动的大模型应用。</exp_intro>
<exp_body>
Agent 指的是以大模型为核心、能围绕一个目标自己决定下一步做什么的程序：它把目标拆成若干步骤，每一步调用模型或工具，再根据结果决定继续、重试还是换一条路。

在这一讲里，Agent 是和「把所有逻辑塞进一个提示词」相对的做法：后者指望模型一次想清楚，前者把控制流写在代码里，每一步只做一件事，更容易调试和评估。

```mermaid
flowchart TD
  A["目标"] --> B["规划步骤"]
  B --> C["执行一步"]
  C --> D{"成功？"}
  D -- "是" --> E["下一步"]
  D -- "否" --> F["重试或回退"]
```
</exp_body>"""


# a follow-up turn of the exploration dialogue answers in plain Markdown
FOLLOWUP = """在文中的最小实现里，planner 的输出本身就带着工具名：每一步是 `{tool, args}`，executor 只负责按名字找到工具并执行，**不再做选择**。

这样选择权集中在规划阶段，执行阶段没有分支，整个过程可以完整重放。代价是 planner 必须在开始前就知道全部工具；下一讲的「动态规划」会让 executor 在失败后把结果交回 planner 重新规划。"""


# an exploration report (#160) answers in four tagged parts, with no tools
REPORT = """<rep_title>Agent 的规划与执行</rep_title>
<rep_summary>Agent 以大模型为核心，围绕一个目标自己决定下一步。它把目标拆成步骤，由 planner 规划、executor 执行。选择权集中在规划阶段，执行过程可以完整重放。</rep_summary>
<rep_body>
## 原理

Agent 是和「把所有逻辑塞进一个提示词」相对的做法：控制流写在代码里，每一步只做一件事，更容易调试和评估。

## 机制

planner 的输出本身带着工具名，每一步是 `{tool, args}`；executor 只按名字找到工具并执行，不再做选择。

```mermaid
flowchart TD
  A["目标"] --> B["planner 规划"]
  B --> C["executor 执行一步"]
  C --> D{"成功？"}
  D -- "是" --> E["下一步"]
  D -- "否" --> B
```

## 取舍

planner 必须在开始前就知道全部工具；失败后把结果交回 planner 重新规划，可以弥补这一点。
</rep_body>
<rep_points>
- Agent 把目标拆成步骤，逐步执行
- 规划和执行分开，选择权集中在规划阶段
- 执行阶段没有分支，过程可以完整重放
</rep_points>"""


def chunk(delta, finish=None):
    return "data: " + json.dumps({"choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}, ensure_ascii=False) + "\n\n"


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        req = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        if not self.path.endswith("/chat/completions"):
            self.send_error(404)
            return
        if self.headers.get("Authorization", "").endswith(" bad"):
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":{"message":"invalid key"}}')
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        tools = [t["function"]["name"] for t in req.get("tools") or []]
        if "<rep_title>" in (req["messages"][0].get("content") or ""):
            time.sleep(0.4)
            for i in range(0, len(REPORT), 4):
                self.wfile.write(chunk({"content": REPORT[i:i + 4]}).encode())
                self.wfile.flush()
                time.sleep(0.03)
            self.wfile.write((chunk({}, "stop") + "data: [DONE]\n\n").encode())
            return
        if tools != ["connection_check"] and not any(m.get("role") == "tool" for m in req.get("messages", [])):
            self.look_up(req)
            return
        if tools != ["connection_check"]:
            time.sleep(0.4)
            # reasoning first, as a thinking model streams it: the popover shows 正在思考
            for part in ["读者选中了 Agent，", "先给标准含义，", "再结合这一讲。"]:
                self.wfile.write(chunk({"reasoning_content": part}).encode())
                self.wfile.flush()
                time.sleep(0.5)
            # only the analysis's system prompt asks for the tagged format
            followup = "<exp_title>" not in (req["messages"][0].get("content") or "")
            text = FOLLOWUP if followup else ANALYSIS
            for i in range(0, len(text), 4):
                self.wfile.write(chunk({"content": text[i:i + 4]}).encode())
                self.wfile.flush()
                time.sleep(0.02)
            self.wfile.write((chunk({}, "stop") + "data: [DONE]\n\n").encode())
            return
        body = "".join(chunk({"content": t}) for t in REPLY) + chunk({}, "stop")
        usage = {"prompt_tokens": 120, "completion_tokens": 8, "prompt_tokens_details": {"cached_tokens": 64}}
        body += "data: " + json.dumps({"choices": [], "usage": usage}) + "\n\ndata: [DONE]\n\n"
        self.wfile.write(body.encode())

    def look_up(self, req):
        """Calls two course tools at once: a search, and a read of another lesson."""
        context = "".join(m.get("content") or "" for m in req["messages"] if m["role"] == "user")
        here = re.search(r"本讲 \[([^\]]+)\]", context)
        others = [k for k in re.findall(r"^  - .* \[([^\]]+)\]$", context, re.M) if not here or k != here.group(1)]
        calls = [("search_course", {"query": "Agent"})]
        if others:
            calls.append(("read_lesson", {"lesson": others[0]}))
        time.sleep(0.6)
        for i, (name, args) in enumerate(calls):
            call = {"index": i, "id": f"call_{i}", "type": "function", "function": {"name": name, "arguments": json.dumps(args, ensure_ascii=False)}}
            self.wfile.write(chunk({"tool_calls": [call]}).encode())
        self.wfile.write((chunk({}, "tool_calls") + "data: [DONE]\n\n").encode())
        self.wfile.flush()

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
