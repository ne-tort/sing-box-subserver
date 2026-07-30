"""Build sing-box client configs from CP subscription outbounds and probe via Docker."""

from __future__ import annotations

import json
import os
import subprocess
import time
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent
DEFAULT_IMAGE = os.environ.get("CP_CLIENT_IMAGE", "ghcr.io/sagernet/sing-box:v1.12.12")
PROBE_URL = os.environ.get("CP_PROBE_URL", "https://1.1.1.1/cdn-cgi/trace")


def wrap_outbound(outbound: dict[str, Any], *, mixed_port: int = 11080) -> dict[str, Any]:
    tag = outbound.get("tag") or "proxy"
    return {
        "log": {"level": "info"},
        "inbounds": [
            {
                "type": "mixed",
                "tag": "mixed-in",
                "listen": "0.0.0.0",
                "listen_port": mixed_port,
            }
        ],
        "outbounds": [outbound, {"type": "direct", "tag": "direct"}],
        "route": {"final": tag},
    }


def write_client_configs(sub: dict[str, Any], out_dir: Path, *, mixed_port: int = 11080) -> list[dict[str, Any]]:
    out_dir.mkdir(parents=True, exist_ok=True)
    metas: list[dict[str, Any]] = []
    for i, ob in enumerate(sub.get("outbounds") or []):
        tag = ob.get("tag") or f"out-{i}"
        safe = "".join(c if c.isalnum() or c in "-_" else "_" for c in tag)
        cfg = wrap_outbound(ob, mixed_port=mixed_port)
        path = out_dir / f"{safe}.json"
        path.write_text(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        metas.append(
            {
                "tag": tag,
                "type": ob.get("type"),
                "server": ob.get("server"),
                "server_port": ob.get("server_port"),
                "config": path,
                "mixed_port": mixed_port,
            }
        )
    return metas


def _docker(*args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["docker", *args],
        check=check,
        text=True,
        capture_output=True,
    )


def stop_container(name: str) -> None:
    _docker("rm", "-f", name, check=False)


def start_client(name: str, config: Path, *, image: str = DEFAULT_IMAGE, mixed_port: int = 11080) -> None:
    stop_container(name)
    cfg_dir = str(config.parent.resolve())
    cfg_name = config.name
    _docker(
        "run",
        "-d",
        "--name",
        name,
        "--restart",
        "no",
        "-p",
        f"127.0.0.1:{mixed_port}:{mixed_port}",
        "-v",
        f"{cfg_dir}:/work:ro",
        image,
        "run",
        "-c",
        f"/work/{cfg_name}",
    )
    # wait for mixed port
    deadline = time.time() + 20
    last_err = ""
    while time.time() < deadline:
        ps = _docker("inspect", "-f", "{{.State.Running}}", name, check=False)
        if ps.stdout.strip() != "true":
            logs = _docker("logs", name, check=False)
            last_err = (logs.stdout or "") + (logs.stderr or "")
            time.sleep(0.5)
            continue
        # quick TCP check via docker exec curl to self is hard; just sleep briefly
        time.sleep(1.5)
        return
    logs = _docker("logs", name, check=False)
    raise RuntimeError(f"client {name} failed to start: {last_err}\n{(logs.stdout or '') + (logs.stderr or '')}")


def probe_proxy(*, mixed_port: int = 11080, url: str = PROBE_URL, timeout: float = 25) -> tuple[bool, str]:
    """HTTP GET through local mixed proxy using curl (more reliable than urllib+HTTPS)."""
    proxy = f"http://127.0.0.1:{mixed_port}"
    try:
        r = subprocess.run(
            [
                "curl",
                "-sS",
                "-m",
                str(int(timeout)),
                "-x",
                proxy,
                url,
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        out = (r.stdout or "") + (r.stderr or "")
        if r.returncode == 0 and out.strip():
            return True, out[:1500]
        return False, out[:1500] or f"curl exit {r.returncode}"
    except FileNotFoundError:
        # fallback
        try:
            import urllib.request

            req = urllib.request.Request(url, method="GET")
            opener = urllib.request.build_opener(
                urllib.request.ProxyHandler({"http": proxy, "https": proxy})
            )
            with opener.open(req, timeout=timeout) as resp:
                body = resp.read(4000).decode("utf-8", errors="replace")
                return True, body
        except Exception as e:
            return False, str(e)


def run_outbound_probe(
    outbound: dict[str, Any],
    *,
    work_dir: Path,
    name: str = "cp-vps-client",
    image: str = DEFAULT_IMAGE,
    mixed_port: int = 11080,
    url: str = PROBE_URL,
) -> dict[str, Any]:
    metas = write_client_configs({"outbounds": [outbound]}, work_dir, mixed_port=mixed_port)
    meta = metas[0]
    try:
        start_client(name, meta["config"], image=image, mixed_port=mixed_port)
        ok, detail = probe_proxy(mixed_port=mixed_port, url=url)
        logs = _docker("logs", "--tail", "80", name, check=False)
        return {
            "tag": meta["tag"],
            "type": meta["type"],
            "server": meta["server"],
            "server_port": meta["server_port"],
            "ok": ok,
            "detail": detail[:1500],
            "logs": ((logs.stdout or "") + (logs.stderr or ""))[-2000:],
        }
    finally:
        stop_container(name)
