#!/usr/bin/env python3
"""Audit custom presets + i18n parity (one-off)."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/controlplane/presets/data"
LOC = ROOT / "internal/controlplane/presets/i18n/locales"


def load_lang(lang: str) -> dict:
    keys: dict = {}
    for f in (LOC / lang).rglob("*.json"):
        keys.update(json.loads(f.read_text(encoding="utf-8")))
    return keys


def main() -> None:
    print("=== customs ===")
    for p in sorted(DATA.rglob("*_custom.json")):
        raw = json.loads(p.read_text(encoding="utf-8"))
        meta = list((raw.get("param_meta") or {}).keys())
        params = sorted(set(re.findall(r"\{\{param\.([a-zA-Z0-9_]+)\}\}", json.dumps(raw))))
        print(f"{p.parent.name}/{p.name}: meta={meta} tpl={params}")

    print("\n=== i18n parity ===")
    langs = sorted(d.name for d in LOC.iterdir() if d.is_dir())
    en = load_lang("en")
    print("langs", langs, "en_keys", len(en))
    for lang in langs:
        k = load_lang(lang)
        miss = sorted(set(en) - set(k))
        print(f"{lang}: keys={len(k)} missing_vs_en={len(miss)}")
        if miss[:5]:
            print("  sample", miss[:5])

    # weak help
    titles = [k for k in en if k.startswith("param.") and k.endswith(".title")]
    weak = []
    for t in titles:
        h = en.get(t[:-6] + ".help.summary", "")
        if h and len(h) < 40:
            weak.append((t[:-6], h))
    print(f"\nweak help (<40 chars): {len(weak)}")
    for k, h in weak:
        print(f"  {k}: {h}")


if __name__ == "__main__":
    main()
