#!/usr/bin/env python3
"""
Semantic controlplane client UX smoke (direct agent HTTP/HTTPS).

Env:
  CP_BASE   default https://127.0.0.1:18080 (local-agent; CP TLS profile often forces HTTPS)
  CP_TOKEN  Bearer agent token (required)
  CP_PORT   listen port for install (default 18443 to avoid clashes)
  CP_MODE   demux | presets (default demux)
  CP_INSECURE  default 1 — skip TLS verify for self-signed mgmt cert

Steps mirror client wizard:
  bootstrap → protocols → presets/demux(+substitutions) → ports
  → from-presets OR from-demux-group → status ready → users
  → subscription-tags → /v1/sub/{token} (+ optional filter)
"""
from __future__ import annotations

import json
import os
import ssl
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("CP_BASE", "https://127.0.0.1:18080").rstrip("/")
TOKEN = os.environ.get("CP_TOKEN", "").strip()
PORT = int(os.environ.get("CP_PORT", "18443"))
MODE = os.environ.get("CP_MODE", "demux")  # demux | presets
INSECURE = os.environ.get("CP_INSECURE", "1").strip() not in ("0", "false", "no")

_SSL_CTX = None
if BASE.startswith("https://") and INSECURE:
    _SSL_CTX = ssl._create_unverified_context()


def req(method: str, path: str, body: dict | None = None) -> tuple[int, dict]:
    data = None
    headers = {"Authorization": f"Bearer {TOKEN}", "Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=60, context=_SSL_CTX) as resp:
            raw = resp.read().decode()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw) if raw else {"error": raw}
        except json.JSONDecodeError:
            return e.code, {"error": raw}


def unwrap(env: dict):
    if isinstance(env, dict) and "data" in env:
        return env["data"]
    return env


def main() -> int:
    if not TOKEN:
        print("CP_TOKEN required", file=sys.stderr)
        return 2
    print(f"=== bootstrap {BASE} mode={MODE} ===")
    code, env = req("GET", "/v1/controlplane/client/bootstrap?lang=ru")
    if code != 200:
        print("bootstrap failed", code, env)
        return 1
    boot = unwrap(env)
    print("counts", boot.get("counts"))
    print("install_modes", [m.get("id") for m in boot.get("install_modes") or []])
    caps = boot.get("capabilities") or {}
    if caps.get("optional_listen_port") is not True:
        print("warn: optional_listen_port expected")

    code, env = req("GET", "/v1/controlplane/protocols?lang=ru")
    if code != 200:
        print("protocols failed", code, env)
        return 1
    protos = unwrap(env)
    print("protocols", len(protos) if isinstance(protos, list) else protos)

    code, env = req("GET", "/v1/controlplane/ports/availability?port=" + str(PORT))
    ports = unwrap(env)
    print("ports", ports)
    if MODE == "demux" and not ports.get("can_demux"):
        print("port not free for demux; set CP_PORT", file=sys.stderr)
        return 1

    if MODE == "demux":
        code, env = req("GET", "/v1/controlplane/demux-groups?lang=ru")
        groups = unwrap(env)
        tag = "dg_443_dual"
        if isinstance(groups, list) and groups:
            for g in groups:
                if g.get("tag") == "dg_443_dual":
                    tag = "dg_443_dual"
                    break
            else:
                tag = groups[0].get("tag") or tag
        print("using group", tag)
        code, env = req("GET", f"/v1/controlplane/demux-groups/{tag}/substitutions")
        if code != 200:
            print("substitutions failed", code, env)
            return 1
        subs = unwrap(env)
        print("substitutions slots", len((subs or {}).get("slots") or []))

        code, env = req(
            "POST",
            "/v1/controlplane/sets/from-demux-group",
            {"group": tag, "name": "e2e-dg", "listen_port": PORT, "activate": True},
        )
        data = unwrap(env)
        print("from-demux-group", code, "activated=", data.get("activated") if isinstance(data, dict) else None)
        if code not in (200, 201):
            print(env)
            return 1
        if isinstance(data, dict) and data.get("activated") is not True:
            print("activate failed", data.get("activate_error"))
            return 1
    else:
        code, env = req("GET", "/v1/controlplane/presets?protocol=vless&lang=ru")
        presets = unwrap(env)
        tag = "vless_reality"
        if isinstance(presets, list):
            for p in presets:
                if p.get("tag") == "vless_reality":
                    tag = "vless_reality"
                    break
        # listen_port omitted → server auto-picks (still pass PORT when set for determinism)
        item: dict = {"preset": tag, "name": "e2e-vless"}
        if PORT:
            item["listen_port"] = PORT
        code, env = req(
            "POST",
            "/v1/controlplane/sets/from-presets",
            {"items": [item], "activate": True},
        )
        data = unwrap(env)
        print("from-presets", code, "activated=", data.get("activated") if isinstance(data, dict) else None)
        if code not in (200, 201):
            print(env)
            return 1
        if isinstance(data, dict) and data.get("activated") is not True:
            print("activate failed", data.get("activate_error"), data)
            return 1

    ready = False
    for i in range(45):
        code, env = req("GET", "/v1/controlplane/status")
        st = unwrap(env)
        r = (st or {}).get("ready") or {}
        print(f"poll[{i}] ready={r.get('ok')} reasons={r.get('reasons')}")
        if r.get("ok"):
            ready = True
            break
        time.sleep(1)
    if not ready:
        print("ready timeout", file=sys.stderr)
        return 1

    uname = f"e2e-user-{int(time.time())}"
    code, env = req("POST", "/v1/controlplane/users", {"name": uname, "enabled": True})
    if code not in (200, 201):
        print("create user", code, env)
        return 1
    user = unwrap(env)
    tok = user.get("sub_token") or ""
    print("user", user.get("id"), "sub_path", user.get("subscription_path"))
    if not tok:
        print("no sub_token", file=sys.stderr)
        return 1

    code, env = req("GET", "/v1/controlplane/subscription-tags?active_only=true")
    print("subscription-tags status", code)
    tags = unwrap(env)
    if isinstance(tags, dict):
        print("tag keys", list(tags.keys())[:8])

    # public sub — no agent bearer
    r = urllib.request.Request(BASE + f"/v1/sub/{tok}", headers={"Accept": "application/json"})
    with urllib.request.urlopen(r, timeout=30, context=_SSL_CTX) as resp:
        sub = json.loads(resp.read().decode())
    outs = (sub.get("outbounds") if isinstance(sub, dict) else None) or []
    print("subscription outbounds", len(outs) if isinstance(outs, list) else type(outs))

    # filter probe (best-effort)
    r2 = urllib.request.Request(BASE + f"/v1/sub/{tok}?tag=hy2", headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(r2, timeout=30, context=_SSL_CTX) as resp:
            sub2 = json.loads(resp.read().decode())
        outs2 = (sub2.get("outbounds") if isinstance(sub2, dict) else None) or []
        print("subscription ?tag=hy2 outbounds", len(outs2) if isinstance(outs2, list) else outs2)
    except urllib.error.HTTPError as e:
        print("subscription filter skip", e.code)

    print("OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
