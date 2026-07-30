#!/usr/bin/env python3
"""Fill remaining inbound preset gaps: transports/reality, ssh pubkey, cloudflared."""
from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"


def load(p: Path) -> dict:
    return json.loads(p.read_text(encoding="utf-8"))


def save(rel: str, obj: dict) -> None:
    p = BASE / rel
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", rel)


def tls_block(alpn: list[str], utls: bool = False) -> dict:
    t = {"enabled": True, "server_name": "{{server}}", "alpn": alpn}
    if utls:
        t["utls"] = {"enabled": True, "fingerprint": "chrome"}
    return t


def grpc_tr() -> dict:
    return {
        "type": "grpc",
        "service_name": "GunService",
        "idle_timeout": "15s",
        "ping_timeout": "15s",
        "permit_without_stream": False,
    }


def http_tr(path: str) -> dict:
    return {
        "type": "http",
        "host": ["www.example.com"],
        "path": path,
        "method": "PUT",
        "headers": {
            "User-Agent": ["Mozilla/5.0"],
            "Accept-Language": ["en-US,en;q=0.9"],
        },
        "idle_timeout": "15s",
        "ping_timeout": "15s",
    }


def reality_proto(
    tag: str,
    protocol: str,
    short: str,
    desc: str,
    transport: dict,
    traits: list[str],
    *,
    alpn: list[str],
    path_note: str,
    cred_fields: list[str],
    default_user_variants: list[str] | None = None,
    default_client_profiles: list[str] | None = None,
    out_extra: dict | None = None,
) -> dict:
    aliases = [tag.replace("_", "-")]
    ib = {
        "type": protocol,
        "tag": "{{tag}}",
        "listen": "{{listen}}",
        "listen_port": 0,
        "users": [],
        "tls": tls_block(alpn),
        "transport": deepcopy(transport),
    }
    ob = {
        "type": protocol,
        "tag": "{{tag}}",
        "server": "{{server}}",
        "server_port": 0,
        "tls": tls_block(alpn, utls=True),
        "transport": deepcopy(transport),
    }
    if protocol == "vless":
        ob["uuid"] = "{{user.uuid}}"
        ob["packet_encoding"] = "xudp"
    elif protocol == "vmess":
        ob["uuid"] = "{{user.uuid}}"
        ob["security"] = "auto"
    elif protocol == "trojan":
        ob["password"] = "{{user.password}}"
    if out_extra:
        ob.update(out_extra)
    obj = {
        "schema_version": 1,
        "tag": tag,
        "protocol": protocol,
        "aliases": aliases,
        "short_name": short,
        "status": "stable",
        "i18n": {"ru": {"title": short, "description": desc}},
        "traits": traits,
        "demux_hints": {
            "network": ["tcp"],
            "looks_like": "tls_clienthello",
            "alpn": alpn,
            "sni_required": True,
            "first_bytes": path_note,
            "compatible_with_demux": True,
        },
        "scores": {"dpi": 9, "speed": 6, "mobile": 5, "setup": 5},
        "cred_fields": cred_fields,
        "inbound_template": ib,
        "outbound_template": ob,
        "requirements": {"tls_profile": True, "reality_assignment": True},
    }
    if default_user_variants:
        obj["default_user_variants"] = default_user_variants
    if default_client_profiles:
        obj["default_client_profiles"] = default_client_profiles
    return obj


