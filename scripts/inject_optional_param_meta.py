#!/usr/bin/env python3
"""Add optional_param_fields + param_meta for {{param.*}} used in preset templates."""
from __future__ import annotations

import json
import re
from copy import deepcopy
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"
PARAM_RE = re.compile(r"\{\{param\.([^}]+)\}\}")

# Schema v2 optional knobs (aligned with materialize/build.go defaults).
PARAM_SPECS: dict[str, dict] = {
    "ws_path": {
        "type": "string",
        "widget": "path",
        "default": "/ws",
        "ui_group": "transport",
        "ui_order": 30,
        "title": {"en": "WebSocket path", "ru": "WebSocket path"},
        "description": {
            "en": "HTTP path for WebSocket after TLS.",
            "ru": "HTTP path для WebSocket после TLS.",
        },
    },
    "ws_host": {
        "type": "string",
        "widget": "text",
        "ui_group": "transport",
        "ui_order": 31,
        "title": {"en": "WebSocket Host", "ru": "WebSocket Host"},
        "description": {
            "en": "Host header for WebSocket (defaults to server/SNI).",
            "ru": "Host header для WebSocket (по умолчанию server/SNI).",
        },
    },
    "http_path": {
        "type": "string",
        "widget": "path",
        "default": "/http",
        "ui_group": "transport",
        "ui_order": 32,
        "title": {"en": "HTTP path", "ru": "HTTP path"},
        "description": {
            "en": "HTTP/2 transport path.",
            "ru": "Path HTTP/2 transport.",
        },
    },
    "http_host": {
        "type": "string",
        "widget": "text",
        "ui_group": "transport",
        "ui_order": 33,
        "title": {"en": "HTTP Host", "ru": "HTTP Host"},
        "description": {
            "en": "Host header for HTTP transport.",
            "ru": "Host header для HTTP transport.",
        },
    },
    "hu_path": {
        "type": "string",
        "widget": "path",
        "default": "/upgrade",
        "ui_group": "transport",
        "ui_order": 34,
        "title": {"en": "HTTPUpgrade path", "ru": "HTTPUpgrade path"},
        "description": {
            "en": "Single HTTP Upgrade path (no WS framing).",
            "ru": "Path одного HTTP Upgrade (без WS framing).",
        },
    },
    "hu_host": {
        "type": "string",
        "widget": "text",
        "ui_group": "transport",
        "ui_order": 35,
        "title": {"en": "HTTPUpgrade Host", "ru": "HTTPUpgrade Host"},
        "description": {
            "en": "Host for HTTPUpgrade.",
            "ru": "Host для HTTPUpgrade.",
        },
    },
    "service_name": {
        "type": "string",
        "widget": "text",
        "default": "GunService",
        "ui_group": "transport",
        "ui_order": 36,
        "title": {"en": "gRPC service name", "ru": "gRPC service name"},
        "description": {
            "en": "gRPC Gun service_name.",
            "ru": "gRPC Gun service_name.",
        },
    },
    "jls_addr": {
        "type": "string",
        "widget": "text",
        "default": "www.cloudflare.com:443",
        "ui_group": "jls",
        "ui_order": 10,
        "title": {"en": "JLS upstream addr", "ru": "JLS upstream addr"},
        "description": {
            "en": "host:port for JLS camouflage QUIC upstream.",
            "ru": "host:port для JLS camouflage QUIC upstream.",
        },
    },
    "jls_server_name": {
        "type": "string",
        "widget": "text",
        "default": "www.cloudflare.com",
        "ui_group": "jls",
        "ui_order": 11,
        "title": {"en": "JLS SNI", "ru": "JLS SNI"},
        "description": {
            "en": "TLS/QUIC SNI for JLS upstream; align with demux_sni on :443 stacks.",
            "ru": "SNI для JLS upstream; на demux :443 выравнивайте с demux_sni.",
        },
    },
    "handshake_server": {
        "type": "string",
        "widget": "text",
        "default": "www.apple.com",
        "ui_group": "tls",
        "ui_order": 20,
        "title": {"en": "Handshake server", "ru": "Handshake server"},
        "description": {
            "en": "ShadowTLS v3 handshake mimic target (SNI).",
            "ru": "Цель mimic handshake ShadowTLS v3 (SNI).",
        },
    },
    "fallback": {
        "type": "string",
        "widget": "text",
        "default": "http://127.0.0.1:80",
        "ui_group": "core",
        "ui_order": 40,
        "title": {"en": "Fallback URL", "ru": "Fallback URL"},
        "description": {
            "en": "Plain HTTP fallback when auth fails (Snell/Sudoku).",
            "ru": "Plain HTTP fallback при неверном auth (Snell/Sudoku).",
        },
    },
    "obfs_host": {
        "type": "string",
        "widget": "text",
        "default": "www.bing.com",
        "ui_group": "obfs",
        "ui_order": 41,
        "title": {"en": "Obfs Host", "ru": "Obfs Host"},
        "description": {
            "en": "Host header for Snell v6 obfs.",
            "ru": "Host header для Snell v6 obfs.",
        },
    },
    "path": {
        "type": "string",
        "widget": "path",
        "default": "/derp",
        "ui_group": "transport",
        "ui_order": 42,
        "title": {"en": "Path", "ru": "Path"},
        "description": {
            "en": "HTTP/WebSocket path (DERP, etc.).",
            "ru": "HTTP/WebSocket path (DERP и др.).",
        },
    },
    "server_version": {
        "type": "string",
        "widget": "text",
        "default": "SSH-2.0-OpenSSH_8.9",
        "ui_group": "banner",
        "ui_order": 43,
        "title": {"en": "SSH banner", "ru": "SSH banner"},
        "description": {
            "en": "SSH version string shown to clients.",
            "ru": "Строка версии SSH для клиентов.",
        },
    },
    "traffic_pattern": {
        "type": "string",
        "widget": "text",
        "ui_group": "core",
        "ui_order": 44,
        "title": {"en": "Traffic pattern", "ru": "Traffic pattern"},
        "description": {
            "en": "Mieru traffic shaping profile name.",
            "ru": "Имя профиля shaping трафика Mieru.",
        },
    },
    "masquerade_url": {
        "type": "string",
        "widget": "text",
        "default": "https://www.cloudflare.com",
        "ui_group": "masquerade",
        "ui_order": 50,
        "title": {"en": "Masquerade URL", "ru": "Masquerade URL"},
        "description": {
            "en": "Upstream URL for Hy2 proxy masquerade.",
            "ru": "Upstream URL для Hy2 proxy masquerade.",
        },
    },
    "fingerprint": {
        "type": "string",
        "widget": "text",
        "default": "chrome",
        "ui_group": "tls",
        "ui_order": 21,
        "title": {"en": "uTLS fingerprint", "ru": "uTLS fingerprint"},
        "description": {
            "en": "Client uTLS fingerprint on outbound.",
            "ru": "uTLS fingerprint клиента на outbound.",
        },
    },
    "flow": {
        "type": "enum",
        "widget": "select",
        "enum": ["", "xtls-rprx-vision"],
        "default": "",
        "ui_group": "core",
        "ui_order": 12,
        "title": {"en": "Flow", "ru": "Flow"},
        "description": {
            "en": "XTLS Vision flow (TCP transports only).",
            "ru": "XTLS Vision flow (только TCP transport).",
        },
    },
    "packet_encoding": {
        "type": "enum",
        "widget": "select",
        "enum": ["", "xudp", "packetaddr"],
        "default": "xudp",
        "ui_group": "core",
        "ui_order": 13,
        "title": {"en": "Packet encoding", "ru": "Packet encoding"},
        "description": {
            "en": "UDP encoding for VLESS/VMess outbound.",
            "ru": "UDP encoding для VLESS/VMess outbound.",
        },
    },
    "transport": {
        "type": "enum",
        "widget": "select",
        "enum": ["tcp", "ws", "grpc", "http", "httpupgrade", "quic"],
        "default": "tcp",
        "ui_group": "transport",
        "ui_order": 1,
        "title": {"en": "Transport", "ru": "Транспорт"},
        "description": {
            "en": "Underlying sing-box transport.type.",
            "ru": "sing-box transport.type.",
        },
    },
    "tls_mode": {
        "type": "enum",
        "widget": "select",
        "enum": ["none", "tls", "reality"],
        "default": "tls",
        "ui_group": "tls",
        "ui_order": 2,
        "title": {"en": "TLS mode", "ru": "Режим TLS"},
        "description": {
            "en": "Plain, TLS profile, or Reality.",
            "ru": "Plain, TLS-профиль CP или Reality.",
        },
    },
    "transport_path": {
        "type": "string",
        "widget": "path",
        "default": "/",
        "ui_group": "transport",
        "ui_order": 37,
        "title": {"en": "Transport path", "ru": "Transport path"},
        "description": {
            "en": "Path for WS/HTTP/HTTPUpgrade constructors.",
            "ru": "Path для WS/HTTP/HTTPUpgrade в конструкторе.",
        },
    },
    "transport_host": {
        "type": "string",
        "widget": "text",
        "ui_group": "transport",
        "ui_order": 38,
        "title": {"en": "Transport Host", "ru": "Transport Host"},
        "description": {
            "en": "Host header for transport constructors.",
            "ru": "Host header для transport в конструкторе.",
        },
    },
    "alpn": {
        "type": "string",
        "widget": "text",
        "default": "h2,http/1.1",
        "ui_group": "tls",
        "ui_order": 22,
        "title": {"en": "ALPN list", "ru": "Список ALPN"},
        "description": {
            "en": "Comma-separated ALPN values.",
            "ru": "ALPN через запятую.",
        },
    },
    "key": {
        "type": "string",
        "widget": "text",
        "ui_group": "carrier",
        "ui_order": 60,
        "title": {"en": "Room key", "ru": "Ключ комнаты"},
        "description": {
            "en": "Optional Carrier room key/token.",
            "ru": "Опциональный ключ/токен комнаты Carrier.",
        },
    },
    "vk_hash": {
        "type": "string",
        "widget": "text",
        "ui_group": "carrier",
        "ui_order": 61,
        "title": {"en": "VK hash", "ru": "VK hash"},
        "description": {
            "en": "VK Carrier session hash parameter.",
            "ru": "Параметр hash для Carrier VK.",
        },
    },
    "wrap_password": {
        "type": "string",
        "widget": "text",
        "ui_group": "carrier",
        "ui_order": 62,
        "title": {"en": "Wrap password", "ru": "Wrap password"},
        "description": {
            "en": "Carrier wrap password when required by profile.",
            "ru": "Wrap password для Carrier при необходимости профиля.",
        },
    },
    "token": {
        "type": "string",
        "widget": "text",
        "ui_group": "core",
        "ui_order": 63,
        "title": {"en": "Token", "ru": "Токен"},
        "description": {
            "en": "Optional tunnel or room token.",
            "ru": "Опциональный токен туннеля или комнаты.",
        },
    },
    "room": {
        "type": "string",
        "widget": "text",
        "ui_group": "carrier",
        "ui_order": 59,
        "title": {"en": "Room URL", "ru": "URL комнаты"},
        "description": {
            "en": "SFU room link for Carrier.",
            "ru": "Ссылка на комнату SFU для Carrier.",
        },
    },
    "masquerade_dir": {
        "type": "string",
        "widget": "text",
        "ui_group": "masquerade",
        "ui_order": 51,
        "title": {"en": "Masquerade directory", "ru": "Каталог masquerade"},
        "description": {
            "en": "Static file root for Hy2 file masquerade.",
            "ru": "Корень статики для Hy2 file masquerade.",
        },
    },
    "realm_server_url": {
        "type": "string",
        "widget": "text",
        "ui_group": "realm",
        "ui_order": 52,
        "title": {"en": "Realm server URL", "ru": "URL realm-сервера"},
        "description": {
            "en": "Hysteria realm control-plane base URL.",
            "ru": "Базовый URL панели Hysteria realm.",
        },
    },
    "realm_id": {
        "type": "string",
        "widget": "text",
        "ui_group": "realm",
        "ui_order": 53,
        "title": {"en": "Realm ID", "ru": "ID realm"},
        "description": {
            "en": "Realm identifier from the control plane.",
            "ru": "Идентификатор realm из панели.",
        },
    },
    "mode": {
        "type": "enum",
        "widget": "select",
        "enum": ["h2", "h3", "auto"],
        "default": "auto",
        "ui_group": "transport",
        "ui_order": 45,
        "title": {"en": "Upstream protocol", "ru": "Upstream protocol"},
        "description": {
            "en": "TrustTunnel transport.upstream_protocol.",
            "ru": "TrustTunnel transport.upstream_protocol.",
        },
    },
    "method": {
        "type": "enum",
        "widget": "select",
        "enum": [
            "aes-128-gcm",
            "aes-256-gcm",
            "chacha20-ietf-poly1305",
            "2022-blake3-aes-128-gcm",
            "2022-blake3-aes-256-gcm",
            "2022-blake3-chacha20-poly1305",
        ],
        "default": "aes-128-gcm",
        "ui_group": "core",
        "ui_order": 46,
        "title": {"en": "Cipher method", "ru": "Метод шифрования"},
        "description": {
            "en": "Shadowsocks AEAD/2022 method.",
            "ru": "Метод Shadowsocks AEAD/2022.",
        },
    },
    "network": {
        "type": "enum",
        "widget": "select",
        "enum": ["tcp", "udp"],
        "default": "tcp",
        "ui_group": "transport",
        "ui_order": 47,
        "title": {"en": "Network", "ru": "Сеть"},
        "description": {
            "en": "Naive network (tcp TLS/H2 vs udp QUIC/H3).",
            "ru": "Naive network (tcp TLS/H2 vs udp QUIC/H3).",
        },
    },
    "congestion_control": {
        "type": "enum",
        "widget": "select",
        "enum": ["cubic", "bbr", "new_reno"],
        "default": "bbr",
        "ui_group": "core",
        "ui_order": 48,
        "title": {"en": "Congestion control", "ru": "Congestion control"},
        "description": {
            "en": "TUIC QUIC congestion_control.",
            "ru": "TUIC QUIC congestion_control.",
        },
    },
    "udp_relay_mode": {
        "type": "enum",
        "widget": "select",
        "enum": ["native", "quic"],
        "default": "native",
        "ui_group": "core",
        "ui_order": 49,
        "title": {"en": "UDP relay mode", "ru": "UDP relay mode"},
        "description": {
            "en": "TUIC outbound udp_relay_mode.",
            "ru": "TUIC outbound udp_relay_mode.",
        },
    },
    "zero_rtt": {
        "type": "bool",
        "widget": "toggle",
        "default": "false",
        "ui_group": "core",
        "ui_order": 50,
        "title": {"en": "0-RTT", "ru": "0-RTT"},
        "description": {
            "en": "Enable TUIC zero_rtt_handshake when true.",
            "ru": "Включить TUIC zero_rtt_handshake.",
        },
    },
}


