"""A stand-in OpenAI-compatible model endpoint for the demo (scripts/demo/run.sh).

POST <url>/chat/completions streams a short reply as Server-Sent Events, so
the site model's connection test passes and AI screens have a model to call.
The key "bad" gets a 401, to show the failed-test state.

    python3 scripts/demo/fake_model.py 18999
"""
import http.server, json, sys

REPLY = ["这是", "演示用的", "模型回复。"]


def chunk(delta, finish=None):
    return "data: " + json.dumps({"choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}, ensure_ascii=False) + "\n\n"


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
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
        body = "".join(chunk({"content": t}) for t in REPLY) + chunk({}, "stop")
        usage = {"prompt_tokens": 120, "completion_tokens": 8, "prompt_tokens_details": {"cached_tokens": 64}}
        body += "data: " + json.dumps({"choices": [], "usage": usage}) + "\n\ndata: [DONE]\n\n"
        self.wfile.write(body.encode())

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
