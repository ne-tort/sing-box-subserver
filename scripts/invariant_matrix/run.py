#!/usr/bin/env python3
"""
Docker + iperf3 invariant matrix runner for controlplane presets.

Examples:
  python run.py --protocol vless --stage 1
  python run.py --stage 1
  python run.py --all-manifests
  python run.py --protocol shadowquic
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import yaml

from render import (
    DEFAULT_LX_BIN,
    DEFAULT_PRESETS,
    IMAGE,
    SERVER_DNS,
    docker_path,
    render_protocol_workdir,
    rewrite_client_iperf_ip,
    write_json,
)

HERE = Path(__file__).resolve().parent
MATRICES = HERE / "matrices"
RESULTS = HERE / "results"
WORK = HERE / "work"
DOCS_MATRIX = HERE / ".." / ".." / "docs" / "guides" / "controlplane-presets" / "06-invariant-matrix.md"

NET_NAME = os.environ.get("INVMATRIX_NET", "invmatrix_net")
PROJECT = os.environ.get("INVMATRIX_PROJECT", "invmatrix")
MIN_MBPS = float(os.environ.get("MIN_MBPS", "0.5"))
IPERF_TIME = int(os.environ.get("IPERF_TIME", "5"))
COMPOSE_FILE_NAME = "docker-compose.generated.yml"


def load_manifest(path: Path) -> dict[str, Any]:
    data = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(f"bad manifest: {path}")
    data.setdefault("protocol", path.stem)
    data.setdefault("mode", "render")
    return data


def list_manifests() -> list[Path]:
    return sorted(MATRICES.glob("*.yaml"))


def filter_cells(manifest: dict[str, Any], stage: int | None) -> list[dict[str, Any]]:
    cells = list(manifest.get("cells") or [])
    # reuse/skip manifests may only have tags:
    if not cells and manifest.get("tags"):
        cells = [{"tag": t, "stage": int(manifest.get("stage") or 3)} for t in manifest["tags"]]
    out = []
    for c in cells:
        if isinstance(c, str):
            c = {"tag": c, "stage": 1}
        st = int(c.get("stage") or 1)
        if stage is not None and st != stage:
            continue
        out.append({"tag": c["tag"], "stage": st, **{k: v for k, v in c.items() if k not in ("tag", "stage")}})
    return out


def run_cmd(
    argv: list[str],
    *,
    cwd: Path | None = None,
    timeout: int | None = None,
    check: bool = False,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    return subprocess.run(
        argv,
        cwd=str(cwd) if cwd else None,
        timeout=timeout,
        check=check,
        capture_output=True,
        text=True,
        env=merged,
    )


def ensure_network() -> None:
    proc = run_cmd(["docker", "network", "inspect", NET_NAME])
    if proc.returncode == 0:
        return
    run_cmd(["docker", "network", "create", NET_NAME], check=True)


def write_compose(workdir: Path, lx_bin: Path) -> Path:
    compose = {
        "name": PROJECT,
        "services": {
            "iperf": {
                "image": IMAGE,
                "container_name": "inv-iperf",
                "networks": [NET_NAME],
                "command": ["iperf3", "-s"],
            },
            "handshake": {
                "image": IMAGE,
                "container_name": "inv-handshake",
                "hostname": "inv-handshake",
                "networks": {
                    NET_NAME: {
                        "aliases": ["inv-handshake", "www.microsoft.com"],
                    }
                },
                "volumes": [
                    f"{docker_path(workdir)}:/work:ro",
                ],
                "command": [
                    "bash",
                    "-c",
                    "openssl s_server -quiet -accept 443 "
                    "-cert /work/certs/reality-hs.crt -key /work/certs/reality-hs.key "
                    "-www >/tmp/hs.log 2>&1",
                ],
            },
            "server": {
                "image": IMAGE,
                "container_name": "inv-server",
                "hostname": SERVER_DNS,
                "networks": {
                    NET_NAME: {
                        "aliases": [SERVER_DNS],
                    }
                },
                "volumes": [
                    f"{docker_path(lx_bin)}:/bin-ro/sing-box:ro",
                    f"{docker_path(workdir)}:/work:ro",
                ],
                "command": [
                    "bash",
                    "-c",
                    "cp /bin-ro/sing-box /tmp/sing-box && chmod +x /tmp/sing-box && "
                    "/tmp/sing-box run -c /work/server.json",
                ],
                "depends_on": ["iperf", "handshake"],
            },
        },
        "networks": {
            NET_NAME: {
                "external": True,
                "name": NET_NAME,
            }
        },
    }
    path = workdir / COMPOSE_FILE_NAME
    path.write_text(yaml.safe_dump(compose, sort_keys=False), encoding="utf-8")
    return path


def compose_up(compose_path: Path) -> None:
    run_cmd(
        ["docker", "compose", "-p", PROJECT, "-f", str(compose_path), "up", "-d", "--force-recreate"],
        check=True,
        timeout=120,
    )


def compose_down(compose_path: Path | None) -> None:
    if compose_path is None or not compose_path.is_file():
        run_cmd(["docker", "compose", "-p", PROJECT, "down", "--remove-orphans"], timeout=120)
        return
    run_cmd(
        ["docker", "compose", "-p", PROJECT, "-f", str(compose_path), "down", "--remove-orphans"],
        timeout=120,
    )


def container_ip(name: str) -> str:
    out = run_cmd(
        [
            "docker",
            "inspect",
            "-f",
            "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}",
            name,
        ],
        check=True,
    ).stdout.strip()
    ips = [p for p in out.split() if p]
    if not ips:
        raise RuntimeError(f"no IP for {name}")
    return ips[0]


def wait_server_ready(timeout_s: float = 20.0) -> None:
    deadline = time.time() + timeout_s
    name = "inv-server"
    while time.time() < deadline:
        proc = run_cmd(["docker", "inspect", "-f", "{{.State.Running}} {{.State.ExitCode}}", name])
        if proc.returncode != 0:
            time.sleep(0.4)
            continue
        parts = proc.stdout.strip().split()
        running = parts[0] if parts else ""
        exit_code = parts[1] if len(parts) > 1 else ""
        if running == "true":
            # give sing-box a moment to bind / fatal-exit
            time.sleep(1.5)
            proc2 = run_cmd(["docker", "inspect", "-f", "{{.State.Running}}", name])
            if proc2.returncode == 0 and proc2.stdout.strip() == "true":
                logs = run_cmd(["docker", "logs", "--tail", "40", name]).stdout or ""
                if "FATAL" in logs:
                    raise RuntimeError(f"server FATAL\n{logs}")
                return
        if running == "false" and exit_code not in ("", "0"):
            logs = run_cmd(["docker", "logs", "--tail", "80", name]).stdout
            raise RuntimeError(f"server exited code={exit_code}\n{logs}")
        time.sleep(0.4)
    logs = run_cmd(["docker", "logs", "--tail", "80", name]).stdout
    raise RuntimeError(f"server container not ready\n{logs}")


def restart_iperf() -> None:
    run_cmd(["docker", "restart", "inv-iperf"], check=True)
    time.sleep(1.0)


def run_iperf_client(
    client_dir: Path,
    lx_bin: Path,
    name: str,
    *,
    udp: bool = False,
) -> tuple[bool, float | None, str]:
    udp_flag = "-u -b 10M" if udp else ""
    script = f"""
