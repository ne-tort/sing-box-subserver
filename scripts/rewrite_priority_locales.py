# -*- coding: utf-8 -*-
"""Rewrite priority locale files with concise DPI-oriented copy (ru/en)."""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "internal/controlplane/presets/i18n/locales"


def dump(lang: str, rel: str, data: dict) -> None:
    path = ROOT / lang / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", path.relative_to(ROOT.parent.parent.parent.parent))


def phelp(summary: str, hint: str, fmt: str = "") -> dict:
    out = {
        "help.summary": summary,
        "help.input_hint": hint,
    }
    if fmt:
        out["help.format"] = fmt
    return out


# --- common shared params ---
common_ru = {
    "schema": "controlplane-presets-i18n/v1",
    "note": "Общие строки UI / param.common.*",
    "param.common.ws_path.title": "WS path",
    "param.common.ws_path.description": "HTTP path после TLS для WebSocket.",
    **{f"param.common.ws_path.{k}": v for k, v in phelp(
        "Path после TLS. Совпадайте с CDN/прокси; /ws и /vless-ws — типичные отпечатки.",
        "Путь с /",
        "/cdn-cgi/trace",
    ).items()},
    "param.common.ws_host.title": "WS Host",
    "param.common.ws_host.description": "Host header WebSocket (часто = SNI).",
    **{f"param.common.ws_host.{k}": v for k, v in phelp(
        "Host/:authority должен совпадать с тем, что ждёт фронт/CDN (часто = SNI Reality/TLS).",
        "Хост без схемы",
        "cdn.example.com",
    ).items()},
    "param.common.http_path.title": "HTTP path",
    "param.common.http_path.description": "Path HTTP/2 transport.",
    **{f"param.common.http_path.{k}": v for k, v in phelp(
        "Path H2-транспорта. Меняйте, если фронт режет дефолтные.",
        "Путь с /",
        "/h2",
    ).items()},
    "param.common.http_host.title": "HTTP Host",
    "param.common.http_host.description": "Host header HTTP transport.",
    **{f"param.common.http_host.{k}": v for k, v in phelp(
        "Host для HTTP transport; при Reality materialize часто выравнивает под SNI.",
        "Хост без схемы",
        "www.example.com",
    ).items()},
    "param.common.hu_path.title": "HTTPUpgrade path",
    "param.common.hu_path.description": "Path одного HTTP Upgrade (без WS framing).",
    **{f"param.common.hu_path.{k}": v for k, v in phelp(
        "Один HTTP Upgrade. Меньше framing, чем WS; path всё равно виден после TLS termination.",
        "Путь с /",
        "/upgrade",
    ).items()},
    "param.common.hu_host.title": "HTTPUpgrade Host",
    "param.common.hu_host.description": "Host для HTTPUpgrade.",
    **{f"param.common.hu_host.{k}": v for k, v in phelp(
        "Host HTTPUpgrade; держите согласованным с SNI/фронтом.",
        "Хост без схемы",
        "cdn.example.com",
    ).items()},
    "param.common.service_name.title": "gRPC service",
    "param.common.service_name.description": "Gun service_name.",
    **{f"param.common.service_name.{k}": v for k, v in phelp(
        "gRPC service_name (Gun). «GunService» — дефолт; смените, если фронт фильтрует.",
        "Имя сервиса",
        "GunService",
    ).items()},
    "param.common.grpc_service_name.title": "gRPC service",
    "param.common.grpc_service_name.description": "Gun service_name.",
    **{f"param.common.grpc_service_name.{k}": v for k, v in phelp(
        "gRPC service_name (Gun). «GunService» — дефолт; смените, если фронт фильтрует.",
        "Имя сервиса",
        "GunService",
    ).items()},
    "param.common.up_mbps.title": "Upload Mbps",
    "param.common.up_mbps.description": "Потолок upload (Mbps).",
    **{f"param.common.up_mbps.{k}": v for k, v in phelp(
        "Жёсткий потолок upload. Занижение режет скорость; завышение на слабом канале даёт буферизацию/потери.",
        "Число Mbps",
        "100",
    ).items()},
    "param.common.down_mbps.title": "Download Mbps",
    "param.common.down_mbps.description": "Потолок download (Mbps).",
    **{f"param.common.down_mbps.{k}": v for k, v in phelp(
        "Жёсткий потолок download. Подгоняйте под реальный uplink VPS/клиента.",
        "Число Mbps",
        "100",
    ).items()},
    "param.common.mtu.title": "MTU",
    "param.common.mtu.description": "MTU интерфейса.",
    **{f"param.common.mtu.{k}": v for k, v in phelp(
        "MTU. 1280–1420 типично за CGNAT/мобильными; 1500 часто режется → фрагментация/дропы.",
        "576–65535",
        "1408",
    ).items()},
    "param.common.room.title": "URL комнаты",
    "param.common.room.description": "Ссылка SFU/комнаты для Carrier underlay.",
    **{f"param.common.room.{k}": v for k, v in phelp(
        "Carrier прячет WG-star в легитимный видеозвонок. Без валидной комнаты инбаунд не поднимется.",
        "Полный https URL",
        "https://meet.jit.si/MyRoom",
    ).items()},
    "param.common.token.title": "Токен",
    "param.common.token.description": "Токен туннеля/провайдера.",
    **{f"param.common.token.{k}": v for k, v in phelp(
        "Opaque-токен из кабинета. Утечка = контроль над туннелем.",
        "Строка токена",
        "eyJ...",
    ).items()},
    "param.common.sni.title": "SNI / ACME",
    "param.common.sni.description": "Домен cert-manager / TLS server_name.",
    **{f"param.common.sni.{k}": v for k, v in phelp(
        "SNI для классического TLS (не Reality). Должен быть в ACME на агенте; синхронизирует demux_sni.",
        "Домен из cert-manager",
        "vpn.example.com",
    ).items()},
    "param.common.alpn.title": "ALPN",
    "param.common.alpn.description": "Список ALPN через запятую.",
    **{f"param.common.alpn.{k}": v for k, v in phelp(
        "ALPN ClientHello. h2+http/1.1 — веб-стек; h3 — QUIC. Расхождение с фронтом = фейл рукопожатия.",
        "Через запятую",
        "h2,http/1.1",
    ).items()},
    "param.common.fingerprint.title": "uTLS fingerprint",
    "param.common.fingerprint.description": "Отпечаток ClientHello (uTLS).",
    **{f"param.common.fingerprint.{k}": v for k, v in phelp(
        "Имитация браузерного ClientHello. Не для QUIC/hysteria transport. chrome — безопасный дефолт.",
        "chrome|firefox|safari|ios|android|edge|qq|random|randomized",
        "chrome",
    ).items()},
    "param.common.flow.title": "Flow",
    "param.common.flow.description": "XTLS Vision flow.",
    **{f"param.common.flow.{k}": v for k, v in phelp(
        "Vision маскирует length pattern на TCP+TLS/Reality. Несовместим с WS/gRPC/HTTP/HUp. Пусто = обычный VLESS.",
        "пусто или xtls-rprx-vision",
        "xtls-rprx-vision",
    ).items()},
    "param.common.packet_encoding.title": "Packet encoding",
    "param.common.packet_encoding.description": "Кодирование UDP в VLESS outbound.",
    **{f"param.common.packet_encoding.{k}": v for k, v in phelp(
        "xudp — современный full-cone; packetaddr — legacy; пусто — без encoding. На DPI почти не влияет.",
        "xudp|packetaddr|пусто",
        "xudp",
    ).items()},
}

