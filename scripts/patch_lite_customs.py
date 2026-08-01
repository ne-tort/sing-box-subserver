#!/usr/bin/env python3
"""Upgrade lite *_custom.json with real protocol knobs (meta + templates)."""
from __future__ import annotations

import json
from pathlib import Path

DATA = Path(__file__).resolve().parents[1] / "internal/controlplane/presets/data"


def save(rel: str, obj: dict) -> None:
    path = DATA / rel
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", rel)


def main() -> None:
    # --- anytls ---
    anytls = json.loads((DATA / "anytls/anytls_custom.json").read_text(encoding="utf-8"))
    anytls["i18n"] = {
        "ru": {
            "title": "AnyTLS конструктор",
            "description": "AnyTLS: uTLS fingerprint, ALPN, опциональные idle_session_* на outbound.",
        },
        "en": {
            "title": "AnyTLS constructor",
            "description": "AnyTLS: uTLS fingerprint, ALPN, optional idle_session_* on outbound.",
        },
    }
    anytls["optional_param_fields"] = ["fingerprint", "alpn", "idle_session"]
    anytls["param_meta"] = {
        "fingerprint": {
            "type": "string",
            "widget": "text",
            "default": "chrome",
            "ui_group": "tls",
            "ui_order": 1,
            "title": {"en": "uTLS fingerprint", "ru": "uTLS fingerprint"},
            "description": {
                "en": "Client uTLS fingerprint on outbound.",
                "ru": "uTLS fingerprint клиента на outbound.",
            },
        },
        "alpn": {
            "type": "string",
            "widget": "text",
            "default": "h2,http/1.1",
            "ui_group": "tls",
            "ui_order": 2,
            "title": {"en": "ALPN list", "ru": "Список ALPN"},
            "description": {
                "en": "Comma-separated ALPN for AnyTLS TLS.",
                "ru": "ALPN через запятую для AnyTLS TLS.",
            },
        },
        "idle_session": {
            "type": "bool",
            "widget": "toggle",
            "default": "false",
            "ui_group": "advanced",
            "ui_order": 10,
            "title": {"en": "Idle session", "ru": "Idle session"},
            "description": {
                "en": "Outbound idle_session_check_interval/timeout=30s (warm TLS).",
                "ru": "Outbound idle_session_* = 30s (warm TLS).",
            },
        },
    }
    anytls["outbound_template"]["tls"]["utls"]["fingerprint"] = "{{param.fingerprint}}"
    save("anytls/anytls_custom.json", anytls)

    # --- shadowtls ---
    st = json.loads((DATA / "shadowtls/shadowtls_custom.json").read_text(encoding="utf-8"))
    st["i18n"] = {
        "ru": {
            "title": "ShadowTLS конструктор",
            "description": "ShadowTLS v3: handshake_server, strict_mode, wildcard_sni, uTLS fingerprint.",
        },
        "en": {
            "title": "ShadowTLS constructor",
            "description": "ShadowTLS v3: handshake_server, strict_mode, wildcard_sni, uTLS fingerprint.",
        },
    }
    st["param_fields"] = ["handshake_server"]
    st["optional_param_fields"] = ["strict_mode", "wildcard_sni", "fingerprint"]
    st["param_meta"] = {
        "handshake_server": st["param_meta"]["handshake_server"],
        "strict_mode": {
            "type": "bool",
            "widget": "toggle",
            "default": "true",
            "ui_group": "core",
            "ui_order": 21,
            "title": {"en": "Strict mode", "ru": "Strict mode"},
            "description": {
                "en": "ShadowTLS v3 strict_mode on inbound.",
                "ru": "strict_mode на inbound ShadowTLS v3.",
            },
        },
        "wildcard_sni": {
            "type": "enum",
            "widget": "select",
            "enum": ["off", "authed", "all"],
            "default": "off",
            "ui_group": "core",
            "ui_order": 22,
            "title": {"en": "Wildcard SNI", "ru": "Wildcard SNI"},
            "description": {
                "en": "Inbound wildcard_sni: off|authed|all.",
                "ru": "Inbound wildcard_sni: off|authed|all.",
            },
        },
        "fingerprint": {
            "type": "string",
            "widget": "text",
            "default": "chrome",
            "ui_group": "tls",
            "ui_order": 23,
            "title": {"en": "uTLS fingerprint", "ru": "uTLS fingerprint"},
            "description": {
                "en": "Outbound uTLS fingerprint.",
                "ru": "uTLS fingerprint на outbound.",
            },
        },
    }
    st["inbound_template"]["strict_mode"] = True
    st["inbound_template"]["wildcard_sni"] = "{{param.wildcard_sni}}"
    st["outbound_template"]["tls"]["utls"]["fingerprint"] = "{{param.fingerprint}}"
    save("shadowtls/shadowtls_custom.json", st)

    # --- sudoku ---
    su = json.loads((DATA / "sudoku/sudoku_custom.json").read_text(encoding="utf-8"))
    su["i18n"] = {
        "ru": {
            "title": "Sudoku конструктор",
            "description": "Sudoku: AEAD method, multiplex, padding, fallback URL.",
        },
        "en": {
            "title": "Sudoku constructor",
            "description": "Sudoku: AEAD method, multiplex, padding bounds, fallback URL.",
        },
    }
    su["param_fields"] = ["aead_method"]
    su["optional_param_fields"] = ["fallback", "multiplex", "padding_min", "padding_max"]
    su["param_meta"] = {
        "aead_method": {
            "type": "enum",
            "widget": "select",
            "enum": ["aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305"],
            "default": "aes-128-gcm",
            "ui_group": "core",
            "ui_order": 1,
            "title": {"en": "AEAD method", "ru": "AEAD method"},
            "description": {
                "en": "Sudoku aead_method (peer-symmetric).",
                "ru": "Sudoku aead_method (симметрично).",
            },
        },
        "multiplex": {
            "type": "enum",
            "widget": "select",
            "enum": ["off", "auto"],
            "default": "off",
            "ui_group": "core",
            "ui_order": 2,
            "title": {"en": "Multiplex", "ru": "Multiplex"},
            "description": {
                "en": "Sudoku multiplex: off|auto.",
                "ru": "Sudoku multiplex: off|auto.",
            },
        },
        "padding_min": {
            "type": "uint16",
            "widget": "text",
            "default": "3",
            "ui_group": "advanced",
            "ui_order": 10,
            "title": {"en": "Padding min", "ru": "Padding min"},
            "description": {"en": "padding_min.", "ru": "padding_min."},
        },
        "padding_max": {
            "type": "uint16",
            "widget": "text",
            "default": "12",
            "ui_group": "advanced",
            "ui_order": 11,
            "title": {"en": "Padding max", "ru": "Padding max"},
            "description": {"en": "padding_max (≥ min).", "ru": "padding_max (≥ min)."},
        },
        "fallback": su["param_meta"]["fallback"],
    }
    for side in ("inbound_template", "outbound_template"):
        su[side]["aead_method"] = "{{param.aead_method}}"
        su[side]["multiplex"] = "{{param.multiplex}}"
    save("sudoku/sudoku_custom.json", su)

    # --- mieru ---
    mi = json.loads((DATA / "mieru/mieru_custom.json").read_text(encoding="utf-8"))
    mi["i18n"] = {
        "ru": {
            "title": "Mieru конструктор",
            "description": "Mieru: transport TCP/UDP, multiplexing, MTU, traffic_pattern.",
        },
        "en": {
            "title": "Mieru constructor",
            "description": "Mieru: TCP/UDP transport, multiplexing, MTU, traffic_pattern.",
        },
    }
    mi["param_fields"] = ["transport"]
    mi["optional_param_fields"] = ["multiplexing", "mtu", "traffic_pattern"]
    mi["param_meta"] = {
        "transport": {
            "type": "enum",
            "widget": "select",
            "enum": ["TCP", "UDP"],
            "default": "TCP",
            "ui_group": "transport",
            "ui_order": 1,
            "title": {"en": "Transport", "ru": "Транспорт"},
            "description": {
                "en": "Mieru transport TCP|UDP.",
                "ru": "Транспорт Mieru TCP|UDP.",
            },
        },
        "multiplexing": {
            "type": "enum",
            "widget": "select",
            "enum": [
                "MULTIPLEXING_OFF",
                "MULTIPLEXING_LOW",
                "MULTIPLEXING_MIDDLE",
                "MULTIPLEXING_HIGH",
            ],
            "default": "MULTIPLEXING_HIGH",
            "ui_group": "core",
            "ui_order": 2,
            "title": {"en": "Multiplexing", "ru": "Multiplexing"},
            "description": {
                "en": "Outbound multiplexing profile.",
                "ru": "Профиль multiplexing на outbound.",
            },
        },
        "mtu": {
            "type": "uint16",
            "widget": "text",
            "default": "1400",
            "ui_group": "core",
            "ui_order": 3,
            "title": {"en": "MTU", "ru": "MTU"},
            "description": {"en": "Mieru MTU.", "ru": "MTU Mieru."},
        },
        "traffic_pattern": mi["param_meta"]["traffic_pattern"],
    }
    mi["inbound_template"]["transport"] = "{{param.transport}}"
    mi["outbound_template"]["transport"] = "{{param.transport}}"
    mi["outbound_template"]["multiplexing"] = "{{param.multiplexing}}"
    save("mieru/mieru_custom.json", mi)

    # --- snell ---
    sn = json.loads((DATA / "snell/snell_custom.json").read_text(encoding="utf-8"))
    sn["i18n"] = {
        "ru": {
            "title": "Snell конструктор",
            "description": "Snell: obfs_mode, obfs_host. Inbound v5 / outbound v4 wire.",
        },
        "en": {
            "title": "Snell constructor",
            "description": "Snell: obfs_mode and obfs_host. Inbound v5 / outbound v4 wire.",
        },
    }
    sn["param_fields"] = ["obfs_mode"]
    sn["optional_param_fields"] = ["obfs_host"]
    sn["param_meta"] = {
        "obfs_mode": {
            "type": "enum",
            "widget": "select",
            "enum": ["off", "http", "tls"],
            "default": "http",
            "ui_group": "obfs",
            "ui_order": 1,
            "title": {"en": "Obfs mode", "ru": "Obfs mode"},
            "description": {
                "en": "Snell obfs_mode on inbound/outbound.",
                "ru": "obfs_mode Snell на inbound/outbound.",
            },
        },
        "obfs_host": {
            **sn["param_meta"]["obfs_host"],
            "description": {
                "en": "Host header for HTTP/TLS obfs.",
                "ru": "Host header для HTTP/TLS obfs.",
            },
            "visible_when": [{"key": "obfs_mode", "in": ["http", "tls"]}],
        },
    }
    sn["inbound_template"]["obfs_mode"] = "{{param.obfs_mode}}"
    sn["outbound_template"]["obfs_mode"] = "{{param.obfs_mode}}"
    save("snell/snell_custom.json", sn)

    # --- derp ---
    de = json.loads((DATA / "derp/derp_custom.json").read_text(encoding="utf-8"))
    de["i18n"] = {
        "ru": {
            "title": "DERP конструктор",
            "description": "DERP: path, websocket, udp mode, uTLS fingerprint.",
        },
        "en": {
            "title": "DERP constructor",
            "description": "DERP: path, websocket, udp mode, uTLS fingerprint.",
        },
    }
    de["optional_param_fields"] = ["path", "websocket", "udp", "fingerprint"]
    de["param_meta"] = {
        "path": de["param_meta"]["path"],
        "websocket": {
            "type": "bool",
            "widget": "toggle",
            "default": "false",
            "ui_group": "transport",
            "ui_order": 43,
            "title": {"en": "WebSocket", "ru": "WebSocket"},
            "description": {
                "en": "DERP websocket upgrade.",
                "ru": "DERP websocket upgrade.",
            },
        },
        "udp": {
            "type": "enum",
            "widget": "select",
            "enum": ["native", "disabled"],
            "default": "native",
            "ui_group": "transport",
            "ui_order": 44,
            "title": {"en": "UDP mode", "ru": "UDP mode"},
            "description": {
                "en": "DERP udp: native|disabled.",
                "ru": "DERP udp: native|disabled.",
            },
        },
        "fingerprint": {
            "type": "string",
            "widget": "text",
            "default": "chrome",
            "ui_group": "tls",
            "ui_order": 45,
            "title": {"en": "uTLS fingerprint", "ru": "uTLS fingerprint"},
            "description": {
                "en": "Outbound uTLS fingerprint.",
                "ru": "uTLS fingerprint на outbound.",
            },
        },
    }
    de["inbound_template"]["udp"] = "{{param.udp}}"
    de["outbound_template"]["udp"] = "{{param.udp}}"
    de["outbound_template"]["tls"]["utls"]["fingerprint"] = "{{param.fingerprint}}"
    save("derp/derp_custom.json", de)

    # --- cloudflared ---
    cf = json.loads((DATA / "cloudflared/cloudflared_custom.json").read_text(encoding="utf-8"))
    cf["i18n"] = {
        "ru": {
            "title": "Cloudflared конструктор",
            "description": "Cloudflare Tunnel: token, protocol, post_quantum, ha_connections.",
        },
        "en": {
            "title": "Cloudflared constructor",
            "description": "Cloudflare Tunnel: token, protocol, post_quantum, ha_connections.",
        },
    }
    cf["optional_param_fields"] = ["protocol", "post_quantum", "ha_connections"]
    meta = cf["param_meta"]
    meta["protocol"] = {
        "type": "enum",
        "widget": "select",
        "enum": ["auto", "http2", "quic"],
        "default": "http2",
        "ui_group": "core",
        "ui_order": 2,
        "title": {"en": "Edge protocol", "ru": "Edge protocol"},
        "description": {
            "en": "cloudflared protocol to Cloudflare edge.",
            "ru": "protocol cloudflared к edge Cloudflare.",
        },
    }
    meta["post_quantum"] = {
        "type": "bool",
        "widget": "toggle",
        "default": "true",
        "ui_group": "core",
        "ui_order": 3,
        "title": {"en": "Post-quantum", "ru": "Post-quantum"},
        "description": {
            "en": "post_quantum key exchange with Cloudflare edge.",
            "ru": "post_quantum обмен ключами с edge Cloudflare.",
        },
    }
    meta["ha_connections"] = {
        "type": "uint16",
        "widget": "text",
        "default": "4",
        "ui_group": "core",
        "ui_order": 4,
        "min": 1,
        "max": 16,
        "title": {"en": "HA connections", "ru": "HA connections"},
        "description": {
            "en": "ha_connections to Cloudflare edge.",
            "ru": "ha_connections к edge Cloudflare.",
        },
    }
    cf["param_meta"] = meta
    cf["inbound_template"]["protocol"] = "{{param.protocol}}"
    save("cloudflared/cloudflared_custom.json", cf)

    # --- ssh: split client_version ---
    ssh = json.loads((DATA / "ssh/ssh_custom.json").read_text(encoding="utf-8"))
    ssh["i18n"] = {
        "ru": {
            "title": "SSH конструктор",
            "description": "SSH: server_version (banner) и client_version на outbound.",
        },
        "en": {
            "title": "SSH constructor",
            "description": "SSH: server_version banner and outbound client_version.",
        },
    }
    ssh["optional_param_fields"] = ["server_version", "client_version"]
    ssh["param_meta"]["client_version"] = {
        "type": "string",
        "widget": "text",
        "default": "SSH-2.0-OpenSSH_8.9",
        "ui_group": "banner",
        "ui_order": 44,
        "title": {"en": "Client version", "ru": "Client version"},
        "description": {
            "en": "Outbound SSH client_version string.",
            "ru": "Строка client_version на outbound.",
        },
    }
    ssh["outbound_template"]["client_version"] = "{{param.client_version}}"
    save("ssh/ssh_custom.json", ssh)


if __name__ == "__main__":
    main()