def add_transport_reality() -> None:
    # gRPC + Reality
    for proto, creds, variants, profiles in (
        ("vless", ["uuid"], ["flow-none"], ["pkt-xudp"]),
        ("vmess", ["uuid"], None, ["sec-auto"]),
        ("trojan", ["password"], None, None),
    ):
        save(
            f"{proto}/{proto}_grpc_reality.json",
            reality_proto(
                f"{proto}_grpc_reality",
                proto,
                f"{proto.upper() if proto!='trojan' else 'Trojan'} gRPC+R",
                f"{proto} + Reality + gRPC (ALPN h2).",
                grpc_tr(),
                ["tcp", "tls", "grpc", "reality"],
                alpn=["h2"],
                path_note="Reality TLS then h2 gRPC",
                cred_fields=creds,
                default_user_variants=variants,
                default_client_profiles=profiles,
            ),
        )
    # HTTP transport + Reality
    for proto, creds, variants, profiles, path in (
        ("vless", ["uuid"], ["flow-none"], ["pkt-xudp"], "/vless-h2"),
        ("vmess", ["uuid"], None, ["sec-auto"], "/vmess-h2"),
        ("trojan", ["password"], None, None, "/trojan-h2"),
    ):
        save(
            f"{proto}/{proto}_http_reality.json",
            reality_proto(
                f"{proto}_http_reality",
                proto,
                f"{'VLESS' if proto=='vless' else 'VMess' if proto=='vmess' else 'Trojan'} HTTP+R",
                f"{proto} + Reality + HTTP/2 transport.",
                http_tr(path),
                ["tcp", "tls", "http", "reality"],
                alpn=["h2", "http/1.1"],
                path_note="Reality TLS then HTTP/2",
                cred_fields=creds,
                default_user_variants=variants,
                default_client_profiles=profiles,
            ),
        )


def add_trojan_http_tls() -> None:
    save(
        "trojan/trojan_http_tls.json",
        {
            "schema_version": 1,
            "tag": "trojan_http_tls",
            "protocol": "trojan",
            "aliases": ["trojan-http-tls"],
            "short_name": "Trojan HTTP",
            "status": "stable",
            "i18n": {
                "ru": {
                    "title": "Trojan HTTP/2 TLS",
                    "description": "Trojan + TLS + HTTP transport (parity with vless/vmess http).",
                }
            },
            "traits": ["tcp", "tls", "http"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": ["h2", "http/1.1"],
                "sni_required": True,
                "first_bytes": "TLS then HTTP/2",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 6, "speed": 5, "mobile": 5, "setup": 5},
            "cred_fields": ["password"],
            "inbound_template": {
                "type": "trojan",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "users": [],
                "tls": tls_block(["h2", "http/1.1"]),
                "transport": http_tr("/trojan-h2"),
            },
            "outbound_template": {
                "type": "trojan",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "password": "{{user.password}}",
                "tls": tls_block(["h2", "http/1.1"], utls=True),
                "transport": http_tr("/trojan-h2"),
            },
            "requirements": {"tls_profile": True},
        },
    )


def add_ssh_pubkey() -> None:
    save(
        "ssh/ssh_pubkey.json",
        {
            "schema_version": 1,
            "tag": "ssh_pubkey",
            "protocol": "ssh",
            "aliases": ["ssh-pubkey", "ssh-key"],
            "short_name": "SSH pubkey",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "SSH public key",
                    "description": "SSH inbound: user + authorized public_key; outbound private_key (ed25519 auto).",
                }
            },
            "traits": ["tcp", "pubkey"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "ssh_banner",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "SSH-2.0 banner",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 4, "speed": 5, "mobile": 4, "setup": 3},
            "cred_fields": ["username", "private_key", "public_key"],
            "cred_generators": {"private_key": "ssh_ed25519"},
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
                "private_key": ["{{user.private_key}}"],
                "client_version": "SSH-2.0-OpenSSH_9.6",
            },
            "requirements": {"build_tag": "with_ssh"},
            "client_notes": {
                "keys": "ensureCreds генерирует ed25519 PEM + authorized_keys; inbound users[].public_key",
            },
        },
    )


def add_shadowtls_all() -> None:
    save(
        "shadowtls/shadowtls_v3_wildcard_all.json",
        {
            "schema_version": 1,
            "tag": "shadowtls_v3_wildcard_all",
            "protocol": "shadowtls",
            "aliases": ["shadowtls-wildcard-all"],
            "short_name": "ShadowTLS all",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "ShadowTLS v3 wildcard=all",
                    "description": "wildcard_sni=all — handshake по SNI из ClientHello для любого имени.",
                }
            },
            "traits": ["tcp", "tls_mimic", "wildcard_sni"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "tls_clienthello",
                "alpn": [],
                "sni_required": True,
                "first_bytes": "TLS ClientHello → wildcard all",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 9, "speed": 6, "mobile": 5, "setup": 3},
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
                "wildcard_sni": "all",
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
        },
    )