common_en = {
    "schema": "controlplane-presets-i18n/v1",
    "note": "Shared UI / param.common.* strings",
    "param.common.ws_path.title": "WS path",
    "param.common.ws_path.description": "HTTP path after TLS for WebSocket.",
    **{f"param.common.ws_path.{k}": v for k, v in phelp(
        "Path after TLS. Match CDN/proxy rules; /ws and /vless-ws are easy DPI fingerprints.",
        "Path starting with /",
        "/cdn-cgi/trace",
    ).items()},
    "param.common.ws_host.title": "WS Host",
    "param.common.ws_host.description": "WebSocket Host header (often = SNI).",
    **{f"param.common.ws_host.{k}": v for k, v in phelp(
        "Host/:authority must match the front/CDN (often = Reality/TLS SNI).",
        "Hostname without scheme",
        "cdn.example.com",
    ).items()},
    "param.common.http_path.title": "HTTP path",
    "param.common.http_path.description": "HTTP/2 transport path.",
    **{f"param.common.http_path.{k}": v for k, v in phelp(
        "H2 transport path. Change if the front blocks stock defaults.",
        "Path starting with /",
        "/h2",
    ).items()},
    "param.common.http_host.title": "HTTP Host",
    "param.common.http_host.description": "HTTP transport Host header.",
    **{f"param.common.http_host.{k}": v for k, v in phelp(
        "Host for HTTP transport; Reality materialize often aligns it to SNI.",
        "Hostname without scheme",
        "www.example.com",
    ).items()},
    "param.common.hu_path.title": "HTTPUpgrade path",
    "param.common.hu_path.description": "Single HTTP Upgrade path (no WS framing).",
    **{f"param.common.hu_path.{k}": v for k, v in phelp(
        "One HTTP Upgrade. Less framing than WS; path still visible after TLS termination.",
        "Path starting with /",
        "/upgrade",
    ).items()},
    "param.common.hu_host.title": "HTTPUpgrade Host",
    "param.common.hu_host.description": "Host for HTTPUpgrade.",
    **{f"param.common.hu_host.{k}": v for k, v in phelp(
        "HTTPUpgrade Host; keep aligned with SNI/front.",
        "Hostname without scheme",
        "cdn.example.com",
    ).items()},
    "param.common.service_name.title": "gRPC service",
    "param.common.service_name.description": "Gun service_name.",
    **{f"param.common.service_name.{k}": v for k, v in phelp(
        "gRPC Gun service_name. GunService is the stock default — change if filtered.",
        "Service name",
        "GunService",
    ).items()},
    "param.common.grpc_service_name.title": "gRPC service",
    "param.common.grpc_service_name.description": "Gun service_name.",
    **{f"param.common.grpc_service_name.{k}": v for k, v in phelp(
        "gRPC Gun service_name. GunService is the stock default — change if filtered.",
        "Service name",
        "GunService",
    ).items()},
    "param.common.up_mbps.title": "Upload Mbps",
    "param.common.up_mbps.description": "Upload cap (Mbps).",
    **{f"param.common.up_mbps.{k}": v for k, v in phelp(
        "Hard upload cap. Too low kills speed; too high on a weak link causes buffering/loss.",
        "Mbps number",
        "100",
    ).items()},
    "param.common.down_mbps.title": "Download Mbps",
    "param.common.down_mbps.description": "Download cap (Mbps).",
    **{f"param.common.down_mbps.{k}": v for k, v in phelp(
        "Hard download cap. Match real VPS/client capacity.",
        "Mbps number",
        "100",
    ).items()},
    "param.common.mtu.title": "MTU",
    "param.common.mtu.description": "Interface MTU.",
    **{f"param.common.mtu.{k}": v for k, v in phelp(
        "MTU. 1280–1420 is typical behind CGNAT/mobile; 1500 often gets clamped → drops.",
        "576–65535",
        "1408",
    ).items()},
    "param.common.room.title": "Room URL",
    "param.common.room.description": "SFU/room link for Carrier underlay.",
    **{f"param.common.room.{k}": v for k, v in phelp(
        "Carrier hides WG-star inside a real video call. Invalid room → inbound will not start.",
        "Full https URL",
        "https://meet.jit.si/MyRoom",
    ).items()},
    "param.common.token.title": "Token",
    "param.common.token.description": "Tunnel/provider token.",
    **{f"param.common.token.{k}": v for k, v in phelp(
        "Opaque token from the dashboard. Leak = tunnel takeover.",
        "Token string",
        "eyJ...",
    ).items()},
    "param.common.sni.title": "SNI / ACME",
    "param.common.sni.description": "cert-manager domain / TLS server_name.",
    **{f"param.common.sni.{k}": v for k, v in phelp(
        "SNI for classic TLS (not Reality). Must exist in ACME on the agent; syncs demux_sni.",
        "Domain from cert-manager",
        "vpn.example.com",
    ).items()},
    "param.common.alpn.title": "ALPN",
    "param.common.alpn.description": "Comma-separated ALPN list.",
    **{f"param.common.alpn.{k}": v for k, v in phelp(
        "ClientHello ALPN. h2+http/1.1 = web stack; h3 = QUIC. Mismatch with front → handshake fail.",
        "Comma-separated",
        "h2,http/1.1",
    ).items()},
    "param.common.fingerprint.title": "uTLS fingerprint",
    "param.common.fingerprint.description": "ClientHello fingerprint (uTLS).",
    **{f"param.common.fingerprint.{k}": v for k, v in phelp(
        "Browser ClientHello mimic. Not for QUIC/hysteria transport. chrome is the safe default.",
        "chrome|firefox|safari|ios|android|edge|qq|random|randomized",
        "chrome",
    ).items()},
    "param.common.flow.title": "Flow",
    "param.common.flow.description": "XTLS Vision flow.",
    **{f"param.common.flow.{k}": v for k, v in phelp(
        "Vision masks length patterns on TCP+TLS/Reality. Incompatible with WS/gRPC/HTTP/HUp. Empty = plain VLESS.",
        "empty or xtls-rprx-vision",
        "xtls-rprx-vision",
    ).items()},
    "param.common.packet_encoding.title": "Packet encoding",
    "param.common.packet_encoding.description": "UDP encoding for VLESS outbound.",
    **{f"param.common.packet_encoding.{k}": v for k, v in phelp(
        "xudp = modern full-cone; packetaddr = legacy; empty = none. Almost no DPI impact.",
        "xudp|packetaddr|empty",
        "xudp",
    ).items()},
}

