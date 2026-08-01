#!/usr/bin/env python3
"""Pass5 audit: stock unused params, hardcoded knobs without meta, weak hints."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/controlplane/presets/data"
LOC = ROOT / "internal/controlplane/presets/i18n/locales"
PARAM_RE = re.compile(r"\{\{param\.([a-zA-Z0-9_]+)\}\}")

GO_KNOBS = {
    "hysteria": {"up_mbps", "down_mbps", "obfs"},
    "hysteria2": {
        "up_mbps",
        "down_mbps",
        "ignore_client_bandwidth",
        "obfs_type",
        "masquerade_mode",
        "masquerade_url",
    },
    "wireguard": {"mtu", "up_mbps", "down_mbps", "jc", "jmin", "jmax", "listen_port"},
    "carrier": {"provider", "token", "key"},
    "tuic": {"congestion_control", "udp_relay_mode", "zero_rtt"},
    "anytls": {"alpn", "idle_session", "fingerprint"},
    "cloudflared": {"ha_connections", "post_quantum", "protocol"},
    "derp": {"websocket", "path", "udp", "fingerprint"},
    "http": {"tls_mode", "fingerprint"},
    "mixed": {"outbound_type", "tls_mode", "fingerprint"},
    "socks": {"udp_over_tcp"},
    "shadowsocks": {"network", "udp_over_tcp", "method"},
    "shadowtls": {"strict_mode", "wildcard_sni", "fingerprint", "handshake_server"},
    "sudoku": {"padding_max", "padding_min", "aead_method", "multiplex", "fallback"},
    "mieru": {"mtu", "multiplexing", "transport", "traffic_pattern"},
    "trojan": {"alpn", "tls_mode", "transport", "fingerprint"},
    "trusttunnel": {"anti_dpi", "enable_protocol_fallback", "mode"},
    "vless": {"alpn", "tls_mode", "transport", "flow", "fingerprint"},
    "vmess": {"alpn", "tls_mode", "transport", "fingerprint"},
    "naive": {"network", "alpn"},
    "snell": {"obfs_mode", "obfs_host"},
    "ssh": {"server_version", "client_version"},
    "shadowquic": {"jls_addr", "jls_server_name", "congestion_control", "zero_rtt"},
}

# Identity fields: hardcoded by preset design (not operator-tunable).
IDENTITY_HARDCODE = {
    "tuic_0rtt": {"zero_rtt_handshake"},
}

HARDCODE_FIELDS = (
    "congestion_control",
    "udp_relay_mode",
    "up_mbps",
    "down_mbps",
    "mtu",
    "ignore_client_bandwidth",
    "strict_mode",
    "websocket",
    "zero_rtt",
    "zero_rtt_handshake",
    "ha_connections",
    "post_quantum",
)


def load_lang(lang: str) -> dict[str, str]:
    keys: dict[str, str] = {}
    for f in (LOC / lang).rglob("*.json"):
        keys.update(json.loads(f.read_text(encoding="utf-8")))
    return keys


def main() -> None:
    print("=== stock META_UNUSED (non-custom, not GO/tpl) ===")
    for p in sorted(DATA.rglob("*.json")):
        if p.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        if raw.get("custom_preset"):
            continue
        tag = raw.get("tag") or p.stem
        proto = raw.get("protocol") or p.parent.name
        meta = set((raw.get("param_meta") or {}).keys())
        used = set(PARAM_RE.findall(json.dumps(raw)))
        unused = sorted(meta - used - GO_KNOBS.get(proto, set()))
        if unused:
            print(f"{proto}/{tag}: {unused}")

    print("\n=== stock hardcoded knobs without param_meta ===")
    for p in sorted(DATA.rglob("*.json")):
        if p.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        if raw.get("custom_preset"):
            continue
        tag = raw.get("tag") or p.stem
        proto = raw.get("protocol") or p.parent.name
        opts = (
            set(raw.get("optional_param_fields") or [])
            | set(raw.get("param_fields") or [])
            | set((raw.get("param_meta") or {}).keys())
        )
        tpl = {
            "inbound": raw.get("inbound_template") or {},
            "outbound": raw.get("outbound_template") or {},
            "endpoint": raw.get("endpoint_template") or {},
        }
        found = []
        for block in tpl.values():
            if not isinstance(block, dict):
                continue
            for k in HARDCODE_FIELDS:
                if k in block and k not in opts and (
                    k != "zero_rtt_handshake" or "zero_rtt" not in opts
                ):
                    if k == "zero_rtt_handshake" and "zero_rtt" in opts:
                        continue
                    found.append(k)
        found = sorted(set(found))
        found = [f for f in found if not (f == "zero_rtt_handshake" and "zero_rtt" in opts)]
        ident = IDENTITY_HARDCODE.get(tag, set())
        found = [f for f in found if f not in ident]
        # Fields with GO knobs + now declared in opts are fine
        go = GO_KNOBS.get(proto, set())
        found = [
            f
            for f in found
            if f not in go and not (f == "zero_rtt_handshake" and "zero_rtt" in go)
        ]
        if found:
            print(f"{proto}/{tag}: hardcoded {found} opts={sorted(opts)}")

    print("\n=== weak input_hint / format ===")
    en = load_lang("en")
    bad_hints = [
        (k, v)
        for k, v in en.items()
        if k.endswith(".help.input_hint")
        and (
            "operator docs" in v.lower()
            or v in ("Value per operator docs", "Hostname / version string")
            or len(v) < 6
        )
    ]
    print("bad_hints", len(bad_hints))
    for k, v in bad_hints[:30]:
        print(f"  {k}: {v}")

    print("\n=== empty first_bytes on demux-compatible stable ===")
    for p in sorted(DATA.rglob("*.json")):
        if p.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        dh = raw.get("demux_hints") or {}
        if not dh.get("compatible_with_demux"):
            continue
        if raw.get("status") != "stable":
            continue
        if not (dh.get("first_bytes") or "").strip():
            print("EMPTY_FIRST_BYTES", raw.get("tag") or p.stem)

    print("\ndone")


if __name__ == "__main__":
    main()
