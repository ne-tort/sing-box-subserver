#!/usr/bin/env python3
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
rw = ROOT / "scripts/rewrite_priority_locales.py"
text = rw.read_text(encoding="utf-8")
old = text
text = text.replace(', "Mbps",', ', "1–65535 Mbps",')
text = text.replace(", 'Mbps',", ", '1–65535 Mbps',")
if text == old:
    raise SystemExit("no Mbps replacements")
rw.write_text(text, encoding="utf-8")
print("rewrite_priority Mbps hints updated")

# Force hint patches that win last in generate_all_catalog_locales
gen = ROOT / "scripts/generate_all_catalog_locales.py"
g = gen.read_text(encoding="utf-8")
marker = '    "presets/hysteria2.json": {\n        "preset.hy2_gecko_masquerade.description"'
insert = '''    "presets/hysteria2.json": {
        "param.hy2.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_custom.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_custom.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_custom.masquerade_dir.help.input_hint": "Absolute directory path on agent",
        "param.hy2_gecko.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_gecko.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_gecko_compact.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_gecko_compact.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_gecko_masquerade.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_gecko_masquerade.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_masquerade.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_masquerade.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_salamander.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.hy2_salamander.down_mbps.help.input_hint": "1–65535 Mbps",
        "preset.hy2_gecko_masquerade.description"'''
if marker not in g:
    raise SystemExit("hysteria2 marker missing")
if "param.hy2.up_mbps.help.input_hint" not in g:
    g = g.replace(marker, insert)
# vless + wg patches
vless_marker = '    "presets/vmess.json": {'
# add vless patch block before vmess if missing
if '"presets/vless.json"' not in g.split("EN_PRESET_PATCHES", 1)[1].split("RU_PRESET_PATCHES", 1)[0]:
    g = g.replace(
        vless_marker,
        '''    "presets/vless.json": {
        "param.vless_custom.alpn.help.input_hint": "h2,http/1.1 (CSV)",
        "param.vless_custom.service_name.help.input_hint": "gRPC service name",
    },
    "presets/wireguard.json": {
        "param.wg_custom.up_mbps.help.input_hint": "1–65535 Mbps",
        "param.wg_custom.down_mbps.help.input_hint": "1–65535 Mbps",
        "param.wg_custom.jmin.help.input_hint": "bytes (AWG junk min)",
        "param.wg_custom.jmax.help.input_hint": "bytes (AWG junk max)",
    },
    '''
        + vless_marker,
        1,
    )
gen.write_text(g, encoding="utf-8")
print("generate patches updated")