protocols_ru = {
    "protocol.vless.title": "VLESS",
    "protocol.vless.description": "Лёгкий UUID-транспорт. С Reality — эталон обхода DPI/ТСПУ на TCP:443. Транспорты WS/gRPC/HTTP меняют post-TLS картину; Vision — только TCP.",
    "protocol.hysteria2.title": "Hysteria2",
    "protocol.hysteria2.description": "QUIC/H3 на UDP:443. Быстрый на lossy/мобильных. Salamander/gecko меняют first-bytes (ломает demux по quic). Masquerade — HTTP-обман при неверном auth.",
    "protocol.wireguard.title": "WireGuard",
    "protocol.wireguard.description": "UDP endpoint (singleton hub). Максимальная скорость, слабый DPI-профиль. AWG2/3 + masquerade усложняют сигнатуру WG.",
    "protocol.trojan.title": "Trojan",
    "protocol.trojan.description": "Пароль поверх TLS. Выглядит как HTTPS; без Reality слабее VLESS+Reality против активного probing.",
    "protocol.anytls.title": "AnyTLS",
    "protocol.anytls.description": "TLS-обёртка с паролем. Удобный TCP TLS-слот в demux рядом с Reality/QUIC.",
    "protocol.trusttunnel.title": "TrustTunnel",
    "protocol.trusttunnel.description": "Anti-DPI туннель H2/H3. h2 — TCP TLS-слот; h3 — QUIC. Lab→usable в demux substitutes.",
    "protocol.shadowquic.title": "ShadowQUIC",
    "protocol.shadowquic.description": "QUIC + JLS SNI-камуфляж. Альтернатива Hy2/TUIC на UDP; JLS SNI должен совпадать с demux_sni.",
    "protocol.sudoku.title": "Sudoku",
    "protocol.sudoku.description": "Plain-TCP обфускация (pad/httpmask/aes). Demux always_plain рядом с TLS-стеком.",
    "protocol.mieru.title": "Mieru",
    "protocol.mieru.description": "Чистый TCP/UDP без TLS. Ключ к demux «plain рядом с Reality/Hy2» — first-bytes / always_plain.",
    "protocol.carrier.title": "Carrier",
    "protocol.carrier.description": "WG-star в underlay легитимного SFU (Jitsi/Telemost/…). Нужен room URL. Не demux-priority.",
    "protocol.tuic.title": "TUIC",
    "protocol.tuic.description": "QUIC/TLS v5. Замена Hy2 в UDP-слоте demux; 0-RTT — отдельный пресет.",
    "protocol.vmess.title": "VMess",
    "protocol.vmess.description": "UUID-транспорт VMess. Покрытие разнообразия; не приоритет demux.",
    "protocol.shadowsocks.title": "Shadowsocks",
    "protocol.shadowsocks.description": "AEAD/2022 без TLS. Классика; легко классифицируется на DPI без обёрток.",
    "protocol.shadowtls.title": "ShadowTLS",
    "protocol.shadowtls.description": "TLS-mimic handshake + password. Нужен handshake_server под правдоподобный SNI.",
    "protocol.naive.title": "NaiveProxy",
    "protocol.naive.description": "Трафик как обычный HTTPS/H2 или QUIC. Сильная маскировка; тяжёлый стек.",
    "protocol.snell.title": "Snell",
    "protocol.snell.description": "PSK plain TCP/UDP. Demux plain-слот; слабый DPI-профиль без TLS.",
    "protocol.ssh.title": "SSH",
    "protocol.ssh.description": "SSH-banner inbound. Lab/plain demux; выглядит как SSH, не как веб.",
    "protocol.derp.title": "DERP",
    "protocol.derp.description": "Tailscale-compatible DERP. Нишевый UDP/TLS путь, не для общего обхода.",
    "protocol.http.title": "HTTP",
    "protocol.http.description": "HTTP CONNECT. Служебный прокси, не anti-DPI.",
    "protocol.socks.title": "SOCKS5",
    "protocol.socks.description": "Классический SOCKS5. Служебный; часто цель demux inject.",
    "protocol.mixed.title": "Mixed",
    "protocol.mixed.description": "HTTP CONNECT + SOCKS на одном порту. Служебный локальный вход.",
    "protocol.hysteria.title": "Hysteria v1",
    "protocol.hysteria.description": "Legacy QUIC. Берите Hysteria2, если нет жёсткой совместимости.",
    "protocol.cloudflared.title": "Cloudflared",
    "protocol.cloudflared.description": "Cloudflare Tunnel по token. Обход блокировок IP ценой зависимости от CF.",
}

