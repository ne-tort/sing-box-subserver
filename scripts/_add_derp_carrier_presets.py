#!/usr/bin/env python3
"""Add DERP + carrier lab presets; enrich thin TCP presets; fix mieru inbound."""
from __future__ import annotations

import json
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"


def load(p: Path) -> dict:
    return json.loads(p.read_text(encoding="utf-8"))


def save(p: Path, obj: dict) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", p.relative_to(BASE))


def enrich_thin() -> None:
    # SS AEAD: advertise tcp+udp network
    for tag in ("ss_aes128", "ss_aes256", "ss_chacha20", "ss_2022_aes128", "ss_2022_aes256", "ss_2022_chacha"):
        p = BASE / "shadowsocks" / f"{tag}.json"
        if not p.exists():
            continue
        obj = load(p)
        for side in ("inbound_template", "outbound_template"):
            t = obj.get(side)
            if isinstance(t, dict):
                t.setdefault("network", "tcp")
        save(p, obj)

    # plaintext auth proxies: set set_system_proxy false / path notes via headers
    for proto, tag, extra_ib, extra_ob in (
        ("http", "http", {}, {"path": "/"}),
        ("socks", "socks", {}, {}),
        ("mixed", "mixed_auth", {}, {}),
        ("vless", "vless_tcp", {}, {}),
        ("vmess", "vmess_tcp", {}, {"security": "auto", "alter_id": 0}),
    ):
        p = BASE / proto / f"{tag}.json"
        if not p.exists():
            continue
        obj = load(p)
        ib = obj.setdefault("inbound_template", {})
        ob = obj.setdefault("outbound_template", {})
        for k, v in extra_ib.items():
            ib.setdefault(k, v)
        for k, v in extra_ob.items():
            ob.setdefault(k, v)
        if tag == "http":
            ib.setdefault("set_system_proxy", False)
        save(p, obj)

    # mieru: multiplexing/handshake are outbound-only in lx option
    for tag in ("mieru_tcp", "mieru_udp"):
        p = BASE / "mieru" / f"{tag}.json"
        if not p.exists():
            continue
        obj = load(p)
        ib = obj.get("inbound_template") or {}
        for k in ("multiplexing", "handshake_mode"):
            ib.pop(k, None)
        ib.setdefault("user_hint_is_mandatory", False)
        if "mtu" not in ib:
            ib["mtu"] = 1400
        obj["inbound_template"] = ib
        ob = obj.get("outbound_template") or {}
        ob.setdefault("multiplexing", "MULTIPLEXING_HIGH")
        ob.setdefault("handshake_mode", "HANDSHAKE_STANDARD")
        ob.setdefault("mtu", 1400)
        obj["outbound_template"] = ob
        save(p, obj)

    # shadowquic windows
    for tag in ("shadowquic_jls", "shadowquic_0rtt"):
        p = BASE / "shadowquic" / f"{tag}.json"
        if not p.exists():
            continue
        obj = load(p)
        for side in ("inbound_template", "outbound_template"):
            t = obj.get(side)
            if not isinstance(t, dict):
                continue
            t.setdefault("recv_window_conn", 15728640)
            t.setdefault("recv_window", 67108864)
            t.setdefault("max_datagram_frame_size", 1200)
        save(p, obj)

    # trusttunnel windows / health
    for tag in ("trusttunnel_h2", "trusttunnel_h3"):
        p = BASE / "trusttunnel" / f"{tag}.json"
        if not p.exists():
            continue
        obj = load(p)
        for side in ("inbound_template", "outbound_template"):
            t = obj.get(side)
            if not isinstance(t, dict):
                continue
            tr = t.setdefault("transport", {})
            if isinstance(tr, dict):
                tr.setdefault("initial_stream_window", 8388608)
                tr.setdefault("initial_connection_window", 16777216)
                tr.setdefault("disable_header_compression", False)
        ob = obj.get("outbound_template") or {}
        hc = ob.setdefault("health_check", {})
        if isinstance(hc, dict):
            hc.setdefault("enabled", True)
            hc.setdefault("interval", "30s")
            hc.setdefault("timeout", "5s")
        save(p, obj)


