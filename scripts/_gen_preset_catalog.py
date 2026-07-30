# One-shot generator for preset catalog skeleton (run from sing-box-subserver root).
import json
from pathlib import Path

ROOT = Path("internal/controlplane/presets/data")

def i18n(title_ru, desc_ru, title_en="", desc_en=""):
    out = {"ru": {"title": title_ru, "description": desc_ru}}
    if title_en or desc_en:
        out["en"] = {"title": title_en or title_ru, "description": desc_en or desc_ru}
    return out

def scores(dpi=None, speed=None, mobile=None, setup=None):
    s = {}
    if dpi is not None: s["dpi"] = dpi
    if speed is not None: s["speed"] = speed
    if mobile is not None: s["mobile"] = mobile
    if setup is not None: s["setup"] = setup
    return s or None

def demux(network, looks, compatible=True, alpn=None, sni=False, first=""):
    return {
        "network": network,
        "looks_like": looks,
        "alpn": alpn or [],
        "sni_required": sni,
        "first_bytes": first,
        "compatible_with_demux": compatible,
    }

protocols = [
    ("vless", "VLESS", "stable", ["uuid"], "Лёгкий UUID-транспорт; часто с TLS/Reality.", {"variants_family": "vless_flow"}),
    ("shadowsocks", "Shadowsocks", "stable", ["password"], "Классический AEAD multi-user без TLS.", {}),
    ("trojan", "Trojan", "stable", ["password"], "Парольный прокси поверх TLS.", {}),
    ("vmess", "VMess", "stable", ["uuid"], "UUID-транспорт VMess (AES/auto).", {}),
    ("hysteria2", "Hysteria2", "stable", ["password"], "UDP/QUIC с агрессивным congestion control.", {}),
    ("tuic", "TUIC", "stable", ["uuid", "password"], "TUIC v5 поверх QUIC/TLS.", {}),
    ("anytls", "AnyTLS", "stable", ["password"], "TLS-обёртка с парольной аутентификацией.", {}),
    ("socks", "SOCKS5", "stable", ["username", "password"], "Классический SOCKS5 (часто цель demux).", {}),
    ("http", "HTTP", "stable", ["username", "password"], "HTTP CONNECT прокси.", {}),
    ("ssh", "SSH", "lab", ["password"], "SSH inbound (сборка with_ssh); lab-скелет.", {}),
    ("hysteria", "Hysteria", "planned", ["password"], "Hysteria v1 (планируется).", {}),
    ("naive", "Naive", "planned", [], "NaiveProxy inbound (планируется).", {}),
    ("shadowtls", "ShadowTLS", "planned", [], "ShadowTLS inbound (планируется).", {}),
    ("snell", "Snell", "planned", [], "Snell inbound (планируется).", {}),
    ("mixed", "Mixed", "planned", ["username", "password"], "HTTP+SOCKS combined inbound (планируется).", {}),
    ("mieru", "Mieru", "planned", [], "Mieru inbound (планируется).", {}),
]

index = {"schema_version": 1, "protocols": [p[0] for p in protocols]}