def path_default_from_notes(data: dict, field: str) -> str | None:
    notes = data.get("client_notes") or {}
    if field == "ws_path":
        return notes.get("ws_path_default")
    if field == "hu_path":
        return notes.get("hu_path_default")
    if field == "http_path":
        return notes.get("http_path_default")
    return None


def find_params(raw: str) -> set[str]:
    return set(PARAM_RE.findall(raw))


def enrich_file(path: Path) -> bool:
    if path.name in {"index.json", "protocol.json"}:
        return False
    text = path.read_text(encoding="utf-8")
    used = find_params(text)
    if not used:
        return False
    data = json.loads(text)
    required = set(data.get("param_fields") or [])
    optional = set(data.get("optional_param_fields") or [])
    meta: dict = dict(data.get("param_meta") or {})

    changed = False
    for name in sorted(used):
        if name in required:
            continue
        if name not in PARAM_SPECS:
            raise SystemExit(f"{path}: unknown param.{name} — extend PARAM_SPECS")
        if name not in optional:
            optional.add(name)
            changed = True
        if name not in meta or not meta[name].get("type"):
            spec = deepcopy(PARAM_SPECS[name])
            override = path_default_from_notes(data, name)
            if override:
                spec["default"] = override
            meta[name] = spec
            changed = True

    new_optional = sorted(optional - required)
    if new_optional != sorted(data.get("optional_param_fields") or []):
        data["optional_param_fields"] = new_optional
        changed = True
    if meta != data.get("param_meta"):
        data["param_meta"] = meta
        changed = True

    if changed:
        path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return changed


def main() -> None:
    n = 0
    for path in sorted(ROOT.rglob("*.json")):
        if enrich_file(path):
            n += 1
            print("updated", path.relative_to(ROOT))
    print(f"done: {n} files updated")


if __name__ == "__main__":
    main()
