#!/usr/bin/env python3
"""Pass6: stock capability params for anytls/derp/ssh + thin descriptions."""
from __future__ import annotations

import json
from pathlib import Path

DATA = Path(__file__).resolve().parents[1] / "internal/controlplane/presets/data"
GEN = Path(__file__).resolve().parents[1] / "scripts/generate_all_catalog_locales.py"

ALPN = {
    "type": "string",
    "widget": "text",
    "default": "h2,http/1.1",
    "ui_group": "tls",
    "ui_order": 20,
    "title": {"en": "ALPN", "ru": "ALPN"},
    "description": {
        "en": "Comma-separated TLS ALPN list.",
        "ru": "Список TLS ALPN через запятую.",
    },
    "help": {
        "summary": {
            "en": "TLS ALPN offered on AnyTLS. Keep h2,http/1.1 unless the front requires a single ALPN.",
            "ru": "TLS ALPN на AnyTLS. Обычно h2,http/1.1, если front не требует один ALPN.",
        },
        "input_hint": {"en": "h2,http/1.1 (CSV)", "ru": "h2,http/1.1 (CSV)"},
        "format": "h2,http/1.1",
    },
}

FP = {
    "type": "enum",
    "widget": "select",
    "enum": ["chrome", "firefox", "safari", "edge", "ios", "android", "random"],
    "default": "chrome",
    "ui_group": "tls",
    "ui_order": 21,
    "title": {"en": "uTLS fingerprint", "ru": "uTLS fingerprint"},
    "description": {
        "en": "Outbound uTLS ClientHello fingerprint.",
        "ru": "uTLS fingerprint ClientHello на outbound.",
    },
    "help": {
        "summary": {
            "en": "Outbound uTLS fingerprint. Match a common browser unless you need a specific ClientHello class.",
            "ru": "uTLS fingerprint на outbound. Держите browser-like, если не нужен особый ClientHello.",
        },
        "input_hint": {"en": "chrome|firefox|…", "ru": "chrome|firefox|…"},
        "format": "chrome",
    },
}

UDP = {
    "type": "enum",
    "widget": "select",
    "enum": ["native", "disabled"],
    "default": "native",
    "ui_group": "transport",
    "ui_order": 30,
    "title": {"en": "UDP mode", "ru": "UDP mode"},
    "description": {
        "en": "DERP udp: native|disabled.",
        "ru": "DERP udp: native|disabled.",
    },
    "help": {
        "summary": {
            "en": "DERP UDP path. native keeps STUN/UDP; disabled forces TCP/WS-only relay behavior.",
            "ru": "UDP-путь DERP. native — STUN/UDP; disabled — только TCP/WS relay.",
        },
        "input_hint": {"en": "native|disabled", "ru": "native|disabled"},
        "format": "native",
    },
}

CLIENT_VER = {
    "type": "string",
    "widget": "text",
    "default": "SSH-2.0-OpenSSH_8.9",
    "ui_group": "banner",
    "ui_order": 44,
    "title": {"en": "Client version", "ru": "Client version"},
    "description": {
        "en": "SSH client identification string on outbound.",
        "ru": "Строка идентификации SSH-клиента на outbound.",
    },
    "help": {
        "summary": {
            "en": "Outbound SSH client banner. Can differ from server_version when probing mixed OpenSSH fingerprints.",
            "ru": "Banner SSH-клиента на outbound. Может отличаться от server_version при смешанных OpenSSH fingerprint.",
        },
        "input_hint": {"en": "SSH-2.0-…", "ru": "SSH-2.0-…"},
        "format": "SSH-2.0-OpenSSH_8.9",
    },
}


def add_opt(raw: dict, field: str, meta: dict) -> None:
    opts = list(raw.get("optional_param_fields") or [])
    if field not in opts:
        opts.append(field)
    raw["optional_param_fields"] = opts
    pm = raw.setdefault("param_meta", {})
    if field not in pm:
        pm[field] = json.loads(json.dumps(meta))