protocols_en = {
    "protocol.vless.title": "VLESS",
    "protocol.vless.description": "Light UUID transport. With Reality — reference DPI bypass on TCP:443. WS/gRPC/HTTP change post-TLS shape; Vision is TCP-only.",
    "protocol.hysteria2.title": "Hysteria2",
    "protocol.hysteria2.description": "QUIC/H3 on UDP:443. Fast on lossy/mobile. Salamander/gecko alter first-bytes (breaks demux quic match). Masquerade = HTTP decoy on bad auth.",
    "protocol.wireguard.title": "WireGuard",
    "protocol.wireguard.description": "UDP endpoint (singleton hub). Top speed, weak DPI profile. AWG2/3 + masquerade harden the WG signature.",
    "protocol.trojan.title": "Trojan",
    "protocol.trojan.description": "Password over TLS. Looks like HTTPS; without Reality weaker than VLESS+Reality vs active probing.",
    "protocol.anytls.title": "AnyTLS",
    "protocol.anytls.description": "Password TLS wrapper. Handy TCP TLS demux slot next to Reality/QUIC.",
    "protocol.trusttunnel.title": "TrustTunnel",
    "protocol.trusttunnel.description": "Anti-DPI H2/H3 tunnel. h2 = TCP TLS slot; h3 = QUIC. Lab→usable in demux substitutes.",
    "protocol.shadowquic.title": "ShadowQUIC",
    "protocol.shadowquic.description": "QUIC + JLS SNI camouflage. Hy2/TUIC alternative on UDP; JLS SNI must match demux_sni.",
    "protocol.sudoku.title": "Sudoku",
    "protocol.sudoku.description": "Plain-TCP obfuscation (pad/httpmask/aes). Demux always_plain beside the TLS stack.",
    "protocol.mieru.title": "Mieru",
    "protocol.mieru.description": "Clean TCP/UDP without TLS. Key to demux «plain next to Reality/Hy2» — first-bytes / always_plain.",
    "protocol.carrier.title": "Carrier",
    "protocol.carrier.description": "WG-star inside a real SFU call (Jitsi/Telemost/…). Needs room URL. Not demux-priority.",
    "protocol.tuic.title": "TUIC",
    "protocol.tuic.description": "QUIC/TLS v5. Hy2 substitute in demux UDP slot; 0-RTT is a separate preset.",
    "protocol.vmess.title": "VMess",
    "protocol.vmess.description": "UUID VMess transport. Diversity coverage; not a demux priority.",
    "protocol.shadowsocks.title": "Shadowsocks",
    "protocol.shadowsocks.description": "AEAD/2022 without TLS. Classic; easy DPI class without wrappers.",
    "protocol.shadowtls.title": "ShadowTLS",
    "protocol.shadowtls.description": "TLS-mimic handshake + password. Needs a plausible handshake_server SNI.",
    "protocol.naive.title": "NaiveProxy",
    "protocol.naive.description": "Looks like normal HTTPS/H2 or QUIC. Strong camouflage; heavy stack.",
    "protocol.snell.title": "Snell",
    "protocol.snell.description": "PSK plain TCP/UDP. Demux plain slot; weak DPI profile without TLS.",
    "protocol.ssh.title": "SSH",
    "protocol.ssh.description": "SSH-banner inbound. Lab/plain demux; looks like SSH, not web.",
    "protocol.derp.title": "DERP",
    "protocol.derp.description": "Tailscale-compatible DERP. Niche UDP/TLS path, not general bypass.",
    "protocol.http.title": "HTTP",
    "protocol.http.description": "HTTP CONNECT. Utility proxy, not anti-DPI.",
    "protocol.socks.title": "SOCKS5",
    "protocol.socks.description": "Classic SOCKS5. Utility; often a demux inject target.",
    "protocol.mixed.title": "Mixed",
    "protocol.mixed.description": "HTTP CONNECT + SOCKS on one port. Local utility inbound.",
    "protocol.hysteria.title": "Hysteria v1",
    "protocol.hysteria.description": "Legacy QUIC. Prefer Hysteria2 unless you need hard compatibility.",
    "protocol.cloudflared.title": "Cloudflared",
    "protocol.cloudflared.description": "Cloudflare Tunnel via token. IP-block bypass at the cost of CF dependency.",
}


def preset(tag: str, title: str, desc: str) -> dict:
    return {f"preset.{tag}.title": title, f"preset.{tag}.description": desc}


def param(tag: str, field: str, title: str, desc: str, summary: str, hint: str, fmt: str = "") -> dict:
    out = {
        f"param.{tag}.{field}.title": title,
        f"param.{tag}.{field}.description": desc,
        f"param.{tag}.{field}.help.summary": summary,
        f"param.{tag}.{field}.help.input_hint": hint,
    }
    if fmt:
        out[f"param.{tag}.{field}.help.format"] = fmt
    return out


vless_ru: dict = {}
vless_ru.update(preset("vless_reality", "VLESS Reality",
    "Эталон TCP:443: VLESS + Reality. ClientHello как у чужого сайта из SNI-пула — сильный ответ на DPI/ТСПУ и active probe."))
vless_ru.update(preset("vless_tls", "VLESS TLS",
    "VLESS + обычный TLS (ACME/self-signed). Проще Reality, слабее против probing и SNI-блокировок."))
vless_ru.update(preset("vless_tcp", "VLESS TCP",
    "VLESS без TLS. Только LAN/lab; на публичном DPI мгновенно виден."))
vless_ru.update(preset("vless_tls_mux", "VLESS TLS + mux",
    "VLESS+TLS с smux+padding: меньше TCP handshake, другой timing-pattern. Не замена Reality."))
vless_ru.update(preset("vless_ws_tls", "VLESS WS TLS",
    "После TLS — WebSocket upgrade. Удобно за CDN/nginx; path/Host — post-TLS отпечаток."))
vless_ru.update(preset("vless_ws_reality", "VLESS WS Reality",
    "Reality снаружи + WS внутри. Fingerprint Reality, path всё равно нужен фронту/CDN."))
vless_ru.update(preset("vless_grpc_tls", "VLESS gRPC TLS",
    "TLS + gRPC/H2 (ALPN h2). Хороший TLS-слот demux с отдельным SNI/.local."))
vless_ru.update(preset("vless_grpc_reality", "VLESS gRPC Reality",
    "Reality + gRPC. H2-картина после Reality; service_name — настраиваемый маркер."))
vless_ru.update(preset("vless_http_tls", "VLESS HTTP TLS",
    "TLS + HTTP/2 transport. Post-TLS как H2; host/path должны быть правдоподобны."))
vless_ru.update(preset("vless_http_reality", "VLESS HTTP Reality",
    "Reality + HTTP transport. Компромисс: Reality снаружи, H2-path внутри."))