def add_derp() -> None:
    save(
        BASE / "derp" / "protocol.json",
        {
            "schema_version": 1,
            "tag": "derp",
            "singbox_type": "derp",
            "short_name": "DERP",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "DERP",
                    "description": "Tailscale-compatible DERP proxy (curve25519), with_derp.",
                },
                "en": {"title": "DERP", "description": "DERP proxy (with_derp)."},
            },
            "default_cred_fields": ["private_key", "public_key"],
        },
    )

    common_creds = {
        "cred_fields": ["private_key", "public_key"],
        "cred_generators": {"private_key": "curve25519"},
        "peer_secret_fields": {"private_key": "curve25519", "public_key": "curve25519"},
    }

    variants = [
        (
            "derp_tls",
            ["derp", "derp-tls"],
            "DERP TLS",
            "DERP over TLS Upgrade:DERP; STUN; require_users ACL.",
            False,
            "native",
            True,
        ),
        (
            "derp_ws",
            ["derp-ws"],
            "DERP WS",
            "DERP WebSocket /derp + TLS; require_users.",
            True,
            "native",
            True,
        ),
        (
            "derp_uot",
            ["derp-uot"],
            "DERP UoT",
            "DERP TLS + udp=uot (UDP over TCP framing).",
            True,
            "uot",
            False,
        ),
    ]
    for tag, aliases, short, desc, websocket, udp, stun in variants:
        ib = {
            "type": "derp",
            "tag": "{{tag}}",
            "listen": "{{listen}}",
            "listen_port": 0,
            "private_key": "{{peer.private_key}}",
            "users": [],
            "require_users": True,
            "udp": udp,
            "tailscale_relay": False,
            "path": "/derp",
            "websocket": websocket,
            "keepalive": "60s",
            "home": "s-ui derp",
            "max_packet_size": 65536,
            "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["http/1.1", "h2"]},
        }
        if stun:
            ib["stun"] = {"listen": "{{listen}}", "listen_port": 3478}
        ob = {
            "type": "derp",
            "tag": "{{tag}}",
            "server": "{{server}}",
            "server_port": 0,
            "private_key": "{{user.private_key}}",
            "peer_public_key": "{{peer.public_key}}",
            "udp": udp,
            "path": "/derp",
            "host": "{{server}}",
            "websocket": websocket,
            "keepalive": "60s",
            "connect_timeout": "10s",
            "max_packet_size": 65536,
            "tls": {
                "enabled": True,
                "server_name": "{{server}}",
                "alpn": ["http/1.1", "h2"],
                "utls": {"enabled": True, "fingerprint": "chrome"},
            },
        }
        save(
            BASE / "derp" / f"{tag}.json",
            {
                "schema_version": 1,
                "tag": tag,
                "protocol": "derp",
                "aliases": aliases,
                "short_name": short,
                "status": "lab",
                "i18n": {"ru": {"title": short, "description": desc}},
                "traits": ["tcp", "tls", "curve25519"] + (["uot"] if udp == "uot" else []) + (["websocket"] if websocket else []),
                "demux_hints": {
                    "network": ["tcp"],
                    "looks_like": "tls_clienthello",
                    "alpn": ["http/1.1", "h2"],
                    "sni_required": True,
                    "first_bytes": "TLS → DERP upgrade/WS",
                    "compatible_with_demux": True,
                },
                "scores": {"dpi": 7, "speed": 6, "mobile": 5, "setup": 4},
                **common_creds,
                "inbound_template": ib,
                "outbound_template": ob,
                "requirements": {"tls_profile": True, "build_tag": "with_derp"},
                "client_notes": {
                    "keys": "server peer private/public + per-user private/public (curve25519 RawURL)",
                    "peer_public_key": "subscription uses set.peer_secrets[preset/public_key]",
                },
            },
        )


def add_carrier() -> None:
    save(
        BASE / "carrier" / "protocol.json",
        {
            "schema_version": 1,
            "tag": "carrier",
            "singbox_type": "carrier",
            "short_name": "Carrier",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Carrier",
                    "description": "WG-star поверх underlay (peer/SFU/VK); with_carrier.",
                },
                "en": {"title": "Carrier", "description": "Carrier WG-star (with_carrier)."},
            },
            "default_cred_fields": ["device_id", "secret"],
        },
    )
    lifecycle = {
        "enabled": True,
        "keepalive": True,
        "liveness_interval": "15s",
        "liveness_timeout": "5s",
        "liveness_failures": 3,
        "reconnect_backoff": "2s",
        "reconnect_backoff_max": "30s",
    }
    # shared auth: peer password + per-user device_id
    save(
        BASE / "carrier" / "carrier_peer_shared.json",
        {
            "schema_version": 1,
            "tag": "carrier_peer_shared",
            "protocol": "carrier",
            "aliases": ["carrier", "carrier-peer", "carrier-peer-shared"],
            "short_name": "Carrier peer shared",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Carrier peer (shared)",
                    "description": "Lab peer TCP underlay + shared password; client device_id.",
                }
            },
            "traits": ["tcp", "shared_auth", "wg_star"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "carrier peer frame",
                "compatible_with_demux": False,
            },
            "scores": {"dpi": 5, "speed": 6, "mobile": 3, "setup": 4},
            "cred_fields": ["device_id"],
            "peer_secret_fields": {"password": "password"},
            "inbound_template": {
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "peer",
                "auth": "shared",
                "link": {
                    "peer": "0.0.0.0:9443",
                    "password": "{{peer.password}}",
                },
                "lifecycle": lifecycle,
            },
            "outbound_template": {
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "peer",
                "link": {
                    "peer": "{{server}}:9443",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": lifecycle,
            },
            "requirements": {"build_tag": "with_carrier"},
            "client_notes": {
                "peer": "lab: inbound listen peer port; outbound dials server:9443 — правьте под стенд",
                "auth": "shared password в peer_secrets; device_id per-user",
            },
        },
    )
    save(
        BASE / "carrier" / "carrier_peer_users.json",
        {
            "schema_version": 1,
            "tag": "carrier_peer_users",
            "protocol": "carrier",
            "aliases": ["carrier-peer-users"],
            "short_name": "Carrier peer users",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Carrier peer (users)",
                    "description": "Peer underlay + auth=users (device_id/secret ACL).",
                }
            },
            "traits": ["tcp", "users_auth", "wg_star"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "carrier peer frame",
                "compatible_with_demux": False,
            },
            "scores": {"dpi": 5, "speed": 6, "mobile": 3, "setup": 3},
            "cred_fields": ["device_id", "secret"],
            "peer_secret_fields": {"password": "password"},
            "inbound_template": {
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "peer",
                "auth": "users",
                "users": [],
                "link": {
                    "peer": "0.0.0.0:9443",
                    "password": "{{peer.password}}",
                },
                "lifecycle": lifecycle,
            },
            "outbound_template": {
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "peer",
                "link": {
                    "peer": "{{server}}:9443",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": lifecycle,
            },
            "requirements": {"build_tag": "with_carrier"},
            "client_notes": {
                "auth": "outbound link.password = user.secret; inbound users[] ACL",
            },
        },
    )


