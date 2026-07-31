#!/usr/bin/env python3
"""Enrich controlplane catalog locales (en+ru): param help, demux groups, common copy; fan-out via generate_all."""
from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LOCALES = ROOT / "internal" / "controlplane" / "presets" / "i18n" / "locales"
DATA = ROOT / "internal" / "controlplane" / "presets" / "data"
CATALOG_GO = ROOT / "internal" / "controlplane" / "demuxgroups" / "catalog.go"
SCRIPTS = ROOT / "scripts"

WEAK_SUMMARY_RE = re.compile(
    r"^(Upload cap\.?|Download cap\.?|Masquerade\.?|Room URL\.?|Token\.?|Path\.?|Host\.?)$",
    re.I,
)

# field → (en_summary, en_hint, en_format, ru_summary, ru_hint, ru_format)
FIELD_HELP: dict[str, tuple[str, str, str, str, str, str]] = {
    "ws_path": (
        "HTTP path after TLS for WebSocket; stock /ws paths are easy DPI fingerprints — match your CDN/nginx rules.",
        "Path starting with /",
        "/cdn-cgi/trace",
        "Path после TLS для WebSocket; стоковые /ws легко ловятся DPI — совпадайте с CDN/nginx.",
        "Путь с /",
        "/cdn-cgi/trace",
    ),
    "ws_host": (
        "Host/:authority for WebSocket must match the front/CDN (often equals TLS/Reality SNI).",
        "Hostname without scheme",
        "cdn.example.com",
        "Host/:authority WebSocket — как ждёт фронт/CDN (часто = SNI TLS/Reality).",
        "Хост без схемы",
        "cdn.example.com",
    ),
    "http_path": (
        "HTTP/2 transport path after TLS; change if the front blocks default paths.",
        "Path starting with /",
        "/h2",
        "Path HTTP/2 transport после TLS; меняйте, если фронт режет дефолты.",
        "Путь с /",
        "/h2",
    ),
    "http_host": (
        "Host header for HTTP transport; with Reality, materialize often aligns it to the visible SNI.",
        "Hostname without scheme",
        "www.example.com",
        "Host HTTP transport; при Reality materialize часто выравнивает под SNI.",
        "Хост без схемы",
        "www.example.com",
    ),
    "hu_path": (
        "Single HTTP Upgrade path (no WebSocket framing); still visible after TLS termination.",
        "Path starting with /",
        "/upgrade",
        "Path одного HTTP Upgrade (без WS framing); виден после TLS termination.",
        "Путь с /",
        "/upgrade",
    ),
    "hu_host": (
        "HTTPUpgrade Host/:authority — keep consistent with SNI and the reverse proxy front.",
        "Hostname without scheme",
        "cdn.example.com",
        "Host HTTPUpgrade — согласован с SNI и фронтом reverse proxy.",
        "Хост без схемы",
        "cdn.example.com",
    ),
    "service_name": (
        "gRPC Gun service_name after TLS ALPN h2; stock names are filtered on some networks — tune per slot.",
        "Service identifier string",
        "GunService",
        "gRPC Gun service_name после TLS/h2; стоковые имена режут — настройте под слот demux.",
        "Строка service name",
        "GunService",
    ),
    "grpc_service_name": (
        "gRPC Gun service_name after TLS ALPN h2; change from stock if the path is blocked.",
        "Service identifier string",
        "GunService",
        "gRPC Gun service_name; смените сток, если режут по имени сервиса.",
        "Строка service name",
        "GunService",
    ),
    "path": (
        "HTTP/WebSocket path for DERP or similar relay transports (not a TLS camouflage layer).",
        "Path starting with /",
        "/derp",
        "HTTP/WebSocket path для DERP и relay-транспортов (не TLS-камуфляж).",
        "Путь с /",
        "/derp",
    ),
    "jls_addr": (
        "host:port of the QUIC/TLS upstream used for JLS camouflage; align with demux_sni on :443 stacks.",
        "host:port",
        "www.cloudflare.com:443",
        "host:port upstream для JLS-камуфляжа QUIC; на demux :443 выравнивайте с demux_sni.",
        "host:port",
        "www.cloudflare.com:443",
    ),
    "jls_server_name": (
        "TLS/QUIC SNI presented to the JLS upstream; must match demux_sni or the handshake fails.",
        "Domain name",
        "www.cloudflare.com",
        "SNI для JLS upstream; должен совпадать с demux_sni, иначе handshake падает.",
        "Домен",
        "www.cloudflare.com",
    ),
    "handshake_server": (
        "ShadowTLS v3 handshake mimic target (SNI); pick a plausible site your clients can reach.",
        "Domain name",
        "www.apple.com",
        "Цель mimic handshake ShadowTLS v3 (SNI); правдоподобный домен.",
        "Домен",
        "www.apple.com",
    ),
    "traffic_pattern": (
        "Mieru traffic-shaping profile name; affects padding/timing, not TLS first bytes.",
        "Profile name from Mieru docs",
        "",
        "Имя профиля shaping Mieru; влияет на padding/timing, не на TLS first bytes.",
        "Имя профиля",
        "",
    ),
    "fingerprint": (
        "uTLS ClientHello fingerprint on TCP+TLS outbounds; useless on raw QUIC/Hy2 transports.",
        "chrome|firefox|safari|…",
        "chrome",
        "uTLS fingerprint ClientHello на TCP+TLS outbound; на сыром QUIC/Hy2 не применяется.",
        "chrome|firefox|safari|…",
        "chrome",
    ),
    "up_mbps": (
        "Hard upload bandwidth cap in Mbps; too low throttles users, too high on weak links causes loss.",
        "Mbps number",
        "100",
        "Жёсткий потолок upload (Mbps); занижение режет скорость, завышение даёт потери на слабом канале.",
        "Число Mbps",
        "100",
    ),
    "down_mbps": (
        "Hard download bandwidth cap in Mbps; should reflect real VPS uplink and client capacity.",
        "Mbps number",
        "100",
        "Жёсткий потолок download (Mbps); подгоняйте под реальный uplink VPS и клиента.",
        "Число Mbps",
        "100",
    ),
    "masquerade_url": (
        "Upstream HTTPS URL Hy2 serves on auth failure — hides bad-password probes, not QUIC first bytes.",
        "https URL",
        "https://www.cloudflare.com",
        "Upstream HTTPS при fail-auth Hy2 — прячет probing пароля, first-bytes QUIC не меняет.",
        "https URL",
        "https://www.cloudflare.com",
    ),
    "room": (
        "Full SFU room URL — Carrier hides WG-star inside a real call; invalid room stops the inbound.",
        "Full https URL",
        "https://meet.jit.si/MyRoom",
        "Полный URL комнаты SFU — Carrier прячет WG-star в звонок; без комнаты inbound не стартует.",
        "Полный https URL",
        "https://meet.jit.si/MyRoom",
    ),
    "token": (
        "Opaque tunnel or provider token from the dashboard; treat as secret — leak equals takeover.",
        "Token string",
        "eyJ...",
        "Opaque token из кабинета; секрет — утечка = захват туннеля.",
        "Строка token",
        "eyJ...",
    ),
    "key": (
        "Optional Carrier room key or secondary auth material required by the SFU provider flow.",
        "Opaque string",
        "",
        "Опциональный ключ комнаты Carrier или доп. auth от провайдера SFU.",
        "Строка",
        "",
    ),
    "provider": (
        "Carrier SFU provider id (jitsi, telemost, wbstream, vk, peer) — selects underlay wiring.",
        "Provider id",
        "jitsi",
        "ID провайдера Carrier SFU — задаёт wiring underlay.",
        "id провайдера",
        "jitsi",
    ),
    "vk_hash": (
        "VK Carrier session hash from the provider join flow; required for VK underlay handshakes.",
        "Hash string",
        "",
        "Hash сессии VK Carrier из провайдерского join-flow.",
        "Строка hash",
        "",
    ),
    "zero_rtt": (
        "Enables TUIC zero_rtt_handshake on server and client — faster resume with weaker replay resistance.",
        "true or false",
        "false",
        "Включает zero_rtt_handshake TUIC — быстрее resume, слабее replay resistance.",
        "true или false",
        "false",
    ),
    "congestion_control": (
        "QUIC congestion-control algorithm for TUIC (cubic, bbr, or new_reno) — affects loss recovery on mobile/lossy links.",
        "cubic|bbr|new_reno",
        "bbr",
        "Алгоритм congestion QUIC для TUIC — влияет на recovery на мобильных/потерянных каналах.",
        "cubic|bbr|new_reno",
        "bbr",
    ),
    "udp_relay_mode": (
        "TUIC outbound UDP relay: native full-cone vs QUIC-encapsulated relay for filtered UDP paths.",
        "native|quic",
        "native",
        "UDP relay TUIC outbound: native full-cone или relay через QUIC при фильтрации UDP.",
        "native|quic",
        "native",
    ),
    "mode": (
        "TrustTunnel upstream_protocol: h2 (TCP/TLS), h3 (QUIC), or auto with timed fallback.",
        "h2|h3|auto",
        "auto",
        "TrustTunnel upstream_protocol: h2 (TCP/TLS), h3 (QUIC) или auto с fallback.",
        "h2|h3|auto",
        "auto",
    ),
    "method": (
        "Shadowsocks cipher method (AEAD or 2022-blake3); no TLS wrapper — trivial DPI class on public networks.",
        "AEAD/2022 method id",
        "aes-128-gcm",
        "Метод Shadowsocks (AEAD/2022); без TLS — слабый DPI-профиль в публичных сетях.",
        "id метода",
        "aes-128-gcm",
    ),
    "network": (
        "NaiveProxy network: tcp presents TLS+ALPN h2; udp uses QUIC/H3 camouflage (see naive_quic stock).",
        "tcp|udp",
        "tcp",
        "Naive network: tcp — TLS+ALPN h2; udp — QUIC/H3 (см. naive_quic).",
        "tcp|udp",
        "tcp",
    ),
}