vless_ru.update(preset("vless_httpupgrade_tls", "VLESS HTTPUpgrade TLS",
    "TLS + один HTTP Upgrade (без WS framing). Легче WS, path всё равно виден прокси."))
vless_ru.update(preset("vless_httpupgrade_reality", "VLESS HTTPUpgrade Reality",
    "Reality + HTTPUpgrade. То же, что HUp TLS, но с Reality fingerprint."))
vless_ru.update(preset("vless_quic_tls", "VLESS QUIC TLS",
    "VLESS + transport=quic (UDP). Не путать с Hy2; отдельный QUIC-стек V2Ray."))
vless_ru.update(preset("vless_hysteria_tls", "VLESS Hysteria TLS",
    "VLESS + transport=hysteria (HY auth поверх QUIC/H3). Не type:hysteria2."))
vless_ru.update(preset("vless_custom", "VLESS конструктор",
    "Собрать нестандартный VLESS: transport × tls/reality/plain + flow/path/ALPN. Lab; проверяйте совместимость Vision↔transport."))

for tag in ("vless_ws_tls", "vless_ws_reality"):
    vless_ru.update(param(tag, "ws_path", "WS path", "HTTP path WebSocket.",
        "Path после TLS. Стоковые /ws,/vless-ws — лёгкий fingerprint.", "Путь с /", "/cdn-cgi/trace"))
    vless_ru.update(param(tag, "ws_host", "WS Host", "Host header WebSocket.",
        "Должен совпадать с фронтом/SNI (Reality выравнивает при materialize).", "Хост", "cdn.example.com"))
for tag in ("vless_http_tls", "vless_http_reality"):
    vless_ru.update(param(tag, "http_path", "HTTP path", "Path HTTP transport.",
        "H2 path; меняйте под фронт.", "Путь с /", "/h2"))
    vless_ru.update(param(tag, "http_host", "HTTP Host", "Host HTTP transport.",
        "Host для H2; часто = SNI.", "Хост", "www.example.com"))
for tag in ("vless_httpupgrade_tls", "vless_httpupgrade_reality"):
    vless_ru.update(param(tag, "hu_path", "HTTPUpgrade path", "Path Upgrade.",
        "Один Upgrade; path виден после TLS termination.", "Путь с /", "/upgrade"))
    vless_ru.update(param(tag, "hu_host", "HTTPUpgrade Host", "Host Upgrade.",
        "Согласуйте с SNI/фронтом.", "Хост", "cdn.example.com"))

vless_ru.update(param("vless_custom", "transport", "Транспорт", "transport.type.",
    "tcp — Vision/Reality; ws/grpc/http/httpupgrade — CDN/H2-картина; quic/hysteria — UDP.", "tcp|ws|grpc|http|httpupgrade|quic|hysteria", "tcp"))
vless_ru.update(param("vless_custom", "tls_mode", "Режим TLS", "none|tls|reality.",
    "reality — лучший DPI на TCP:443; tls — ACME/self-signed; none — только lab/LAN.", "none|tls|reality", "reality"))
vless_ru.update(param("vless_custom", "flow", "Flow", "XTLS Vision.",
    "Только tcp + tls/reality. Ломает WS/gRPC/HTTP/HUp.", "пусто или xtls-rprx-vision", ""))
vless_ru.update(param("vless_custom", "packet_encoding", "Packet encoding", "UDP encoding outbound.",
    "xudp по умолчанию. На обход DPI почти не влияет.", "xudp|packetaddr|пусто", "xudp"))
vless_ru.update(param("vless_custom", "fingerprint", "uTLS fingerprint", "ClientHello uTLS.",
    "chrome — дефолт. Не для quic/hysteria transport.", "chrome|firefox|…", "chrome"))
vless_ru.update(param("vless_custom", "transport_path", "Path", "Path WS/HTTP/HUp.",
    "Post-TLS path. Избегайте стоковых /ws,/vless.", "Путь с /", "/api/connect"))
vless_ru.update(param("vless_custom", "transport_host", "Host", "Host WS/HTTP/HUp.",
    "Часто = SNI/фронт.", "Хост", "cdn.example.com"))
vless_ru.update(param("vless_custom", "service_name", "gRPC service", "Gun service_name.",
    "Маркер gRPC. Смените GunService, если фильтруют.", "Имя", "GunService"))
vless_ru.update(param("vless_custom", "alpn", "ALPN", "ALPN через запятую.",
    "h2,http/1.1 для TCP TLS; h3 для QUIC-транспортов.", "Список", "h2,http/1.1"))

# English vless (real texts, not placeholders)
vless_en: dict = {}
vless_en.update(preset("vless_reality", "VLESS Reality",
    "Reference TCP:443: VLESS + Reality. ClientHello mimics a pooled site — strong vs DPI and active probe."))
vless_en.update(preset("vless_tls", "VLESS TLS",
    "VLESS + classic TLS (ACME/self-signed). Simpler than Reality; weaker vs probing and SNI blocks."))
vless_en.update(preset("vless_tcp", "VLESS TCP",
    "VLESS without TLS. LAN/lab only — trivial on public DPI."))
vless_en.update(preset("vless_tls_mux", "VLESS TLS + mux",
    "VLESS+TLS with smux+padding: fewer handshakes, different timing. Not a Reality replacement."))
vless_en.update(preset("vless_ws_tls", "VLESS WS TLS",
    "WebSocket upgrade after TLS. Fits CDN/nginx; path/Host are post-TLS fingerprints."))
vless_en.update(preset("vless_ws_reality", "VLESS WS Reality",
    "Reality outside + WS inside. Reality fingerprint; path still required by front/CDN."))
vless_en.update(preset("vless_grpc_tls", "VLESS gRPC TLS",
    "TLS + gRPC/H2 (ALPN h2). Good demux TLS slot with its own SNI/.local."))
vless_en.update(preset("vless_grpc_reality", "VLESS gRPC Reality",
    "Reality + gRPC. H2 shape after Reality; service_name is a tunable marker."))
vless_en.update(preset("vless_http_tls", "VLESS HTTP TLS",
    "TLS + HTTP/2 transport. Post-TLS looks like H2; keep host/path plausible."))
vless_en.update(preset("vless_http_reality", "VLESS HTTP Reality",
    "Reality + HTTP transport. Reality outside, H2 path inside."))
