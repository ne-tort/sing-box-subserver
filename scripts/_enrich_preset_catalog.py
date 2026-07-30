#!/usr/bin/env python3
"""Enrich CP preset invariants: TLS/uTLS/ALPN, transport knobs, mux depth; add lx protocols."""
from __future__ import annotations

import json
import os
from copy import deepcopy
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"


def load(p: Path) -> dict:
    return json.loads(p.read_text(encoding="utf-8"))


def save(p: Path, obj: dict) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", p.relative_to(BASE))


def ensure_tls(obj: dict, *, alpn: list[str] | None, utls: bool, inbound: bool) -> None:
    for side in ("inbound_template", "outbound_template"):
        t = obj.get(side)
        if not isinstance(t, dict):
            continue
        tls = t.get("tls")
        if not isinstance(tls, dict) or not tls.get("enabled"):
            continue
        if alpn and "alpn" not in tls:
            tls["alpn"] = list(alpn)
        if utls and side == "outbound_template":
            u = tls.get("utls")
            if not isinstance(u, dict):
                tls["utls"] = {"enabled": True, "fingerprint": "chrome"}
            else:
                u.setdefault("enabled", True)
                u.setdefault("fingerprint", "chrome")
        t["tls"] = tls


def enrich_transport(tr: dict) -> None:
    if not isinstance(tr, dict):
        return
    typ = tr.get("type")
    if typ == "ws":
        tr.setdefault("path", "/ws")
        headers = tr.setdefault("headers", {})
        if isinstance(headers, dict):
            headers.setdefault("Host", ["cdn.example.com"])
            headers.setdefault("User-Agent", ["Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"])
        tr.setdefault("max_early_data", 2048)
        tr.setdefault("early_data_header_name", "Sec-WebSocket-Protocol")
    elif typ == "grpc":
        tr.setdefault("service_name", "GunService")
        tr.setdefault("idle_timeout", "15s")
        tr.setdefault("ping_timeout", "15s")
        tr.setdefault("permit_without_stream", False)
    elif typ == "httpupgrade":
        tr.setdefault("path", "/upgrade")
        tr.setdefault("host", "cdn.example.com")
        headers = tr.setdefault("headers", {})
        if isinstance(headers, dict):
            headers.setdefault("User-Agent", ["Mozilla/5.0"])
    elif typ == "http":
        tr.setdefault("path", "/http")
        tr.setdefault("method", "PUT")
        tr.setdefault("host", ["www.example.com"])
        headers = tr.setdefault("headers", {})
        if isinstance(headers, dict):
            headers.setdefault("User-Agent", ["Mozilla/5.0"])
            headers.setdefault("Accept-Language", ["en-US,en;q=0.9"])
        tr.setdefault("idle_timeout", "15s")
        tr.setdefault("ping_timeout", "15s")


def enrich_mux(obj: dict) -> None:
    for side in ("inbound_template", "outbound_template"):
        t = obj.get(side)
        if not isinstance(t, dict):
            continue
        mx = t.get("multiplex")
        if not isinstance(mx, dict) or not mx.get("enabled"):
            continue
        if side == "outbound_template":
            mx.setdefault("protocol", "smux")
            mx.setdefault("max_connections", 4)
            mx.setdefault("min_streams", 4)
            mx.setdefault("max_streams", 16)
        mx.setdefault("padding", True)