for tag, short, status, creds, desc, notes in protocols:
    d = ROOT / tag
    d.mkdir(parents=True, exist_ok=True)
    meta = {
        "schema_version": 1,
        "tag": tag,
        "singbox_type": tag,
        "short_name": short,
        "status": status,
        "i18n": i18n(short, desc, short, desc),
        "default_cred_fields": creds,
    }
    if notes:
        meta["notes"] = notes
    (d / "protocol.json").write_text(json.dumps(meta, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

# --- invariants with templates ---

def inv(path, obj):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

def base_inv(tag, protocol, aliases, short, status, traits, demux_hints, scores_d, creds, ib, ob, title_ru, desc_ru, req=None):
    o = {
        "schema_version": 1,
        "tag": tag,
        "protocol": protocol,
        "aliases": aliases,
        "short_name": short,
        "status": status,
        "i18n": i18n(title_ru, desc_ru),
        "traits": traits,
        "demux_hints": demux_hints,
        "scores": scores_d,
        "cred_fields": creds,
        "inbound_template": ib,
        "outbound_template": ob,
    }
    if req:
        o["requirements"] = req
    return o

# Shadowsocks
inv(ROOT / "shadowsocks/ss_aes128.json", base_inv(
    "ss_aes128", "shadowsocks", ["shadowsocks-tcp"], "SS AES128", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True, first="AEAD stream"),
    scores(2, 7, 6, 8), ["password"],
    {"type": "shadowsocks", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "method": "aes-128-gcm", "users": []},
    {"type": "shadowsocks", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "method": "aes-128-gcm", "password": "{{user.password}}"},
    "SS AES-128-GCM", "Shadowsocks AES-128-GCM multi-user, TCP, без TLS.",
))
inv(ROOT / "shadowsocks/ss_aes256.json", base_inv(
    "ss_aes256", "shadowsocks", ["shadowsocks-aes-256-gcm"], "SS AES256", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True),
    scores(2, 7, 6, 8), ["password"],
    {"type": "shadowsocks", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "method": "aes-256-gcm", "users": []},
    {"type": "shadowsocks", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "method": "aes-256-gcm", "password": "{{user.password}}"},
    "SS AES-256-GCM", "Shadowsocks AES-256-GCM multi-user, TCP.",
))
inv(ROOT / "shadowsocks/ss_chacha20.json", base_inv(
    "ss_chacha20", "shadowsocks", ["shadowsocks-chacha20"], "SS ChaCha20", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True),
    scores(2, 7, 7, 8), ["password"],
    {"type": "shadowsocks", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "method": "chacha20-ietf-poly1305", "users": []},
    {"type": "shadowsocks", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "method": "chacha20-ietf-poly1305", "password": "{{user.password}}"},
    "SS ChaCha20", "Shadowsocks chacha20-ietf-poly1305, TCP.",
))

