#!/usr/bin/env python3
"""Docker/local smoke: traffic status, quota omit/restore, shaping publish, reset.

Usage against traffic+controlplane agent:
  python scripts/smoke_traffic_scenarios.py --base https://127.0.0.1:18082 --token SECRET --insecure
"""

from __future__ import annotations

import argparse
import json
import ssl
import sys
import time
import urllib.error
import urllib.request


def req(base: str, token: str, method: str, path: str, body=None, ctx=None, expect: int | None = 200):
    url = base.rstrip("/") + path
    data = None
    headers = {"Authorization": f"Bearer {token}"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, context=ctx, timeout=20) as resp:
            raw = resp.read().decode()
            code = resp.status
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        code = e.code
        if expect is not None and code != expect:
            raise SystemExit(f"{method} {path}: HTTP {code} (want {expect}): {raw}")
        try:
            return code, json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            return code, {"raw": raw}
    if expect is not None and code != expect:
        raise SystemExit(f"{method} {path}: HTTP {code} (want {expect}): {raw}")
    return code, json.loads(raw) if raw else {}


def config_mentions_user(cfg_body: dict, name: str) -> bool:
    data = cfg_body.get("data") or {}
    raw = data.get("raw") or data.get("config") or data
    if isinstance(raw, (dict, list)):
        text = json.dumps(raw)
    else:
        text = str(raw)
    return name in text


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--base", required=True)
    p.add_argument("--token", required=True)
    p.add_argument("--insecure", action="store_true")
    args = p.parse_args()
    ctx = ssl._create_unverified_context() if args.insecure else None

    print("== traffic status ==")
    _, st = req(args.base, args.token, "GET", "/v1/traffic/status", ctx=ctx)
    data = st.get("data") or {}
    if not st.get("ok") or not data.get("enabled"):
        raise SystemExit(f"traffic not enabled: {st}")
    print(json.dumps(data, indent=2))

    print("== create user + set + activate ==")
    _, uenv = req(args.base, args.token, "POST", "/v1/controlplane/users", {"name": "tuser"}, ctx=ctx)
    uid = uenv["data"]["id"]
    tok = uenv["data"]["sub_token"]
    req(
        args.base,
        args.token,
        "POST",
        "/v1/controlplane/sets",
        {
            "name": "tss",
            "listen": "0.0.0.0",
            "listen_port": 8443,
            "presets": ["shadowsocks-tcp"],
        },
        ctx=ctx,
    )
    req(args.base, args.token, "POST", "/v1/controlplane/sets/tss/activate", ctx=ctx)

    print("== shaping via speed_* ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"speed_up_bytes_per_sec": 1024, "speed_down_bytes_per_sec": 2048},
        ctx=ctx,
    )
    _, lim = req(args.base, args.token, "GET", "/v1/traffic/limits", ctx=ctx)
    eff = ((lim.get("data") or {}).get("effective") or {})
    tlim = eff.get("tuser") or {}
    if int(tlim.get("up_bytes_per_sec") or 0) != 1024 or int(tlim.get("down_bytes_per_sec") or 0) != 2048:
        raise SystemExit(f"expected CP speed on tuser, got {lim}")

    print("== manual limits survive rematerialize ==")
    req(
        args.base,
        args.token,
        "PUT",
        "/v1/traffic/limits",
        {"limits": {"ops-key": {"up_bytes_per_sec": 111, "down_bytes_per_sec": 222}}},
        ctx=ctx,
    )
    # Trigger CP rematerialize via no-op-ish speed touch.
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"speed_up_bytes_per_sec": 1024},
        ctx=ctx,
    )
    _, lim = req(args.base, args.token, "GET", "/v1/traffic/limits", ctx=ctx)
    data = lim.get("data") or {}
    if int(((data.get("manual") or {}).get("ops-key") or {}).get("up_bytes_per_sec") or 0) != 111:
        raise SystemExit(f"manual layer wiped by CP: {lim}")
    if int(((data.get("effective") or {}).get("tuser") or {}).get("up_bytes_per_sec") or 0) != 1024:
        raise SystemExit(f"CP layer missing after rematerialize: {lim}")

    print("== inbound metrics inject ==")
    req(
        args.base,
        args.token,
        "POST",
        "/v1/traffic/inject",
        {"inbound": "cp-in-tss-shadowsocks-tcp", "up": 10, "down": 20},
        ctx=ctx,
    )
    _, st_in = req(
        args.base,
        args.token,
        "GET",
        "/v1/traffic/stats?series_type=inbound&key=cp-in-tss-shadowsocks-tcp",
        ctx=ctx,
    )
    cum = (st_in.get("data") or {}).get("cumulative") or []
    if not cum:
        raise SystemExit(f"expected inbound cumulative, got {st_in}")

    _, subjects = req(args.base, args.token, "GET", "/v1/traffic/subjects", ctx=ctx)
    print("subjects keys:", list((subjects.get("data") or subjects).keys()) if isinstance(subjects.get("data") or subjects, dict) else type(subjects))

    print("== sub ok while eligible ==")
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx)

    print("== inject live bytes -> flush -> bridge quota ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_limit_bytes": 1000, "traffic_used_bytes": 0},
        ctx=ctx,
    )
    req(
        args.base,
        args.token,
        "POST",
        "/v1/traffic/inject",
        {"user": "tuser", "up": 700, "down": 400},
        ctx=ctx,
    )
    # Bridge polls every ~10s after Flush; wait for used + rematerialize.
    crossed = False
    for _ in range(30):
        _, u = req(args.base, args.token, "GET", f"/v1/controlplane/users/{uid}", ctx=ctx)
        used = int((u.get("data") or {}).get("traffic_used_bytes") or 0)
        code, _ = req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx, expect=None)
        if used >= 1000 and code == 403:
            crossed = True
            break
        time.sleep(1)
    if not crossed:
        raise SystemExit("inject path did not cross quota via bridge")
    _, st = req(
        args.base,
        args.token,
        "GET",
        f"/v1/traffic/stats?subject=cp:user:{uid}",
        ctx=ctx,
    )
    usage = ((st.get("data") or {}).get("subject_usage") or {})
    if int(usage.get("total") or 0) < 1000:
        raise SystemExit(f"stats subject_usage too low: {usage}")

    print("== restore via used=0 ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_used_bytes": 0},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx)

    print("== cross quota via PATCH used ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_limit_bytes": 1000, "traffic_used_bytes": 1000},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx, expect=403)
    _, cfg = req(args.base, args.token, "GET", "/v1/config", ctx=ctx)
    if config_mentions_user(cfg, "tuser"):
        raise SystemExit("tuser still present in config over quota")

    print("== restore via used=0 (after PATCH quota) ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_used_bytes": 0},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx)

    print("== expiry omit ==")
    past = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 3600))
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"expires_at": past},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx, expect=403)
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"expires_at": None},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx)

    print("== traffic reset_at restore ==")
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_limit_bytes": 100, "traffic_used_bytes": 100},
        ctx=ctx,
    )
    req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx, expect=403)
    past_reset = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 60))
    req(
        args.base,
        args.token,
        "PATCH",
        f"/v1/controlplane/users/{uid}",
        {"traffic_reset_at": past_reset, "traffic_reset_period_sec": 3600},
        ctx=ctx,
    )
    restored = False
    for _ in range(25):
        code, _ = req(args.base, args.token, "GET", f"/v1/sub/{tok}", ctx=ctx, expect=None)
        if code == 200:
            restored = True
            break
        if code != 403:
            raise SystemExit(f"unexpected sub status during reset wait: {code}")
        time.sleep(1)
    if not restored:
        raise SystemExit("user not restored after traffic_reset_at tick")

    print("== OK traffic scenarios ==")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
