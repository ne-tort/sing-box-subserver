#!/usr/bin/env python3
from __future__ import annotations
import json, re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/controlplane/presets/data"
LOC = ROOT / "internal/controlplane/presets/i18n/locales"


def load(lang: str) -> dict[str, str]:
    keys: dict[str, str] = {}
    for f in (LOC / lang).rglob("*.json"):
        keys.update(json.loads(f.read_text(encoding="utf-8")))
    return keys


def main() -> None:
    en, ru = load("en"), load("ru")
    print("keys", len(en), len(ru), "parity", len(set(en) - set(ru)), len(set(ru) - set(en)))

    weak = [(k, en[k], len(en[k])) for k in en if k.endswith(".help.summary") and len(en[k]) < 45]
    print("weak_help", len(weak))
    for k, v, n in sorted(weak, key=lambda t: t[2])[:30]:
        print(f"  {n} {k}: {v}")

    thin = []
    for k, v in en.items():
        if not (k.startswith("preset.") and k.endswith(".description")):
            continue
        low = v.lower()
        if "minimal" in low or "based on" in low or len(v) < 45:
            thin.append((k, v))
    print("thin_preset_desc", len(thin))
    for k, v in thin[:25]:
        print(f"  {k}: {v}")

    # RU empty/same-as-en for priority keys
    bad_ru = []
    for k, v in en.items():
        if not (k.endswith(".description") or k.endswith(".help.summary") or k.endswith(".title")):
            continue
        rv = ru.get(k, "")
        if not rv:
            bad_ru.append((k, "missing"))
        elif rv == v and any(ord(c) > 127 for c in v) is False and k.endswith((".description", ".help.summary")):
            # identical English in RU for prose keys — may be intentional for tech terms
            if len(v) > 60 and " " in v and not k.startswith("param.") or k.startswith("preset."):
                if k.endswith(".description"):
                    bad_ru.append((k, "same_as_en"))
    print("ru_issues", len(bad_ru))
    for k, why in bad_ru[:20]:
        print(f"  {why} {k}")

    print("=== customs ===")
    for p in sorted(DATA.rglob("*_custom.json")):
        raw = json.loads(p.read_text(encoding="utf-8"))
        meta = list((raw.get("param_meta") or {}).keys())
        sc = raw.get("scores") or {}
        print(f"{p.parent.name}/{p.name}: n={len(meta)} {meta} scores={sc}")

    # presets missing scores / demux_hints / protocol.json
    print("=== protocol coverage ===")
    for d in sorted(p for p in DATA.iterdir() if p.is_dir()):
        presets = list(d.glob("*.json"))
        presets = [p for p in presets if p.name != "protocol.json"]
        missing_scores = 0
        for p in presets:
            raw = json.loads(p.read_text(encoding="utf-8"))
            if not raw.get("scores"):
                missing_scores += 1
        print(f"{d.name}: presets={len(presets)} missing_scores={missing_scores}")


if __name__ == "__main__":
    main()
