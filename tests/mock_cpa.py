#!/usr/bin/env python3
"""模拟 CPA 宿主，用来在无头浏览器里验证插件页面渲染。

启动：python tests/mock_cpa.py [port]
然后访问 http://127.0.0.1:<port>/v0/resource/plugins/window-anchor/status
"""
import json
import pathlib
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = pathlib.Path(__file__).resolve().parents[1]
WEB = ROOT / "web"

AUTH_A = "11111111-1111-4111-8111-111111111111"
AUTH_B = "22222222-2222-4222-8222-222222222222"

STATUS = {
    "version": "0.1.2",
    "enabled": True,
    "timezone": "Asia/Shanghai",
    "running": False,
    "now": "2026-10-27T10:50:00+08:00",
    "plans": [
        {
            "auth_id": AUTH_A,
            "label": "alice@example.com",
            "model": "gpt-6-luna",
            "times": ["05:30"],
            "enabled": True,
            "missing": False,
            "disabled": False,
            "next_trigger": "2026-10-28T05:30:00+08:00",
        },
        {
            "auth_id": AUTH_B,
            "label": "bob@example.com",
            "model": "gpt-6-luna",
            "times": ["08:30"],
            "enabled": True,
            "missing": False,
            "disabled": False,
            "next_trigger": "2026-10-28T08:30:00+08:00",
        },
    ],
    "results": [
        {
            "id": "manual:1",
            "auth_id": AUTH_A,
            "label": "alice@example.com",
            "model": "gpt-6-luna",
            "mode": "manual",
            "scheduled_for": "2026-10-27T10:30:00+08:00",
            "started_at": "2026-10-27T10:30:05+08:00",
            "completed_at": "2026-10-27T10:30:09+08:00",
            "status": "success",
            "message": "请求成功，已读到新的 5h 窗口重置时间",
            "reset_at": "2026-10-27T15:30:00+08:00",
            "used_percent": 1.5,
        }
    ],
}

# 只保留一条计划，用来验证"未配置账号自动补行"的逻辑
STATUS["plans"] = STATUS["plans"][:1]
STATUS["plans"].append({
    "provider": "antigravity",
    "auth_id": "antigravity-user@example.com",
    "label": "user@example.com",
    "model": "gemini-2.5-pro",
    "times": ["07:00"],
    "enabled": True,
    "missing": False,
    "disabled": False,
    "next_trigger": "2026-10-28T07:00:00+08:00",
})

ACCOUNTS = {
    "accounts": [
        {"id": AUTH_A, "name": "codex-11111111-alice", "email": "alice@example.com",
         "provider": "codex", "plan_type": "plus", "disabled": False, "status": "enabled"},
        {"id": AUTH_B, "name": "codex-22222222-bob", "email": "bob@example.com",
         "provider": "codex", "plan_type": "plus", "disabled": False, "status": "enabled"},
        {"id": "antigravity-user@example.com", "name": "antigravity-user@example.com.json",
         "email": "user@example.com", "provider": "antigravity", "plan_type": "pro",
         "disabled": False, "status": "enabled"},
    ]
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, body, content_type="application/json; charset=utf-8"):
        if isinstance(body, str):
            body = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?")[0]

        # 管理 API：为了验证"只读降级"，这里统一返回 401
        if path.startswith("/v0/management/"):
            return self._send(401, json.dumps({"error": "unauthorized"}))

        if path.startswith("/v0/resource/plugins/window-anchor/"):
            name = path.rsplit("/", 1)[-1] or "status"
            if name in ("", "status"):
                name = "status.html"
            if name == "status.json":
                return self._send(200, json.dumps(STATUS))
            if name == "accounts.json":
                return self._send(200, json.dumps(ACCOUNTS))
            target = WEB / name
            if not target.is_file():
                return self._send(404, "not found", "text/plain")
            ctype = {
                ".html": "text/html; charset=utf-8",
                ".js": "application/javascript; charset=utf-8",
                ".css": "text/css; charset=utf-8",
                ".png": "image/png",
                ".svg": "image/svg+xml",
            }.get(target.suffix, "application/octet-stream")
            return self._send(200, target.read_bytes(), ctype)

        return self._send(404, "not found", "text/plain")

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)
        return self._send(401, json.dumps({"error": "unauthorized"}))


def main() -> int:
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 18765
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"mock CPA: http://127.0.0.1:{port}/v0/resource/plugins/window-anchor/status")
    print("管理 API 故意返回 401，用来验证只读降级路径")
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
