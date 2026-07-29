#!/usr/bin/env python3
"""Multi-mode traffic/shaping matrix: controlplane + subscribe + static, VLESS.

Prereq: docker compose -f docker-compose.traffic-modes.yml up (see traffic_modes_docker.ps1).

Topology: client on front net only; httpsink/iperf on internal dataplane.
Traffic path: curl --socks5 → client sing-box → VLESS → agent → httpsink.

Modes:
  1) controlplane — many users, quotas/shaping/expiry, VLESS activate + real transfer
  2) subscribe — stub panel JSON, manual limits, inject, real VLESS transfer
  3) static — PUT /v1/config, reshape, inject, real VLESS transfer

Usage:
  python scripts/smoke_traffic_modes.py --insecure
  python scripts/smoke_traffic_modes.py --skip-iperf   # skip real VLESS transfers
"""

from __future__ import annotations

import argparse
import json
import os
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

TOKEN = "smoke-token-not-for-prod"
CP_BASE = os.environ.get("CP_BASE", "https://127.0.0.1:18082")
EDGE_BASE = os.environ.get("EDGE_BASE", "http://127.0.0.1:18083")
CLIENT = os.environ.get("CLIENT_CONTAINER", "traffic-modes-client")
ROOT = Path(__file__).resolve().parents[1]


def req(base: str, method: str, path: str, body=None, ctx=None, expect: int | None = 200, token: str = TOKEN):
    url = base.rstrip("/") + path
    data = None
    headers = {"Authorization": f"Bearer {token}"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, context=ctx, timeout=30) as resp:
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


def sh(cmd: list[str], check: bool = True) -> subprocess.CompletedProcess:
    print("+", " ".join(cmd))
    return subprocess.run(cmd, check=check, capture_output=True, text=True)


def docker_exec(container: str, cmd: list[str], check: bool = True) -> subprocess.CompletedProcess:
    return sh(["docker", "exec", container, *cmd], check=check)


def ensure_client_tools() -> None:
    r = docker_exec(CLIENT, ["sh", "-c", "command -v curl && command -v sing-box"], check=False)
    if r.returncode != 0:
        raise SystemExit(
            f"client container missing curl/sing-box: {r.stdout}{r.stderr}\n"
            "Rebuild with Dockerfile.traffic.client"
        )


def write_client_config(path: Path, server: str, port: int, uuid: str, socks_port: int = 11080) -> None:
    cfg = {
        "log": {"level": "error"},
        "inbounds": [
            {"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": socks_port}
        ],
        "outbounds": [
            {
                "type": "vless",
                "tag": "proxy",
                "server": server,
                "server_port": port,
                "uuid": uuid,
                "packet_encoding": "xudp",
            },
            {"type": "direct", "tag": "direct"},
        ],
        "route": {"final": "proxy"},
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(cfg, indent=2), encoding="utf-8")


