#!/usr/bin/env python3
"""Strengthen short param.*.help.summary in en/ru, then fan-out to other langs."""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LOC = ROOT / "internal/controlplane/presets/i18n/locales"

EN_PATCH: dict[str, str] = {
    "param.carrier_jitsi_sei_shared.room.help.summary": "Full Jitsi Meet room URL; required for SEI shared SFU auth.",
    "param.carrier_jitsi_sei_users.room.help.summary": "Full Jitsi Meet room URL; required for SEI per-user SFU auth.",
    "param.carrier_jitsi_shared.room.help.summary": "Full Jitsi Meet room URL; shared carrier underlay joins this room.",
    "param.carrier_jitsi_users.room.help.summary": "Full Jitsi Meet room URL; per-user carrier underlay joins this room.",
    "param.carrier_telemost_shared.room.help.summary": "Full Yandex Telemost room URL for the shared carrier underlay.",
    "param.carrier_telemost_users.room.help.summary": "Full Yandex Telemost room URL for the per-user carrier underlay.",
    "param.carrier_wbstream_shared.room.help.summary": "Full WB Stream room URL for the shared carrier underlay.",
    "param.carrier_wbstream_users.room.help.summary": "Full WB Stream room URL for the per-user carrier underlay.",
    "param.cloudflared_custom.token.help.summary": "Cloudflare Tunnel token from Zero Trust; opens the named tunnel.",
    "param.cloudflared_token.token.help.summary": "Cloudflare Tunnel token from Zero Trust; opens the named tunnel.",
    "param.hy2.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_custom.down_mbps.help.summary": "Download Mbps cap written into Hy2 inbound/outbound templates.",
    "param.hy2_custom.up_mbps.help.summary": "Upload Mbps cap written into Hy2 inbound/outbound templates.",
    "param.hy2_custom.masquerade_dir.help.summary": "Filesystem root for file masquerade; required when masquerade_mode=file.",
    "param.hy2_custom.masquerade_url.help.summary": "Upstream HTTPS URL for proxy masquerade on failed Hy2 auth.",
    "param.hy2_gecko.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_gecko.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_gecko_compact.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_gecko_compact.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_gecko_masquerade.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_gecko_masquerade.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_masquerade.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_masquerade.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_salamander.down_mbps.help.summary": "Hard download Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_salamander.up_mbps.help.summary": "Hard upload Mbps cap advertised on the Hy2 link (server/client).",
    "param.hy2_masquerade_file.masquerade_dir.help.summary": "Directory served as HTTP decoy when Hy2 auth fails (file masquerade).",
    "param.hy2_realm.realm_id.help.summary": "Realm identifier issued by the Hy2 realm operator control plane.",
    "param.hy2_realm.realm_server_url.help.summary": "Operator HTTPS control URL used to register this Hy2 realm node.",
    "param.vless_custom.packet_encoding.help.summary": "Outbound UDP encoding; xudp is the default full-cone choice.",
    "param.vless_custom.transport_host.help.summary": "Host/:authority for WS/HTTP transports; usually matches SNI/front.",
    "param.vless_custom.transport_path.help.summary": "Post-TLS path for WS/HTTP; avoid stock /ws and /vless defaults.",
    "param.vless_http_reality.http_host.help.summary": "HTTP/2 Host after Reality; usually equals Reality SNI.",
    "param.vless_http_reality.http_path.help.summary": "HTTP/2 path after Reality; tune to match the front route.",
    "param.vless_http_tls.http_host.help.summary": "HTTP/2 Host after TLS; usually equals SNI/front.",
    "param.vless_http_tls.http_path.help.summary": "HTTP/2 path after TLS; tune to match the front route.",
    "param.vless_httpupgrade_reality.hu_host.help.summary": "HTTPUpgrade Host after Reality; align with Reality SNI.",
    "param.vless_httpupgrade_tls.hu_host.help.summary": "HTTPUpgrade Host after TLS; align with SNI/front.",
    "param.wg_awg2.mtu.help.summary": "WireGuard interface MTU; match path MTU / NAT expectations.",
    "param.wg_awg3.mtu.help.summary": "WireGuard interface MTU; match path MTU / NAT expectations.",
}

