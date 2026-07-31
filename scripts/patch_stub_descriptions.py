# -*- coding: utf-8 -*-
"""Replace Controlplane preset stubs with technical EN/RU descriptions and re-run locale fan-out."""
from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
LOCALES = ROOT / "internal/controlplane/presets/i18n/locales"
DATA = ROOT / "internal/controlplane/presets/data"
STUB = re.compile(r"Controlplane preset")

# tag → (en_title, en_desc, ru_title, ru_desc)
PRESETS: dict[str, tuple[str, str, str, str]] = {
    "anytls_idle": (
        "AnyTLS idle",
        "AnyTLS with idle session timeout tuning. Same TLS wrapper; use when long-idle clients need keep-alive policy.",
        "AnyTLS idle",
        "AnyTLS с настройкой idle timeout. Та же TLS-обёртка; для клиентов с долгим простоем.",
    ),
    "anytls_custom": (
        "AnyTLS constructor",
        "Lab AnyTLS with optional uTLS fingerprint. Stock anytls is enough for demux TLS slots.",
        "AnyTLS конструктор",
        "Lab AnyTLS с опциональным uTLS fingerprint. Для demux TLS-слота обычно хватает anytls.",
    ),
    "carrier_jitsi_shared": (
        "Carrier Jitsi shared",
        "WG-star underlay via Jitsi SFU, shared auth. Requires room URL. Traffic looks like a video call, not a VPN handshake.",
        "Carrier Jitsi shared",
        "WG-star underlay через Jitsi SFU, shared auth. Нужен room URL. Трафик как видеозвонок, не как VPN handshake.",
    ),
    "carrier_jitsi_users": (
        "Carrier Jitsi users",
        "Jitsi underlay with per-user credentials. Stronger isolation; still needs a valid room URL.",
        "Carrier Jitsi users",
        "Jitsi underlay с учётками на пользователя. Жёстче изоляция; room URL обязателен.",
    ),
    "carrier_jitsi_sei_shared": (
        "Carrier Jitsi SEI shared",
        "Jitsi + SEI channel transport (shared). Use when datachannel path is filtered.",
        "Carrier Jitsi SEI shared",
        "Jitsi + SEI-канал (shared). Когда datachannel режут.",
    ),
    "carrier_jitsi_sei_users": (
        "Carrier Jitsi SEI users",
        "Jitsi + SEI with per-user creds. Same room requirement as other Carrier presets.",
        "Carrier Jitsi SEI users",
        "Jitsi + SEI с per-user creds. Room обязателен, как у остальных Carrier.",
    ),
    "carrier_telemost_shared": (
        "Carrier Telemost shared",
        "Yandex Telemost SFU underlay, shared auth. Room URL from Telemost; good when Jitsi is blocked.",
        "Carrier Telemost shared",
        "Underlay Яндекс Телемост, shared auth. Room из Телемоста; запасной путь, если Jitsi режут.",
    ),
    "carrier_telemost_users": (
        "Carrier Telemost users",
        "Telemost underlay with per-user credentials.",
        "Carrier Telemost users",
        "Телемост underlay с учётками на пользователя.",
    ),
    "carrier_wbstream_shared": (
        "Carrier WB Stream shared",
        "Wildberries stream SFU underlay (shared). Needs room + often token/key params.",
        "Carrier WB Stream shared",
        "Underlay WB stream SFU (shared). Нужен room; часто token/key.",
    ),
    "carrier_wbstream_users": (
        "Carrier WB Stream users",
        "WB stream underlay with per-user credentials.",
        "Carrier WB Stream users",
        "WB stream underlay с per-user creds.",
    ),
    "carrier_peer_shared": (
        "Carrier peer shared",
        "Direct peer underlay (self-host), shared auth. No public SFU — you run the peer endpoint.",
        "Carrier peer shared",
        "Прямой peer underlay (self-host), shared auth. Без публичного SFU — свой endpoint.",
    ),
    "carrier_peer_users": (
        "Carrier peer users",
        "Peer underlay with per-user credentials.",
        "Carrier peer users",
        "Peer underlay с учётками на пользователя.",
    ),
    "carrier_vk_shared": (
        "Carrier VK shared",
        "VK calls underlay, shared auth. Needs VK-specific room/hash params from the provider flow.",
        "Carrier VK shared",
        "Underlay VK звонков, shared auth. Нужны VK room/hash из провайдерского флоу.",
    ),
    "carrier_vk_users": (
        "Carrier VK users",
        "VK underlay with per-user credentials.",
        "Carrier VK users",
        "VK underlay с учётками на пользователя.",
    ),
    "carrier_custom": (
        "Carrier constructor",
        "Lab Carrier: pick provider/room/token knobs. Prefer a concrete SFU preset when possible.",
        "Carrier конструктор",
        "Lab Carrier: provider/room/token. Для боя берите конкретный SFU-пресет.",
    ),
    "cloudflared_token": (
        "Cloudflared token",
        "Cloudflare Tunnel inbound via token. Bypasses IP blocks; control and trust shift to Cloudflare.",
        "Cloudflared token",
        "Cloudflare Tunnel по token. Обход блокировок IP; контроль у Cloudflare.",
    ),
    "cloudflared_custom": (
        "Cloudflared constructor",
        "Minimal token constructor for Cloudflare Tunnel.",
        "Cloudflared конструктор",
        "Минимальный конструктор token для Cloudflare Tunnel.",
    ),
    "derp_tls": (
        "DERP TLS",
        "Tailscale-compatible DERP over TLS. Niche relay path, not a general DPI bypass.",
        "DERP TLS",
        "DERP (Tailscale-совместимый) поверх TLS. Нишевый relay, не общий anti-DPI.",
    ),
    "derp_ws": (
        "DERP WS",
        "DERP over WebSocket. Useful behind HTTP-only fronts; still a relay, not a stealth VPN.",
        "DERP WS",
        "DERP поверх WebSocket. За HTTP-only фронтом; всё ещё relay, не stealth VPN.",
    ),
    "derp_uot": (
        "DERP UoT",
        "DERP UDP-over-TCP mode. When raw UDP DERP is filtered.",
        "DERP UoT",
        "DERP UDP-over-TCP. Когда сырой UDP DERP режут.",
    ),
    "derp_custom": (
        "DERP constructor",
        "Lab DERP with path overrides. Prefer derp_tls for standard relay.",
        "DERP конструктор",
        "Lab DERP с path overrides. Для стандартного relay — derp_tls.",
    ),
    "http": (
        "HTTP CONNECT",
        "Utility HTTP CONNECT proxy. Not anti-DPI; local/service use.",
        "HTTP CONNECT",
        "Служебный HTTP CONNECT. Не anti-DPI; локальный/сервисный вход.",
    ),
    "http_tls": (
        "HTTP CONNECT TLS",
        "HTTP CONNECT behind TLS. Slightly less obvious than plain CONNECT; still not Reality-class.",
        "HTTP CONNECT TLS",
        "HTTP CONNECT за TLS. Чуть менее очевиден, чем plain CONNECT; не уровень Reality.",
    ),
    "http_tls_path": (
        "HTTP CONNECT TLS path",
        "HTTP CONNECT over TLS with path routing. For fronts that split by URL path.",
        "HTTP CONNECT TLS path",
        "HTTP CONNECT+TLS с path. Для фронтов, режущих по URL path.",
    ),
    "http_custom": (
        "HTTP constructor",
        "Lab HTTP CONNECT constructor. Utility only.",
        "HTTP конструктор",
        "Lab конструктор HTTP CONNECT. Только служебный.",
    ),
    "hy1": (
        "Hysteria1",
        "Legacy Hysteria v1 QUIC. Prefer Hysteria2 unless clients require hy1.",
        "Hysteria1",
        "Legacy Hysteria v1 QUIC. Берите Hy2, если нет жёсткой совместимости.",
    ),
    "hy1_obfs": (
        "Hysteria1 obfs",
        "Hy1 with obfuscation. Still legacy; migrate to Hy2 salamander/gecko when possible.",
        "Hysteria1 obfs",
        "Hy1 с обфускацией. Legacy; мигрируйте на Hy2 salamander/gecko.",
    ),
    "hysteria_custom": (
        "Hysteria1 constructor",
        "Lab hy1 constructor. Prefer hy2_custom for new stacks.",
        "Hysteria1 конструктор",
        "Lab конструктор hy1. Для новых стеков — hy2_custom.",
    ),
    "hy2_masquerade_proxy": (
        "Hy2 Masquerade proxy",
        "Hy2 fail-auth HTTP proxied to an upstream URL. Hides bad-password probes; does not change QUIC first-bytes.",
        "Hy2 Masquerade proxy",
        "Hy2: fail-auth HTTP проксируется на upstream. Прячет probing пароля; first-bytes QUIC не меняет.",
    ),
    "mieru_udp": (
        "Mieru UDP",
        "Mieru over UDP. Plain UDP class — demux only if you have a UDP plain strategy; usually TCP mieru for :443 stacks.",
        "Mieru UDP",
        "Mieru поверх UDP. Класс plain UDP — в demux :443 обычно берут mieru_tcp.",
    ),
    "mieru_custom": (
        "Mieru constructor",
        "Lab Mieru. Use mieru_tcp as demux always_plain slot.",
        "Mieru конструктор",
        "Lab Mieru. В full-stack demux — mieru_tcp (always_plain).",
    ),
    "mixed_auth": (
        "Mixed auth",
        "HTTP CONNECT + SOCKS5 with auth on one port. Local utility inbound.",
        "Mixed auth",
        "HTTP CONNECT + SOCKS5 с auth на одном порту. Служебный локальный вход.",
    ),
    "mixed_tls": (
        "Mixed TLS",
        "Mixed HTTP/SOCKS behind TLS. Utility; not a DPI strategy.",
        "Mixed TLS",
        "Mixed HTTP/SOCKS за TLS. Служебный; не стратегия DPI.",
    ),
    "mixed_custom": (
        "Mixed constructor",
        "Lab mixed inbound constructor.",
        "Mixed конструктор",
        "Lab конструктор mixed inbound.",
    ),
    "naive_tls": (
        "NaiveProxy TLS",
        "NaiveProxy over TLS/H2. Strong HTTPS camouflage; heavy Chromium/cronet stack.",
        "NaiveProxy TLS",
        "NaiveProxy поверх TLS/H2. Сильная HTTPS-маскировка; тяжёлый Chromium/cronet стек.",
    ),
    "naive_quic": (
        "NaiveProxy QUIC",
        "NaiveProxy over QUIC. Looks like normal H3; good UDP camouflage when TLS path is hot.",
        "NaiveProxy QUIC",
        "NaiveProxy поверх QUIC. Как обычный H3; UDP-маскировка, когда TLS-путь горячий.",
    ),
    "naive_custom": (
        "NaiveProxy constructor",
        "Lab Naive constructor. Prefer naive_tls/naive_quic stock presets.",
        "NaiveProxy конструктор",
        "Lab конструктор Naive. Обычно хватает naive_tls/naive_quic.",
    ),
    "shadowquic_jls": (
        "ShadowQUIC JLS",
        "ShadowQUIC with JLS SNI camouflage. Align jls_server_name with demux_sni or the front will fail.",
        "ShadowQUIC JLS",
        "ShadowQUIC + JLS SNI-камуфляж. jls_server_name должен совпадать с demux_sni, иначе handshake падает.",
    ),
    "shadowquic_0rtt": (
        "ShadowQUIC 0-RTT",
        "ShadowQUIC with 0-RTT. Faster resume; slightly different replay tradeoffs.",
        "ShadowQUIC 0-RTT",
        "ShadowQUIC с 0-RTT. Быстрее resume; другие tradeoff по replay.",
    ),
    "shadowquic_uot": (
        "ShadowQUIC UoT",
        "ShadowQUIC UDP-over-TCP variant when raw UDP is filtered.",
        "ShadowQUIC UoT",
        "ShadowQUIC UDP-over-TCP, если сырой UDP режут.",
    ),
    "shadowquic_custom": (
        "ShadowQUIC constructor",
        "Lab ShadowQUIC with JLS knobs. Prefer shadowquic_jls for demux QUIC substitutes.",
        "ShadowQUIC конструктор",
        "Lab ShadowQUIC с JLS. Для demux QUIC — shadowquic_jls.",
    ),
    "ss_aes128": (
        "SS AES-128-GCM",
        "Shadowsocks AEAD AES-128-GCM. Fast; easy DPI class without TLS wrappers.",
        "SS AES-128-GCM",
        "Shadowsocks AEAD AES-128-GCM. Быстрый; без TLS-обёртки легко классифицируется DPI.",
    ),
    "ss_aes256": (
        "SS AES-256-GCM",
        "Shadowsocks AEAD AES-256-GCM. Same DPI profile as AES-128; slightly heavier crypto.",
        "SS AES-256-GCM",
        "Shadowsocks AEAD AES-256-GCM. Тот же DPI-профиль, чуть тяжелее crypto.",
    ),
    "ss_chacha20": (
        "SS ChaCha20",
        "Shadowsocks AEAD ChaCha20-Poly1305. Better on mobile CPUs without AES-NI.",
        "SS ChaCha20",
        "Shadowsocks AEAD ChaCha20-Poly1305. Удобнее на мобильных без AES-NI.",
    ),
    "ss_aes128_mux": (
        "SS AES-128 + mux",
        "SS AES-128 with multiplex. Fewer handshakes; still plaintext SS to DPI.",
        "SS AES-128 + mux",
        "SS AES-128 с multiplex. Меньше handshake; для DPI всё ещё plain SS.",
    ),
    "ss_aes128_uot": (
        "SS AES-128 UoT",
        "SS AES-128 UDP-over-TCP. When UDP SS is filtered.",
        "SS AES-128 UoT",
        "SS AES-128 UDP-over-TCP. Когда UDP SS режут.",
    ),
    "ss_2022_aes128": (
        "SS 2022 AES-128",
        "Shadowsocks 2022 AES-128. Newer AEAD framing; still no TLS camouflage.",
        "SS 2022 AES-128",
        "Shadowsocks 2022 AES-128. Новее AEAD framing; TLS-камуфляжа нет.",
    ),
    "ss_2022_aes256": (
        "SS 2022 AES-256",
        "Shadowsocks 2022 AES-256. Same family as 2022 AES-128.",
        "SS 2022 AES-256",
        "Shadowsocks 2022 AES-256. То же семейство, что 2022 AES-128.",
    ),
    "ss_2022_chacha": (
        "SS 2022 ChaCha",
        "Shadowsocks 2022 ChaCha20. Prefer on ARM/mobile.",
        "SS 2022 ChaCha",
        "Shadowsocks 2022 ChaCha20. Предпочтительнее на ARM/мобильных.",
    ),
    "ss_2022_aes128_mux": (
        "SS 2022 AES-128 mux",
        "SS2022 AES-128 with multiplex.",
        "SS 2022 AES-128 mux",
        "SS2022 AES-128 с multiplex.",
    ),
    "shadowsocks_custom": (
        "Shadowsocks constructor",
        "Lab SS method/password constructor. Pick a concrete AEAD/2022 preset for production.",
        "Shadowsocks конструктор",
        "Lab конструктор method/password. В бою — конкретный AEAD/2022 пресет.",
    ),
    "shadowtls_v3": (
        "ShadowTLS v3",
        "ShadowTLS v3: TLS-mimic handshake + password. Set handshake_server to a plausible SNI target.",
        "ShadowTLS v3",
        "ShadowTLS v3: TLS-mimic handshake + password. handshake_server — правдоподобный SNI-таргет.",
    ),
    "shadowtls_v3_wildcard": (
        "ShadowTLS v3 wildcard",
        "ShadowTLS v3 with wildcard handshake matching. Broader SNI accept; easier misconfig.",
        "ShadowTLS v3 wildcard",
        "ShadowTLS v3 с wildcard handshake. Шире приём SNI; легче ошибиться в конфиге.",
    ),
    "shadowtls_v3_wildcard_all": (
        "ShadowTLS v3 wildcard-all",
        "Most permissive ShadowTLS wildcard mode. Lab/edge cases only.",
        "ShadowTLS v3 wildcard-all",
        "Самый permissive wildcard ShadowTLS. Только lab/edge.",
    ),
    "shadowtls_custom": (
        "ShadowTLS constructor",
        "Lab ShadowTLS with handshake_server override. Prefer shadowtls_v3.",
        "ShadowTLS конструктор",
        "Lab ShadowTLS с handshake_server. Обычно shadowtls_v3.",
    ),
    "snell_v5": (
        "Snell v5",
        "Snell v5 PSK. Plain TCP/UDP demux candidate; weak without TLS.",
        "Snell v5",
        "Snell v5 PSK. Кандидат demux plain; без TLS слабый DPI-профиль.",
    ),
    "snell_v6": (
        "Snell v6",
        "Snell v6 PSK. Prefer over v5 when clients support it.",
        "Snell v6",
        "Snell v6 PSK. Предпочтительнее v5 при поддержке клиентом.",
    ),
    "snell_custom": (
        "Snell constructor",
        "Lab Snell with obfs_host/fallback knobs.",
        "Snell конструктор",
        "Lab Snell с obfs_host/fallback.",
    ),
    "socks": (
        "SOCKS5",
        "Classic SOCKS5. Utility inbound / demux inject target — not anti-DPI.",
        "SOCKS5",
        "Классический SOCKS5. Служебный / цель demux inject — не anti-DPI.",
    ),
    "socks_uot": (
        "SOCKS5 UoT",
        "SOCKS5 with UDP-over-TCP. Utility when UDP associate is blocked.",
        "SOCKS5 UoT",
        "SOCKS5 с UDP-over-TCP. Когда UDP associate режут.",
    ),
    "socks_custom": (
        "SOCKS constructor",
        "Lab SOCKS5 constructor.",
        "SOCKS конструктор",
        "Lab конструктор SOCKS5.",
    ),
    "ssh_password": (
        "SSH password",
        "SSH inbound with password auth. Looks like SSH banner — demux plain, not web TLS.",
        "SSH password",
        "SSH inbound с password. Banner SSH — demux plain, не web TLS.",
    ),
    "ssh_pubkey": (
        "SSH pubkey",
        "SSH inbound with public-key auth. Same plain SSH fingerprint as password variant.",
        "SSH pubkey",
        "SSH inbound с pubkey. Тот же plain SSH fingerprint, что password-вариант.",
    ),
    "ssh_uot": (
        "SSH UoT",
        "SSH with UDP-over-TCP helper path.",
        "SSH UoT",
        "SSH с UDP-over-TCP.",
    ),
    "ssh_custom": (
        "SSH constructor",
        "Lab SSH with server_version banner override.",
        "SSH конструктор",
        "Lab SSH с подменой server_version banner.",
    ),
    "sudoku_pad": (
        "Sudoku pad",
        "Sudoku with padding obfuscation. Plain-TCP demux slot; shared key.",
        "Sudoku pad",
        "Sudoku с padding. Plain-TCP demux; shared key.",
    ),
    "sudoku_httpmask": (
        "Sudoku HTTP mask",
        "Sudoku with HTTP-looking mask. Still not TLS — first-bytes differ from Reality/Hy2.",
        "Sudoku HTTP mask",
        "Sudoku с HTTP-маской. Это не TLS — другой first-bytes класс, чем Reality/Hy2.",
    ),
    "sudoku_aes": (
        "Sudoku AES",
        "Sudoku AES mode. Stronger payload crypto inside the sudoku framing.",
        "Sudoku AES",
        "Sudoku AES. Жёстче crypto внутри sudoku framing.",
    ),
    "sudoku_custom": (
        "Sudoku constructor",
        "Lab Sudoku. Prefer sudoku_pad for demux plain slots.",
        "Sudoku конструктор",
        "Lab Sudoku. Для demux plain — sudoku_pad.",
    ),
    "trusttunnel_h2": (
        "TrustTunnel H2",
        "TrustTunnel over HTTP/2 TLS. TCP demux TLS substitute; anti-DPI oriented.",
        "TrustTunnel H2",
        "TrustTunnel поверх HTTP/2 TLS. TCP demux TLS-замена; anti-DPI.",
    ),
    "trusttunnel_h3": (
        "TrustTunnel H3",
        "TrustTunnel over HTTP/3 QUIC. UDP demux substitute next to Hy2/TUIC.",
        "TrustTunnel H3",
        "TrustTunnel поверх HTTP/3 QUIC. UDP demux рядом с Hy2/TUIC.",
    ),
    "trusttunnel_auto": (
        "TrustTunnel auto",
        "TrustTunnel auto H2/H3 selection. Convenient; less predictable demux role — pin h2/h3 when composing stacks.",
        "TrustTunnel auto",
        "TrustTunnel auto H2/H3. Удобно; для demux лучше фиксировать h2/h3.",
    ),
    "trusttunnel_custom": (
        "TrustTunnel constructor",
        "Lab TrustTunnel. Prefer trusttunnel_h2/h3 for demux roles.",
        "TrustTunnel конструктор",
        "Lab TrustTunnel. Для demux — trusttunnel_h2/h3.",
    ),
    "tuic": (
        "TUIC",
        "TUIC v5 over QUIC/TLS. Primary Hy2 substitute on UDP:443 demux.",
        "TUIC",
        "TUIC v5 поверх QUIC/TLS. Основная замена Hy2 на UDP:443 demux.",
    ),
    "tuic_0rtt": (
        "TUIC 0-RTT",
        "TUIC with 0-RTT. Faster resume; accept the 0-RTT security tradeoff.",
        "TUIC 0-RTT",
        "TUIC с 0-RTT. Быстрее resume; учитывайте security tradeoff 0-RTT.",
    ),
    "tuic_custom": (
        "TUIC constructor",
        "Lab TUIC. Prefer tuic / tuic_0rtt stock presets.",
        "TUIC конструктор",
        "Lab TUIC. Обычно tuic / tuic_0rtt.",
    ),
    "vmess_tcp": (
        "VMess TCP",
        "VMess without TLS. Lab/LAN only — trivial on public DPI.",
        "VMess TCP",
        "VMess без TLS. Только LAN/lab — на публичном DPI мгновенно виден.",
    ),
    "vmess_tls": (
        "VMess TLS",
        "VMess + classic TLS. Weaker probing resistance than VLESS+Reality.",
        "VMess TLS",
        "VMess + обычный TLS. Слабее против probing, чем VLESS+Reality.",
    ),
    "vmess_reality": (
        "VMess Reality",
        "VMess + Reality. Same Reality fingerprint idea; VMess AEAD framing inside.",
        "VMess Reality",
        "VMess + Reality. Тот же Reality fingerprint; внутри AEAD VMess.",
    ),
    "vmess_tls_mux": (
        "VMess TLS mux",
        "VMess+TLS with multiplex padding. Timing change only — not Reality-class.",
        "VMess TLS mux",
        "VMess+TLS с multiplex. Меняет timing, не даёт уровень Reality.",
    ),
    "vmess_ws_tls": (
        "VMess WS TLS",
        "VMess + TLS + WebSocket. CDN-friendly post-TLS; set ws_path/ws_host.",
        "VMess WS TLS",
        "VMess + TLS + WebSocket. CDN-friendly post-TLS; задайте ws_path/ws_host.",
    ),
    "vmess_ws_reality": (
        "VMess WS Reality",
        "VMess + Reality + WebSocket. Reality outside; path still needed for fronts.",
        "VMess WS Reality",
        "VMess + Reality + WebSocket. Reality снаружи; path нужен фронту.",
    ),
    "vmess_grpc_tls": (
        "VMess gRPC TLS",
        "VMess + TLS + gRPC/H2. Demux TLS slot candidate with distinct SNI.",
        "VMess gRPC TLS",
        "VMess + TLS + gRPC/H2. Кандидат demux TLS с отдельным SNI.",
    ),
    "vmess_grpc_reality": (
        "VMess gRPC Reality",
        "VMess + Reality + gRPC. H2 after Reality; tune service_name if filtered.",
        "VMess gRPC Reality",
        "VMess + Reality + gRPC. H2 после Reality; меняйте service_name при фильтрации.",
    ),
    "vmess_http_tls": (
        "VMess HTTP TLS",
        "VMess + TLS + HTTP/2 transport. Post-TLS H2 shape.",
        "VMess HTTP TLS",
        "VMess + TLS + HTTP/2 transport. Post-TLS картина H2.",
    ),
    "vmess_http_reality": (
        "VMess HTTP Reality",
        "VMess + Reality + HTTP transport.",
        "VMess HTTP Reality",
        "VMess + Reality + HTTP transport.",
    ),
    "vmess_httpupgrade_tls": (
        "VMess HUp TLS",
        "VMess + TLS + HTTPUpgrade. Single upgrade; path visible after TLS termination.",
        "VMess HUp TLS",
        "VMess + TLS + HTTPUpgrade. Один upgrade; path виден после TLS termination.",
    ),
    "vmess_httpupgrade_reality": (
        "VMess HUp Reality",
        "VMess + Reality + HTTPUpgrade.",
        "VMess HUp Reality",
        "VMess + Reality + HTTPUpgrade.",
    ),
    "vmess_quic_tls": (
        "VMess QUIC TLS",
        "VMess + TLS + QUIC transport (UDP). Not Hy2.",
        "VMess QUIC TLS",
        "VMess + TLS + QUIC transport (UDP). Не Hy2.",
    ),
    "vmess_custom": (
        "VMess constructor",
        "Lab VMess with transport/TLS/fingerprint knobs. Prefer VLESS for new Reality stacks.",
        "VMess конструктор",
        "Lab VMess с transport/TLS/fingerprint. Для новых Reality-стеков предпочтительнее VLESS.",
    ),
    "trojan_tls": (
        "Trojan TLS",
        "Password over TLS. HTTPS shape; without Reality weaker vs active probe than VLESS+Reality.",
        "Trojan TLS",
        "Пароль поверх TLS. HTTPS-картина; без Reality слабее против active probe, чем VLESS+Reality.",
    ),
}


