#!/usr/bin/env python3
"""Rewrite carrier preset catalog: peer/vk self-host + SFU no-reg (jitsi/telemost/wbstream)."""
from __future__ import annotations

import json
from pathlib import Path

BASE = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data" / "carrier"


def save(name: str, obj: dict) -> None:
    p = BASE / name
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("wrote", p.name)


LIFECYCLE = {
    "enabled": True,
    "keepalive": True,
    "liveness_interval": "15s",
    "liveness_timeout": "5s",
    "liveness_failures": 3,
    "reconnect_backoff": "2s",
    "reconnect_backoff_max": "30s",
}


def demux_raw(compatible: bool = False) -> dict:
    return {
        "network": ["tcp"],
        "looks_like": "raw_tcp",
        "alpn": [],
        "sni_required": False,
        "first_bytes": "carrier underlay",
        "compatible_with_demux": compatible,
    }


def base(
    tag: str,
    aliases: list[str],
    short: str,
    desc: str,
    traits: list[str],
    creds: list[str],
    *,
    param_fields: list[str] | None = None,
    peer_secret: bool = True,
    scores: dict | None = None,
    notes: dict | None = None,
    inbound: dict,
    outbound: dict,
) -> dict:
    obj = {
        "schema_version": 1,
        "tag": tag,
        "protocol": "carrier",
        "aliases": aliases,
        "short_name": short,
        "status": "lab",
        "i18n": {"ru": {"title": short, "description": desc}},
        "traits": traits,
        "demux_hints": demux_raw("needs_listen" in traits),
        "scores": scores or {"dpi": 5, "speed": 6, "mobile": 3, "setup": 4},
        "cred_fields": creds,
        "inbound_template": inbound,
        "outbound_template": outbound,
        "requirements": {"build_tag": "with_carrier"},
        "client_notes": notes or {},
    }
    if peer_secret:
        obj["peer_secret_fields"] = {"password": "password"}
    if param_fields:
        obj["param_fields"] = param_fields
    return obj