def read_json(path: Path) -> dict[str, str]:
    if not path.is_file():
        return {}
    raw = json.loads(path.read_text(encoding="utf-8"))
    return {k: v for k, v in raw.items() if isinstance(v, str)}


def write_json(path: Path, data: dict[str, str], meta: dict | None = None) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = {**(meta or {}), **dict(sorted(data.items()))}
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def is_weak_summary(text: str) -> bool:
    t = (text or "").strip()
    if len(t) < 28:
        return True
    if WEAK_SUMMARY_RE.match(t):
        return True
    if t.endswith(".") and len(t.split()) <= 4:
        return True
    return False


def field_from_param_key(key: str) -> str | None:
    # param.preset.field.title | .description | .help.summary
    parts = key.split(".")
    if len(parts) < 3 or parts[0] != "param":
        return None
    if parts[-1] in ("title", "description", "summary", "input_hint", "format"):
        return parts[-2] if parts[-2] != "help" else parts[-3]
    return None


def help_for_field(field: str, lang: str, description: str = "") -> tuple[str, str, str]:
    if field in FIELD_HELP:
        en_s, en_h, en_f, ru_s, ru_h, ru_f = FIELD_HELP[field]
        if lang == "ru":
            return ru_s, ru_h, ru_f
        return en_s, en_h, en_f
    base = description.strip() or field.replace("_", " ")
    if lang == "ru":
        summary = base if len(base) > 40 else f"Параметр {field}: {base}. Влияет на sing-box конфиг inbound/outbound."
    else:
        summary = base if len(base) > 40 else f"Preset parameter {field}: {base}. Affects sing-box inbound/outbound wiring."
    return summary, "Value per operator docs", ""