def patch_locale_file(path: Path, lang: str) -> int:
    data = json.loads(path.read_text(encoding="utf-8"))
    n = 0
    for tag, (en_t, en_d, ru_t, ru_d) in PRESETS.items():
        title_k = f"preset.{tag}.title"
        desc_k = f"preset.{tag}.description"
        title = en_t if lang != "ru" else ru_t
        desc = en_d if lang != "ru" else ru_d
        # Always upgrade stubs; for ru/en also fill if missing
        cur = data.get(desc_k, "")
        if desc_k not in data or STUB.search(str(cur) or "") or (lang in ("en", "ru") and not str(cur).strip()):
            data[desc_k] = desc
            n += 1
        if title_k not in data or not str(data.get(title_k, "")).strip() or STUB.search(str(data.get(title_k, ""))):
            data[title_k] = title
            n += 1
    if n:
        path.write_text(json.dumps(dict(sorted(data.items())), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return n


def patch_data_inline_en() -> None:
    """Ensure invariant JSON en i18n is not empty so scanners stop inventing stubs."""
    for path in DATA.rglob("*.json"):
        if path.name in ("index.json", "protocol.json"):
            continue
        raw = json.loads(path.read_text(encoding="utf-8"))
        tag = raw.get("tag") or path.stem
        if tag not in PRESETS:
            continue
        en_t, en_d, ru_t, ru_d = PRESETS[tag]
        i18n = raw.setdefault("i18n", {})
        if not isinstance(i18n, dict):
            continue
        ru = i18n.setdefault("ru", {})
        en = i18n.setdefault("en", {})
        if isinstance(ru, dict):
            ru.setdefault("title", ru_t)
            if not str(ru.get("description", "")).strip():
                ru["description"] = ru_d
        if isinstance(en, dict):
            en["title"] = en_t
            en["description"] = en_d
        path.write_text(json.dumps(raw, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    patch_data_inline_en()
    total = 0
    for lang_dir in LOCALES.iterdir():
        if not lang_dir.is_dir():
            continue
        lang = lang_dir.name
        for path in (lang_dir / "presets").glob("*.json"):
            total += patch_locale_file(path, "ru" if lang == "ru" else "en" if lang == "en" else "en")
    # For non-en/ru: after setting EN text, re-run generate translations
    print(f"patched locale entries touches={total}")
    # Fix remaining stubs by scanning
    left = 0
    for path in (LOCALES / "en").rglob("*.json"):
        text = path.read_text(encoding="utf-8")
        left += len(STUB.findall(text))
    print(f"remaining Controlplane stub mentions in en: {left}")


if __name__ == "__main__":
    main()