def add_shadowtls_wildcard() -> None:
    save(
        BASE / "shadowtls" / "shadowtls_v3_wildcard.json",
        {
            "schema_version": 1,
            "tag": "shadowtls_v3_wildcard",
            "protocol": "shadowtls",
            "aliases": ["shadowtls-wildcard", "shadowtls-v3-wildcard"],
            "short_name": "ShadowTLS wild",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "ShadowTLS v3 wildcard",
                    "description": "ShadowTLS v3 wildcard_sni=authed — SNI из ClientHello для handshake.",
                }
            },
            "traits": ["tcp", "tls_mimic", "wildcard_sni"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": [],
                "sni_required": True,
                "first_bytes": "TLS ClientHello → wildcard handshake",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 9, "speed": 6, "mobile": 5, "setup": 4},
            "cred_fields": ["password"],
            "inbound_template": {
                "type": "shadowtls",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "version": 3,
                "strict_mode": True,
                "users": [],
                "handshake": {"server": "www.google.com", "server_port": 443},
                "wildcard_sni": "authed",
            },
            "outbound_template": {
                "type": "shadowtls",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "version": 3,
                "password": "{{user.password}}",
                "tls": {
                    "enabled": True,
                    "server_name": "www.google.com",
                    "utls": {"enabled": True, "fingerprint": "chrome"},
                    "alpn": ["h2", "http/1.1"],
                },
            },
            "client_notes": {
                "wildcard_sni": "authed — только после успешной auth; outbound SNI = handshake target",
            },
        },
    )


def add_sudoku_aes() -> None:
    save(
        BASE / "sudoku" / "sudoku_aes.json",
        {
            "schema_version": 1,
            "tag": "sudoku_aes",
            "protocol": "sudoku",
            "aliases": ["sudoku-aes"],
            "short_name": "Sudoku AES",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Sudoku AES-GCM",
                    "description": "Sudoku shared key + aes-128-gcm + entropy table.",
                }
            },
            "traits": ["tcp", "shared_key"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "sudoku AES stream",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 6, "speed": 7, "mobile": 5, "setup": 5},
            "cred_fields": ["key"],
            "peer_secret_fields": {"key": "password"},
            "inbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "aes-128-gcm",
                "padding_min": 3,
                "padding_max": 12,
                "table_type": "prefer_entropy",
                "enable_pure_downlink": True,
                "handshake_timeout": 5,
                "fallback": "127.0.0.1:80",
                "suspicious_action": "fallback",
                "multiplex": "on",
            },
            "outbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "aes-128-gcm",
                "padding_min": 3,
                "padding_max": 12,
                "table_type": "prefer_entropy",
                "enable_pure_downlink": True,
                "multiplex": "on",
            },
            "requirements": {"build_tag": "with_sudoku"},
        },
    )


def update_index() -> None:
    idx = load(BASE / "index.json")
    protos = list(idx.get("protocols") or [])
    for p in ("derp", "carrier"):
        if p not in protos:
            protos.append(p)
    idx["protocols"] = protos
    save(BASE / "index.json", idx)


def main() -> None:
    enrich_thin()
    add_derp()
    add_carrier()
    add_shadowtls_wildcard()
    add_sudoku_aes()
    update_index()


if __name__ == "__main__":
    main()