RU_PATCH: dict[str, str] = {
    "param.carrier_jitsi_sei_shared.room.help.summary": "Полный URL комнаты Jitsi Meet; нужен для SEI shared SFU.",
    "param.carrier_jitsi_sei_users.room.help.summary": "Полный URL комнаты Jitsi Meet; нужен для SEI per-user SFU.",
    "param.carrier_jitsi_shared.room.help.summary": "Полный URL комнаты Jitsi Meet для shared carrier underlay.",
    "param.carrier_jitsi_users.room.help.summary": "Полный URL комнаты Jitsi Meet для per-user carrier underlay.",
    "param.carrier_telemost_shared.room.help.summary": "Полный URL комнаты Яндекс Телемост для shared carrier underlay.",
    "param.carrier_telemost_users.room.help.summary": "Полный URL комнаты Яндекс Телемост для per-user carrier underlay.",
    "param.carrier_wbstream_shared.room.help.summary": "Полный URL комнаты WB Stream для shared carrier underlay.",
    "param.carrier_wbstream_users.room.help.summary": "Полный URL комнаты WB Stream для per-user carrier underlay.",
    "param.cloudflared_custom.token.help.summary": "Токен Cloudflare Tunnel из Zero Trust; поднимает named tunnel.",
    "param.cloudflared_token.token.help.summary": "Токен Cloudflare Tunnel из Zero Trust; поднимает named tunnel.",
    "param.hy2.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_custom.down_mbps.help.summary": "Лимит download (Mbps) в шаблонах Hy2 inbound/outbound.",
    "param.hy2_custom.up_mbps.help.summary": "Лимит upload (Mbps) в шаблонах Hy2 inbound/outbound.",
    "param.hy2_custom.masquerade_dir.help.summary": "Корень FS для file masquerade; обязателен при masquerade_mode=file.",
    "param.hy2_custom.masquerade_url.help.summary": "Upstream HTTPS URL для proxy masquerade при неверном Hy2 auth.",
    "param.hy2_gecko.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2_gecko.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_gecko_compact.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2_gecko_compact.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_gecko_masquerade.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2_gecko_masquerade.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_masquerade.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2_masquerade.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_salamander.down_mbps.help.summary": "Жёсткий лимит download (Mbps) на Hy2 link (server/client).",
    "param.hy2_salamander.up_mbps.help.summary": "Жёсткий лимит upload (Mbps) на Hy2 link (server/client).",
    "param.hy2_masquerade_file.masquerade_dir.help.summary": "Каталог HTTP-приманки при неверном Hy2 auth (file masquerade).",
    "param.hy2_realm.realm_id.help.summary": "Идентификатор realm от оператора Hy2 realm control plane.",
    "param.hy2_realm.realm_server_url.help.summary": "HTTPS URL оператора для регистрации этой Hy2 realm-ноды.",
    "param.vless_custom.packet_encoding.help.summary": "Кодирование UDP на outbound; xudp — обычный full-cone выбор.",
    "param.vless_custom.transport_host.help.summary": "Host/:authority для WS/HTTP; обычно совпадает с SNI/front.",
    "param.vless_custom.transport_path.help.summary": "Path после TLS для WS/HTTP; избегайте стоковых /ws и /vless.",
    "param.vless_http_reality.http_host.help.summary": "HTTP/2 Host после Reality; обычно равен Reality SNI.",
    "param.vless_http_reality.http_path.help.summary": "HTTP/2 path после Reality; подгоните под маршрут front.",
    "param.vless_http_tls.http_host.help.summary": "HTTP/2 Host после TLS; обычно равен SNI/front.",
    "param.vless_http_tls.http_path.help.summary": "HTTP/2 path после TLS; подгоните под маршрут front.",
    "param.vless_httpupgrade_reality.hu_host.help.summary": "HTTPUpgrade Host после Reality; выровняйте с Reality SNI.",
    "param.vless_httpupgrade_tls.hu_host.help.summary": "HTTPUpgrade Host после TLS; выровняйте с SNI/front.",
    "param.wg_awg2.mtu.help.summary": "MTU интерфейса WireGuard; согласуйте с path MTU / NAT.",
    "param.wg_awg3.mtu.help.summary": "MTU интерфейса WireGuard; согласуйте с path MTU / NAT.",
}


def load_flat(lang: str) -> dict[str, dict[str, str]]:
    """path -> key->value for all json under lang."""
    out: dict[str, dict[str, str]] = {}
    root = LOC / lang
    for f in root.rglob("*.json"):
        out[str(f.relative_to(root)).replace("\\", "/")] = json.loads(f.read_text(encoding="utf-8"))
    return out


def apply_patch(files: dict[str, dict[str, str]], patch: dict[str, str]) -> int:
    n = 0
    for key, val in patch.items():
        for path, data in files.items():
            if key in data:
                if data[key] != val:
                    data[key] = val
                    n += 1
                break
        else:
            # find by key prefix file heuristic: param.X -> presets
            pass
    return n


def write_flat(lang: str, files: dict[str, dict[str, str]]) -> None:
    root = LOC / lang
    for rel, data in files.items():
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def fanout_en_help(en_files: dict[str, dict[str, str]], langs: list[str]) -> None:
    en_keys = {}
    for data in en_files.values():
        en_keys.update(data)
    for lang in langs:
        if lang in ("en", "ru"):
            continue
        files = load_flat(lang)
        changed = False
        for rel, data in files.items():
            for k, v in list(data.items()):
                if k.endswith(".help.summary") and k in en_keys:
                    # Keep non-English if already longer/local; else take EN
                    if len(v) < 40 or v == en_keys.get(k.replace(".help.summary", ".title"), ""):
                        data[k] = en_keys[k]
                        changed = True
                    elif k in EN_PATCH:
                        data[k] = EN_PATCH[k]
                        changed = True
        if changed:
            write_flat(lang, files)


def main() -> None:
    en = load_flat("en")
    ru = load_flat("ru")
    n_en = apply_patch(en, EN_PATCH)
    n_ru = apply_patch(ru, RU_PATCH)
    write_flat("en", en)
    write_flat("ru", ru)
    fanout_en_help(en, sorted(d.name for d in LOC.iterdir() if d.is_dir()))
    print(f"patched en={n_en} ru={n_ru}")


if __name__ == "__main__":
    main()
