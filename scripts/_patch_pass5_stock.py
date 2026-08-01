#!/usr/bin/env python3
"""Pass5: add stock optional params + first_bytes + fix weak hints in JSON."""
from __future__ import annotations

import json
from pathlib import Path

DATA = Path(__file__).resolve().parents[1] / "internal/controlplane/presets/data"

STRICT_MODE = {
    "type": "bool",
    "widget": "toggle",
    "default": "true",
    "ui_group": "core",
    "ui_order": 20,
    "title": {"en": "Strict mode", "ru": "Strict mode"},
    "description": {
        "en": "Reject handshakes that fail ShadowTLS strict checks.",
        "ru": "Отклонять handshake, не прошедшие strict-проверки ShadowTLS.",
    },
    "help": {
        "summary": {
            "en": "When true, ShadowTLS drops clients that fail strict handshake validation.",
            "ru": "При true ShadowTLS отбрасывает клиенты, не прошедшие strict handshake validation.",
        },
        "input_hint": {"en": "true|false", "ru": "true|false"},
        "format": "true",
    },
}

MTU = {
    "type": "uint16",
    "widget": "text",
    "default": "1400",
    "ui_group": "core",
    "ui_order": 30,
    "title": {"en": "MTU", "ru": "MTU"},
    "description": {
        "en": "Mieru path MTU.",
        "ru": "MTU пути Mieru.",
    },
    "help": {
        "summary": {
            "en": "Mieru MTU. Keep ≤ path MTU; 1280–1400 is typical behind CGNAT/mobile.",
            "ru": "MTU Mieru. Держите ≤ path MTU; 1280–1400 типично за CGNAT/mobile.",
        },
        "input_hint": {"en": "576–65535", "ru": "576–65535"},
        "format": "1400",
    },
}

WEBSOCKET = {
    "type": "bool",
    "widget": "toggle",
    "default": "false",
    "ui_group": "core",
    "ui_order": 25,
    "title": {"en": "WebSocket", "ru": "WebSocket"},
    "description": {
        "en": "Serve DERP over WebSocket (HTTP upgrade path).",
        "ru": "DERP поверх WebSocket (HTTP upgrade path).",
    },
    "help": {
        "summary": {
            "en": "Enable WebSocket transport for DERP. derp_ws defaults true; other profiles default false.",
            "ru": "Включить WebSocket transport для DERP. derp_ws по умолчанию true; остальные — false.",
        },
        "input_hint": {"en": "true|false", "ru": "true|false"},
        "format": "true",
    },
}

IGNORE_BW = {
    "type": "bool",
    "widget": "toggle",
    "default": "false",
    "ui_group": "core",
    "ui_order": 15,
    "title": {"en": "Ignore client bandwidth", "ru": "Ignore client bandwidth"},
    "description": {
        "en": "Ignore client-advertised up/down Mbps; use server caps only.",
        "ru": "Игнорировать up/down Mbps клиента; брать только серверные caps.",
    },
    "help": {
        "summary": {
            "en": "When true, Hy2 ignores client bandwidth advertisements and uses server up/down caps.",
            "ru": "При true Hy2 игнорирует bandwidth клиента и использует серверные up/down caps.",
        },
        "input_hint": {"en": "true|false", "ru": "true|false"},
        "format": "false",
    },
}

CC = {
    "type": "enum",
    "widget": "select",
    "enum": ["cubic", "bbr", "new_reno"],
    "default": "bbr",
    "ui_group": "core",
    "ui_order": 20,
    "title": {"en": "Congestion control", "ru": "Congestion control"},
    "description": {
        "en": "QUIC congestion algorithm (shadowquic.congestion_control).",
        "ru": "Алгоритм congestion QUIC (shadowquic.congestion_control).",
    },
}

ZERO_RTT = {
    "type": "bool",
    "widget": "toggle",
    "default": "false",
    "ui_group": "core",
    "ui_order": 21,
    "title": {"en": "0-RTT", "ru": "0-RTT"},
    "description": {
        "en": "Enable QUIC 0-RTT (faster resume, weaker replay resistance).",
        "ru": "Включить QUIC 0-RTT (быстрее resume, слабее replay resistance).",
    },
}

HA = {
    "type": "uint16",
    "widget": "text",
    "default": "4",
    "ui_group": "core",
    "ui_order": 20,
    "title": {"en": "HA connections", "ru": "HA connections"},
    "description": {
        "en": "Cloudflared edge HA connection count.",
        "ru": "Число HA-соединений cloudflared к edge.",
    },
}

PQ = {
    "type": "bool",
    "widget": "toggle",
    "default": "true",
    "ui_group": "core",
    "ui_order": 21,
    "title": {"en": "Post-quantum", "ru": "Post-quantum"},
    "description": {
        "en": "Prefer post-quantum key exchange on the tunnel.",
        "ru": "Предпочитать post-quantum key exchange на туннеле.",
    },
}

FIRST_BYTES = {
    "anytls": "TLS ClientHello (password TLS wrapper)",
    "anytls_idle": "TLS ClientHello (AnyTLS idle session)",
    "http_tls": "TLS → HTTP CONNECT",
    "hy2": "QUIC/H3 ClientHello",
    "mixed_tls": "TLS → HTTP CONNECT / SOCKS",
    "ss_aes256": "AEAD stream (SS)",
    "ss_chacha20": "AEAD stream (SS ChaCha)",
    "trojan_tls": "TLS ClientHello → Trojan",
    "trojan_tls_mux": "TLS ClientHello → Trojan+mux",
    "tuic": "QUIC/H3 TUIC",
    "vless_tls": "TLS ClientHello → VLESS",
    "vless_tls_mux": "TLS ClientHello → VLESS+mux",
    "vmess_tcp": "raw TCP VMess (no TLS)",
    "vmess_tls": "TLS ClientHello → VMess",
    "vmess_tls_mux": "TLS ClientHello → VMess+mux",
}

