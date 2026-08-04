#!/usr/bin/env python3
"""Render a demux-group workdir for Docker+iperf matrix."""
from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
SUBSERVER = HERE.parents[1]
INV = HERE.parent / "invariant_matrix"
sys.path.insert(0, str(INV))

from render import (  # noqa: E402
    DEFAULT_LX_BIN,
    IMAGE,
    REALITY_SNI,
    SERVER_DNS,
    TLS_SNI,
    build_client_config,
    generate_reality_keypair,
    load_preset,
    lx_bin_ro_mount,
    lx_stage_cmds,
    needs_reality,
    needs_tls_certs,
    render_cell,
    traits_of,
    write_json,
)
from gen_certs import ensure_certs  # noqa: E402

PUBLIC_PORT = int(os.environ.get("DG_PUBLIC_PORT", "1443"))
DEFAULT_PRESETS = SUBSERVER / "internal" / "controlplane" / "catalogsqlite" / "ref"
# Cronet resolves TLS SNI itself; matrix.local often times out under demux/QUIC even with hosts pin.
NAIVE_PUBLIC_SNI = "www.apple.com"


def go_install(group: str, port: int, slot_presets: dict[str, str] | None = None) -> dict[str, Any]:
    gen = HERE / "gen_install.go"
    args = ["go", "run", "-tags", "with_controlplane", str(gen), group, str(port)]
    if slot_presets:
        args.append(",".join(f"{k}={v}" for k, v in slot_presets.items()))
    proc = subprocess.run(
        args,
        cwd=str(SUBSERVER),
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"gen_install failed:\n{proc.stderr or proc.stdout}")
    return json.loads(proc.stdout)


def set_tls_server_name(obj: dict[str, Any], sni: str) -> None:
    tls = obj.get("tls")
    if isinstance(tls, dict):
        tls["server_name"] = sni
        if "reality" in tls and isinstance(tls["reality"], dict):
            # Reality server_name is the ClientHello SNI demux matches.
            pass


def sanitize_naive_outbound(ob: dict[str, Any]) -> None:
    """Cronet naive rejects insecure/utls/alpn (mirror materialize.sanitizeNaiveOutboundTLS)."""
    if str(ob.get("type") or "") != "naive":
        return
    tls = ob.get("tls")
    if not isinstance(tls, dict):
        return
    tls.pop("insecure", None)
    tls.pop("utls", None)
    tls.pop("alpn", None)
    tls.pop("reality", None)
    tls.pop("ech", None)