def enrich_locale_file(path: Path, lang: str) -> int:
    data = read_json(path)
    if not data:
        return 0
    meta_keys = {k for k in data if k in ("schema", "note")}
    meta = {k: data[k] for k in meta_keys}
    n = 0
    titles = [k for k in data if k.startswith("param.") and k.endswith(".title")]
    for title_k in titles:
        base = title_k[: -len(".title")]
        field = title_k.split(".")[-2]
        desc_k = base + ".description"
        description = data.get(desc_k, "")
        for suffix, idx in (("summary", 0), ("input_hint", 1), ("format", 2)):
            hk = base + f".help.{suffix}"
            cur = data.get(hk, "").strip()
            gen = help_for_field(field, lang, description)
            new_val = gen[idx]
            if not new_val:
                continue
            if suffix == "summary":
                if cur and not is_weak_summary(cur):
                    continue
                if not new_val:
                    continue
            else:
                if cur:
                    continue
            if data.get(hk) != new_val:
                data[hk] = new_val
                n += 1
    if n:
        write_json(path, {k: v for k, v in data.items() if k not in meta_keys}, meta if meta else None)
    return n


def parse_demux_from_catalog() -> tuple[dict[str, str], dict[str, str]]:
    text = CATALOG_GO.read_text(encoding="utf-8")
    en: dict[str, str] = {}
    ru: dict[str, str] = {}
    tag_re = re.compile(r'Tag:\s*"(dg_[^"]+)"')
    blocks = re.split(r"\{\s*\n\s*Tag:", text)
    for chunk in blocks:
        m = tag_re.search("Tag:" + chunk if not chunk.startswith("Tag:") else chunk)
        if not m:
            continue
        tag = m.group(1)
        ru_title = re.search(r'"ru":\s*\{Title:\s*"([^"]+)"', chunk)
        ru_desc = re.search(r'"ru":\s*\{Title:\s*"[^"]+",\s*Description:\s*"([^"]+)"', chunk)
        en_title = re.search(r'"en":\s*\{Title:\s*"([^"]+)"', chunk)
        en_desc = re.search(r'"en":\s*\{Title:\s*"[^"]+",\s*Description:\s*"([^"]+)"', chunk)
        if en_title:
            en[f"demux.{tag}.title"] = en_title.group(1)
        if en_desc:
            d = en_desc.group(1)
            en[f"demux.{tag}.description"] = d
        if ru_title:
            ru[f"demux.{tag}.title"] = ru_title.group(1)
        if ru_desc:
            ru[f"demux.{tag}.description"] = ru_desc.group(1)
    # DPI-oriented upgrades for groups with thin catalog.go copy
    extras_en = {
        "demux.dg_443_sni_stack.description": "Up to five TCP TLS/Reality slots on one :443, each with a unique demux_sni — maximizes SNI-based separation without ALPN matching.",
        "demux.dg_443_modern5.description": "Two Reality + TLS + Hy2 + TUIC on unique SNIs; QUIC slots need distinct demux_sni where match supports it.",
        "demux.dg_443_dense8.description": "Eight slots: 2×Reality, 3×TLS, plain always_plain, Hy2 + TUIC — widest substitute set on one port (lab).",
        "demux.dg_443_alpn_split.description": "Three TCP TLS slots by SNI; PreferredALPN sets inbound tls.alpn only — demux matcher uses tls.sni, not ALPN.",
        "demux.dg_443_reality_sq.description": "TCP Reality plus QUIC (Hy2 default); ShadowQUIC substitutes are demux_lab — salamander obfs breaks SNI match.",
        "demux.dg_8443_quic_pair.description": "UDP-only :8443 with Hy2 + TUIC; split by SNI where the preset supports QUIC demux hints.",
        "demux.dg_443_quic_pair_sni.description": "Hy2 + TUIC on UDP :443 with SNI-based demux separation (UDP-only group).",
    }
    extras_ru = {
        "demux.dg_443_sni_stack.description": "До пяти TCP TLS/Reality слотов на :443 с уникальным demux_sni — максимум разведения по SNI без ALPN match.",
        "demux.dg_443_modern5.description": "2×Reality + TLS + Hy2 + TUIC на уникальных SNI; QUIC-слоты требуют demux_sni где match поддерживается.",
        "demux.dg_443_dense8.description": "8 слотов: 2×Reality, 3×TLS, plain always_plain, Hy2 + TUIC — широкий набор замен (lab).",
        "demux.dg_443_alpn_split.description": "Три TLS-слота по SNI; PreferredALPN только для inbound tls.alpn — demux матчит tls.sni, не ALPN.",
        "demux.dg_443_reality_sq.description": "TCP Reality + QUIC (дефолт Hy2); ShadowQUIC — demux_lab, salamander ломает SNI match.",
        "demux.dg_8443_quic_pair.description": "Только UDP :8443 — Hy2 + TUIC; разведение по SNI где пресет поддерживает demux hints.",
        "demux.dg_443_quic_pair_sni.description": "Hy2 + TUIC на UDP :443 с разведением по SNI (UDP-only группа).",
    }
    en.update(extras_en)
    ru.update(extras_ru)
    return en, ru