def main() -> None:
    save(
        "protocol.json",
        {
            "schema_version": 1,
            "tag": "carrier",
            "singbox_type": "carrier",
            "short_name": "Carrier",
            "status": "lab",
            "i18n": {
                "ru": {
                    "title": "Carrier",
                    "description": "WG-star поверх underlay: peer/VK self-host + SFU (jitsi/telemost/wbstream) без регистрации на join.",
                },
                "en": {
                    "title": "Carrier",
                    "description": "WG-star underlay: peer/VK + SFU join-without-account.",
                },
            },
            "default_cred_fields": ["device_id", "secret"],
            "notes": {
                "params": "SFU presets require bindings[].params.room (meeting URL). Optional: token, transport, key.",
                "auth": "shared_auth → inbound без users; users_auth → users[]{device_id,secret}, outbound password=secret.",
            },
        },
    )

    # --- peer self-host ---
    save(
        "carrier_peer_shared.json",
        base(
            "carrier_peer_shared",
            ["carrier", "carrier-peer", "carrier-peer-shared"],
            "Carrier peer shared",
            "Self-host TCP peer underlay + shared password; client device_id. link.peer ← set listen:port.",
            ["tcp", "shared_auth", "wg_star", "needs_listen", "self_host"],
            ["device_id"],
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "peer",
                "auth": "shared",
                "link": {"peer": "{{listen}}:{{listen_port}}", "password": "{{peer.password}}"},
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "peer",
                "link": {
                    "peer": "{{server}}:{{listen_port}}",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": LIFECYCLE,
            },
            notes={"peer": "self-host: inbound listens link.peer; outbound dials publicHost:listen_port"},
        ),
    )
    save(
        "carrier_peer_users.json",
        base(
            "carrier_peer_users",
            ["carrier-peer-users"],
            "Carrier peer users",
            "Self-host peer + auth=users (device_id/secret ACL).",
            ["tcp", "users_auth", "wg_star", "needs_listen", "self_host"],
            ["device_id", "secret"],
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "peer",
                "auth": "users",
                "users": [],
                "link": {"peer": "{{listen}}:{{listen_port}}", "password": "{{peer.password}}"},
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "peer",
                "link": {
                    "peer": "{{server}}:{{listen_port}}",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": LIFECYCLE,
            },
        ),
    )

    # --- VK self-host WRAP ---
    save(
        "carrier_vk_shared.json",
        base(
            "carrier_vk_shared",
            ["carrier-vk", "carrier-vk-shared"],
            "Carrier VK shared",
            "Self-host VK WRAP/DTLS (не vk.ru/call). server/port ← set; опц. params.vk_hash.",
            ["udp", "shared_auth", "wg_star", "needs_listen", "self_host", "vk"],
            ["device_id"],
            param_fields=None,
            scores={"dpi": 6, "speed": 5, "mobile": 4, "setup": 3},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "vk",
                "auth": "shared",
                "link": {
                    "server": "0.0.0.0",
                    "server_port": "{{listen_port}}",
                    "password": "{{peer.password}}",
                    "vk_hash": "{{param.vk_hash}}",
                    "wrap_password": "{{param.wrap_password}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "vk",
                "link": {
                    "server": "{{server}}",
                    "server_port": "{{listen_port}}",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                    "vk_hash": "{{param.vk_hash}}",
                    "wrap_password": "{{param.wrap_password}}",
                },
                "lifecycle": LIFECYCLE,
            },
            notes={
                "vk": "userspace WRAP inbound; params.vk_hash = user:pass@turn-host:port (опц.)",
            },
        ),
    )
    save(
        "carrier_vk_users.json",
        base(
            "carrier_vk_users",
            ["carrier-vk-users"],
            "Carrier VK users",
            "VK WRAP self-host + auth=users; outbound password=user.secret, WRAP=peer/wrap_password.",
            ["udp", "users_auth", "wg_star", "needs_listen", "self_host", "vk"],
            ["device_id", "secret"],
            scores={"dpi": 6, "speed": 5, "mobile": 4, "setup": 3},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "listen": "{{listen}}",
                "listen_port": 0,
                "provider": "vk",
                "auth": "users",
                "users": [],
                "link": {
                    "server": "0.0.0.0",
                    "server_port": "{{listen_port}}",
                    "password": "{{peer.password}}",
                    "vk_hash": "{{param.vk_hash}}",
                    "wrap_password": "{{param.wrap_password}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "vk",
                "link": {
                    "server": "{{server}}",
                    "server_port": "{{listen_port}}",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                    "vk_hash": "{{param.vk_hash}}",
                    "wrap_password": "{{peer.password}}",
                },
                "lifecycle": LIFECYCLE,
            },
        ),
    )

    # --- Jitsi SFU (cloud meet.jit.si OR self-host URL in params.room) ---
    save(
        "carrier_jitsi_shared.json",
        base(
            "carrier_jitsi_shared",
            ["carrier-jitsi", "carrier-jitsi-shared"],
            "Carrier Jitsi shared",
            "SFU Jitsi: join без учётки. params.room=https://host/Room (meet.jit.si или self-host).",
            ["tcp", "shared_auth", "wg_star", "sfu", "no_registration", "jitsi"],
            ["device_id"],
            param_fields=["room"],
            scores={"dpi": 7, "speed": 5, "mobile": 4, "setup": 5},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "jitsi",
                "auth": "shared",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "datachannel",
                    "password": "{{peer.password}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "jitsi",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "datachannel",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
            notes={
                "room": "обязателен в bindings[].params.room; self-host = свой Jitsi host в URL",
                "transport": "datachannel|seichannel (vp8channel запрещён для jitsi)",
            },
        ),
    )
    save(
        "carrier_jitsi_users.json",
        base(
            "carrier_jitsi_users",
            ["carrier-jitsi-users"],
            "Carrier Jitsi users",
            "Jitsi SFU + auth=users; params.room обязателен.",
            ["tcp", "users_auth", "wg_star", "sfu", "no_registration", "jitsi"],
            ["device_id", "secret"],
            param_fields=["room"],
            scores={"dpi": 7, "speed": 5, "mobile": 4, "setup": 4},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "jitsi",
                "auth": "users",
                "users": [],
                "link": {
                    "room": "{{param.room}}",
                    "transport": "datachannel",
                    "password": "{{peer.password}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "jitsi",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "datachannel",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
        ),
    )

    # --- Telemost (guest join, meeting must already exist) ---
    save(
        "carrier_telemost_shared.json",
        base(
            "carrier_telemost_shared",
            ["carrier-telemost", "carrier-telemost-shared"],
            "Carrier Telemost",
            "Yandex Telemost: guest join без Яндекс-логина. params.room=https://telemost.yandex.ru/j/<id>.",
            ["tcp", "shared_auth", "wg_star", "sfu", "no_registration", "telemost"],
            ["device_id"],
            param_fields=["room"],
            scores={"dpi": 7, "speed": 5, "mobile": 5, "setup": 4},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "telemost",
                "auth": "shared",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "telemost",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": LIFECYCLE,
            },
            notes={"room": "встречу создаёт ops в UI Telemost; CP только join по URL"},
        ),
    )
    save(
        "carrier_telemost_users.json",
        base(
            "carrier_telemost_users",
            ["carrier-telemost-users"],
            "Carrier Telemost users",
            "Telemost + auth=users; params.room обязателен.",
            ["tcp", "users_auth", "wg_star", "sfu", "no_registration", "telemost"],
            ["device_id", "secret"],
            param_fields=["room"],
            scores={"dpi": 7, "speed": 5, "mobile": 5, "setup": 3},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "telemost",
                "auth": "users",
                "users": [],
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "telemost",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                },
                "lifecycle": LIFECYCLE,
            },
        ),
    )

    # --- WB Stream guest ---
    save(
        "carrier_wbstream_shared.json",
        base(
            "carrier_wbstream_shared",
            ["carrier-wbstream", "carrier-wbstream-shared"],
            "Carrier WB Stream",
            "WB Stream: guest-register join. params.room=URL комнаты; опц. params.token.",
            ["tcp", "shared_auth", "wg_star", "sfu", "no_registration", "wbstream"],
            ["device_id"],
            param_fields=["room"],
            scores={"dpi": 6, "speed": 5, "mobile": 4, "setup": 3},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "wbstream",
                "auth": "shared",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                    "token": "{{param.token}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "wbstream",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                    "device_id": "{{user.device_id}}",
                    "token": "{{param.token}}",
                    "key": "{{param.key}}",
                },
                "lifecycle": LIFECYCLE,
            },
            notes={"token": "опц. bindings[].params.token; без token — guest-register path"},
        ),
    )
    save(
        "carrier_wbstream_users.json",
        base(
            "carrier_wbstream_users",
            ["carrier-wbstream-users"],
            "Carrier WB users",
            "WB Stream + auth=users; params.room обязателен.",
            ["tcp", "users_auth", "wg_star", "sfu", "no_registration", "wbstream"],
            ["device_id", "secret"],
            param_fields=["room"],
            scores={"dpi": 6, "speed": 5, "mobile": 4, "setup": 3},
            inbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "wbstream",
                "auth": "users",
                "users": [],
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{peer.password}}",
                    "token": "{{param.token}}",
                },
                "lifecycle": LIFECYCLE,
            },
            outbound={
                "type": "carrier",
                "tag": "{{tag}}",
                "provider": "wbstream",
                "link": {
                    "room": "{{param.room}}",
                    "transport": "vp8channel",
                    "password": "{{user.secret}}",
                    "device_id": "{{user.device_id}}",
                    "token": "{{param.token}}",
                },
                "lifecycle": LIFECYCLE,
            },
        ),
    )


if __name__ == "__main__":
    main()