def enrich_file(p: Path) -> None:
    obj = load(p)
    traits = set(obj.get("traits") or [])
    tag = obj.get("tag", "")

    # TLS ALPN / uTLS by family
    if "quic" in traits or tag.startswith("hy") or tag.startswith("tuic") or "quic" in tag:
        ensure_tls(obj, alpn=["h3"], utls=False, inbound=True)
    elif "reality" in traits:
        ensure_tls(obj, alpn=["h2", "http/1.1"], utls=True, inbound=True)
    elif "tls" in traits or "grpc" in traits or "ws" in traits or "httpupgrade" in traits or "http" in traits:
        # grpc prefers h2
        alpn = ["h2"] if "grpc" in traits else ["h2", "http/1.1"]
        ensure_tls(obj, alpn=alpn, utls=True, inbound=True)

    for side in ("inbound_template", "outbound_template"):
        t = obj.get(side)
        if isinstance(t, dict) and isinstance(t.get("transport"), dict):
            enrich_transport(t["transport"])

    if "multiplex" in traits or "mux" in tag:
        enrich_mux(obj)

    # QUIC family extras
    if obj.get("protocol") == "hysteria2":
        ib = obj.setdefault("inbound_template", {})
        ob = obj.setdefault("outbound_template", {})
        ib.setdefault("ignore_client_bandwidth", True)
        if "obfs" not in tag and "masquerade" not in tag and "gecko" not in tag:
            ib.setdefault("up_mbps", 100)
            ib.setdefault("down_mbps", 100)
            ob.setdefault("up_mbps", 100)
            ob.setdefault("down_mbps", 100)
    if obj.get("protocol") == "tuic":
        ib = obj.setdefault("inbound_template", {})
        ob = obj.setdefault("outbound_template", {})
        ib.setdefault("congestion_control", "bbr")
        ib.setdefault("auth_timeout", "3s")
        ib.setdefault("heartbeat", "10s")
        ob.setdefault("congestion_control", "bbr")
        ob.setdefault("heartbeat", "10s")
        ob.setdefault("udp_relay_mode", "native")

    if obj.get("protocol") == "anytls":
        ensure_tls(obj, alpn=["h2", "http/1.1"], utls=True, inbound=True)

    if obj.get("protocol") == "mieru":
        ib = obj.setdefault("inbound_template", {})
        ob = obj.setdefault("outbound_template", {})
        ib.setdefault("multiplexing", "MULTIPLEXING_HIGH")
        ib.setdefault("handshake_mode", "HANDSHAKE_STANDARD")
        ob.setdefault("multiplexing", "MULTIPLEXING_HIGH")
        ob.setdefault("handshake_mode", "HANDSHAKE_STANDARD")
        if "tcp" in (obj.get("traits") or []):
            ib.setdefault("mtu", 1400)
            ob.setdefault("mtu", 1400)

    if obj.get("protocol") == "shadowtls":
        ib = obj.setdefault("inbound_template", {})
        ib.setdefault("wildcard_sni", "off")
        ob = obj.setdefault("outbound_template", {})
        tls = ob.setdefault("tls", {"enabled": True, "server_name": "www.google.com"})
        if isinstance(tls, dict):
            tls.setdefault("utls", {"enabled": True, "fingerprint": "chrome"})
            tls.setdefault("alpn", ["h2", "http/1.1"])

    if obj.get("protocol") == "ssh":
        ib = obj.setdefault("inbound_template", {})
        ob = obj.setdefault("outbound_template", {})
        ib.setdefault("server_version", "SSH-2.0-OpenSSH_9.6")
        ob.setdefault("client_version", "SSH-2.0-OpenSSH_9.6")

    if obj.get("protocol") == "vmess":
        ob = obj.setdefault("outbound_template", {})
        if "tcp" not in tag or "tls" in tag or "reality" in tag or "ws" in tag or "grpc" in tag:
            # keep security from profiles; add padding knobs as defaults off
            pass

    if obj.get("protocol") == "naive":
        ob = obj.setdefault("outbound_template", {})
        if "quic" not in tag:
            ob.setdefault("extra_headers", {"User-Agent": ["Mozilla/5.0"]})

    save(p, obj)


