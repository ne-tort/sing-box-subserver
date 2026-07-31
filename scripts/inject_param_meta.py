#!/usr/bin/env python3
"""Inject param_meta (help + required_guide) into presets that declare param_fields."""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "internal" / "controlplane" / "presets" / "data"

ROOM_JITSI = {
    "title": {"en": "Room URL", "ru": "URL комнаты"},
    "description": {
        "en": "Video-call room link used by the Carrier transport.",
        "ru": "Ссылка на комнату видеозвонка для Carrier.",
    },
    "help": {
        "summary": {
            "en": "Paste the full URL of a Jitsi room.",
            "ru": "Вставьте полный URL комнаты Jitsi.",
        },
        "input_hint": {
            "en": "Create a room on meet.jit.si and copy the link.",
            "ru": "Создайте комнату на meet.jit.si и скопируйте ссылку.",
        },
        "format": "https://meet.jit.si/MyRoom",
    },
    "required_guide": {
        "title": {"en": "How to get a room URL", "ru": "Как получить URL комнаты"},
        "steps": [
            {
                "text": {"en": "Open Jitsi Meet", "ru": "Откройте Jitsi Meet"},
                "url": "https://meet.jit.si",
            },
            {
                "text": {
                    "en": "Create a video-call room",
                    "ru": "Создайте комнату видеозвонка",
                }
            },
            {
                "text": {
                    "en": "Copy the room link and paste it into «Room URL»",
                    "ru": "Скопируйте ссылку комнаты и вставьте в «URL комнаты»",
                }
            },
            {
                "text": {
                    "en": "Example format: https://meet.jit.si/MyRoom",
                    "ru": "Пример формата: https://meet.jit.si/MyRoom",
                }
            },
        ],
    },
}

ROOM_TELEMOST = {
    "title": {"en": "Room URL", "ru": "URL комнаты"},
    "description": {
        "en": "Yandex Telemost room link used by the Carrier transport.",
        "ru": "Ссылка на комнату Яндекс Телемост для Carrier.",
    },
    "help": {
        "summary": {
            "en": "Paste the full Telemost room URL.",
            "ru": "Вставьте полный URL комнаты Телемост.",
        },
        "input_hint": {
            "en": "Create a room in Telemost and copy the link.",
            "ru": "Создайте комнату в Телемосте и скопируйте ссылку.",
        },
        "format": "https://telemost.yandex.ru/j/…",
    },
    "required_guide": {
        "title": {
            "en": "How to get a Telemost room URL",
            "ru": "Как получить URL комнаты Телемост",
        },
        "steps": [
            {
                "text": {
                    "en": "Open Yandex Telemost",
                    "ru": "Откройте Яндекс Телемост",
                },
                "url": "https://telemost.yandex.ru",
            },
            {
                "text": {
                    "en": "Create a video-call room",
                    "ru": "Создайте комнату видеозвонка",
                }
            },
            {
                "text": {
                    "en": "Copy the room link and paste it into «Room URL»",
                    "ru": "Скопируйте ссылку комнаты и вставьте в «URL комнаты»",
                }
            },
        ],
    },
}

ROOM_WBSTREAM = {
    "title": {"en": "Room URL", "ru": "URL комнаты"},
    "description": {
        "en": "WB Stream room / broadcast URL used by the Carrier transport.",
        "ru": "URL комнаты / трансляции WB Stream для Carrier.",
    },
    "help": {
        "summary": {
            "en": "Paste the full WB Stream room URL.",
            "ru": "Вставьте полный URL комнаты WB Stream.",
        },
        "input_hint": {
            "en": "Copy the room URL from the browser address bar.",
            "ru": "Скопируйте URL комнаты из адресной строки браузера.",
        },
        "format": "https://…/room/…",
    },
    "required_guide": {
        "title": {
            "en": "How to get a WB Stream room URL",
            "ru": "Как получить URL комнаты WB Stream",
        },
        "steps": [
            {
                "text": {
                    "en": "Open the WB Stream room / broadcast page",
                    "ru": "Откройте страницу комнаты / трансляции WB Stream",
                }
            },
            {
                "text": {
                    "en": "Copy the full room URL from the browser address bar",
                    "ru": "Скопируйте полный URL комнаты из адресной строки браузера",
                }
            },
            {
                "text": {
                    "en": "Paste it into «Room URL»",
                    "ru": "Вставьте его в «URL комнаты»",
                }
            },
        ],
    },
}

TOKEN = {
    "title": {"en": "Tunnel token", "ru": "Токен туннеля"},
    "description": {
        "en": "Cloudflare Tunnel token from Zero Trust.",
        "ru": "Токен Cloudflare Tunnel из Zero Trust.",
    },
    "help": {
        "summary": {
            "en": "Paste the tunnel token from Cloudflare.",
            "ru": "Вставьте токен туннеля из Cloudflare.",
        },
        "input_hint": {
            "en": "Create a tunnel and copy the token string.",
            "ru": "Создайте туннель и скопируйте строку токена.",
        },
        "format": "eyJhIjoi…",
    },
    "required_guide": {
        "title": {
            "en": "How to get a tunnel token",
            "ru": "Как получить токен туннеля",
        },
        "steps": [
            {
                "text": {
                    "en": "Open Cloudflare Zero Trust",
                    "ru": "Откройте Cloudflare Zero Trust",
                },
                "url": "https://one.dash.cloudflare.com",
            },
            {
                "text": {
                    "en": "Go to Networks → Tunnels",
                    "ru": "Перейдите в Networks → Tunnels",
                }
            },
            {
                "text": {
                    "en": "Create a Cloudflare Tunnel",
                    "ru": "Создайте Cloudflare Tunnel",
                }
            },
            {
                "text": {
                    "en": "Copy the tunnel token",
                    "ru": "Скопируйте токен туннеля",
                }
            },
            {
                "text": {
                    "en": "Paste it into «Tunnel token»",
                    "ru": "Вставьте его в «Токен туннеля»",
                }
            },
        ],
    },
}