def merge_demux(lang: str, demux_blob: dict[str, str]) -> int:
    path = LOCALES / lang / "demux.json"
    cur = read_json(path)
    n = 0
    for k, v in demux_blob.items():
        if cur.get(k) != v:
            cur[k] = v
            n += 1
    write_json(path, cur)
    return n


def enrich_common(lang: str) -> int:
    path = LOCALES / lang / "common.json"
    data = read_json(path)
    if not data:
        return 0
    meta = {k: data[k] for k in ("schema", "note") if k in data}
    n = 0
    for k in list(data.keys()):
        if not k.startswith("param.common.") or not k.endswith(".help.summary"):
            continue
        if is_weak_summary(data[k]):
            field = k.split(".")[2]
            summary, hint, fmt = help_for_field(field, lang, data.get(f"param.common.{field}.description", ""))
            if summary and data[k] != summary:
                data[k] = summary
                n += 1
            hk = f"param.common.{field}.help.input_hint"
            if hint and not data.get(hk):
                data[hk] = hint
                n += 1
            fk = f"param.common.{field}.help.format"
            if fmt and not data.get(fk):
                data[fk] = fmt
                n += 1
    if n:
        write_json(path, {k: v for k, v in data.items() if k not in meta}, meta or None)
    return n


def count_missing_summary(lang: str) -> int:
    missing = 0
    for p in (LOCALES / lang).rglob("*.json"):
        d = read_json(p)
        keys = set(d)
        for k in keys:
            if k.startswith("param.") and k.endswith(".title"):
                hs = k[: -len(".title")] + ".help.summary"
                if hs not in keys or not str(d.get(hs, "")).strip():
                    missing += 1
    return missing