def add_cloudflared() -> None:
    save(
        "cloudflared/protocol.json",
        {
            "schema_version": 1,
            "tag": "cloudflared",
            "singbox_type": "cloudflared",
            "short_name": "Cloudflared",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Cloudflared",
                    "description": "Cloudflare Tunnel inbound (with_cloudflared); token в bindings[].params.",
                },
                "en": {"title": "Cloudflared", "description": "Cloudflare Tunnel inbound."},
            },
            "default_cred_fields": [],
            "notes": {
                "subscription": "нет симметричного outbound — только edge→router; sub не эмитит outbounds",
                "params": "обязателен bindings[].params.token",
            },
        },
    )
    save(
        "cloudflared/cloudflared_token.json",
        {
            "schema_version": 1,
            "tag": "cloudflared_token",
            "protocol": "cloudflared",
            "aliases": ["cloudflared", "cf-tunnel"],
            "short_name": "CF Tunnel",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Cloudflared token",
                    "description": "Cloudflare Tunnel: params.token; HA + http2 + post_quantum; без listen/users.",
                }
            },
            "traits": ["no_listen", "no_users", "cloudflare", "inbound_only"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "cloudflared control plane",
                "compatible_with_demux": False,
            },
            "scores": {"dpi": 8, "speed": 7, "mobile": 6, "setup": 4},
            "cred_fields": [],
            "param_fields": ["token"],
            "inbound_template": {
                "type": "cloudflared",
                "tag": "{{tag}}",
                "token": "{{param.token}}",
                "ha_connections": 4,
                "protocol": "http2",
                "post_quantum": True,
                "edge_ip_version": 0,
                "datagram_version": "quic",
                "grace_period": "30s",
            },
            "outbound_template": {},
            "requirements": {"build_tag": "with_cloudflared"},
            "client_notes": {
                "token": "Cloudflare tunnel token (ops) в bindings[].params.token",
                "subscription": "inbound_only — RenderSubscription пропускает",
            },
        },
    )


def add_ss2022_mux() -> None:
    save(
        "shadowsocks/ss_2022_aes128_mux.json",
        {
            "schema_version": 1,
            "tag": "ss_2022_aes128_mux",
            "protocol": "shadowsocks",
            "aliases": ["ss2022-aes128-mux"],
            "short_name": "SS2022+mux",
            "status": "stable",
            "i18n": {
                "ru": {
                    "title": "SS2022 AES-128 + mux",
                    "description": "2022-blake3-aes-128-gcm multi-user + multiplex smux.",
                }
            },
            "traits": ["tcp", "multiplex"],
            "demux_hints": {
                "network": ["tcp"],
                "looks_like": "raw_tcp",
                "alpn": [],
                "sni_required": False,
                "first_bytes": "SS2022 AEAD+mux",
                "compatible_with_demux": True,
            },
            "scores": {"dpi": 3, "speed": 8, "mobile": 6, "setup": 5},
            "cred_fields": ["password"],
            "cred_generators": {"password": "ss2022_16"},
            "peer_secret_fields": {"password": "ss2022_16"},
            "inbound_template": {
                "type": "shadowsocks",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "method": "2022-blake3-aes-128-gcm",
                "password": "{{peer.password}}",
                "network": "tcp",
                "users": [],
                "multiplex": {"enabled": True, "padding": True},
            },
            "outbound_template": {
                "type": "shadowsocks",
                "tag": "{{tag}}",
                "server": "{{server}}",
                "server_port": 0,
                "method": "2022-blake3-aes-128-gcm",
                "password": "{{user.password}}",
                "network": "tcp",
                "multiplex": {
                    "enabled": True,
                    "protocol": "smux",
                    "padding": True,
                    "max_connections": 4,
                    "min_streams": 4,
                    "max_streams": 16,
                },
            },
            "client_notes": {
                "password": "subscription password = peer.password + ':' + user.password",
            },
        },
    )


def update_index() -> None:
    idx = load(BASE / "index.json")
    protos = list(idx.get("protocols") or [])
    if "cloudflared" not in protos:
        protos.append("cloudflared")
    idx["protocols"] = protos
    save("index.json", idx)


def main() -> None:
    add_transport_reality()
    add_trojan_http_tls()
    add_ssh_pubkey()
    add_shadowtls_all()
    add_cloudflared()
    add_ss2022_mux()
    update_index()


if __name__ == "__main__":
    main()