def write_new() -> None:
    # --- vmess symmetry ---
    save(
        BASE / "vmess" / "vmess_ws_reality.json",
        {
            "schema_version": 1,
            "tag": "vmess_ws_reality",
            "protocol": "vmess",
            "aliases": ["vmess-ws-reality"],
            "short_name": "VMess WS+R",
            "status": "stable",
            "i18n": {
                "ru": {
                    "title": "VMess WS Reality",
                    "description": "VMess + Reality + WebSocket (path/Host/early-data; uTLS chrome).",
                }
            },
            "traits": ["tcp", "tls", "reality", "ws"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": ["h2", "http/1.1"],
                "sni_required": True,
                "first_bytes": "Reality TLS then WS",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 8, "speed": 5, "mobile": 5, "setup": 5},
            "cred_fields": ["uuid"],
            "default_client_profiles": ["sec-auto"],
            "inbound_template": {
                "type": "vmess",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["h2", "http/1.1"]},
                "transport": {
                    "type": "ws",
                    "path": "/vmess",
                    "headers": {
                        "Host": ["www.microsoft.com"],
                        "User-Agent": ["Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"],
                    },
                    "max_early_data": 2048,
                    "early_data_header_name": "Sec-WebSocket-Protocol",
                },
            },
            "outbound_template": {
                "type": "vmess",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "uuid": "{{user.uuid}}",
                "security": "auto",
                "packet_encoding": "xudp",
                "tls": {
                    "enabled": True,
                    "server_name": "{{server}}",
                    "alpn": ["h2", "http/1.1"],
                    "utls": {"enabled": True, "fingerprint": "chrome"},
                },
                "transport": {
                    "type": "ws",
                    "path": "/vmess",
                    "headers": {
                        "Host": ["www.microsoft.com"],
                        "User-Agent": ["Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"],
                    },
                    "max_early_data": 2048,
                    "early_data_header_name": "Sec-WebSocket-Protocol",
                },
            },
            "requirements": {"tls_profile": True, "reality_assignment": True},
        },
    )
    save(
        BASE / "vmess" / "vmess_tls_mux.json",
        {
            "schema_version": 1,
            "tag": "vmess_tls_mux",
            "protocol": "vmess",
            "aliases": ["vmess-tls-mux"],
            "short_name": "VMess mux",
            "status": "stable",
            "i18n": {
                "ru": {
                    "title": "VMess TLS + multiplex",
                    "description": "VMess+TLS с smux multiplex (padding, stream limits).",
                }
            },
            "traits": ["tcp", "tls", "multiplex"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": ["h2", "http/1.1"],
                "sni_required": True,
                "first_bytes": "",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 5, "speed": 7, "mobile": 5, "setup": 6},
            "cred_fields": ["uuid"],
            "default_client_profiles": ["sec-auto"],
            "inbound_template": {
                "type": "vmess",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["h2", "http/1.1"]},
                "multiplex": {"enabled": True, "padding": True},
            },
            "outbound_template": {
                "type": "vmess",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "uuid": "{{user.uuid}}",
                "security": "auto",
                "packet_encoding": "xudp",
                "tls": {
                    "enabled": True,
                    "server_name": "{{server}}",
                    "alpn": ["h2", "http/1.1"],
                    "utls": {"enabled": True, "fingerprint": "chrome"},
                },
                "multiplex": {
                    "enabled": True,
                    "protocol": "smux",
                    "padding": True,
                    "max_connections": 4,
                    "min_streams": 4,
                    "max_streams": 16,
                },
            },
            "requirements": {"tls_profile": True},
        },
    )

    # --- shadowquic ---
    save(
        BASE / "shadowquic" / "protocol.json",
        {
            "schema_version": 1,
            "tag": "shadowquic",
            "singbox_type": "shadowquic",
            "short_name": "ShadowQUIC",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "ShadowQUIC",
                    "description": "ShadowQUIC + JLS SNI camouflage (with_shadowquic).",
                },
                "en": {"title": "ShadowQUIC", "description": "ShadowQUIC + JLS (with_shadowquic)."},
            },
            "default_cred_fields": ["username", "password"],
        },
    )
    save(
        BASE / "shadowquic" / "shadowquic_jls.json",
        {
            "schema_version": 1,
            "tag": "shadowquic_jls",
            "protocol": "shadowquic",
            "aliases": ["shadowquic", "shadowquic-jls"],
            "short_name": "SQ JLS",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "ShadowQUIC JLS",
                    "description": "UDP/QUIC ShadowQUIC: JLS upstream camouflage, ALPN h3, congestion bbr.",
                }
            },
            "traits": ["udp", "quic", "jls"],
            "demux_hints": {
                "network": ["udp"],
                "looks_like": "quic",
                "alpn": ["h3"],
                "sni_required": True,
                "first_bytes": "QUIC ClientHello → JLS upstream",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 9, "speed": 8, "mobile": 5, "setup": 5},
            "cred_fields": ["username", "password"],
            "inbound_template": {
                "type": "shadowquic",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "jls_upstream": {
                    "addr": "www.cloudflare.com:443",
                    "server_name": "www.cloudflare.com",
                    "quic_version_probe": True,
                },
                "alpn": ["h3"],
                "quic_versions": ["v1"],
                "congestion_control": "bbr",
                "idle_timeout": "30s",
                "zero_rtt": False,
            },
            "outbound_template": {
                "type": "shadowquic",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "username": "{{user.username}}",
                "password": "{{user.password}}",
                "server_name": "www.cloudflare.com",
                "sni": "www.cloudflare.com",
                "alpn": ["h3"],
                "quic_versions": ["v1"],
                "congestion_control": "bbr",
                "keep_alive_interval": "15s",
                "idle_timeout": "30s",
                "udp_over_stream": False,
                "zero_rtt": False,
            },
            "requirements": {"udp": True, "quic": True, "build_tag": "with_shadowquic"},
            "client_notes": {
                "jls_upstream": "должен быть доступен с edge; SNI outbound обычно совпадает",
            },
        },
    )
    save(
        BASE / "shadowquic" / "shadowquic_0rtt.json",
        {
            "schema_version": 1,
            "tag": "shadowquic_0rtt",
            "protocol": "shadowquic",
            "aliases": ["shadowquic-0rtt"],
            "short_name": "SQ 0-RTT",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "ShadowQUIC 0-RTT",
                    "description": "ShadowQUIC JLS + zero_rtt (быстрее, слабее replay resistance).",
                }
            },
            "traits": ["udp", "quic", "jls", "0rtt"],
            "demux_hints": {
                "network": ["udp"],
                "looks_like": "quic",
                "alpn": ["h3"],
                "sni_required": True,
                "first_bytes": "QUIC 0-RTT",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 8, "speed": 9, "mobile": 5, "setup": 4},
            "cred_fields": ["username", "password"],
            "inbound_template": {
                "type": "shadowquic",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "jls_upstream": {
                    "addr": "www.cloudflare.com:443",
                    "server_name": "www.cloudflare.com",
                },
                "alpn": ["h3"],
                "congestion_control": "bbr",
                "idle_timeout": "30s",
                "zero_rtt": True,
            },
            "outbound_template": {
                "type": "shadowquic",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "username": "{{user.username}}",
                "password": "{{user.password}}",
                "server_name": "www.cloudflare.com",
                "sni": "www.cloudflare.com",
                "alpn": ["h3"],
                "congestion_control": "bbr",
                "keep_alive_interval": "15s",
                "idle_timeout": "30s",
                "zero_rtt": True,
            },
            "requirements": {"udp": True, "quic": True, "build_tag": "with_shadowquic"},
        },
    )

    # --- sudoku (shared key) ---
    save(
        BASE / "sudoku" / "protocol.json",
        {
            "schema_version": 1,
            "tag": "sudoku",
            "singbox_type": "sudoku",
            "short_name": "Sudoku",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Sudoku",
                    "description": "Sudoku-ASCII obfuscation (with_sudoku); shared key.",
                },
                "en": {"title": "Sudoku", "description": "Sudoku-ASCII (with_sudoku)."},
            },
            "default_cred_fields": ["key"],
        },
    )
    save(
        BASE / "sudoku" / "sudoku_pad.json",
        {
            "schema_version": 1,
            "tag": "sudoku_pad",
            "protocol": "sudoku",
            "aliases": ["sudoku", "sudoku-pad"],
            "short_name": "Sudoku pad",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Sudoku padding",
                    "description": "Sudoku TCP: AEAD + padding + fallback decoy; shared peer key.",
                }
            },
            "traits": ["tcp", "shared_key"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "sudoku ASCII stream",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 6, "speed": 6, "mobile": 5, "setup": 5},
            "cred_fields": ["key"],
            "peer_secret_fields": {"key": "password"},
            "inbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "chacha20-poly1305",
                "padding_min": 5,
                "padding_max": 15,
                "table_type": "prefer_ascii",
                "enable_pure_downlink": True,
                "handshake_timeout": 5,
                "fallback": "127.0.0.1:80",
                "suspicious_action": "fallback",
                "multiplex": "auto",
            },
            "outbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "chacha20-poly1305",
                "padding_min": 5,
                "padding_max": 15,
                "table_type": "prefer_ascii",
                "enable_pure_downlink": True,
                "multiplex": "auto",
            },
            "requirements": {"build_tag": "with_sudoku"},
            "client_notes": {"key": "shared peer key (не per-user); set.peer_secrets"},
        },
    )
    save(
        BASE / "sudoku" / "sudoku_httpmask.json",
        {
            "schema_version": 1,
            "tag": "sudoku_httpmask",
            "protocol": "sudoku",
            "aliases": ["sudoku-httpmask"],
            "short_name": "Sudoku HTTP",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Sudoku HTTPMask",
                    "description": "Sudoku + httpmask legacy (DPI: HTTP-like framing поверх TCP).",
                }
            },
            "traits": ["tcp", "shared_key", "httpmask"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "http",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "HTTP-like sudoku mask",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 7, "speed": 5, "mobile": 5, "setup": 4},
            "cred_fields": ["key"],
            "peer_secret_fields": {"key": "password"},
            "inbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "chacha20-poly1305",
                "padding_min": 8,
                "padding_max": 24,
                "table_type": "prefer_ascii",
                "enable_pure_downlink": True,
                "handshake_timeout": 5,
                "fallback": "127.0.0.1:80",
                "suspicious_action": "fallback",
                "multiplex": "off",
                "httpmask": {"disable": False, "mode": "legacy", "path_root": "/sudoku"},
            },
            "outbound_template": {
                "type": "sudoku",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "key": "{{peer.key}}",
                "aead_method": "chacha20-poly1305",
                "padding_min": 8,
                "padding_max": 24,
                "table_type": "prefer_ascii",
                "enable_pure_downlink": True,
                "multiplex": "off",
                "httpmask": {"disable": False, "mode": "legacy", "path_root": "/sudoku"},
            },
            "requirements": {"build_tag": "with_sudoku"},
        },
    )

    # --- trusttunnel ---
    save(
        BASE / "trusttunnel" / "protocol.json",
        {
            "schema_version": 1,
            "tag": "trusttunnel",
            "singbox_type": "trusttunnel",
            "short_name": "TrustTunnel",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "TrustTunnel",
                    "description": "TrustTunnel H2/H3 anti-DPI (with_trusttunnel).",
                },
                "en": {"title": "TrustTunnel", "description": "TrustTunnel (with_trusttunnel)."},
            },
            "default_cred_fields": ["username", "password"],
        },
    )
    save(
        BASE / "trusttunnel" / "trusttunnel_h2.json",
        {
            "schema_version": 1,
            "tag": "trusttunnel_h2",
            "protocol": "trusttunnel",
            "aliases": ["trusttunnel", "trusttunnel-h2"],
            "short_name": "TT H2",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "TrustTunnel HTTP/2",
                    "description": "TrustTunnel: anti_dpi, H2 transport, auth users; TLS через CP (custom tls block).",
                }
            },
            "traits": ["tcp", "tls_custom", "http2"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": ["h2"],
                "sni_required": True,
                "first_bytes": "",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 8, "speed": 7, "mobile": 5, "setup": 4},
            "cred_fields": ["username", "password"],
            "inbound_template": {
                "type": "trusttunnel",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "hostname": "{{server}}",
                "users": [],
                "tls": {"server_name": "{{server}}"},
                "transport": {
                    "upstream_protocol": "http2",
                    "anti_dpi": True,
                    "connection_timeout": "10s",
                    "max_idle_timeout": "60s",
                },
                "enable_tcp": True,
                "enable_udp": True,
                "enable_icmp": False,
            },
            "outbound_template": {
                "type": "trusttunnel",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "hostname": "{{server}}",
                "username": "{{user.username}}",
                "password": "{{user.password}}",
                "tls": {"server_name": "{{server}}", "skip_verification": True},
                "transport": {
                    "upstream_protocol": "http2",
                    "anti_dpi": True,
                    "connection_timeout": "10s",
                    "max_idle_timeout": "60s",
                },
                "headers": {
                    "user_agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
                    "app_name": "Chrome",
                    "platform": "windows",
                },
                "enable_tcp": True,
                "enable_udp": True,
                "enable_icmp": False,
            },
            "requirements": {"tls_profile": True, "build_tag": "with_trusttunnel"},
            "client_notes": {
                "tls": "TrustTunnel TLS — отдельный блок (не InboundTLSOptions); CP подставляет PEM при materialize",
            },
        },
    )
    save(
        BASE / "trusttunnel" / "trusttunnel_h3.json",
        {
            "schema_version": 1,
            "tag": "trusttunnel_h3",
            "protocol": "trusttunnel",
            "aliases": ["trusttunnel-h3"],
            "short_name": "TT H3",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "TrustTunnel HTTP/3",
                    "description": "TrustTunnel: upstream_protocol http3 / auto fallback.",
                }
            },
            "traits": ["udp", "quic", "tls_custom", "http3"],
            "demux_hints": {
                "network": ["udp"],
                "looks_like": "quic",
                "alpn": ["h3"],
                "sni_required": True,
                "first_bytes": "",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 8, "speed": 8, "mobile": 4, "setup": 4},
            "cred_fields": ["username", "password"],
            "inbound_template": {
                "type": "trusttunnel",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "hostname": "{{server}}",
                "users": [],
                "tls": {"server_name": "{{server}}"},
                "transport": {
                    "upstream_protocol": "http3",
                    "anti_dpi": True,
                    "enable_protocol_fallback": True,
                    "protocol_fallback_delay": "1s",
                    "connection_timeout": "10s",
                },
                "enable_tcp": True,
                "enable_udp": True,
                "enable_icmp": False,
            },
            "outbound_template": {
                "type": "trusttunnel",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "hostname": "{{server}}",
                "username": "{{user.username}}",
                "password": "{{user.password}}",
                "tls": {"server_name": "{{server}}", "skip_verification": True},
                "transport": {
                    "upstream_protocol": "auto",
                    "anti_dpi": True,
                    "enable_protocol_fallback": True,
                    "protocol_fallback_delay": "1s",
                    "connection_timeout": "10s",
                },
                "headers": {
                    "user_agent": "Mozilla/5.0",
                    "app_name": "Chrome",
                    "platform": "windows",
                },
                "enable_tcp": True,
                "enable_udp": True,
            },
            "requirements": {"tls_profile": True, "udp": True, "quic": True, "build_tag": "with_trusttunnel"},
        },
    )

    # hy2 masquerade proxy variant
    save(
        BASE / "hysteria2" / "hy2_masquerade_proxy.json",
        {
            "schema_version": 1,
            "tag": "hy2_masquerade_proxy",
            "protocol": "hysteria2",
            "aliases": ["hysteria2-masquerade-proxy"],
            "short_name": "Hy2 maskP",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Hysteria2 Masquerade Proxy",
                    "description": "Hy2 + masquerade.type=proxy (пробы уходят на внешний URL).",
                }
            },
            "traits": ["udp", "quic", "tls", "masquerade"],
            "demux_hints": {
                "network": ["udp"],
                "looks_like": "quic",
                "alpn": ["h3"],
                "sni_required": True,
                "first_bytes": "QUIC/H3 masquerade proxy",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 8, "speed": 9, "mobile": 5, "setup": 4},
            "cred_fields": ["password"],
            "inbound_template": {
                "type": "hysteria2",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "ignore_client_bandwidth": True,
                "masquerade": {
                    "type": "proxy",
                    "url": "https://www.microsoft.com",
                    "rewrite_host": True,
                },
                "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["h3"]},
            },
            "outbound_template": {
                "type": "hysteria2",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "password": "{{user.password}}",
                "tls": {"enabled": True, "server_name": "{{server}}", "alpn": ["h3"]},
            },
            "requirements": {"tls_profile": True, "udp": True, "quic": True},
            "client_notes": {"masquerade": "inbound-only"},
        },
    )

    # ssh pubkey + uot
    save(
        BASE / "ssh" / "ssh_uot.json",
        {
            "schema_version": 1,
            "tag": "ssh_uot",
            "protocol": "ssh",
            "aliases": ["ssh-uot"],
            "short_name": "SSH UoT",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "SSH + UDP-over-TCP",
                    "description": "SSH password auth; outbound udp_over_tcp (with_ssh).",
                }
            },
            "traits": ["tcp", "udp_over_tcp"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "ssh_banner",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "SSH-2.0 banner",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 4, "speed": 5, "mobile": 4, "setup": 4},
            "cred_fields": ["username", "password"],
            "inbound_template": {
                "type": "ssh",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "server_version": "SSH-2.0-OpenSSH_9.6",
            },
            "outbound_template": {
                "type": "ssh",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "user": "{{user.username}}",
                "password": "{{user.password}}",
                "client_version": "SSH-2.0-OpenSSH_9.6",
                "udp_over_tcp": {"enabled": True, "version": 2},
            },
            "requirements": {"build_tag": "with_ssh"},
        },
    )


