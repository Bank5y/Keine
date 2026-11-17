"""Fills a fresh Keine instance with sample data for looking at the UI.

Run by scripts/demo/run.sh against an empty database; see that script for
what it creates. Users: alice, bob, carol (password <name>-password) and the
admin from KEINE_ADMIN_USERNAME / KEINE_ADMIN_PASSWORD.
"""
import io, json, os, zipfile, zlib, struct, urllib.request, http.cookiejar, uuid, sys
BASE = os.environ.get("DEMO_URL", "http://127.0.0.1:18080")
ADMIN = (os.environ.get("KEINE_ADMIN_USERNAME", "admin"), os.environ.get("KEINE_ADMIN_PASSWORD", "admin-password"))

class C:
    def __init__(s):
        s.o = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def req(s, method, path, body=None, ctype=None, raw=None):
        data = raw
        h = {}
        if body is not None:
            data = json.dumps(body).encode(); h["Content-Type"] = "application/json"
        if ctype: h["Content-Type"] = ctype
        r = urllib.request.Request(BASE + path, data=data, method=method, headers=h)
        try:
            with s.o.open(r) as resp:
                t = resp.read()
                return json.loads(t) if t and resp.headers.get("Content-Type","").startswith("application/json") else None
        except urllib.error.HTTPError as e:
            sys.exit(f"{method} {path} -> {e.code} {e.read()[:300]}")
    def login(s, u, p): s.req("POST", "/api/session", {"provider": "password", "username": u, "password": p}); return s
    def upload(s, path, zbytes, method="POST"):
        b = uuid.uuid4().hex
        body = (f"--{b}\r\nContent-Disposition: form-data; name=\"package\"; filename=\"course.zip\"\r\nContent-Type: application/zip\r\n\r\n").encode() + zbytes + f"\r\n--{b}--\r\n".encode()
        return s.req(method, path, raw=body, ctype=f"multipart/form-data; boundary={b}")

def png(w, h, rgb):
    raw = b"".join(b"\x00" + bytes(rgb) * w for _ in range(h))
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")

def zipof(files):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        for n, d in files.items(): z.writestr(n, d)
    return buf.getvalue()

def para(topic, n):
    base = [f"{topic}不是一个新概念，但在大模型出现之后，它第一次变得真正可用。这一讲我们从一个具体的问题出发，看看它在工程上到底意味着什么。",
            "很多团队在第一次落地时都会遇到同一个坑：把所有逻辑塞进一个提示词里，期待模型自己想清楚。结果是效果不稳定，出了问题也很难排查。",
            "更好的做法是把任务拆成几个职责清晰的步骤，每一步只做一件事，并且有明确的输入和输出。这样不仅更容易调试，也更容易评估每一步的质量。",
            "下面这段代码展示了一个最小的实现。注意我们没有引入任何框架，所有的控制流都是显式的。"]
    return "\n\n".join(base[:n])

agent_lessons = [
  ("opening", "开篇", [("l00", "开篇词｜为什么 Agent 值得认真对待")]),
  ("intent", "意图识别", [("l01", "01｜用户输入规范化"), ("l02", "02｜三层意图识别"), ("l03", "03｜槽位抽取与追问")]),
  ("plan", "规划与执行", [("l04", "04｜任务拆解：从目标到步骤"), ("l05", "05｜工具调用的边界"), ("l06", "06｜失败重试与回退")]),
  ("memory", "记忆", [("l07", "07｜短期记忆与上下文窗口"), ("l08", "08｜长期记忆的写入策略")]),
]
def agent_pkg(completion="ongoing"):
    chapters, files = [], {"cover.png": png(240, 320, (178, 58, 42)), "images/flow.png": png(640, 240, (226, 218, 206))}
    for cid, ct, ls in agent_lessons:
        lessons = []
        for lid, lt in ls:
            fn = f"{lid}.md"
            lessons.append({"id": lid, "title": lt, "file": fn})
            files[fn] = f"# {lt}\n\n{para('Agent', 3)}\n\n## 一个最小的例子\n\n{para('Agent', 4).split(chr(10)+chr(10))[3]}\n\n```go\nfunc run(ctx context.Context, goal string) error {{\n\tplan, err := planner.Plan(ctx, goal)\n\tif err != nil {{\n\t\treturn err\n\t}}\n\tfor _, step := range plan.Steps {{\n\t\tif err := executor.Do(ctx, step); err != nil {{\n\t\t\treturn retry(ctx, step, err)\n\t\t}}\n\t}}\n\treturn nil\n}}\n```\n\n![执行流程](images/flow.png)\n\n## 小结\n\n- 把任务拆成职责清晰的步骤\n- 每一步都有明确的输入和输出\n- 失败时有可预期的回退路径\n\n> 思考题：在你的业务里，哪一步最容易出错？为什么？\n"
        chapters.append({"id": cid, "title": ct, "lessons": lessons})
    m = {"version": 1, "title": "Agent 系统设计 40 讲", "type": "column", "completion": completion, "cover": "cover.png",
         "intro": "从意图识别、任务规划到记忆与评估，系统讲解如何把大模型做成可靠的 Agent 系统。每一讲都配有可运行的最小实现。",
         "author": {"name": "陈明远", "intro": "前大厂搜索推荐负责人，现专注于 Agent 平台建设。"}, "chapters": chapters}
    files["course.json"] = json.dumps(m, ensure_ascii=False)
    return zipof(files)

