#!/usr/bin/env python3
"""CPA 宿主模拟器：加载插件动态库，完整走一遍真实调用链。

覆盖：
  1. cliproxy_plugin_init 能成功初始化
  2. plugin.register 返回的 metadata 通过 CPA 的 validPlugin 校验
  3. management.register 注册了带 Menu 的资源页和三条管理路由
  4. management.handle 的 /status 能返回配置里的 plans
  5. management.handle 的 /accounts 能列出 mock 账号
  6. management.handle 的资源路径能返回 HTML / JSON
  7. host.model.execute 触发后，结果被记录且带 reset_at

用法：
  python tests/host_sim.py dist/windows_amd64/window-anchor.dll
  python tests/host_sim.py dist/linux_amd64/window-anchor.so
"""
import base64
import ctypes
import json
import sys
import time
from pathlib import Path


class Buffer(ctypes.Structure):
    _fields_ = [("ptr", ctypes.c_void_p), ("len", ctypes.c_size_t)]


HOST_CALL = ctypes.CFUNCTYPE(
    ctypes.c_int, ctypes.c_void_p, ctypes.c_char_p,
    ctypes.POINTER(ctypes.c_uint8), ctypes.c_size_t, ctypes.POINTER(Buffer))
HOST_FREE = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_size_t)
PLUGIN_CALL = ctypes.CFUNCTYPE(
    ctypes.c_int, ctypes.c_char_p, ctypes.POINTER(ctypes.c_uint8),
    ctypes.c_size_t, ctypes.POINTER(Buffer))
PLUGIN_FREE = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_size_t)
PLUGIN_SHUTDOWN = ctypes.CFUNCTYPE(None)


class HostAPI(ctypes.Structure):
    _fields_ = [("abi_version", ctypes.c_uint32), ("host_ctx", ctypes.c_void_p),
                ("call", HOST_CALL), ("free_buffer", HOST_FREE)]


class PluginAPI(ctypes.Structure):
    _fields_ = [("abi_version", ctypes.c_uint32), ("call", PLUGIN_CALL),
                ("free_buffer", PLUGIN_FREE), ("shutdown", PLUGIN_SHUTDOWN)]


# 回调对象必须保持引用，否则被 GC 后调用会崩
_KEEPALIVE = []
_HOST_REQUESTS = []
MODEL_CALLS = []

MOCK_ACCOUNTS = [
    {
        "id": "11111111-1111-4111-8111-111111111111",
        "name": "codex-11111111-alice@example.com-plus.json",
        "email": "alice@example.com",
        "provider": "codex",
        "type": "codex",
        "plan_type": "plus",
        "status": "enabled",
        "disabled": False,
    },
    {
        "id": "antigravity-user@example.com",
        "name": "antigravity-user@example.com.json",
        "email": "user@example.com",
        "provider": "antigravity",
        "type": "antigravity",
        "plan_type": "pro",
        "status": "enabled",
        "disabled": False,
    },
    {
        "id": "22222222-2222-4222-8222-222222222222",
        "name": "codex-22222222-bob@example.com-plus.json",
        "email": "bob@example.com",
        "provider": "codex",
        "type": "codex",
        "plan_type": "plus",
        "status": "enabled",
        "disabled": False,
    },
]


def _respond(resp_ptr, payload: dict) -> int:
    raw = json.dumps(payload).encode()
    keep = ctypes.create_string_buffer(raw, len(raw))
    _KEEPALIVE.append(keep)
    resp = ctypes.cast(resp_ptr, ctypes.POINTER(Buffer))
    resp.contents.ptr = ctypes.cast(keep, ctypes.c_void_p)
    resp.contents.len = len(raw)
    return 0


@HOST_FREE
def host_free(ptr, size):
    # 宿主侧的缓冲区由 Python 自己管，这里不需要释放
    return