def transfer_through_vless(server_host: str, server_port: int, uuid: str, label: str) -> float:
    """SOCKS -> VLESS -> agent -> httpsink/blob.bin; return elapsed seconds."""
    ensure_client_tools()
    socks_port = 11080 + (abs(hash(label)) % 1000)
    cfg_host = ROOT / "testdata" / "docker" / "client" / f"{label}.json"
    write_client_config(cfg_host, server_host, server_port, uuid, socks_port=socks_port)
    # Hard-reset any prior client so the SOCKS port is ours.
    docker_exec(CLIENT, ["sh", "-c", "killall -9 sing-box 2>/dev/null || true; pkill -9 -f sing-box || true"], check=False)
    time.sleep(0.8)
    docker_exec(
        CLIENT,
        ["sh", "-c", f"sing-box run -c /client/{label}.json >/tmp/sb-{label}.log 2>&1 & echo $! >/tmp/sb-{label}.pid"],
        check=False,
    )
    # Wait until SOCKS accepts connections (python slim has no ss/netstat).
    up = False
    for _ in range(30):
        alive = docker_exec(
            CLIENT,
            [
                "python",
                "-c",
                f"import socket;s=socket.socket();s.settimeout(0.5);"
                f"s.connect(('127.0.0.1',{socks_port}));s.close()",
            ],
            check=False,
        )
        if alive.returncode == 0:
            up = True
            break
        time.sleep(0.3)
    if not up:
        logs = docker_exec(CLIENT, ["sh", "-c", f"cat /tmp/sb-{label}.log || true"], check=False)
        raise SystemExit(f"sing-box client {label} failed to listen on {socks_port}: {logs.stdout}{logs.stderr}")

    # Direct reachability check must FAIL (internal dataplane).
    direct = docker_exec(
        CLIENT,
        ["sh", "-c", "curl -m 2 -s -o /dev/null -w '%{http_code}' http://httpsink:8088/blob.bin || echo FAIL"],
        check=False,
    )
    if "200" in (direct.stdout or ""):
        raise SystemExit("client can reach httpsink directly — network isolation broken")

    t0 = time.time()
    r = docker_exec(
        CLIENT,
        [
            "curl",
            "-sS",
            "-o",
            "/dev/null",
            "-w",
            "%{http_code} %{size_download} %{time_total}",
            "--connect-timeout",
            "5",
            "--max-time",
            "180",
            "--socks5-hostname",
            f"127.0.0.1:{socks_port}",
            "http://httpsink:8088/blob.bin",
        ],
        check=False,
    )
    elapsed = time.time() - t0
    docker_exec(CLIENT, ["sh", "-c", "killall -9 sing-box 2>/dev/null || true"], check=False)
    out = (r.stdout or "").strip()
    if r.returncode != 0 or not out.startswith("200"):
        logs = docker_exec(CLIENT, ["sh", "-c", f"tail -n 60 /tmp/sb-{label}.log || true"], check=False)
        raise SystemExit(
            f"VLESS transfer ({label}) failed rc={r.returncode} out={out!r}\n"
            f"stderr={r.stderr}\nclientlog={logs.stdout}"
        )
    parts = out.split()
    size = int(float(parts[1])) if len(parts) > 1 else 0
    curl_t = float(parts[2]) if len(parts) > 2 else elapsed
    if size < 1_500_000:
        raise SystemExit(f"download too small ({size}) for {label}")
    print(f"  transfer {label}: {size} bytes in {curl_t:.2f}s (wall {elapsed:.2f}s) via :{socks_port}")
    return curl_t