vless_en.update(preset("vless_httpupgrade_tls", "VLESS HTTPUpgrade TLS",
    "TLS + single HTTP Upgrade (no WS framing). Lighter than WS; path still visible to proxies."))
vless_en.update(preset("vless_httpupgrade_reality", "VLESS HTTPUpgrade Reality",
    "Reality + HTTPUpgrade. Same as HUp TLS with Reality fingerprint."))
vless_en.update(preset("vless_quic_tls", "VLESS QUIC TLS",
    "VLESS + transport=quic (UDP). Not Hy2 — V2Ray QUIC stack."))
vless_en.update(preset("vless_hysteria_tls", "VLESS Hysteria TLS",
    "VLESS + transport=hysteria (HY auth over QUIC/H3). Not type:hysteria2."))
vless_en.update(preset("vless_custom", "VLESS constructor",
    "Build non-stock VLESS: transport × tls/reality/plain + flow/path/ALPN. Lab; check Vision↔transport compatibility."))

for tag in ("vless_ws_tls", "vless_ws_reality"):
    vless_en.update(param(tag, "ws_path", "WS path", "WebSocket HTTP path.",
        "Path after TLS. Stock /ws,/vless-ws are easy fingerprints.", "Path with /", "/cdn-cgi/trace"))
    vless_en.update(param(tag, "ws_host", "WS Host", "WebSocket Host header.",
        "Must match front/SNI (Reality materialize often aligns).", "Hostname", "cdn.example.com"))
for tag in ("vless_http_tls", "vless_http_reality"):
    vless_en.update(param(tag, "http_path", "HTTP path", "HTTP transport path.",
        "H2 path; tune for your front.", "Path with /", "/h2"))
    vless_en.update(param(tag, "http_host", "HTTP Host", "HTTP transport Host.",
        "H2 Host; often = SNI.", "Hostname", "www.example.com"))
for tag in ("vless_httpupgrade_tls", "vless_httpupgrade_reality"):
    vless_en.update(param(tag, "hu_path", "HTTPUpgrade path", "Upgrade path.",
        "Single Upgrade; path visible after TLS termination.", "Path with /", "/upgrade"))
    vless_en.update(param(tag, "hu_host", "HTTPUpgrade Host", "Upgrade Host.",
        "Align with SNI/front.", "Hostname", "cdn.example.com"))

vless_en.update(param("vless_custom", "transport", "Transport", "transport.type.",
    "tcp for Vision/Reality; ws/grpc/http/httpupgrade for CDN/H2; quic/hysteria for UDP.", "tcp|ws|grpc|http|httpupgrade|quic|hysteria", "tcp"))
vless_en.update(param("vless_custom", "tls_mode", "TLS mode", "none|tls|reality.",
    "reality = best TCP:443 DPI; tls = ACME/self-signed; none = lab/LAN only.", "none|tls|reality", "reality"))
vless_en.update(param("vless_custom", "flow", "Flow", "XTLS Vision.",
    "Only tcp + tls/reality. Breaks WS/gRPC/HTTP/HUp.", "empty or xtls-rprx-vision", ""))
vless_en.update(param("vless_custom", "packet_encoding", "Packet encoding", "Outbound UDP encoding.",
    "xudp default. Almost no DPI impact.", "xudp|packetaddr|empty", "xudp"))
vless_en.update(param("vless_custom", "fingerprint", "uTLS fingerprint", "ClientHello uTLS.",
    "chrome default. Not for quic/hysteria transport.", "chrome|firefox|…", "chrome"))
vless_en.update(param("vless_custom", "transport_path", "Path", "WS/HTTP/HUp path.",
    "Post-TLS path. Avoid stock /ws,/vless.", "Path with /", "/api/connect"))
vless_en.update(param("vless_custom", "transport_host", "Host", "WS/HTTP/HUp Host.",
    "Often = SNI/front.", "Hostname", "cdn.example.com"))
vless_en.update(param("vless_custom", "service_name", "gRPC service", "Gun service_name.",
    "gRPC marker. Change GunService if filtered.", "Name", "GunService"))
vless_en.update(param("vless_custom", "alpn", "ALPN", "Comma-separated ALPN.",
    "h2,http/1.1 for TCP TLS; h3 for QUIC transports.", "List", "h2,http/1.1"))


hy2_ru: dict = {}
hy2_ru.update(preset("hy2", "Hysteria2",
    "Базовый Hy2 QUIC/H3 + TLS. Главный UDP:443 рядом с Reality. Без obfs — demux может матчить protocol=quic."))
hy2_ru.update(preset("hy2_salamander", "Hy2 Salamander",
    "Hy2 + salamander obfs: first-bytes больше не чистый QUIC → demux по quic/SNI обычно не работает."))
hy2_ru.update(preset("hy2_gecko", "Hy2 Gecko",
    "Hy2 + gecko obfs (вариативный размер). Сильнее рандом DPI, хуже для demux-классификации."))
hy2_ru.update(preset("hy2_gecko_compact", "Hy2 Gecko compact",
    "Gecko с узким окном пакетов: меньше overhead, слабее рандом."))
hy2_ru.update(preset("hy2_masquerade", "Hy2 Masquerade",
    "При неверном auth отвечает как HTTP-сайт. Не маскирует QUIC first-bytes для demux."))
hy2_ru.update(preset("hy2_masquerade_file", "Hy2 Masquerade file",
    "Masquerade из каталога на диске. Нужен masquerade_dir."))
hy2_ru.update(preset("hy2_gecko_masquerade", "Hy2 Gecko + Masquerade",
    "Gecko obfs + HTTP decoy на fail auth."))
hy2_ru.update(preset("hy2_realm", "Hy2 Realm",
    "Hy2 через realm control-plane. Нужны realm_server_url + realm_id."))
hy2_ru.update(preset("hy2_custom", "Hy2 конструктор",
    "Obfs × bandwidth × masquerade. Salamander ломает demux quic-match — учитывайте при стеке на :443."))

for tag in ("hy2", "hy2_gecko", "hy2_gecko_compact", "hy2_gecko_masquerade", "hy2_masquerade", "hy2_salamander"):
    hy2_ru.update(param(tag, "up_mbps", "Upload Mbps", "Потолок upload.",
        "Жёсткий потолок upload под канал.", "Mbps", "100"))
    hy2_ru.update(param(tag, "down_mbps", "Download Mbps", "Потолок download.",
        "Жёсткий потолок download под канал.", "Mbps", "100"))