MASQ = {
    "title": {"en": "Masquerade directory", "ru": "Каталог masquerade"},
    "description": {
        "en": "Absolute path to static files served as Hy2 file masquerade.",
        "ru": "Абсолютный путь к статике для Hy2 file masquerade.",
    },
    "help": {
        "summary": {
            "en": "Absolute path to a directory served as file masquerade.",
            "ru": "Абсолютный путь к каталогу, который отдаётся как masquerade.",
        },
        "input_hint": {
            "en": "Directory must exist on the VPS and be readable.",
            "ru": "Каталог должен существовать на VPS и быть читаемым.",
        },
        "format": "/var/www/html",
    },
    "required_guide": {
        "title": {
            "en": "How to set masquerade directory",
            "ru": "Как указать каталог masquerade",
        },
        "steps": [
            {
                "text": {
                    "en": "On the VPS, pick a directory with static files (HTML/CSS/images)",
                    "ru": "На VPS выберите каталог со статическими файлами (HTML/CSS/изображения)",
                }
            },
            {
                "text": {
                    "en": "Ensure the subserver process can read that path",
                    "ru": "Убедитесь, что процесс subserver может читать этот путь",
                }
            },
            {
                "text": {
                    "en": "Paste the absolute path into «Masquerade directory»",
                    "ru": "Вставьте абсолютный путь в «Каталог masquerade»",
                }
            },
            {
                "text": {"en": "Example: /var/www/html", "ru": "Пример: /var/www/html"}
            },
        ],
    },
}

REALM_URL = {
    "title": {"en": "Realm server URL", "ru": "URL realm-сервера"},
    "description": {
        "en": "Base HTTPS URL of the Hysteria realm control plane.",
        "ru": "Базовый HTTPS URL панели Hysteria realm.",
    },
    "help": {
        "summary": {
            "en": "Base URL of the realm control plane.",
            "ru": "Базовый URL панели управления realm.",
        },
        "input_hint": {
            "en": "HTTPS URL without trailing path clutter.",
            "ru": "HTTPS URL без лишнего хвоста пути.",
        },
        "format": "https://realm.example.com",
    },
    "required_guide": {
        "title": {
            "en": "How to set realm server URL",
            "ru": "Как указать URL realm-сервера",
        },
        "steps": [
            {
                "text": {
                    "en": "Open your Hysteria realm control-plane dashboard",
                    "ru": "Откройте панель управления Hysteria realm",
                }
            },
            {
                "text": {
                    "en": "Copy the base HTTPS URL of the realm server",
                    "ru": "Скопируйте базовый HTTPS URL realm-сервера",
                }
            },
            {
                "text": {
                    "en": "Paste it into «Realm server URL»",
                    "ru": "Вставьте его в «URL realm-сервера»",
                }
            },
        ],
    },
}

REALM_ID = {
    "title": {"en": "Realm ID", "ru": "ID realm"},
    "description": {
        "en": "Realm identifier from the control plane.",
        "ru": "Идентификатор realm из панели управления.",
    },
    "help": {
        "summary": {
            "en": "Realm identifier assigned by the control plane.",
            "ru": "Идентификатор realm, выданный панелью.",
        },
        "input_hint": {
            "en": "Copy the id from the realm dashboard.",
            "ru": "Скопируйте id из панели realm.",
        },
        "format": "realm-xxxxxxxx",
    },
    "required_guide": {
        "title": {"en": "How to set realm ID", "ru": "Как указать ID realm"},
        "steps": [
            {
                "text": {
                    "en": "In the realm dashboard, open the target realm",
                    "ru": "В панели realm откройте нужный realm",
                }
            },
            {
                "text": {
                    "en": "Copy the realm id / identifier",
                    "ru": "Скопируйте id / идентификатор realm",
                }
            },
            {
                "text": {
                    "en": "Paste it into «Realm ID»",
                    "ru": "Вставьте его в «ID realm»",
                }
            },
        ],
    },
}


def room_meta(traits: list[str]) -> dict:
    if "telemost" in traits:
        return ROOM_TELEMOST
    if "wbstream" in traits:
        return ROOM_WBSTREAM
    return ROOM_JITSI


def build_param_meta(data: dict) -> dict:
    fields = data.get("param_fields") or []
    traits = data.get("traits") or []
    out: dict = {}
    for f in fields:
        if f == "room":
            out[f] = room_meta(traits)
        elif f == "token":
            out[f] = TOKEN
        elif f == "masquerade_dir":
            out[f] = MASQ
        elif f == "realm_server_url":
            out[f] = REALM_URL
        elif f == "realm_id":
            out[f] = REALM_ID
        else:
            raise SystemExit(f"unknown param field {f} in {data.get('tag')}")
    return out


def main() -> None:
    updated = 0
    for path in sorted(ROOT.rglob("*.json")):
        if path.name in {"index.json", "protocol.json"}:
            continue
        data = json.loads(path.read_text(encoding="utf-8"))
        if not data.get("param_fields"):
            continue
        data["param_meta"] = build_param_meta(data)
        path.write_text(
            json.dumps(data, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        updated += 1
        print(f"updated {path.relative_to(ROOT)} -> {list(data['param_meta'])}")
    print(f"done: {updated} presets")


if __name__ == "__main__":
    main()