def mode_controlplane(ctx, skip_iperf: bool) -> None:
    print("\n======== MODE 1: controlplane (multi-user VLESS) ========")
    base = CP_BASE
    _, st = req(base, "GET", "/v1/traffic/status", ctx=ctx)
    if not (st.get("data") or {}).get("enabled"):
        raise SystemExit(f"traffic disabled on CP: {st}")

    users = {}
    specs = [
        ("fast", {"speed_up_bytes_per_sec": 0, "speed_down_bytes_per_sec": 0, "traffic_limit_bytes": 50_000_000}),
        ("slow", {"speed_up_bytes_per_sec": 65536, "speed_down_bytes_per_sec": 65536, "traffic_limit_bytes": 50_000_000}),
        ("quota", {"speed_up_bytes_per_sec": 0, "speed_down_bytes_per_sec": 0, "traffic_limit_bytes": 1500}),
        ("expiring", {"speed_up_bytes_per_sec": 0, "speed_down_bytes_per_sec": 0}),
    ]
    for name, patch in specs:
        # unique names per run
        uname = f"v-{name}"
        code, env = req(base, "POST", "/v1/controlplane/users", {"name": uname}, ctx=ctx, expect=None)
        if code == 409:
            # reuse existing
            _, listing = req(base, "GET", "/v1/controlplane/users", ctx=ctx)
            for u in listing.get("data") or []:
                if u.get("name") == uname:
                    uid = u["id"]
                    break
            else:
                raise SystemExit(f"conflict but user {uname} not found")
            _, uenv = req(base, "GET", f"/v1/controlplane/users/{uid}?secrets=1", ctx=ctx)
            data = uenv["data"]
        elif code != 200:
            raise SystemExit(f"create {uname}: {code} {env}")
        else:
            data = env["data"]
            uid = data["id"]
        req(base, "PATCH", f"/v1/controlplane/users/{uid}", patch, ctx=ctx)
        _, sec = req(base, "GET", f"/v1/controlplane/users/{uid}?secrets=1", ctx=ctx)
        data = sec["data"]
        creds = (data.get("creds") or {}).get("vless-tcp") or {}
        # may be empty until set activated with vless — create set first
        users[name] = {
            "id": data["id"],
            "name": data["name"],
            "sub_token": data.get("sub_token"),
            "uuid": creds.get("uuid"),
        }

    print("== activate VLESS set ==")
    code, _ = req(
        base,
        "POST",
        "/v1/controlplane/sets",
        {"name": "vless1", "listen": "0.0.0.0", "listen_port": 8443, "presets": ["vless-tcp"]},
        ctx=ctx,
        expect=None,
    )
    if code not in (200, 409):
        raise SystemExit(f"create set: {code}")
    req(base, "POST", "/v1/controlplane/sets/vless1/activate", ctx=ctx)

    # refresh UUIDs after activate (creds backfill)
    for name, u in users.items():
        _, sec = req(base, "GET", f"/v1/controlplane/users/{u['id']}?secrets=1", ctx=ctx)
        creds = (sec["data"].get("creds") or {}).get("vless-tcp") or {}
        u["uuid"] = creds.get("uuid")
        u["sub_token"] = sec["data"].get("sub_token")
        if not u["uuid"]:
            raise SystemExit(f"no vless uuid for {name}: {sec}")
        print(f"  user {name}: id={u['id']} uuid={u['uuid'][:8]}...")

    print("== shaping layers visible (CP speed_*) ==")
    _, lim = req(base, "GET", "/v1/traffic/limits", ctx=ctx)
    data_lim = lim.get("data") or {}
    eff = data_lim.get("effective") or {}
    cp_layer = data_lim.get("controlplane") or {}
    if "v-slow" not in eff:
        raise SystemExit(f"expected v-slow in effective limits: {lim}")
    if int(eff["v-slow"].get("up_bytes_per_sec") or 0) != 65536:
        raise SystemExit(f"slow shaping mismatch: {eff.get('v-slow')}")
    if "v-slow" not in cp_layer:
        raise SystemExit(f"v-slow missing from controlplane layer: {lim}")

    print("== manual layer wins over CP (ops override) ==")
    # Bare display name must expand onto VLESS variant keys (alice → alice-flow-*).
    req(
        base,
        "PUT",
        "/v1/traffic/limits",
        {"limits": {"v-slow": {"up_bytes_per_sec": 4096, "down_bytes_per_sec": 4096}}},
        ctx=ctx,
    )
    _, lim2 = req(base, "GET", "/v1/traffic/limits", ctx=ctx)
    d2 = lim2.get("data") or {}
    eff2 = d2.get("effective") or {}
    slow_variants = [k for k in eff2 if k == "v-slow" or k.startswith("v-slow-")]
    if not slow_variants:
        raise SystemExit(f"manual bare v-slow did not expand: {lim2}")
    for k in slow_variants:
        if int((eff2.get(k) or {}).get("down_bytes_per_sec") or 0) != 4096:
            raise SystemExit(f"expanded key {k} mismatch: {eff2.get(k)}")
    if int((d2.get("controlplane") or {}).get("v-slow", {}).get("up_bytes_per_sec") or 0) != 65536:
        raise SystemExit(f"CP layer wiped by manual PUT?: {lim2}")
    # clear manual so real transfer uses CP speed_* only
    req(base, "PUT", "/v1/traffic/limits", {"limits": {}}, ctx=ctx)

    print("== quota via inject (user quota) ==")
    q = users["quota"]
    req(base, "POST", "/v1/traffic/inject", {"user": q["name"], "up": 800, "down": 800}, ctx=ctx)
    crossed = False
    for _ in range(30):
        _, u = req(base, "GET", f"/v1/controlplane/users/{q['id']}", ctx=ctx)
        used = int((u.get("data") or {}).get("traffic_used_bytes") or 0)
        code, _ = req(base, "GET", f"/v1/sub/{q['sub_token']}", ctx=ctx, expect=None)
        if used >= 1500 and code == 403:
            crossed = True
            break
        time.sleep(1)
    if not crossed:
        raise SystemExit("quota user did not become ineligible via inject/bridge")
    # restore
    req(base, "PATCH", f"/v1/controlplane/users/{q['id']}", {"traffic_used_bytes": 0}, ctx=ctx)
    req(base, "GET", f"/v1/sub/{q['sub_token']}", ctx=ctx)

    print("== expiry semantics ==")
    ex = users["expiring"]
    past = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 3600))
    req(base, "PATCH", f"/v1/controlplane/users/{ex['id']}", {"expires_at": past}, ctx=ctx)
    req(base, "GET", f"/v1/sub/{ex['sub_token']}", ctx=ctx, expect=403)
    req(base, "PATCH", f"/v1/controlplane/users/{ex['id']}", {"expires_at": None}, ctx=ctx)
    req(base, "GET", f"/v1/sub/{ex['sub_token']}", ctx=ctx)

    if skip_iperf:
        print("== skip real VLESS transfer (flag) ==")
        return

    print("== real VLESS + SOCKS transfer shaping ==")
    # 2 MiB @ 64 KiB/s ≈ 32s (burst ≤64KiB); fast should be much quicker.
    t_slow = transfer_through_vless("subserver-cp", 8443, users["slow"]["uuid"], "cp-slow")
    t_fast = transfer_through_vless("subserver-cp", 8443, users["fast"]["uuid"], "cp-fast")
    if t_slow < 15.0:
        raise SystemExit(f"slow user too fast ({t_slow:.2f}s) — shaping not applied on VLESS path?")
    if t_fast >= t_slow * 0.5:
        raise SystemExit(f"fast ({t_fast:.2f}s) not clearly faster than slow ({t_slow:.2f}s)")
    print(f"  shaping OK: slow={t_slow:.2f}s fast={t_fast:.2f}s")

    print("== live unthrottle: clear CP speed_* then re-transfer ==")
    req(
        base,
        "PATCH",
        f"/v1/controlplane/users/{users['slow']['id']}",
        {"speed_up_bytes_per_sec": 0, "speed_down_bytes_per_sec": 0},
        ctx=ctx,
    )
    time.sleep(2)  # rematerialize + publishTrafficPolicy
    t_unthrottled = transfer_through_vless("subserver-cp", 8443, users["slow"]["uuid"], "cp-unthrottled")
    if t_unthrottled >= t_slow * 0.7:
        raise SystemExit(
            f"unthrottle failed: after clear still {t_unthrottled:.2f}s vs slow {t_slow:.2f}s"
        )
    print(f"  unthrottle OK: {t_unthrottled:.2f}s (was {t_slow:.2f}s)")
    # restore slow for any later probes
    req(
        base,
        "PATCH",
        f"/v1/controlplane/users/{users['slow']['id']}",
        {"speed_up_bytes_per_sec": 65536, "speed_down_bytes_per_sec": 65536},
        ctx=ctx,
    )


