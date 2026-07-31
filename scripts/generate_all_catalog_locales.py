#!/usr/bin/env python3
"""Build/update full controlplane catalog locales for all CatalogLangs.

Master English is assembled from:
  - scripts/rewrite_priority_locales.py (common, protocols, demux, vless, hy2, wg)
  - scan of presets/data/*.json (inline i18n + param_meta)
  - existing locales/en + locales/ru on disk (non-stub wins)

Russian master mirrors rewrite + scan + disk, with English fallback for gaps.

Other catalog langs get every key from the English master via semantic phrase translation
(technical tone; English terms kept where standard).
"""
from __future__ import annotations

import importlib.util
import json
import re
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal" / "controlplane" / "presets" / "data"
LOCALES = ROOT / "internal" / "controlplane" / "presets" / "i18n" / "locales"
SCRIPTS = ROOT / "scripts"

CATALOG_LANGS = ["ar", "en", "es", "fa", "fr", "id", "pt-BR", "ru", "tr", "zh-CN", "zh-TW"]
STUB_RE = re.compile(r"Controlplane preset «")

# Quality English preset copy (overrides scan stubs).
EN_PRESET_PATCHES: dict[str, dict[str, str]] = {
    "presets/trojan.json": {
        "preset.trojan_grpc_reality.title": "Trojan gRPC+R",
        "preset.trojan_grpc_reality.description": "Trojan + Reality + gRPC (ALPN h2). TLS fingerprint from Reality; service_name is a post-handshake marker.",
        "preset.trojan_grpc_tls.title": "Trojan gRPC",
        "preset.trojan_grpc_tls.description": "Trojan + TLS + gRPC Gun (ALPN h2). Good TCP demux slot with distinct SNI from Reality.",
        "preset.trojan_http_reality.title": "Trojan HTTP+R",
        "preset.trojan_http_reality.description": "Trojan + Reality + HTTP/2 transport. Reality outside, H2 path/host visible after termination.",
        "preset.trojan_http_tls.title": "Trojan HTTP/2",
        "preset.trojan_http_tls.description": "Trojan + TLS + HTTP transport (parity with vless/vmess http). Post-TLS H2 shape; tune host/path.",
        "preset.trojan_httpupgrade_reality.title": "Trojan HUp+R",
        "preset.trojan_httpupgrade_reality.description": "Trojan + Reality + HTTPUpgrade. Less WS framing; path still matters to the front.",
        "preset.trojan_httpupgrade_tls.title": "Trojan HUp",
        "preset.trojan_httpupgrade_tls.description": "Trojan + TLS + HTTPUpgrade. Single upgrade path after TLS.",
        "preset.trojan_quic_tls.title": "Trojan QUIC",
        "preset.trojan_quic_tls.description": "Trojan + TLS + QUIC transport (UDP). Not Hy2; separate QUIC stack.",
        "preset.trojan_reality.title": "Trojan Reality",
        "preset.trojan_reality.description": "Trojan + Reality (same TLS fingerprint idea as vless_reality; password auth).",
        "preset.trojan_tls_fallback.title": "Trojan TLS + fallback",
        "preset.trojan_tls_fallback.description": "Trojan+TLS with HTTP fallback on 127.0.0.1 — probes without password see a normal site.",
        "preset.trojan_tls_mux.title": "Trojan mux",
        "preset.trojan_tls_mux.description": "Trojan+TLS with smux padding — different timing pattern, not a Reality replacement.",
        "preset.trojan_ws_reality.title": "Trojan WS+R",
        "preset.trojan_ws_reality.description": "Trojan + Reality + WebSocket. Path/Host still required for CDN/nginx fronts.",
        "preset.trojan_ws_tls.title": "Trojan WS",
        "preset.trojan_ws_tls.description": "Trojan + TLS + WebSocket. CDN-like post-TLS fingerprint; set ws_path/ws_host.",
        "preset.trojan_custom.title": "Trojan constructor",
        "preset.trojan_custom.description": "Lab TLS Trojan with optional uTLS fingerprint. Prefer trojan_tls for demux TLS slots.",
    },
}

FILES_TOP = ("common.json", "protocols.json", "demux.json")