def scan_data_param_locales() -> int:
    """Ensure locale files have param.* from preset param_meta (new grpc/custom knobs)."""
    by_proto: dict[tuple[str, str], dict[str, str]] = {}
    n = 0
    for path in sorted(DATA.rglob("*.json")):
        if path.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(path.read_text(encoding="utf-8"))
        tag = raw.get("tag") or path.stem
        protocol = raw.get("protocol") or path.parent.name
        pm = raw.get("param_meta") or {}
        if not pm:
            continue
        for lang in ("en", "ru"):
            key = (lang, protocol)
            blob = by_proto.setdefault(key, read_json(LOCALES / lang / "presets" / f"{protocol}.json"))
            for field, meta in pm.items():
                if not isinstance(meta, dict):
                    continue
                base = f"param.{tag}.{field}"
                title = (meta.get("title") or {}).get(lang) or (meta.get("title") or {}).get("en") or field
                desc = (meta.get("description") or {}).get(lang) or (meta.get("description") or {}).get("en") or ""
                tk, dk = f"{base}.title", f"{base}.description"
                if tk not in blob:
                    blob[tk] = title
                    n += 1
                if desc and dk not in blob:
                    blob[dk] = desc
                    n += 1
                summary, hint, fmt = help_for_field(field, lang, desc)
                for suffix, val in (("summary", summary), ("input_hint", hint), ("format", fmt)):
                    if not val:
                        continue
                    hk = f"{base}.help.{suffix}"
                    if hk not in blob or (suffix == "summary" and is_weak_summary(blob.get(hk, ""))):
                        blob[hk] = val
                        n += 1
    for (lang, protocol), blob in by_proto.items():
        write_json(LOCALES / lang / "presets" / f"{protocol}.json", blob)
    return n


def main() -> None:
    stats = {"help_filled": 0, "demux_keys": 0, "common": 0}
    stats["help_filled"] += scan_data_param_locales()
    for lang in ("en", "ru"):
        for path in (LOCALES / lang).rglob("*.json"):
            if path.name in ("demux.json",) and lang == "en":
                continue
            stats["help_filled"] += enrich_locale_file(path, lang)
        stats["common"] += enrich_common(lang)

    demux_en, demux_ru = parse_demux_from_catalog()
    stats["demux_keys"] = len(demux_en)
    merge_demux("en", demux_en)
    merge_demux("ru", demux_ru)

    gen = SCRIPTS / "generate_all_catalog_locales.py"
    subprocess.run([sys.executable, str(gen)], check=True, cwd=ROOT)

    missing_en = count_missing_summary("en")
    print("enrich_catalog_copy complete:")
    print(f"  help fields filled/upgraded: {stats['help_filled']}")
    print(f"  demux en keys written: {stats['demux_keys']}")
    print(f"  common.json touches: {stats['common']}")
    print(f"  en param help.summary still missing: {missing_en}")


if __name__ == "__main__":
    main()