FALLBACK_HELP = {
    "summary": {
        "en": "HTTP(S) fallback URL shown to probes that fail Sudoku auth.",
        "ru": "HTTP(S) fallback URL для проб, не прошедших Sudoku auth.",
    },
    "input_hint": {
        "en": "http(s)://host[:port]/path]",
        "ru": "http(s)://host[:port]/path]",
    },
    "format": "http://127.0.0.1:80",
}


def add_opt(raw: dict, field: str, meta: dict, default_ws: str | None = None) -> None:
    opts = list(raw.get("optional_param_fields") or [])
    if field not in opts:
        opts.append(field)
    raw["optional_param_fields"] = opts
    pm = raw.setdefault("param_meta", {})
    if field not in pm:
        m = json.loads(json.dumps(meta))
        if default_ws is not None and "default" in m:
            m["default"] = default_ws
        pm[field] = m
    elif "help" not in pm[field] and "help" in meta:
        pm[field]["help"] = meta["help"]


def main() -> None:
    for p in sorted(DATA.rglob("*.json")):
        if p.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        tag = raw.get("tag") or p.stem
        proto = raw.get("protocol") or p.parent.name
        changed = False

        if proto == "shadowtls" and not raw.get("custom_preset"):
            add_opt(raw, "strict_mode", STRICT_MODE)
            changed = True

        if proto == "mieru" and not raw.get("custom_preset"):
            add_opt(raw, "mtu", MTU)
            changed = True

        if proto == "derp" and not raw.get("custom_preset"):
            default = "true" if tag == "derp_ws" else "false"
            add_opt(raw, "websocket", WEBSOCKET, default_ws=default)
            changed = True

        if proto == "hysteria2" and not raw.get("custom_preset"):
            # masquerade presets often already ignore client bw = true as identity
            ib = raw.get("inbound_template") or {}
            default = "true" if ib.get("ignore_client_bandwidth") is True else "false"
            add_opt(raw, "ignore_client_bandwidth", IGNORE_BW, default_ws=default)
            # ensure bandwidth options on masquerade_file
            if "up_mbps" not in (raw.get("param_meta") or {}) and "up_mbps" in json.dumps(ib):
                pass
            if tag == "hy2_masquerade_file":
                # add bandwidth if missing
                from copy import deepcopy

                bw_up = {
                    "type": "uint16",
                    "widget": "text",
                    "default": "100",
                    "ui_group": "core",
                    "ui_order": 10,
                    "title": {"en": "Upload Mbps", "ru": "Upload Mbps"},
                    "description": {
                        "en": "Hy2 upload cap (Mbps).",
                        "ru": "Лимит upload Hy2 (Mbps).",
                    },
                }
                bw_down = deepcopy(bw_up)
                bw_down["ui_order"] = 11
                bw_down["title"] = {"en": "Download Mbps", "ru": "Download Mbps"}
                bw_down["description"] = {
                    "en": "Hy2 download cap (Mbps).",
                    "ru": "Лимит download Hy2 (Mbps).",
                }
                add_opt(raw, "up_mbps", bw_up)
                add_opt(raw, "down_mbps", bw_down)
            changed = True

        if proto == "shadowquic" and not raw.get("custom_preset"):
            zdef = "true" if tag == "shadowquic_0rtt" else "false"
            add_opt(raw, "congestion_control", CC)
            add_opt(raw, "zero_rtt", ZERO_RTT, default_ws=zdef)
            changed = True

        if tag == "cloudflared_token":
            add_opt(raw, "ha_connections", HA)
            add_opt(raw, "post_quantum", PQ)
            changed = True

        dh = raw.get("demux_hints")
        if isinstance(dh, dict) and tag in FIRST_BYTES:
            if not (dh.get("first_bytes") or "").strip():
                dh["first_bytes"] = FIRST_BYTES[tag]
                changed = True

        if proto == "sudoku":
            for field, meta in (raw.get("param_meta") or {}).items():
                if field != "fallback":
                    continue
                help_m = meta.setdefault("help", {})
                if help_m.get("input_hint", {}).get("en") == "Value per operator docs" or not help_m.get(
                    "summary"
                ):
                    help_m.update(json.loads(json.dumps(FALLBACK_HELP)))
                    changed = True

        # tighten Mbps hints
        for field, meta in (raw.get("param_meta") or {}).items():
            if field in ("up_mbps", "down_mbps") and isinstance(meta, dict):
                help_m = meta.setdefault("help", {})
                hint = help_m.get("input_hint")
                if isinstance(hint, dict) and hint.get("en") == "Mbps":
                    hint["en"] = "1–65535 Mbps"
                    hint["ru"] = "1–65535 Mbps"
                    changed = True
                elif not hint:
                    help_m["input_hint"] = {"en": "1–65535 Mbps", "ru": "1–65535 Mbps"}
                    changed = True

        if changed:
            p.write_text(json.dumps(raw, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            print("patched", p.relative_to(DATA.parent.parent.parent))


if __name__ == "__main__":
    main()