@HOST_CALL
def host_call(_ctx, method, req_ptr, req_len, resp_ptr):
    name = method.decode() if method else ""
    raw = ctypes.string_at(req_ptr, req_len) if req_len else b""
    try:
        req = json.loads(raw) if raw else {}
    except json.JSONDecodeError:
        req = {}
    _HOST_REQUESTS.append((name, req))

    if name == "host.auth.list":
        return _respond(resp_ptr, {"ok": True, "result": {"files": MOCK_ACCOUNTS}})

    if name == "host.model.execute":
        MODEL_CALLS.append(req)
        body = json.dumps({
            "choices": [{"message": {"content": "OK"}}],
        }).encode()
        return _respond(resp_ptr, {
            "ok": True,
            "result": {
                "status_code": 200,
                "headers": {
                    "x-codex-primary-window-minutes": ["300"],
                    "x-codex-primary-reset-after-seconds": ["17990"],
                    "x-codex-primary-used-percent": ["1.5"],
                },
                "body": base64.b64encode(body).decode(),
            },
        })

    if name in ("host.log",):
        return _respond(resp_ptr, {"ok": True, "result": {}})

    return _respond(resp_ptr, {
        "ok": False,
        "error": {"code": "unsupported", "message": f"模拟宿主未实现 {name}"},
    })


CONFIG_YAML = b"""timezone: Asia/Shanghai
state_file: state/window-anchor.json
plans:
  - auth_id: "11111111-1111-4111-8111-111111111111"
    model: "gpt-6-luna"
    times: ["05:30"]
  - auth_id: "22222222-2222-4222-8222-222222222222"
    model: "gpt-6-luna"
    times: ["08:30"]
  - provider: "antigravity"
    auth_id: "antigravity-user@example.com"
    model: "gemini-2.5-pro"
    times: ["07:00"]
"""

FAILED = []


def check(label, condition, detail=""):
    if condition:
        print(f"  [OK ] {label}")
    else:
        print(f"  [ERR] {label}  {detail}")
        FAILED.append(label)


def call_plugin(plugin, method, payload):
    raw = json.dumps(payload).encode()
    buf = (ctypes.c_uint8 * len(raw)).from_buffer_copy(raw)
    resp = Buffer()
    code = plugin.call(method.encode(), buf, len(raw), ctypes.byref(resp))
    if code != 0:
        raise RuntimeError(f"{method} 返回非零: {code}")
    data = ctypes.string_at(resp.ptr, resp.len) if resp.ptr else b""
    plugin.free_buffer(resp.ptr, resp.len)
    env = json.loads(data)
    if not env.get("ok"):
        raise RuntimeError(f"{method} 失败: {env.get('error')}")
    return env.get("result")


def mgmt_json(plugin, path, method="GET", body=None):
    """management.handle 必须返回 {status_code, headers, body(base64)} 结构。"""
    payload = {"method": method, "path": path}
    if body is not None:
        payload["body"] = base64.b64encode(json.dumps(body).encode()).decode()
    resp = call_plugin(plugin, "management.handle", payload)
    if resp.get("status_code") != 200:
        raise RuntimeError(f"{path} 返回 {resp.get('status_code')}")
    return json.loads(base64.b64decode(resp.get("body") or b"{}"))


