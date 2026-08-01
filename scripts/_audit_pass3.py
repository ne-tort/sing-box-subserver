#!/usr/bin/env python3
"""Pass3 audit: template/param wiring, scores, locale quality."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/controlplane/presets/data"
LOC = ROOT / "internal/controlplane/presets/i18n/locales"
PARAM_RE = re.compile(r"\{\{param\.([a-zA-Z0-9_]+)\}\}")


def load_lang(lang: str) -> dict[str, str]:
    keys: dict[str, str] = {}
    for f in (LOC / lang).rglob("*.json"):
        keys.update(json.loads(f.read_text(encoding="utf-8")))
    return keys


def main() -> None:
    en, ru = load_lang("en"), load_lang("ru")
    print("=== i18n ===")
    print("keys", len(en), len(ru), "parity", len(set(en) - set(ru)), len(set(ru) - set(en)))
    weak = [(k, en[k]) for k in en if k.endswith(".help.summary") and len(en[k]) < 45]
    print("weak_help", len(weak))
    for k, v in weak[:15]:
        print(f"  {len(v)} {k}: {v}")
    thin = [
        (k, v)
        for k, v in en.items()
        if k.startswith("preset.") and k.endswith(".description") and ("minimal" in v.lower() or "based on" in v.lower() or len(v) < 45)
    ]
    print("thin_desc", len(thin))
    for k, v in thin[:15]:
        print(f"  {k}: {v}")

    print("\n=== template vs param_meta ===")
    issues = 0
    for p in sorted(DATA.rglob("*.json")):
        if p.name == "protocol.json" or p.parent.name.startswith("_"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        tag = raw.get("tag") or p.stem
        meta = set((raw.get("param_meta") or {}).keys())
        declared = set(raw.get("param_fields") or []) | set(raw.get("optional_param_fields") or []) | meta
        used = set(PARAM_RE.findall(json.dumps(raw)))
        # Custom knobs may be applied in Go without {{param}} — note only for non-custom gaps
        unused_meta = sorted(meta - used)
        undeclared_used = sorted(used - declared)
        if unused_meta or undeclared_used:
            # filter known materialize-only knobs
            custom = bool(raw.get("custom_preset"))
            if custom and unused_meta:
                # expected for knobs applied in custom_knobs.go
                pass
            if undeclared_used or (unused_meta and not custom):
                issues += 1
                print(f"{p.parent.name}/{p.name}:")
                if undeclared_used:
                    print(f"  USED_NOT_DECLARED {undeclared_used}")
                if unused_meta and not custom:
                    print(f"  META_UNUSED {unused_meta}")
            elif custom and unused_meta:
                print(f"{tag}: custom_meta_not_in_tpl {unused_meta}")

    print("\n=== scores gaps ===")
    for p in sorted(DATA.rglob("*.json")):
        if p.name == "protocol.json":
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        sc = raw.get("scores")
        st = raw.get("status")
        if not sc:
            print("NO_SCORES", p)
            continue
        for k in ("dpi", "speed", "mobile", "setup"):
            if k not in sc:
                print("MISSING_SCORE_KEY", p.name, k)
        if st == "stable" and sc.get("setup", 0) <= 2:
            print("SUSPICIOUS_SETUP", p.name, sc)

    print("\n=== demux_hints missing (non-wg/carrier) ===")
    for p in sorted(DATA.rglob("*.json")):
        if p.name == "protocol.json":
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        proto = raw.get("protocol") or p.parent.name
        if proto in ("wireguard", "carrier", "cloudflared"):
            continue
        if not raw.get("demux_hints"):
            print("NO_DEMUX", p)

    print("\ndone issues_flagged", issues)


if __name__ == "__main__":
    main()