def mode_subscribe(ctx, skip_iperf: bool) -> None:
    print("\n======== MODE 2: subscribe (stub panel -> VLESS multi-user) ========")
    base = EDGE_BASE
    _, st = req(base, "GET", "/v1/traffic/status", ctx=None)
    if not (st.get("data") or {}).get("enabled"):
        raise SystemExit(f"traffic disabled on edge: {st}")

    # Cancel any prior ownership
    req(base, "DELETE", "/v1/subscribe", ctx=None, expect=None)
    time.sleep(0.5)

    print("== subscribe to mock vless-multi.json ==")
    _, sub = req(
        base,
        "POST",
        "/v1/subscribe",
        {
            "url": "http://mock-panel:8080/vless-multi.json",
            "interval_sec": 60,
            "jitter_sec": 0,
            "timeout_sec": 10,
        },
        ctx=None,
    )
    print("  subscribe:", json.dumps(sub.get("data") or sub)[:300])

    # wait ready
    ready = False
    for _ in range(30):
        code, r = req(base, "GET", "/v1/ready", ctx=None, expect=None)
        if code == 200:
            ready = True
            break
        time.sleep(1)
    if not ready:
        raise SystemExit("edge not ready after subscribe")

    print("== manual shaping for dataplane users ==")
    req(
        base,
        "PUT",
        "/v1/traffic/limits",
        {
            "limits": {
                "alice": {"up_bytes_per_sec": 0, "down_bytes_per_sec": 0},
                "bob": {"up_bytes_per_sec": 32768, "down_bytes_per_sec": 32768},
            }
        },
        ctx=None,
    )
    _, lim = req(base, "GET", "/v1/traffic/limits", ctx=None)
    eff = (lim.get("data") or {}).get("effective") or {}
    if int((eff.get("bob") or {}).get("up_bytes_per_sec") or 0) != 32768:
        raise SystemExit(f"subscribe mode limits missing: {lim}")
    manual = (lim.get("data") or {}).get("manual") or {}
    if "bob" not in manual:
        raise SystemExit(f"bob missing from manual layer: {lim}")

    print("== inject alice/bob counters ==")
    req(base, "POST", "/v1/traffic/inject", {"user": "alice", "up": 1000, "down": 2000}, ctx=None)
    req(base, "POST", "/v1/traffic/inject", {"user": "bob", "up": 100, "down": 50}, ctx=None)
    req(base, "POST", "/v1/traffic/inject", {"inbound": "in-vless", "up": 50, "down": 50}, ctx=None)
    _, st_alice = req(base, "GET", "/v1/traffic/stats?series_type=dataplane_user&key=alice", ctx=None)
    cum = (st_alice.get("data") or {}).get("cumulative") or []
    if not cum:
        raise SystemExit(f"no alice counters: {st_alice}")
    _, on = req(base, "GET", "/v1/traffic/onlines", ctx=None)
    print("  onlines:", on.get("data"))

    print("== subjects auto-discover ==")
    _, subj = req(base, "GET", "/v1/traffic/subjects", ctx=None)
    print("  subjects:", json.dumps(subj.get("data") or subj)[:400])

    if skip_iperf:
        print("== skip real VLESS transfer (flag) ==")
        return

    print("== real VLESS shaping (alice unlimited vs bob 32KiB/s) ==")
    uuid_alice = "11111111-1111-1111-1111-111111111111"
    uuid_bob = "22222222-2222-2222-2222-222222222222"
    t_bob = transfer_through_vless("subserver-edge", 8443, uuid_bob, "sub-bob")
    t_alice = transfer_through_vless("subserver-edge", 8443, uuid_alice, "sub-alice")
    # 2MiB @ 32KiB/s ≈ 64s
    if t_bob < 30.0:
        raise SystemExit(f"bob too fast ({t_bob:.2f}s) — subscribe shaping not on path?")
    if t_alice >= t_bob * 0.5:
        raise SystemExit(f"alice ({t_alice:.2f}s) not clearly faster than bob ({t_bob:.2f}s)")
    print(f"  subscribe shaping OK: bob={t_bob:.2f}s alice={t_alice:.2f}s")


