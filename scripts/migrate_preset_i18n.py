#!/usr/bin/env python3
"""Extract preset/protocol/param i18n from data/**/*.json into embedded locale files."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal" / "controlplane" / "presets" / "data"
LOCALES = ROOT / "internal" / "controlplane" / "presets" / "i18n" / "locales"
LANGS = ("en", "ru")


def humanize(tag: str) -> str:
    s = tag.replace("_", " ").replace("-", " ")
    return re.sub(r"\s+", " ", s).strip().title()


def pick_lang(texts: dict, lang: str) -> str:
    if not isinstance(texts, dict):
        return ""
    for key in (lang, "ru", "en"):
        v = texts.get(key)
        if isinstance(v, str) and v.strip():
            return v.strip()
    for v in texts.values():
        if isinstance(v, str) and v.strip():
            return v.strip()
    return ""


def synth_en_title(short_name: str, tag: str) -> str:
    if short_name and short_name.strip():
        return short_name.strip()
    return humanize(tag)


def synth_en_description(ru_desc: str, tag: str) -> str:
    if ru_desc and ru_desc.strip():
        # Keep technical preset names; use short English stub when only RU exists.
        return f"Controlplane preset «{humanize(tag)}»."
    return f"Controlplane preset «{humanize(tag)}»."


def merge_i18n_block(
    out: dict[str, str], prefix: str, block: dict | None, tag: str, short_name: str, lang: str
) -> None:
    if not block:
        return
    ru = block.get("ru") if isinstance(block.get("ru"), dict) else {}
    en = block.get("en") if isinstance(block.get("en"), dict) else {}
    ru_title = (ru.get("title") or "").strip()
    ru_desc = (ru.get("description") or "").strip()
    en_title = (en.get("title") or "").strip()
    en_desc = (en.get("description") or "").strip()
    if lang == "en":
        if not en_title:
            en_title = synth_en_title(short_name, tag)
        if not en_desc:
            en_desc = synth_en_description(ru_desc, tag) if ru_desc else synth_en_description("", tag)
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


def extract_param_meta(out_en: dict[str, str], out_ru: dict[str, str], preset: str, meta: dict) -> None:
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


def walk_data() -> tuple[dict[str, str], dict[str, str], dict[str, dict[str, str]], dict[str, dict[str, str]]]:
    protocols_en: dict[str, str] = {}
    protocols_ru: dict[str, str] = {}
    presets_en: dict[str, dict[str, str]] = {}
    presets_ru: dict[str, dict[str, str]] = {}

    for path in sorted(DATA.rglob("*.json")):
        if path.name == "index.json":
            continue
        rel = path.relative_to(DATA)
        parts = rel.parts
        if len(parts) < 2:
            continue
        protocol = parts[0]
        if protocol.startswith("_"):
            continue

        raw = json.loads(path.read_text(encoding="utf-8"))
        if path.name == "protocol.json":
            tag = raw.get("tag") or protocol
            short = raw.get("short_name") or tag
            merge_i18n_block(protocols_ru, f"protocol.{tag}", raw.get("i18n"), tag, short, "ru")
            merge_i18n_block(protocols_en, f"protocol.{tag}", raw.get("i18n"), tag, short, "en")
            continue

        tag = raw.get("tag") or path.stem
        short = raw.get("short_name") or tag
        pe = presets_en.setdefault(protocol, {})
        pr = presets_ru.setdefault(protocol, {})
        merge_i18n_block(pr, f"preset.{tag}", raw.get("i18n"), tag, short, "ru")
        merge_i18n_block(pe, f"preset.{tag}", raw.get("i18n"), tag, short, "en")
        pm = raw.get("param_meta")
        if isinstance(pm, dict):
            extract_param_meta(pe, pr, tag, pm)

    return protocols_en, protocols_ru, presets_en, presets_ru


def write_locale_file(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    protocols_en, protocols_ru, presets_en, presets_ru = walk_data()
    for lang, protocols, presets in (
        ("en", protocols_en, presets_en),
        ("ru", protocols_ru, presets_ru),
    ):
        write_locale_file(LOCALES / lang / "protocols.json", protocols)
        preset_dir = LOCALES / lang / "presets"
        for protocol, blob in sorted(presets.items()):
            write_locale_file(preset_dir / f"{protocol}.json", blob)
    print(f"Wrote locales under {LOCALES}")


if __name__ == "__main__":
    main()