def main() -> None:
    # anytls stock
    for p in (DATA / "anytls").glob("*.json"):
        raw = json.loads(p.read_text(encoding="utf-8"))
        if raw.get("custom_preset"):
            continue
        add_opt(raw, "alpn", ALPN)
        add_opt(raw, "fingerprint", FP)
        p.write_text(json.dumps(raw, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("anytls", raw.get("tag"))

    # derp stock
    for p in (DATA / "derp").glob("*.json"):
        raw = json.loads(p.read_text(encoding="utf-8"))
        if raw.get("custom_preset"):
            continue
        add_opt(raw, "udp", UDP)
        add_opt(raw, "fingerprint", FP)
        p.write_text(json.dumps(raw, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("derp", raw.get("tag"))

    # ssh stock: separate client_version
    for p in (DATA / "ssh").glob("*.json"):
        raw = json.loads(p.read_text(encoding="utf-8"))
        if raw.get("custom_preset"):
            continue
        ob = raw.get("outbound_template") or {}
        if ob.get("client_version") == "{{param.server_version}}":
            ob["client_version"] = "{{param.client_version}}"
            raw["outbound_template"] = ob
        add_opt(raw, "client_version", CLIENT_VER)
        notes = raw.setdefault("client_notes", {})
        notes["params"] = "optional: server_version (inbound banner), client_version (outbound)"
        p.write_text(json.dumps(raw, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("ssh", raw.get("tag"))

    # thin description patches in generate script
    g = GEN.read_text(encoding="utf-8")
    en_extra = {
        "preset.carrier_wbstream_users.description": "WB Stream underlay with per-user secrets. Room URL + often token; users_auth path.",
        "preset.ss_2022_chacha.description": "Shadowsocks 2022 ChaCha20-Poly1305. Prefer on ARM/mobile where AES is weaker.",
        "preset.ss_aes128_uot.description": "SS AES-128 with UDP-over-TCP helper when native UDP SS is filtered.",
    }
    ru_extra = {
        "preset.carrier_wbstream_users.description": "WB Stream underlay с per-user secrets. URL комнаты + часто token; путь users_auth.",
        "preset.ss_2022_chacha.description": "Shadowsocks 2022 ChaCha20-Poly1305. Предпочтительнее на ARM/mobile, где AES слабее.",
        "preset.ss_aes128_uot.description": "SS AES-128 с UDP-over-TCP helper, когда native UDP SS режется.",
    }
    # inject into EN_PRESET_PATCHES carrier/shadowsocks blocks if missing
    if "preset.ss_2022_chacha.description" not in g:
        g = g.replace(
            '"preset.ss_2022_aes128_mux.description":',
            f'"preset.ss_2022_chacha.description": "{en_extra["preset.ss_2022_chacha.description"]}",\n        "preset.ss_aes128_uot.description": "{en_extra["preset.ss_aes128_uot.description"]}",\n        "preset.ss_2022_aes128_mux.description":',
            1,
        )
        g = g.replace(
            '"preset.carrier_vk_users.description":',
            f'"preset.carrier_wbstream_users.description": "{en_extra["preset.carrier_wbstream_users.description"]}",\n        "preset.carrier_vk_users.description":',
            1,
        )
        # RU block
        g = g.replace(
            '"preset.ss_2022_aes128_mux.description": "SS2022 AES-128-GCM с multiplex',
            f'"preset.ss_2022_chacha.description": "{ru_extra["preset.ss_2022_chacha.description"]}",\n        "preset.ss_aes128_uot.description": "{ru_extra["preset.ss_aes128_uot.description"]}",\n        "preset.ss_2022_aes128_mux.description": "SS2022 AES-128-GCM с multiplex',
            1,
        )
        g = g.replace(
            '"preset.carrier_vk_users.description": "VK WRAP/DTLS underlay с per-user secrets',
            f'"preset.carrier_wbstream_users.description": "{ru_extra["preset.carrier_wbstream_users.description"]}",\n        "preset.carrier_vk_users.description": "VK WRAP/DTLS underlay с per-user secrets',
            1,
        )
        GEN.write_text(g, encoding="utf-8")
        print("description patches injected")
    else:
        print("description patches already present")


if __name__ == "__main__":
    main()