# Trojan
inv(ROOT / "trojan/trojan_tls.json", base_inv(
    "trojan_tls", "trojan", ["trojan-tcp"], "Trojan TLS", "stable", ["tcp", "tls"],
    demux(["tcp"], "tls_clienthello", True, sni=True),
    scores(6, 6, 6, 7), ["password"],
    {"type": "trojan", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "trojan", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "password": "{{user.password}}", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "Trojan TLS", "Trojan поверх TCP+TLS (профиль CP).",
    {"tls_profile": True},
))

# VLESS
inv(ROOT / "vless/vless_tcp.json", base_inv(
    "vless_tcp", "vless", ["vless-tcp"], "VLESS TCP", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True, first="часто цель demux inject"),
    scores(3, 8, 7, 8), ["uuid", "uuid_xtls", "uuid_udp"],
    {"type": "vless", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": []},
    {"type": "vless", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}"},
    "VLESS TCP", "VLESS без TLS — lab / demux plaintext target.",
))
inv(ROOT / "vless/vless_tls.json", base_inv(
    "vless_tls", "vless", ["vless-tls"], "VLESS TLS", "stable", ["tcp", "tls"],
    demux(["tcp"], "tls_clienthello", True, sni=True),
    scores(6, 7, 7, 7), ["uuid", "uuid_xtls", "uuid_udp"],
    {"type": "vless", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "vless", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "VLESS TLS", "VLESS TCP с TLS-профилем controlplane.",
    {"tls_profile": True},
))
inv(ROOT / "vless/vless_reality.json", base_inv(
    "vless_reality", "vless", ["vless-reality-tcp"], "VLESS Reality", "stable", ["tcp", "tls", "reality"],
    demux(["tcp"], "tls_clienthello", True, sni=True, first="Reality TLS fingerprint"),
    scores(10, 7, 7, 6), ["uuid", "uuid_xtls", "uuid_udp"],
    {"type": "vless", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "vless", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "VLESS Reality", "Эталон обхода DPI: VLESS + Reality (ключи на inbound).",
    {"tls_profile": True, "reality_assignment": True},
))

# VMess
inv(ROOT / "vmess/vmess_tcp.json", base_inv(
    "vmess_tcp", "vmess", ["vmess-tcp"], "VMess TCP", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True),
    scores(3, 6, 5, 7), ["uuid"],
    {"type": "vmess", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": []},
    {"type": "vmess", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}", "security": "auto"},
    "VMess TCP", "VMess без TLS.",
))
inv(ROOT / "vmess/vmess_tls.json", base_inv(
    "vmess_tls", "vmess", ["vmess-tls"], "VMess TLS", "stable", ["tcp", "tls"],
    demux(["tcp"], "tls_clienthello", True, sni=True),
    scores(5, 6, 5, 7), ["uuid"],
    {"type": "vmess", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "vmess", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}", "security": "auto", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "VMess TLS", "VMess TCP с TLS-профилем CP.",
    {"tls_profile": True},
))

# Hy2 / TUIC / AnyTLS
inv(ROOT / "hysteria2/hy2.json", base_inv(
    "hy2", "hysteria2", ["hysteria2"], "Hy2", "stable", ["udp", "quic", "tls"],
    demux(["udp"], "quic", True, alpn=["h3"], sni=True),
    scores(7, 9, 5, 6), ["password"],
    {"type": "hysteria2", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "hysteria2", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "password": "{{user.password}}", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "Hysteria2", "Hysteria2 UDP/QUIC + TLS.",
    {"tls_profile": True, "udp": True, "quic": True},
))
inv(ROOT / "tuic/tuic.json", base_inv(
    "tuic", "tuic", ["tuic"], "TUIC", "stable", ["udp", "quic", "tls"],
    demux(["udp"], "quic", True, sni=True),
    scores(7, 8, 5, 6), ["uuid", "password"],
    {"type": "tuic", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}, "congestion_control": "bbr"},
    {"type": "tuic", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "uuid": "{{user.uuid}}", "password": "{{user.password}}", "tls": {"enabled": True, "server_name": "{{server}}"}, "congestion_control": "bbr"},
    "TUIC v5", "TUIC v5 UDP/QUIC + TLS (BBR).",
    {"tls_profile": True, "udp": True, "quic": True},
))
inv(ROOT / "anytls/anytls.json", base_inv(
    "anytls", "anytls", ["anytls"], "AnyTLS", "stable", ["tcp", "tls"],
    demux(["tcp"], "tls_clienthello", True, sni=True),
    scores(6, 6, 6, 6), ["password"],
    {"type": "anytls", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": [], "tls": {"enabled": True, "server_name": "{{server}}"}},
    {"type": "anytls", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "password": "{{user.password}}", "tls": {"enabled": True, "server_name": "{{server}}"}},
    "AnyTLS", "AnyTLS TCP + TLS.",
    {"tls_profile": True},
))

# socks / http
inv(ROOT / "socks/socks.json", base_inv(
    "socks", "socks", ["socks"], "SOCKS5", "stable", ["tcp"],
    demux(["tcp"], "raw_tcp", True, first="SOCKS greeting"),
    scores(1, 8, 7, 9), ["username", "password"],
    {"type": "socks", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": []},
    {"type": "socks", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "username": "{{user.username}}", "password": "{{user.password}}"},
    "SOCKS5", "SOCKS5 с логином — lab / demux plaintext.",
))
inv(ROOT / "http/http.json", base_inv(
    "http", "http", ["http"], "HTTP", "stable", ["tcp"],
    demux(["tcp"], "http", True, first="HTTP CONNECT/methods"),
    scores(1, 8, 7, 9), ["username", "password"],
    {"type": "http", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": []},
    {"type": "http", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "username": "{{user.username}}", "password": "{{user.password}}"},
    "HTTP CONNECT", "HTTP CONNECT прокси с логином.",
))

# SSH lab skeleton (minimal — refine after studying lx SPEC)
inv(ROOT / "ssh/ssh_password.json", base_inv(
    "ssh_password", "ssh", [], "SSH pass", "lab", ["tcp"],
    demux(["tcp"], "ssh_banner", True, first="SSH-2.0 banner"),
    scores(4, 5, 4, 4), ["password"],
    {"type": "ssh", "tag": "{{tag}}", "listen": "{{listen}}", "listen_port": 0, "users": []},
    {"type": "ssh", "tag": "{{tag}}", "server": "{{server}}", "server_port": 0, "user": "{{user.username}}", "password": "{{user.password}}"},
    "SSH password", "Lab-скелет SSH inbound (with_ssh); шаблон уточнять по lx SPEC.",
    {"build_tag": "with_ssh"},
))

(ROOT / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print("wrote catalog under", ROOT)