def video_pkg():
    chs = [("basics", "基础", [("v01", "01｜容器到底是什么", "18:24"), ("v02", "02｜镜像分层与缓存", "22:10")]),
           ("net", "网络与存储", [("v03", "03｜容器网络模型", "25:31"), ("v04", "04｜数据卷与持久化", "19:47")])]
    chapters = [{"id": c, "title": t, "lessons": [{"id": i, "title": n, "video": {"url": f"https://example.com/v/{i}.mp4"}, "duration": d} for i, n, d in ls]} for c, t, ls in chs]
    m = {"version": 1, "title": "深入浅出 Kubernetes", "type": "video", "completion": "finished", "cover": "cover.png",
         "intro": "用 40 段视频从容器原理讲到集群调度。", "author": {"name": "王一凡", "intro": "云原生布道师。"}, "chapters": chapters}
    return zipof({"course.json": json.dumps(m, ensure_ascii=False), "cover.png": png(240, 320, (44, 82, 130))})

def paper_pkg(title, color, completion="finished"):
    m = {"version": 1, "title": title, "type": "column", "completion": completion, "cover": "cover.png",
         "intro": "一门短小精悍的专栏。", "author": {"name": "李想", "intro": "工程师。"},
         "chapters": [{"id": "c1", "title": "第一部分", "lessons": [{"id": "p1", "title": "01｜开始之前", "file": "p1.md"}, {"id": "p2", "title": "02｜核心概念", "file": "p2.md"}]}]}
    return zipof({"course.json": json.dumps(m, ensure_ascii=False), "cover.png": png(240, 320, color), "p1.md": "# 01\n\n" + para("这门课", 2), "p2.md": "# 02\n\n" + para("这门课", 3)})

admin = C().login(*ADMIN)
def user(name):
    c = C(); c.req("POST", "/api/users", {"username": name, "password": name + "-password"}); return c.login(name, name + "-password")
alice, bob, carol = user("alice"), user("bob"), user("carol")

def publish(c, z):
    cid = c.upload("/api/courses", z)["id"]
    c.req("POST", f"/api/courses/{cid}/publish-request")
    rid = [r for r in admin.req("GET", "/api/admin/reviews")["reviews"] if r["course"]["id"] == cid][0]["id"]
    admin.req("POST", f"/api/admin/reviews/{rid}/approve")
    return cid

agent = publish(alice, agent_pkg())
k8s = publish(alice, video_pkg())
others = [publish(carol, paper_pkg(t, col)) for t, col in [("Go 并发编程实战", (47, 107, 79)), ("数据库内核 20 讲", (154, 107, 18)), ("前端工程化指南", (79, 74, 67)), ("分布式系统入门", (108, 63, 158))]]
private = carol.upload("/api/courses", paper_pkg("编译原理笔记", (139, 132, 120), "incomplete"))["id"]
carol.req("POST", f"/api/courses/{private}/publish-request")

ids = {l["key"]: l["id"] for ch in bob.req("GET", f"/api/courses/{agent}")["chapters"] for l in ch["lessons"]}
bob.req("POST", f"/api/courses/{agent}/subscribe")
for k in ["l00", "l01", "l02"]: bob.req("PUT", f"/api/courses/{agent}/lessons/{ids[k]}/progress", {"done": True})
bob.req("PUT", f"/api/courses/{agent}/lessons/{ids['l03']}/progress", {"position": 0.4})
ap = f"/api/courses/{agent}/lessons/{ids['l01']}/annotations"
bob.req("POST", ap, {"style": "highlight", "color": "yellow", "quote": "第一次变得真正可用", "prefix": "但在大模型出现之后，它", "suffix": "。这一讲"})
bob.req("POST", ap, {"style": "highlight", "color": "green", "quote": "把所有逻辑塞进一个提示词里", "prefix": "遇到同一个坑：", "suffix": "，期待模型"})
bob.req("POST", ap, {"style": "underline", "quote": "每一步只做一件事，并且有明确的输入和输出", "prefix": "职责清晰的步骤，", "suffix": "。这样", "note": "这一条可以直接放进团队的设计规范里。"})
bob.req("POST", ap, {"style": "highlight", "color": "blue", "quote": "所有的控制流都是显式的", "prefix": "没有引入任何框架，", "suffix": "。"})

# bob's draft request on the ongoing course, and a whole-package request from carol
u = bob.req("POST", f"/api/courses/{agent}/draft/uploads")["upload"]
bob.req("PUT", f"/api/courses/{agent}/draft/uploads/{u}/files/09.md", raw="# 09｜评估体系\n\n先有评估，再谈优化。\n".encode(), ctype="application/octet-stream")
bob.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "addLesson", "chapter": "memory", "title": "09｜评估：先有尺子再谈优化", "content": {"upload": u}})
bob.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "renameLesson", "lesson": "l05", "title": "05｜工具调用：边界与权限"})
bob.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "moveLesson", "lesson": "l06", "chapter": "plan", "after": ""})
bob.req("POST", f"/api/courses/{agent}/draft/commit", {"skip": []})

# alice's own draft with a conflict: she renames l08, then the admin deletes it directly
alice.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "renameLesson", "lesson": "l08", "title": "08｜长期记忆：写什么、何时写"})
alice.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "addChapter", "title": "评估与上线"})
d = alice.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "renameChapter", "chapter": "plan", "title": "规划、执行与回退"})
newch = [c for c in d["course"]["chapters"] if c["title"] == "评估与上线"][0]["key"]
alice.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "addUpcomingLesson", "chapter": newch, "title": "10｜灰度上线与监控"})
alice.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "moveLesson", "lesson": "l07", "chapter": "memory", "after": "l08"})
admin.req("POST", f"/api/courses/{agent}/draft/ops", {"type": "deleteLesson", "lesson": "l08"})
admin.req("POST", f"/api/courses/{agent}/draft/commit", {"skip": []})

print(json.dumps({"agent": agent, "k8s": k8s, "others": others, "private": private, "l01": ids["l01"], "ids": ids}))