def render_group_workdir(
    group: str,
    *,
    workdir: Path,
    lx_bin: Path = DEFAULT_LX_BIN,
    image: str = IMAGE,
    presets_root: Path = DEFAULT_PRESETS,
    public_port: int = PUBLIC_PORT,
    slot_presets: dict[str, str] | None = None,
) -> dict[str, Any]:
    workdir.mkdir(parents=True, exist_ok=True)
    install = go_install(group, public_port, slot_presets=slot_presets)
    set_obj = install["Set"] if "Set" in install else install["set"]
    member_ports = install.get("MemberPorts") or install.get("member_ports") or {}
    slot_snis = install.get("SlotSNIs") or install.get("slot_snis") or {}
    demux = set_obj.get("demux_template") or {}

    preset_sni: dict[str, str] = {}
    # TrustTunnel is picky: hostname + cert CN should align with ClientHello SNI.
    # Force matrix.local for TT slots and rewrite demux match accordingly.
    # Reality: keep unique demux_sni from go_install (multi-Reality groups need distinct SNIs).
    # Naive (Cronet): keep a public-resolvable demux_sni. matrix.local + hosts pin is
    # enough for direct TCP, but demux/QUIC often dies with "name not resolved".
    for b in set_obj.get("bindings") or []:
        pn = str(b.get("preset") or "")
        if pn.startswith("trusttunnel"):
            if not b.get("params"):
                b["params"] = {}
            b["params"]["demux_sni"] = TLS_SNI
            preset_sni[pn] = TLS_SNI
        elif pn.startswith("naive"):
            if not b.get("params"):
                b["params"] = {}
            cur = str((b.get("params") or {}).get("demux_sni") or "").strip()
            if not cur or cur == TLS_SNI:
                b["params"]["demux_sni"] = NAIVE_PUBLIC_SNI
            preset_sni[pn] = str(b["params"]["demux_sni"])
    # Rebuild preset_sni map after TT/naive override
    for b in set_obj.get("bindings") or []:
        pn = b.get("preset")
        params = b.get("params") or {}
        if pn and params.get("demux_sni"):
            preset_sni[str(pn)] = params["demux_sni"]

    # Patch demux rules: dial port → SNI from member
    port_to_sni: dict[int, str] = {}
    for tag, port in member_ports.items():
        if tag in preset_sni:
            port_to_sni[int(port)] = preset_sni[tag]
    rules = demux.get("rules") or []
    for rule in rules:
        if not isinstance(rule, dict):
            continue
        action = rule.get("action") or {}
        dial = action.get("dial") or {}
        port = dial.get("port")
        if port is None:
            continue
        sni = port_to_sni.get(int(port))
        if not sni:
            continue
        match = rule.get("match") or {}
        if isinstance(match.get("tls"), dict) and "sni" in match["tls"]:
            match["tls"]["sni"] = [sni]
        elif "sni" in match:
            match["sni"] = [sni]
        rule["match"] = match
    demux["rules"] = rules

    presets_list = list(set_obj.get("presets") or [])
    need_reality = False
    need_tls = False
    loaded: list[dict[str, Any]] = []
    for tag in presets_list:
        p = load_preset(presets_root, tag)
        loaded.append(p)
        if needs_reality(p):
            need_reality = True
        if needs_tls_certs(p) or p.get("protocol") == "trusttunnel":
            need_tls = True

    certs = workdir / "certs"
    certs.mkdir(exist_ok=True)
    # Cronet/naive cannot use insecure: leaf must SAN-match every demux ClientHello SNI.
    san_extra = {
        str(v).strip()
        for v in list(preset_sni.values()) + list(slot_snis.values())
        if str(v).strip() and str(v).strip() not in (TLS_SNI, "inv-server", "localhost")
    }
    if any(str(p.get("tag") or "").startswith("naive") for p in loaded):
        san_extra.add(NAIVE_PUBLIC_SNI)
    san_extra_list = sorted(san_extra)
    cert_host, key_host = ensure_certs(certs, extra_dns=san_extra_list, force=bool(san_extra_list))
    cert_in = "/work/certs/server.crt"
    key_in = "/work/certs/server.key"

    reality_keys = generate_reality_keypair(lx_bin, image=image) if need_reality else None

    cells: list[dict[str, Any]] = []
    for p in loaded:
        tag = p["tag"]
        priv_port = int(member_ports.get(tag) or 0)
        if priv_port == 0:
            raise RuntimeError(f"missing member port for {tag}")
        sni = preset_sni.get(tag)
        if not sni:
            if needs_reality(p):
                sni = REALITY_SNI
            elif p.get("protocol") == "shadowquic":
                sni = ""  # resolve from inbound jls after render
            else:
                sni = TLS_SNI
        cell = render_cell(
            p,
            port=priv_port,
            cert_in_container=cert_in,
            key_in_container=key_in,
            cert_host=cert_host,
            key_host=key_host,
            reality_keys=reality_keys,
            lx_bin=lx_bin,
            image=image,
            server_host=SERVER_DNS,
            listen="127.0.0.1",
        )
        # Force demux-facing SNI on inbound/outbound TLS
        set_tls_server_name(cell["inbound"], sni)
        set_tls_server_name(cell["outbound"], sni)
        if p.get("protocol") == "trusttunnel":
            cell["inbound"]["hostname"] = sni
            cell["outbound"]["hostname"] = sni
            # anti_dpi + demux dial can yield zero throughput in matrix; keep DPI off for harness.
            for side in (cell["inbound"], cell["outbound"]):
                tr = side.get("transport")
                if isinstance(tr, dict):
                    tr["anti_dpi"] = False
        if p.get("protocol") == "shadowquic":
            # Prefer demux_sni; else keep template JLS identity (do NOT force matrix.local).
            if not preset_sni.get(tag):
                jls = cell["inbound"].get("jls_upstream") if isinstance(cell["inbound"].get("jls_upstream"), dict) else {}
                sni = str(jls.get("server_name") or "www.cloudflare.com")
            for side in (cell["inbound"], cell["outbound"]):
                if not isinstance(side, dict):
                    continue
                if "server_name" in side:
                    side["server_name"] = sni
                if "sni" in side:
                    side["sni"] = sni
                jls = side.get("jls_upstream")
                if isinstance(jls, dict) and sni:
                    jls["server_name"] = sni
            # JLS camouflage SNI must match client wire SNI and demux match.
        if needs_reality(p):
            # Client dials public demux port with Reality SNI
            cell["outbound"]["server_port"] = public_port
            tls = cell["outbound"].setdefault("tls", {})
            tls["server_name"] = sni
        elif needs_tls_certs(p) or p.get("protocol") == "trusttunnel":
            cell["outbound"]["server_port"] = public_port
            set_tls_server_name(cell["outbound"], sni)
            if isinstance(cell["outbound"].get("tls"), dict) and cell["outbound"].get("type") != "naive":
                cell["outbound"]["tls"]["insecure"] = True
        else:
            # plain / quic without template tls — still dial public port
            cell["outbound"]["server_port"] = public_port
            if isinstance(cell["outbound"].get("tls"), dict):
                set_tls_server_name(cell["outbound"], sni)
                if cell["outbound"].get("type") != "naive":
                    cell["outbound"]["tls"]["insecure"] = True

        # QUIC/Hy2 often need server_name on tls even when protocol_only
        if "quic" in traits_of(p) or "udp" in traits_of(p):
            tls = cell["outbound"].get("tls")
            if isinstance(tls, dict) and sni:
                tls["server_name"] = sni
                if cell["outbound"].get("type") != "naive":
                    tls.setdefault("insecure", True)
            tls_ib = cell["inbound"].get("tls")
            if isinstance(tls_ib, dict) and sni:
                tls_ib["server_name"] = sni

        sanitize_naive_outbound(cell["outbound"])
        cell["demux_sni"] = sni
        cell["public_port"] = public_port
        cells.append(cell)

    demux_ib = {
        "type": "demux",
        "tag": "cp-demux-dg-matrix",
        "listen": "0.0.0.0",
        "listen_port": public_port,
        "network": demux.get("network") or ["tcp", "udp"],
        "inspect_timeout": "3s",
        "rules": demux.get("rules") or [],
    }

    server = {
        "log": {"level": "warn"},
        "inbounds": [demux_ib] + [c["inbound"] for c in cells],
        "outbounds": [{"type": "direct", "tag": "direct"}],
    }
    write_json(workdir / "server.json", server)
    write_json(
        workdir / "cells.json",
        [
            {
                "tag": c["tag"],
                "port": c["port"],
                "protocol": c["protocol"],
                "traits": c["traits"],
                "iperf_mode": "tcp",
                "demux_sni": c.get("demux_sni"),
            }
            for c in cells
        ],
    )

    clients = workdir / "clients"
    clients.mkdir(exist_ok=True)
    for c in cells:
        tag = c["tag"]
        cdir = clients / tag
        cdir.mkdir(exist_ok=True)
        iperf_mode = "tcp"
        client = build_client_config(
            c["outbound"],
            "127.0.0.1",  # rewritten by run.py to inv-iperf IP
            traits=c["traits"],
            iperf_mode=iperf_mode,
        )
        write_json(cdir / "client.json", client)
        write_json(
            cdir / "meta.json",
            {
                "tag": tag,
                "iperf_mode": iperf_mode,
                "demux_sni": c.get("demux_sni"),
                "public_port": public_port,
                "member_port": c["port"],
            },
        )

    # compose
    work = str(workdir.resolve()).replace("\\", "/")
    lx_mount = lx_bin_ro_mount(lx_bin)
    stage = lx_stage_cmds()
    compose = f"""name: dgmatrix
services:
  iperf:
    image: {image}
    container_name: inv-iperf
    networks: [invmatrix_net]
    command: [iperf3, -s]
  handshake:
    image: {image}
    container_name: inv-handshake
    hostname: inv-handshake
    networks:
      invmatrix_net:
        aliases: [inv-handshake, www.microsoft.com, www.apple.com, www.amazon.com]
    volumes:
      - {work}:/work:ro
    command:
      - bash
      - -c
      - openssl s_server -quiet -accept 443 -cert /work/certs/reality-hs.crt -key /work/certs/reality-hs.key -www >/tmp/hs.log 2>&1
  server:
    image: {image}
    container_name: inv-server
    hostname: inv-server
    networks:
      invmatrix_net:
        aliases: [inv-server]
    volumes:
      - {lx_mount}
      - {work}:/work:ro
    command:
      - bash
      - -c
      - {stage} && /tmp/sing-box run -c /work/server.json
    depends_on: [iperf, handshake]
networks:
  invmatrix_net:
    external: true
    name: invmatrix_net
"""
    (workdir / "docker-compose.generated.yml").write_text(compose, encoding="utf-8")
    write_json(
        workdir / "render_info.json",
        {
            "group": group,
            "public_port": public_port,
            "member_ports": member_ports,
            "slot_snis": slot_snis,
            "preset_sni": preset_sni,
            "tags": [c["tag"] for c in cells],
            "reality": bool(reality_keys),
        },
    )
    return {"cells": cells, "install": install, "workdir": str(workdir)}


if __name__ == "__main__":
    g = sys.argv[1] if len(sys.argv) > 1 else "dg_443_dual"
    wd = HERE / "work" / g
    info = render_group_workdir(g, workdir=wd)
    print(json.dumps({"ok": True, "tags": [c["tag"] for c in info["cells"]], "workdir": info["workdir"]}, indent=2))