def main():
    if len(sys.argv) < 2:
        print("用法: host_sim.py <动态库路径>", file=sys.stderr)
        return 2
    lib_path = Path(sys.argv[1])
    if not lib_path.is_file():
        print(f"找不到动态库: {lib_path}", file=sys.stderr)
        return 2

    # 每次从干净状态开始，避免上一轮的执行记录累积
    state_dir = Path("state")
    state_dir.mkdir(exist_ok=True)
    for leftover in state_dir.glob("*.json"):
        leftover.unlink()

    print(f"加载 {lib_path}")
    lib = ctypes.CDLL(str(lib_path))
    init = lib.cliproxy_plugin_init
    init.argtypes = [ctypes.POINTER(HostAPI), ctypes.POINTER(PluginAPI)]
    init.restype = ctypes.c_int

    host = HostAPI(abi_version=1, host_ctx=None, call=host_call, free_buffer=host_free)
    plugin = PluginAPI()
    rc = init(ctypes.byref(host), ctypes.byref(plugin))

    print("\n[1] 初始化")
    check("cliproxy_plugin_init 返回 0", rc == 0, f"返回 {rc}")
    check("plugin.abi_version == 1", plugin.abi_version == 1, str(plugin.abi_version))
    if FAILED:
        return 1

    print("\n[2] plugin.register")
    result = call_plugin(plugin, "plugin.register", {
        "config_yaml": base64.b64encode(CONFIG_YAML).decode(),
        "schema_version": 6,
    })
    meta = result.get("metadata") or {}
    caps = result.get("capabilities") or {}
    check("schema_version 是数字", isinstance(result.get("schema_version"), int), str(result.get("schema_version")))
    # 对应 CPA 的 validPlugin：这四个字段 TrimSpace 后都必须非空
    for field in ("Name", "Version", "Author", "GitHubRepository"):
        value = str(meta.get(field, "")).strip()
        check(f"metadata.{field} 非空", bool(value), repr(value))
    check("capabilities 至少有一项", any(caps.values()), str(caps))
    check("声明了 management_api", caps.get("management_api") is True, str(caps))

    print("\n[3] management.register")
    reg = call_plugin(plugin, "management.register", {})
    resources = {r["Path"]: r for r in reg.get("resources", [])}
    routes = {(r["Method"], r["Path"]) for r in reg.get("routes", [])}
    check("注册了带 Menu 的 /status 资源", "/status" in resources and bool(resources["/status"].get("Menu")),
          str(list(resources)))
    check("注册了 /app.js", "/app.js" in resources, str(list(resources)))
    check("注册了 status 路由", ("GET", "/plugins/window-anchor/status") in routes, str(routes))
    check("注册了 accounts 路由", ("GET", "/plugins/window-anchor/accounts") in routes, str(routes))
    check("注册了 run 路由", ("POST", "/plugins/window-anchor/run") in routes, str(routes))

    print("\n[4] management.handle -> /status")
    status = mgmt_json(plugin, "/plugins/window-anchor/status")
    plans = status.get("plans") or []
    check("plans 数量为 3（2 codex + 1 antigravity）", len(plans) == 3, f"实际 {len(plans)}")
    if plans:
        check("plan[0] 有 auth_id", bool(plans[0].get("auth_id")), str(plans[0]))
        check("plan[0] 有 times", plans[0].get("times") == ["05:30"], str(plans[0].get("times")))
        check("plan[0] 有 model", plans[0].get("model") == "gpt-6-luna", str(plans[0].get("model")))
        check("plan[0] 显示了账号邮箱", "alice" in str(plans[0].get("label", "")), str(plans[0].get("label")))
        check("plan[0] 有下次触发时间", bool(plans[0].get("next_trigger")), str(plans[0].get("next_trigger")))
        check("plan[1] 时间是 08:30", plans[1].get("times") == ["08:30"], str(plans[1].get("times")))
        ag = [p for p in plans if p.get("provider") == "antigravity"]
        check("存在 antigravity 计划", len(ag) == 1, str([p.get("provider") for p in plans]))
        if ag:
            check("antigravity 模型正确", ag[0].get("model") == "gemini-2.5-pro", str(ag[0].get("model")))

    print("\n[5] management.handle -> /accounts")
    acc = mgmt_json(plugin, "/plugins/window-anchor/accounts")
    accounts = acc.get("accounts") or []
    check("返回了 3 个账号（两个 provider）", len(accounts) == 3, f"实际 {len(accounts)}")

    print("\n[6] management.handle -> 资源")
    html = call_plugin(plugin, "management.handle", {"method": "GET", "path": "/status"})
    check("资源 /status 返回 200", html.get("status_code") == 200, str(html.get("status_code")))
    body = base64.b64decode(html.get("body") or "")
    check("资源 /status 返回 HTML", b"<html" in body.lower() or b"<!doctype" in body.lower(),
          body[:60].decode(errors="replace"))

    js = call_plugin(plugin, "management.handle", {"method": "GET", "path": "/app.js"})
    check("资源 /app.js 返回 200", js.get("status_code") == 200, str(js.get("status_code")))

    js_v = call_plugin(plugin, "management.handle", {"method": "GET", "path": "/app.js?v=0.2.2"})
    check("资源带 ?v= 版本参数也能返回", js_v.get("status_code") == 200, str(js_v.get("status_code")))

    raw_status = call_plugin(plugin, "management.handle", {"method": "GET", "path": "/status.json"})
    check("status.json 返回 200", raw_status.get("status_code") == 200, str(raw_status.get("status_code")))
    raw_body = json.loads(base64.b64decode(raw_status.get("body") or b"{}"))
    check("status.json 里有 plans", len(raw_body.get("plans") or []) == 3, str(len(raw_body.get("plans") or [])))

    print("\n[7] 手动触发（host.model.execute）")
    MODEL_CALLS.clear()
    triggered = mgmt_json(plugin, "/plugins/window-anchor/run", "POST",
                          {"auth_id": "11111111-1111-4111-8111-111111111111"})
    check("触发了 1 个账号", triggered.get("triggered") == 1, str(triggered))
    time.sleep(1.2)

    check("调用了 host.model.execute", len(MODEL_CALLS) == 1, f"实际 {len(MODEL_CALLS)}")
    if MODEL_CALLS:
        req = MODEL_CALLS[0]
        check("请求带 auth_id 且是目标账号",
              req.get("auth_id") == "11111111-1111-4111-8111-111111111111",
              str(req.get("auth_id")))
        check("请求 model 正确", req.get("model") == "gpt-6-luna", str(req.get("model")))
        check("指定了 forced_provider=codex", req.get("forced_provider") == "codex",
              str(req.get("forced_provider")))

    status2 = mgmt_json(plugin, "/plugins/window-anchor/status")
    results = status2.get("results") or []
    check("执行记录里有 1 条", len(results) == 1, f"实际 {len(results)}")
    if results:
        row = results[0]
        check("记录状态是 success", row.get("status") == "success", str(row.get("status")))
        check("记录带 reset_at（说明读到了 5h 窗口）", bool(row.get("reset_at")), str(row.get("reset_at")))
        check("记录带 model", row.get("model") == "gpt-6-luna", str(row.get("model")))

    print("\n[8] 配置校验（负向）")
    try:
        call_plugin(plugin, "plugin.reconfigure", {
            "config_yaml": base64.b64encode(b"timezone: ''\nplans: []\n").decode(),
            "schema_version": 6,
        })
        check("空 timezone 被拒绝", False, "竟然接受了")
    except RuntimeError as exc:
        check("空 timezone 被拒绝", "timezone" in str(exc), str(exc))

    try:
        call_plugin(plugin, "plugin.reconfigure", {
            "config_yaml": base64.b64encode(
                b'timezone: Asia/Shanghai\nplans:\n  - auth_id: a\n    model: m\n    times: ["05:30"]\n'
                b'  - auth_id: a\n    model: m\n    times: ["08:30"]\n').decode(),
            "schema_version": 6,
        })
        check("重复 auth_id 被拒绝", False, "竟然接受了")
    except RuntimeError as exc:
        check("重复 auth_id 被拒绝", "重复" in str(exc), str(exc))

    print("\n[9] 关闭")
    plugin.shutdown()
    check("shutdown 不崩溃", True)

    print()
    if FAILED:
        print(f"失败 {len(FAILED)} 项：")
        for item in FAILED:
            print(f"  - {item}")
        return 1
    print("全部通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