set -e
cp /bin-ro/sing-box /tmp/sing-box
chmod +x /tmp/sing-box
/tmp/sing-box run -c /work/client.json >/tmp/box.log 2>&1 &
pid=$!
sleep 2
set +e
iperf3 -c 127.0.0.1 -p 15201 {udp_flag} -t {IPERF_TIME} -J >/tmp/iperf.json 2>/tmp/iperf.err
rc=$?
set -e
kill $pid 2>/dev/null || true
wait $pid 2>/dev/null || true
echo "IPERF_RC=$rc"
if [ "$rc" -ne 0 ]; then
  echo FAIL
  cat /tmp/iperf.err 2>/dev/null || true
  tail -80 /tmp/box.log 2>/dev/null || true
  exit 1
fi
python3 - <<'PY'
import json
d=json.load(open("/tmp/iperf.json"))
end=d.get("end") or {{}}
recv=(end.get("sum_received") or {{}}).get("bits_per_second")
sent=(end.get("sum_sent") or {{}}).get("bits_per_second")
# UDP often only reports sum / sum_sent
if recv is None and sent is None:
    sm=end.get("sum") or {{}}
    recv=sm.get("bits_per_second")
bps=recv or sent or 0
print(f"BPS={{bps}}")
print(f"MBPS={{bps/1e6:.2f}}")
PY
echo PASS
"""
    proc = run_cmd(
        [
            "docker",
            "run",
            "--rm",
            "--name",
            name,
            "--network",
            NET_NAME,
            "-v",
            f"{docker_path(lx_bin)}:/bin-ro/sing-box:ro",
            "-v",
            f"{docker_path(client_dir)}:/work:ro",
            IMAGE,
            "bash",
            "-c",
            script.replace("\r\n", "\n"),
        ],
        timeout=IPERF_TIME + 90,
    )
    out = (proc.stdout or "") + (proc.stderr or "")
    mbps = None
    for line in out.splitlines():
        if line.startswith("MBPS="):
            try:
                mbps = float(line.split("=", 1)[1])
            except ValueError:
                pass
    ok = proc.returncode == 0 and "PASS" in out and mbps is not None and mbps >= MIN_MBPS
    return ok, mbps, out


def resolve_reuse_dir(raw: str) -> Path:
    p = Path(raw)
    if not p.is_absolute():
        p = (HERE / p).resolve()
    return p


def run_reuse(manifest: dict[str, Any], cells: list[dict[str, Any]]) -> list[dict[str, Any]]:
    if manifest.get("mode") == "skip":
        reason = manifest.get("skip_reason") or "skipped"
        return [
            {
                "tag": c["tag"],
                "stage": c.get("stage"),
                "status": "skip",
                "mbps": None,
                "detail": reason,
            }
            for c in cells
        ]

    reuse_dir = resolve_reuse_dir(str(manifest["reuse_dir"]))
    cmd = list(manifest.get("reuse_cmd") or ["docker", "compose", "up", "--abort-on-container-exit"])
    if not reuse_dir.is_dir():
        return [
            {
                "tag": c["tag"],
                "stage": c.get("stage"),
                "status": "fail",
                "mbps": None,
                "detail": f"reuse_dir missing: {reuse_dir}",
            }
            for c in cells
        ]

    print(f"[reuse] {manifest.get('protocol')}: cd {reuse_dir} && {' '.join(cmd)}")
    proc = run_cmd(cmd, cwd=reuse_dir, timeout=int(manifest.get("reuse_timeout") or 600))
    ok = proc.returncode == 0
    detail = ((proc.stdout or "") + (proc.stderr or ""))[-4000:]
    status = "pass" if ok else "fail"
    return [
        {
            "tag": c["tag"],
            "stage": c.get("stage"),
            "status": status,
            "mbps": None,
            "detail": detail if not ok else "reuse_cmd ok",
        }
        for c in cells
    ]


def run_render_matrix(
    protocol: str,
    cells: list[dict[str, Any]],
    *,
    lx_bin: Path,
    presets_root: Path,
    keep: bool,
) -> list[dict[str, Any]]:
    tags = [c["tag"] for c in cells]
    stage_by = {c["tag"]: c.get("stage") for c in cells}
    workdir = WORK / protocol
    if workdir.exists():
        # keep certs cache; clear generated configs
        for p in workdir.glob("**/*"):
            if p.is_file() and p.parent.name != "certs" and p.name not in ("server.crt", "server.key", "openssl.cnf"):
                try:
                    p.unlink()
                except OSError:
                    pass
    workdir.mkdir(parents=True, exist_ok=True)

    print(f"[render] {protocol}: {tags}")
    render_protocol_workdir(
        tags=tags,
        workdir=workdir,
        presets_root=presets_root,
        lx_bin=lx_bin,
        iperf_modes={c["tag"]: str(c.get("iperf") or c.get("iperf_mode") or "tcp") for c in cells},
    )
    compose_path = write_compose(workdir, lx_bin)
    results: list[dict[str, Any]] = []
    try:
        ensure_network()
        compose_down(compose_path)
        compose_up(compose_path)
        wait_server_ready()
        iperf_ip = container_ip("inv-iperf")
        server_ip = container_ip("inv-server")
        print(f"[iperf] {iperf_ip}:5201 server={server_ip}")

        for cell in cells:
            tag = cell["tag"]
            restart_iperf()
            rewrite_client_iperf_ip(workdir, tag, iperf_ip, server_ip=server_ip)
            client_dir = workdir / "clients" / tag
            # ensure client.json uses latest outbound (rewrite only IP)
            cname = f"{PROJECT}-cli-{re.sub(r'[^a-zA-Z0-9_.-]', '-', tag)[:40]}"
            # drop leftover client container if any
            run_cmd(["docker", "rm", "-f", cname])
            meta_path = client_dir / "meta.json"
            iperf_mode = "tcp"
            if meta_path.is_file():
                iperf_mode = str(json.loads(meta_path.read_text(encoding="utf-8")).get("iperf_mode") or "tcp")
            # Optional per-cell override from matrix yaml
            if cell.get("iperf") or cell.get("iperf_mode"):
                iperf_mode = str(cell.get("iperf") or cell.get("iperf_mode"))
            ok, mbps, out = run_iperf_client(client_dir, lx_bin, cname, udp=(iperf_mode == "udp"))
            status = "pass" if ok else "fail"
            print(f"  [{status}] {tag} mbps={mbps}")
            results.append(
                {
                    "tag": tag,
                    "stage": stage_by.get(tag),
                    "status": status,
                    "mbps": mbps,
                    "detail": out[-3000:] if not ok else f"mbps={mbps}",
                }
            )
    finally:
        if not keep:
            compose_down(compose_path)
    return results


def write_protocol_results(protocol: str, results: list[dict[str, Any]], meta: dict[str, Any]) -> Path:
    RESULTS.mkdir(parents=True, exist_ok=True)
    path = RESULTS / f"{protocol}.json"
    doc = {
        "protocol": protocol,
        "ts": datetime.now(timezone.utc).isoformat(),
        "min_mbps": MIN_MBPS,
        "iperf_time": IPERF_TIME,
        "results": results,
        "meta": meta,
    }
    write_json(path, doc)
    return path


def load_all_result_files() -> dict[str, list[dict[str, Any]]]:
    out: dict[str, list[dict[str, Any]]] = {}
    if not RESULTS.is_dir():
        return out
    for path in sorted(RESULTS.glob("*.json")):
        if path.name.startswith("_"):
            continue
        try:
            doc = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        proto = str(doc.get("protocol") or path.stem)
        out[proto] = list(doc.get("results") or [])
    return out


def write_summary(all_results: dict[str, list[dict[str, Any]]]) -> Path:
    RESULTS.mkdir(parents=True, exist_ok=True)
    merged = load_all_result_files()
    merged.update(all_results)
    lines = [
        "# Invariant matrix SUMMARY",
        "",
        f"Generated: {datetime.now(timezone.utc).isoformat()}",
        f"MIN_MBPS={MIN_MBPS} IPERF_TIME={IPERF_TIME}",
        "",
        "| protocol | tag | stage | status | Mbps |",
        "|----------|-----|-------|--------|------|",
    ]
    fails = passes = skips = 0
    for proto in sorted(merged):
        for r in merged[proto]:
            st = str(r.get("status") or "")
            if st == "pass":
                passes += 1
            elif st == "fail":
                fails += 1
            elif st == "skip":
                skips += 1
            mbps = r.get("mbps")
            mbps_s = f"{mbps:.2f}" if isinstance(mbps, (int, float)) else "-"
            lines.append(
                f"| {proto} | {r.get('tag')} | {r.get('stage')} | {r.get('status')} | {mbps_s} |"
            )
    lines.extend(
        [
            "",
            f"**Totals:** pass={passes} fail={fails} skip={skips}",
            "",
        ]
    )
    path = RESULTS / "SUMMARY.md"
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def update_docs_status(all_results: dict[str, list[dict[str, Any]]]) -> None:
    """Best-effort: replace status column cells matching `tag` in 06-invariant-matrix.md."""
    if not DOCS_MATRIX.is_file():
        return
    text = DOCS_MATRIX.read_text(encoding="utf-8")
    status_by_tag: dict[str, str] = {}
    for rows in all_results.values():
        for r in rows:
            tag = str(r.get("tag") or "")
            st = str(r.get("status") or "pending")
            if tag:
                status_by_tag[tag] = st
    if not status_by_tag:
        return

    def repl_line(line: str) -> str:
        # table rows like: | `vless_tls` | 1 | pending |
        for tag, st in status_by_tag.items():
            if f"`{tag}`" in line or f"| {tag} |" in line:
                # replace last non-empty status-ish token among pending/pass/fail/skip
                line2 = re.sub(
                    r"\b(pending|pass|fail|skip)\b(\s*\|)?\s*$",
                    lambda m: f"{st}{m.group(2) or ''}",
                    line.rstrip(),
                    count=1,
                    flags=re.IGNORECASE,
                )
                if line2 != line.rstrip():
                    return line2
                # also try middle status column: | tag | stage | status | notes |
                parts = [p.strip() for p in line.split("|")]
                if len(parts) >= 5:
                    for i, p in enumerate(parts):
                        if p.lower() in ("pending", "pass", "fail", "skip"):
                            parts[i] = st
                            return "| " + " | ".join(parts[1:-1]) + " |"
        return line

    new_lines = [repl_line(ln) for ln in text.splitlines()]
    DOCS_MATRIX.write_text("\n".join(new_lines) + "\n", encoding="utf-8")


def run_one_manifest(
    path: Path,
    *,
    stage: int | None,
    lx_bin: Path,
    presets_root: Path,
    keep: bool,
) -> tuple[str, list[dict[str, Any]]]:
    manifest = load_manifest(path)
    protocol = str(manifest.get("protocol") or path.stem)
    cells = filter_cells(manifest, stage)
    if not cells:
        print(f"[skip] {protocol}: no cells for stage filter {stage}")
        return protocol, []

    mode = str(manifest.get("mode") or "render")
    if mode in ("reuse", "skip"):
        results = run_reuse(manifest, cells)
    else:
        try:
            results = run_render_matrix(
                protocol,
                cells,
                lx_bin=lx_bin,
                presets_root=presets_root,
                keep=keep,
            )
        except Exception as exc:  # noqa: BLE001 — matrix continues other protocols
            detail = str(exc)[-3000:]
            print(f"[error] {protocol}: {detail}")
            results = [
                {
                    "tag": c["tag"],
                    "stage": c.get("stage"),
                    "status": "fail",
                    "mbps": None,
                    "detail": detail,
                }
                for c in cells
            ]
    write_protocol_results(protocol, results, {"manifest": str(path), "mode": mode})
    return protocol, results


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--protocol", help="Run a single matrices/<protocol>.yaml")
    ap.add_argument("--stage", type=int, help="Only cells with this stage number")
    ap.add_argument("--all-manifests", action="store_true", help="Run every matrices/*.yaml")
    ap.add_argument("--lx-bin", type=Path, default=DEFAULT_LX_BIN)
    ap.add_argument("--presets", type=Path, default=DEFAULT_PRESETS)
    ap.add_argument("--keep", action="store_true", help="Do not tear down compose after run")
    ap.add_argument("--list", action="store_true", help="List manifests/cells and exit")
    args = ap.parse_args()

    if not args.lx_bin.is_file() and not args.list:
        print(f"missing lx binary: {args.lx_bin}", file=sys.stderr)
        return 2

    manifests = list_manifests()
    if args.list:
        for mpath in manifests:
            man = load_manifest(mpath)
            cells = filter_cells(man, args.stage)
            print(f"{mpath.name}: mode={man.get('mode')} cells={len(cells)}")
            for c in cells:
                print(f"  - stage {c.get('stage')}: {c['tag']}")
        return 0

    selected: list[Path] = []
    if args.all_manifests:
        selected = manifests
    elif args.protocol:
        p = MATRICES / f"{args.protocol}.yaml"
        if not p.is_file():
            print(f"manifest not found: {p}", file=sys.stderr)
            return 2
        selected = [p]
    elif args.stage is not None:
        selected = manifests
    else:
        ap.print_help()
        print("\nSpecify --protocol, --stage, or --all-manifests", file=sys.stderr)
        return 2

    all_results: dict[str, list[dict[str, Any]]] = {}
    failed = 0
    for mpath in selected:
        proto, results = run_one_manifest(
            mpath,
            stage=args.stage,
            lx_bin=args.lx_bin,
            presets_root=args.presets,
            keep=args.keep,
        )
        if results:
            all_results[proto] = results
            failed += sum(1 for r in results if r.get("status") == "fail")

    if all_results:
        summary = write_summary(all_results)
        update_docs_status(all_results)
        print(f"wrote {summary}")
    print(f"done fails={failed}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