def update_index() -> None:
    idx = {
        "schema_version": 1,
        "protocols": [
            "vless",
            "shadowsocks",
            "trojan",
            "vmess",
            "hysteria2",
            "tuic",
            "anytls",
            "socks",
            "http",
            "ssh",
            "hysteria",
            "naive",
            "shadowtls",
            "snell",
            "mixed",
            "mieru",
            "shadowquic",
            "sudoku",
            "trusttunnel",
        ],
    }
    save(BASE / "index.json", idx)


def main() -> None:
    for proto in sorted(os.listdir(BASE)):
        d = BASE / proto
        if not d.is_dir():
            continue
        for f in sorted(d.glob("*.json")):
            if f.name == "protocol.json":
                continue
            enrich_file(f)
    write_new()
    update_index()
    # enrich newly written too
    for tag in [
        "vmess/vmess_ws_reality.json",
        "vmess/vmess_tls_mux.json",
        "shadowquic/shadowquic_jls.json",
        "shadowquic/shadowquic_0rtt.json",
        "sudoku/sudoku_pad.json",
        "sudoku/sudoku_httpmask.json",
        "trusttunnel/trusttunnel_h2.json",
        "trusttunnel/trusttunnel_h3.json",
        "hysteria2/hy2_masquerade_proxy.json",
        "ssh/ssh_uot.json",
    ]:
        enrich_file(BASE / tag)
    print("done")


if __name__ == "__main__":
    main()