def mode_static(ctx, skip_iperf: bool) -> None:
    print("\n======== MODE 3: static PUT /v1/config (VLESS multi-user) ========")
    base = EDGE_BASE
    # Leave subscribe if any — PUT should claim direct ownership
    cfg_path = ROOT / "testdata" / "docker" / "configs" / "vless-multi.json"
    cfg = json.loads(cfg_path.read_text(encoding="utf-8"))
    print("== PUT static config ==")
    _, put = req(base, "PUT", "/v1/config", cfg, ctx=None)
    print("  put:", json.dumps(put.get("data") or put)[:300])

    ready = False
    for _ in range(30):
        code, _ = req(base, "GET", "/v1/ready", ctx=None, expect=None)
        if code == 200:
            ready = True
            break
        time.sleep(1)
    if not ready:
        raise SystemExit("edge not ready after static PUT")

    print("== status / mode ==")
    _, status = req(base, "GET", "/v1/status", ctx=None)
    print("  status snippet:", json.dumps(status.get("data") or status)[:400])

    print("== reshape carol slow, alice unlimited ==")
    req(
        base,
        "PUT",
        "/v1/traffic/limits",
        {
            "limits": {
                "carol": {"up_bytes_per_sec": 65536, "down_bytes_per_sec": 65536},
                "alice": {"up_bytes_per_sec": 0, "down_bytes_per_sec": 0},
            }
        },
        ctx=None,
    )
    _, lim = req(base, "GET", "/v1/traffic/limits", ctx=None)
    eff = (lim.get("data") or {}).get("effective") or {}
    if "carol" not in eff:
        raise SystemExit(f"carol missing after static limits: {lim}")
    # alice 0/0 should be absent from effective
    if "alice" in eff and int(eff["alice"].get("up_bytes_per_sec") or 0) > 0:
        raise SystemExit(f"alice should be unlimited/absent: {eff.get('alice')}")

    print("== inject carol + flush semantics ==")
    req(base, "POST", "/v1/traffic/inject", {"user": "carol", "up": 5000, "down": 7000}, ctx=None)
    _, st = req(base, "GET", "/v1/traffic/stats?series_type=dataplane_user&key=carol", ctx=None)
    cum = (st.get("data") or {}).get("cumulative") or []
    total = 0
    for c in cum:
        total += int(c.get("up") or 0) + int(c.get("down") or 0)
    if total < 12000:
        raise SystemExit(f"carol cumulative too low: {st}")

    if not skip_iperf:
        print("== real VLESS shaping (carol 64KiB/s vs alice unlimited) ==")
        uuid_alice = "11111111-1111-1111-1111-111111111111"
        uuid_carol = "33333333-3333-3333-3333-333333333333"
        t_carol = transfer_through_vless("subserver-edge", 8443, uuid_carol, "static-carol")
        t_alice = transfer_through_vless("subserver-edge", 8443, uuid_alice, "static-alice")
        if t_carol < 15.0:
            raise SystemExit(f"carol too fast ({t_carol:.2f}s) — static shaping not on path?")
        if t_alice >= t_carol * 0.5:
            raise SystemExit(f"alice ({t_alice:.2f}s) not clearly faster than carol ({t_carol:.2f}s)")
        print(f"  static shaping OK: carol={t_carol:.2f}s alice={t_alice:.2f}s")

    print("== zero-ish: replace limits empty ==")
    req(base, "PUT", "/v1/traffic/limits", {"limits": {}}, ctx=None)
    _, lim2 = req(base, "GET", "/v1/traffic/limits", ctx=None)
    if (lim2.get("data") or {}).get("effective"):
        # may still have empty dict
        eff2 = (lim2.get("data") or {}).get("effective") or {}
        if any(int(v.get("up_bytes_per_sec") or 0) > 0 or int(v.get("down_bytes_per_sec") or 0) > 0 for v in eff2.values()):
            raise SystemExit(f"expected empty effective after clear: {lim2}")


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--insecure", action="store_true", default=True)
    p.add_argument("--skip-iperf", action="store_true", help="skip real VLESS SOCKS transfers")
    p.add_argument("--only", choices=["cp", "subscribe", "static", "all"], default="all")
    args = p.parse_args()
    ctx = ssl._create_unverified_context() if args.insecure else None

    # health waits
    print("== wait CP health ==")
    for _ in range(60):
        try:
            code, _ = req(CP_BASE, "GET", "/v1/health", ctx=ctx, expect=None, token="")
            # health may not need auth
            r = urllib.request.Request(CP_BASE.rstrip("/") + "/v1/health")
            with urllib.request.urlopen(r, context=ctx, timeout=3) as resp:
                if resp.status == 200:
                    break
        except Exception:
            time.sleep(2)
    else:
        raise SystemExit("CP health timeout")

    print("== wait edge health ==")
    for _ in range(60):
        try:
            with urllib.request.urlopen(EDGE_BASE.rstrip("/") + "/v1/health", timeout=3) as resp:
                if resp.status == 200:
                    break
        except Exception:
            time.sleep(2)
    else:
        raise SystemExit("edge health timeout")

    if args.only in ("cp", "all"):
        mode_controlplane(ctx, skip_iperf=args.skip_iperf)
    if args.only in ("subscribe", "all"):
        mode_subscribe(ctx, skip_iperf=args.skip_iperf)
    if args.only in ("static", "all"):
        mode_static(ctx, skip_iperf=args.skip_iperf)

    print("\n== OK traffic modes matrix ==")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
