#!/usr/bin/env python3
"""Pass6: capability gaps stock vs custom; thin help; scores honesty; template oddities."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/controlplane/presets/data"
LOC = ROOT / "internal/controlplane/presets/i18n/locales"
PARAM_RE = re.compile(r"\{\{param\.([a-zA-Z0-9_]+)\}\}")
PEER_RE = re.compile(r"\{\{peer\.([a-zA-Z0-9_]+)\}\}")
USER_RE = re.compile(r"\{\{user\.([a-zA-Z0-9_]+)\}\}")


def load_lang(lang: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for f in (LOC / lang).rglob("*.json"):
        out.update(json.loads(f.read_text(encoding="utf-8")))
    return out


def main() -> None:
    presets: dict[str, list[dict]] = {}
    for p in sorted(DATA.rglob("*.json")):
        if p.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(p.read_text(encoding="utf-8"))
        proto = raw.get("protocol") or p.parent.name
        presets.setdefault(proto, []).append(raw)

    print("=== custom vs stock param coverage ===")
    for proto, items in sorted(presets.items()):
        customs = [x for x in items if x.get("custom_preset")]
        stocks = [x for x in items if not x.get("custom_preset")]
        if not customs:
            print(f"{proto}: NO_CUSTOM")
            continue
        c = customs[0]
        c_params = set((c.get("param_meta") or {}).keys()) | set(c.get("optional_param_fields") or []) | set(
            c.get("param_fields") or []
        )
        stock_union: set[str] = set()
        for s in stocks:
            stock_union |= set((s.get("param_meta") or {}).keys())
            stock_union |= set(s.get("optional_param_fields") or [])
            stock_union |= set(s.get("param_fields") or [])
        only_custom = sorted(c_params - stock_union)
        # interesting: params only on custom that stock family might want
        if only_custom:
            print(f"{proto}: custom_only {only_custom}")

    print("\n=== template placeholders without declared params ===")
    for proto, items in sorted(presets.items()):
        for raw in items:
            tag = raw.get("tag")
            declared = (
                set(raw.get("param_fields") or [])
                | set(raw.get("optional_param_fields") or [])
                | set((raw.get("param_meta") or {}).keys())
            )
            used = set(PARAM_RE.findall(json.dumps(raw)))
            miss = sorted(used - declared)
            if miss:
                print(f"{proto}/{tag}: USED_NOT_DECLARED {miss}")

    print("\n=== peer/user fields vs declarations ===")
    for proto, items in sorted(presets.items()):
        for raw in items:
            tag = raw.get("tag")
            peer_decl = set((raw.get("peer_secret_fields") or {}).keys())
            peer_used = set(PEER_RE.findall(json.dumps(raw)))
            if peer_used - peer_decl:
                print(f"{proto}/{tag}: PEER_USED_UNDECLARED {sorted(peer_used - peer_decl)}")
            cred = set(raw.get("cred_fields") or [])
            user_used = set(USER_RE.findall(json.dumps(raw)))
            # user.name is injected; ignore
            user_used.discard("name")
            if user_used - cred and not raw.get("custom_preset"):
                # some templates use user.* from generators
                extra = sorted(user_used - cred)
                if extra:
                    print(f"{proto}/{tag}: USER_USED_NOT_IN_CRED {extra}")

    print("\n=== scores outliers ===")
    for proto, items in sorted(presets.items()):
        for raw in items:
            sc = raw.get("scores") or {}
            tag = raw.get("tag")
            st = raw.get("status")
            if not sc:
                continue
            if st == "stable" and sc.get("dpi", 0) <= 2:
                print(f"LOW_DPI_STABLE {tag} {sc}")
            if sc.get("speed", 0) == 10 and sc.get("dpi", 0) >= 9:
                print(f"SUSPICIOUS_BOTH_HIGH {tag} {sc}")
            if st == "lab" and sc.get("setup", 0) >= 8:
                print(f"LAB_EASY_SETUP {tag} {sc}")

    print("\n=== en descriptions still short / stubby ===")
    en = load_lang("en")
    thin = []
    for k, v in en.items():
        if not k.startswith("preset.") or not k.endswith(".description"):
            continue
        if len(v) < 50 or "minimal" in v.lower() or "based on" in v.lower() or "stub" in v.lower():
            thin.append((k, v))
    print("thin", len(thin))
    for k, v in thin[:25]:
        print(f"  {k}: {v}")

    print("\n=== param help.summary missing for declared meta (en) ===")
    missing_help = 0
    for k in en:
        if k.startswith("param.") and k.endswith(".title"):
            base = k[: -len(".title")]
            if f"{base}.help.summary" not in en:
                # skip if no description either — still flag
                missing_help += 1
                if missing_help <= 20:
                    print("NO_HELP", base)
    print("missing_help_total", missing_help)

    print("\ndone")


if __name__ == "__main__":
    main()