hy2_ru.update(param("hy2_masquerade_file", "masquerade_dir", "Каталог masquerade", "Корень статики.",
    "Каталог, отдаваемый при неверном auth.", "Абсолютный путь", "/var/www/html"))
hy2_ru.update(param("hy2_realm", "realm_server_url", "Realm URL", "URL realm control.",
    "HTTPS control URL оператора realm.", "https URL", "https://realm.example.com"))
hy2_ru.update(param("hy2_realm", "realm_id", "Realm ID", "Идентификатор realm.",
    "ID у оператора realm.", "Opaque id", "my-realm"))
hy2_ru.update(param("hy2_custom", "obfs_type", "Обфускация", "none|salamander.",
    "none — demux-friendly QUIC; salamander — сильнее DPI, ломает demux quic/SNI.", "none|salamander", "none"))
hy2_ru.update(param("hy2_custom", "up_mbps", "Upload Mbps", "Потолок upload.",
    "Потолок upload.", "Mbps", "100"))
hy2_ru.update(param("hy2_custom", "down_mbps", "Download Mbps", "Потолок download.",
    "Потолок download.", "Mbps", "100"))
hy2_ru.update(param("hy2_custom", "ignore_client_bandwidth", "Игнор bandwidth клиента", "ignore_client_bandwidth.",
    "Сервер игнорирует клиентские up/down — стабильнее на кривых клиентах.", "true|false", "true"))
hy2_ru.update(param("hy2_custom", "masquerade_mode", "Masquerade", "Режим decoy.",
    "HTTP-ответ при неверном auth. Не прячет QUIC first-bytes.", "none|proxy|file|string", "none"))
hy2_ru.update(param("hy2_custom", "masquerade_dir", "Каталог", "File masquerade root.",
    "Нужен при masquerade_mode=file.", "Путь", "/var/www/html"))
hy2_ru.update(param("hy2_custom", "masquerade_url", "Proxy URL", "Upstream для proxy masquerade.",
    "Куда проксировать fail-auth HTTP.", "https URL", "https://www.cloudflare.com"))

hy2_en: dict = {}
hy2_en.update(preset("hy2", "Hysteria2",
    "Base Hy2 QUIC/H3 + TLS. Primary UDP:443 next to Reality. No obfs → demux can match protocol=quic."))
hy2_en.update(preset("hy2_salamander", "Hy2 Salamander",
    "Hy2 + salamander obfs: first-bytes no longer clean QUIC → demux quic/SNI usually fails."))
hy2_en.update(preset("hy2_gecko", "Hy2 Gecko",
    "Hy2 + gecko obfs (variable size). Stronger DPI randomness, worse for demux classification."))
hy2_en.update(preset("hy2_gecko_compact", "Hy2 Gecko compact",
    "Gecko with a narrow packet window: less overhead, weaker randomness."))
hy2_en.update(preset("hy2_masquerade", "Hy2 Masquerade",
    "Bad auth gets an HTTP site response. Does not hide QUIC first-bytes for demux."))
hy2_en.update(preset("hy2_masquerade_file", "Hy2 Masquerade file",
    "Masquerade from an on-disk directory. Needs masquerade_dir."))
hy2_en.update(preset("hy2_gecko_masquerade", "Hy2 Gecko + Masquerade",
    "Gecko obfs + HTTP decoy on failed auth."))
hy2_en.update(preset("hy2_realm", "Hy2 Realm",
    "Hy2 via realm control-plane. Needs realm_server_url + realm_id."))
hy2_en.update(preset("hy2_custom", "Hy2 constructor",
    "Obfs × bandwidth × masquerade. Salamander breaks demux quic-match — mind :443 stacks."))

for tag in ("hy2", "hy2_gecko", "hy2_gecko_compact", "hy2_gecko_masquerade", "hy2_masquerade", "hy2_salamander"):
    hy2_en.update(param(tag, "up_mbps", "Upload Mbps", "Upload cap.",
        "Hard upload cap for the link.", "Mbps", "100"))
    hy2_en.update(param(tag, "down_mbps", "Download Mbps", "Download cap.",
        "Hard download cap for the link.", "Mbps", "100"))
hy2_en.update(param("hy2_masquerade_file", "masquerade_dir", "Masquerade dir", "Static root.",
    "Directory served on bad auth.", "Absolute path", "/var/www/html"))
hy2_en.update(param("hy2_realm", "realm_server_url", "Realm URL", "Realm control URL.",
    "Operator HTTPS control URL.", "https URL", "https://realm.example.com"))
hy2_en.update(param("hy2_realm", "realm_id", "Realm ID", "Realm identifier.",
    "ID from the realm operator.", "Opaque id", "my-realm"))
hy2_en.update(param("hy2_custom", "obfs_type", "Obfuscation", "none|salamander.",
    "none = demux-friendly QUIC; salamander = stronger DPI, breaks demux quic/SNI.", "none|salamander", "none"))
hy2_en.update(param("hy2_custom", "up_mbps", "Upload Mbps", "Upload cap.", "Upload cap.", "Mbps", "100"))
hy2_en.update(param("hy2_custom", "down_mbps", "Download Mbps", "Download cap.", "Download cap.", "Mbps", "100"))
hy2_en.update(param("hy2_custom", "ignore_client_bandwidth", "Ignore client bandwidth", "ignore_client_bandwidth.",
    "Server ignores client up/down — stabler with odd clients.", "true|false", "true"))
hy2_en.update(param("hy2_custom", "masquerade_mode", "Masquerade", "Decoy mode.",
    "HTTP response on bad auth. Does not hide QUIC first-bytes.", "none|proxy|file|string", "none"))
hy2_en.update(param("hy2_custom", "masquerade_dir", "Directory", "File masquerade root.",
    "Required when masquerade_mode=file.", "Path", "/var/www/html"))
hy2_en.update(param("hy2_custom", "masquerade_url", "Proxy URL", "Upstream for proxy masquerade.",
    "Where to proxy fail-auth HTTP.", "https URL", "https://www.cloudflare.com"))


wg_ru: dict = {}
wg_ru.update(preset("wg", "WireGuard",
    "Чистый WG endpoint. Максимальная скорость; DPI легко видит WG. Не demux."))
wg_ru.update(preset("wg_awg2", "AmneziaWG 2",
    "AWG2 junk (jc/jmin/jmax) + masquerade. Ломает наивные сигнатуры WG ценой overhead."))