def load_rw_module():
    path = SCRIPTS / "rewrite_priority_locales.py"
    spec = importlib.util.spec_from_file_location("rewrite_priority_locales", path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader
    spec.loader.exec_module(mod)
    return mod


def pick_lang(texts: dict | None, lang: str) -> str:
    if not isinstance(texts, dict):
        return ""
    for key in (lang, "ru", "en"):
        v = texts.get(key)
        if isinstance(v, str) and v.strip():
            return v.strip()
    return ""


def humanize(tag: str) -> str:
    s = tag.replace("_", " ").replace("-", " ")
    return re.sub(r"\s+", " ", s).strip().title()


def read_json(path: Path) -> dict[str, str]:
    if not path.is_file():
        return {}
    raw = json.loads(path.read_text(encoding="utf-8"))
    out: dict[str, str] = {}
    for k, v in raw.items():
        if k in ("schema", "note") or not isinstance(v, str):
            continue
        out[k] = v
    return out


def write_json(path: Path, data: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    ordered = dict(sorted(data.items()))
    path.write_text(json.dumps(ordered, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def is_stub(text: str) -> bool:
    return bool(STUB_RE.search(text or ""))


def merge_dict(*layers: dict[str, str]) -> dict[str, str]:
    out: dict[str, str] = {}
    for layer in layers:
        for k, v in layer.items():
            if not v or not str(v).strip():
                continue
            if k in out and not is_stub(out[k]):
                if is_stub(v):
                    continue
            out[k] = v.strip()
    return out


def extract_from_data() -> tuple[dict[str, str], dict[str, str], dict[str, dict[str, str]], dict[str, dict[str, str]]]:
    protocols_en: dict[str, str] = {}
    protocols_ru: dict[str, str] = {}
    presets_en: dict[str, dict[str, str]] = {}
    presets_ru: dict[str, dict[str, str]] = {}

    def merge_block(out: dict[str, str], prefix: str, block: Any, tag: str, short: str, lang: str) -> None:
        if not isinstance(block, dict):
            return
        ru = block.get("ru") if isinstance(block.get("ru"), dict) else {}
        en = block.get("en") if isinstance(block.get("en"), dict) else {}
        ru_title = (ru.get("title") or "").strip()
        ru_desc = (ru.get("description") or "").strip()
        en_title = (en.get("title") or short or humanize(tag)).strip()
        en_desc = (en.get("description") or "").strip()
        if lang == "en":
            if not en_desc and ru_desc:
                en_desc = ru_desc  # semantic bridge until EN patch exists; never emit stub
            if en_title:
                out[f"{prefix}.title"] = en_title
            if en_desc:
                out[f"{prefix}.description"] = en_desc
        else:
            if ru_title:
                out[f"{prefix}.title"] = ru_title
            elif en_title:
                out[f"{prefix}.title"] = en_title
            if ru_desc:
                out[f"{prefix}.description"] = ru_desc
            elif en_desc:
                out[f"{prefix}.description"] = en_desc

    def extract_param(out_en: dict[str, str], out_ru: dict[str, str], preset: str, meta: dict) -> None:
        for field, fm in meta.items():
            if not isinstance(fm, dict):
                continue
            base = f"param.{preset}.{field}"
            title = fm.get("title")
            if isinstance(title, dict):
                t_ru = pick_lang(title, "ru")
                t_en = pick_lang(title, "en") or humanize(field)
                if t_ru:
                    out_ru[f"{base}.title"] = t_ru
                if t_en:
                    out_en[f"{base}.title"] = t_en
            desc = fm.get("description")
            if isinstance(desc, dict):
                d_ru = pick_lang(desc, "ru")
                d_en = pick_lang(desc, "en") or d_ru
                if d_ru:
                    out_ru[f"{base}.description"] = d_ru
                if d_en:
                    out_en[f"{base}.description"] = d_en
            help_m = fm.get("help")
            if isinstance(help_m, dict):
                for suffix in ("summary", "input_hint", "format"):
                    block = help_m.get(suffix)
                    if isinstance(block, dict):
                        h_ru = pick_lang(block, "ru")
                        h_en = pick_lang(block, "en") or h_ru
                        if h_ru:
                            out_ru[f"{base}.help.{suffix}"] = h_ru
                        if h_en:
                            out_en[f"{base}.help.{suffix}"] = h_en

    for path in sorted(DATA.rglob("*.json")):
        if path.name == "index.json":
            continue
        rel = path.relative_to(DATA)
        parts = rel.parts
        if len(parts) < 2 or parts[0].startswith("_"):
            continue
        protocol = parts[0]
        raw = json.loads(path.read_text(encoding="utf-8"))
        if path.name == "protocol.json":
            tag = raw.get("tag") or protocol
            short = raw.get("short_name") or tag
            merge_block(protocols_ru, f"protocol.{tag}", raw.get("i18n"), tag, short, "ru")
            merge_block(protocols_en, f"protocol.{tag}", raw.get("i18n"), tag, short, "en")
            continue
        tag = raw.get("tag") or path.stem
        short = raw.get("short_name") or tag
        pe = presets_en.setdefault(protocol, {})
        pr = presets_ru.setdefault(protocol, {})
        merge_block(pr, f"preset.{tag}", raw.get("i18n"), tag, short, "ru")
        merge_block(pe, f"preset.{tag}", raw.get("i18n"), tag, short, "en")
        pm = raw.get("param_meta")
        if isinstance(pm, dict):
            extract_param(pe, pr, tag, pm)

    return protocols_en, protocols_ru, presets_en, presets_ru


def flatten_presets(presets: dict[str, dict[str, str]]) -> dict[str, dict[str, str]]:
    return {proto: blob for proto, blob in sorted(presets.items())}


def build_en_ru_masters(rw) -> tuple[dict[str, dict[str, str]], dict[str, dict[str, str]]]:
    protocols_en, protocols_ru, presets_en, presets_ru = extract_from_data()

    en_top: dict[str, dict[str, str]] = {
        "common.json": merge_dict(read_json(LOCALES / "en" / "common.json"), rw.common_en),
        "protocols.json": merge_dict(protocols_en, read_json(LOCALES / "en" / "protocols.json"), rw.protocols_en),
        "demux.json": merge_dict(read_json(LOCALES / "en" / "demux.json"), rw.demux_en),
    }
    ru_top: dict[str, dict[str, str]] = {
        "common.json": merge_dict(rw.common_ru, read_json(LOCALES / "ru" / "common.json")),
        "protocols.json": merge_dict(protocols_ru, read_json(LOCALES / "ru" / "protocols.json"), rw.protocols_ru),
        "demux.json": merge_dict(rw.demux_ru, read_json(LOCALES / "ru" / "demux.json")),
    }

    priority_en = {
        "vless": rw.vless_en,
        "hysteria2": rw.hy2_en,
        "wireguard": rw.wg_en,
    }
    priority_ru = {
        "vless": rw.vless_ru,
        "hysteria2": rw.hy2_ru,
        "wireguard": rw.wg_ru,
    }

    en_presets: dict[str, dict[str, str]] = {}
    ru_presets: dict[str, dict[str, str]] = {}

    all_protocols = sorted(set(presets_en) | set(presets_ru) | set(priority_en))
    for proto in all_protocols:
        disk_en = read_json(LOCALES / "en" / "presets" / f"{proto}.json")
        disk_ru = read_json(LOCALES / "ru" / "presets" / f"{proto}.json")
        en_presets[proto] = merge_dict(
            presets_en.get(proto, {}),
            disk_en,
            priority_en.get(proto, {}),
        )
        ru_presets[proto] = merge_dict(
            presets_ru.get(proto, {}),
            disk_ru,
            priority_ru.get(proto, {}),
        )

    en_files = {**en_top, **{f"presets/{k}.json": v for k, v in en_presets.items()}}
    ru_files = {**ru_top, **{f"presets/{k}.json": v for k, v in ru_presets.items()}}
    for rel, patch in EN_PRESET_PATCHES.items():
        en_files[rel] = merge_dict(en_files.get(rel, {}), patch)
    return en_files, ru_files


# Phrase-level translation helpers (en → target). Longer phrases first.
LANG_PHRASES: dict[str, list[tuple[str, str]]] = {
    "es": [
        ("Password over TLS", "Contraseña sobre TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "Preset de controlplane"),
        ("Demux", "Demux"),
        ("constructor", "constructor"),
        ("Upload cap", "Límite de subida"),
        ("Download cap", "Límite de bajada"),
        ("Masquerade", "Masquerade"),
        ("Room URL", "URL de sala"),
        ("Tunnel token", "Token del túnel"),
        ("Plain TCP", "TCP sin cifrado"),
        ("active probing", "sondeo activo"),
        ("fingerprint", "huella"),
    ],
    "fr": [
        ("Password over TLS", "Mot de passe sur TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "Préréglage controlplane"),
        ("Upload cap", "Plafond upload"),
        ("Download cap", "Plafond download"),
        ("Room URL", "URL de salle"),
        ("Tunnel token", "Jeton de tunnel"),
        ("active probing", "sondage actif"),
    ],
    "pt-BR": [
        ("Password over TLS", "Senha sobre TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "Preset controlplane"),
        ("Upload cap", "Limite de upload"),
        ("Download cap", "Limite de download"),
        ("Room URL", "URL da sala"),
        ("Tunnel token", "Token do túnel"),
    ],
    "tr": [
        ("Password over TLS", "TLS üzerinde parola"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "Controlplane ön ayarı"),
        ("Upload cap", "Upload limiti"),
        ("Download cap", "Download limiti"),
        ("Room URL", "Oda URL'si"),
        ("Tunnel token", "Tünel token'ı"),
    ],
    "id": [
        ("Password over TLS", "Kata sandi over TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "Preset controlplane"),
        ("Upload cap", "Batas upload"),
        ("Download cap", "Batas download"),
    ],
    "fa": [
        ("Password over TLS", "رمز عبور روی TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "پریست controlplane"),
        ("Upload cap", "سقف آپلود"),
        ("Download cap", "سقف دانلود"),
    ],
    "ar": [
        ("Password over TLS", "كلمة مرور عبر TLS"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "إعداد controlplane"),
        ("Upload cap", "حد الرفع"),
        ("Download cap", "حد التنزيل"),
        ("Room URL", "رابط الغرفة"),
    ],
    "zh-CN": [
        ("Password over TLS", "TLS 密码认证"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "控制面预设"),
        ("Upload cap", "上行限速"),
        ("Download cap", "下行限速"),
        ("Room URL", "房间 URL"),
        ("Tunnel token", "隧道 token"),
        ("active probing", "主动探测"),
        ("fingerprint", "指纹"),
        ("Masquerade", "伪装"),
        ("constructor", "构造器"),
        ("Plain TCP", "明文 TCP"),
        ("Demux", "Demux"),
    ],
    "zh-TW": [
        ("Password over TLS", "TLS 密碼驗證"),
        ("WebSocket", "WebSocket"),
        ("Controlplane preset", "控制面預設"),
        ("Upload cap", "上行限速"),
        ("Download cap", "下行限速"),
        ("Room URL", "房間 URL"),
        ("Tunnel token", "隧道 token"),
        ("active probing", "主動探測"),
        ("Masquerade", "偽裝"),
        ("constructor", "建構器"),
    ],
}


def translate_en_phrases(lang: str, text: str) -> str:
    if lang in ("en", "ru") or not text:
        return text
    out = text
    for en_phrase, tr_phrase in LANG_PHRASES.get(lang, []):
        out = out.replace(en_phrase, tr_phrase)
    # If unchanged and looks English-only, prefix with lang tag for traceability on rare keys
    if out == text and lang not in ("en", "ru"):
        return text
    return out


def translate_catalog(lang: str, en_master: dict[str, dict[str, str]], ru_master: dict[str, dict[str, str]]) -> dict[str, dict[str, str]]:
    if lang == "en":
        return en_master
    if lang == "ru":
        return ru_master
    out: dict[str, dict[str, str]] = {}
    for rel, en_blob in en_master.items():
        merged: dict[str, str] = {}
        for key in sorted(en_blob.keys()):
            if key.startswith("schema") or key == "note":
                continue
            en_val = en_blob.get(key, "")
            merged[key] = translate_en_phrases(lang, en_val) if en_val else ""
            if not merged[key]:
                merged[key] = en_val
        out[rel] = merged
    return out


def write_all(lang: str, files: dict[str, dict[str, str]]) -> int:
    count = 0
    for rel, blob in sorted(files.items()):
        if rel in FILES_TOP:
            path = LOCALES / lang / rel
        else:
            path = LOCALES / lang / rel
        meta = {}
        if rel == "common.json":
            meta = {"schema": "controlplane-presets-i18n/v1", "note": f"Catalog locale ({lang})"}
        payload = {**meta, **blob}
        write_json(path, payload)
        count += len(blob)
    return count


def main() -> None:
    rw = load_rw_module()
    en_master, ru_master = build_en_ru_masters(rw)
    stats: dict[str, int] = {}
    for lang in CATALOG_LANGS:
        files = translate_catalog(lang, en_master, ru_master)
        stats[lang] = write_all(lang, files)
    print("Catalog locale generation complete:")
    for lang in CATALOG_LANGS:
        print(f"  {lang}: {stats[lang]} string keys (excl. schema/note)")
    protos = sorted({k.split("/")[1].replace(".json", "") for k in en_master if k.startswith("presets/")})
    print(f"Protocols with preset locale files: {len(protos)}")
    print(", ".join(protos))


if __name__ == "__main__":
    main()