wg_ru.update(preset("wg_awg3", "AmneziaWG 3",
    "AWG3 timings/HP/CPA + AWG2. Максимальная устойчивость сигнатуры среди WG-семейства."))
wg_ru.update(preset("wg_custom", "WG / AWG конструктор",
    "MTU + опциональные AWG jc/jmin/jmax/i1–i5. Пустые AWG-поля = обычный WG."))
wg_ru.update(param("wg", "mtu", "MTU", "MTU интерфейса.",
    "1280–1420 за мобильным/CGNAT; 1500 часто режется.", "576–65535", "1408"))
wg_ru.update(param("wg_awg2", "mtu", "MTU", "MTU.", "Под канал/NAT.", "Число", "1408"))
wg_ru.update(param("wg_awg3", "mtu", "MTU", "MTU.", "Под канал/NAT.", "Число", "1408"))
wg_ru.update(param("wg_custom", "mtu", "MTU", "MTU.", "Под канал/NAT.", "Число", "1408"))
wg_ru.update(param("wg_custom", "listen_port", "Listen UDP", "Порт hub.",
    "UDP listen hub. Не путать с публичным demux :443.", "1–65535", "51820"))
wg_ru.update(param("wg_custom", "jc", "AWG jc", "Число junk-пакетов.",
    "Сколько junk до handshake. 0/пусто = без AWG junk.", "Число", "4"))
wg_ru.update(param("wg_custom", "jmin", "AWG jmin", "Мин. размер junk.",
    "Нижняя граница размера junk-пакета.", "Байты", "40"))
wg_ru.update(param("wg_custom", "jmax", "AWG jmax", "Макс. размер junk.",
    "Верхняя граница; jmax ≥ jmin.", "Байты", "70"))
for i in range(1, 6):
    wg_ru.update(param("wg_custom", f"i{i}", f"AWG i{i}", f"Спец. пакет i{i}.",
        f"Опциональный init-пакет AWG i{i}. Пусто = выкл.", "Hex/строка по профилю AWG", ""))

wg_en: dict = {}
wg_en.update(preset("wg", "WireGuard",
    "Plain WG endpoint. Max speed; DPI spots WG easily. Not demux."))
wg_en.update(preset("wg_awg2", "AmneziaWG 2",
    "AWG2 junk (jc/jmin/jmax) + masquerade. Breaks naive WG signatures at overhead cost."))
wg_en.update(preset("wg_awg3", "AmneziaWG 3",
    "AWG3 timings/HP/CPA + AWG2. Strongest signature hardening in the WG family."))
wg_en.update(preset("wg_custom", "WG / AWG constructor",
    "MTU + optional AWG jc/jmin/jmax/i1–i5. Empty AWG fields = plain WG."))
wg_en.update(param("wg", "mtu", "MTU", "Interface MTU.",
    "1280–1420 behind mobile/CGNAT; 1500 often clamped.", "576–65535", "1408"))
wg_en.update(param("wg_awg2", "mtu", "MTU", "MTU.", "Match link/NAT.", "Number", "1408"))
wg_en.update(param("wg_awg3", "mtu", "MTU", "MTU.", "Match link/NAT.", "Number", "1408"))
wg_en.update(param("wg_custom", "mtu", "MTU", "MTU.", "Match link/NAT.", "Number", "1408"))
wg_en.update(param("wg_custom", "listen_port", "Listen UDP", "Hub port.",
    "UDP hub listen. Not the public demux :443.", "1–65535", "51820"))
wg_en.update(param("wg_custom", "jc", "AWG jc", "Junk packet count.",
    "Junk packets before handshake. 0/empty = no AWG junk.", "Number", "4"))
wg_en.update(param("wg_custom", "jmin", "AWG jmin", "Min junk size.",
    "Lower bound of junk packet size.", "Bytes", "40"))
wg_en.update(param("wg_custom", "jmax", "AWG jmax", "Max junk size.",
    "Upper bound; jmax ≥ jmin.", "Bytes", "70"))
for i in range(1, 6):
    wg_en.update(param("wg_custom", f"i{i}", f"AWG i{i}", f"Special packet i{i}.",
        f"Optional AWG init packet i{i}. Empty = off.", "Hex/string per AWG profile", ""))


demux_ru = {
    "demux.dg_443_fullstack.title": "443 Full stack",
    "demux.dg_443_fullstack.description": "Осознанный :443: Reality + 2×TLS (разные SNI/ALPN) + Hy2 + plain mieru. Максимум разнообразия first-bytes на одном порту.",
    "demux.dg_443_dual.title": "443 Dual",
    "demux.dg_443_dual.description": "Классика: TCP Reality + UDP Hy2. Минимум слотов, сильный DPI-профиль.",
    "demux.dg_443_triple.title": "443 Triple",
    "demux.dg_443_triple.description": "Reality + отдельный TLS (другой SNI) + Hy2. TLS-слот — запасной путь при блокировке Reality SNI.",
}

demux_en = {
    "demux.dg_443_fullstack.title": "443 Full stack",
    "demux.dg_443_fullstack.description": "Intentional :443: Reality + 2×TLS (distinct SNI/ALPN) + Hy2 + plain mieru. Max first-bytes diversity on one port.",
    "demux.dg_443_dual.title": "443 Dual",
    "demux.dg_443_dual.description": "Classic: TCP Reality + UDP Hy2. Minimal slots, strong DPI profile.",
    "demux.dg_443_triple.title": "443 Triple",
    "demux.dg_443_triple.description": "Reality + separate TLS (other SNI) + Hy2. TLS slot is a fallback if Reality SNI is blocked.",
}


def main() -> None:
    dump("ru", "common.json", common_ru)
    dump("en", "common.json", common_en)
    dump("ru", "protocols.json", protocols_ru)
    dump("en", "protocols.json", protocols_en)
    dump("ru", "presets/vless.json", vless_ru)
    dump("en", "presets/vless.json", vless_en)
    dump("ru", "presets/hysteria2.json", hy2_ru)
    dump("en", "presets/hysteria2.json", hy2_en)
    dump("ru", "presets/wireguard.json", wg_ru)
    dump("en", "presets/wireguard.json", wg_en)
    dump("ru", "demux.json", demux_ru)
    dump("en", "demux.json", demux_en)


if __name__ == "__main__":
    main()
