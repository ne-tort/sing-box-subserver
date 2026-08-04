# AmneziaWG signatures bank (from amnezia-wg-easy)

Local copy of research / seed artefacts from
[ne-tort/amnezia-wg-easy](https://github.com/ne-tort/amnezia-wg-easy):

| File | Role |
|------|------|
| `signatures.seed.json` | Real UDP capture dumps as Amnezia CPS (`i1`–`i5`), ~10 variants × 11 protocols |
| `junk-ranges.seed.json` | Jc/Jmin/Jmax/S* ranges calibrated per protocol cluster |
| `fixtures_*.json` | CI templates with `<r>`, `<rc>`, `<t>` (not shipped as the production bank) |
| `signaturesBank.js` / `masqueradeBank.js` | Upstream helpers (`masqueradeBank` is Hysteria HTTPS mirrors — unrelated to AWG CPS) |

Canonical embeds used at runtime:

- Go agent: `../data/signatures.seed.json` + `../data/junk-ranges.seed.json` (`//go:embed`)
- Flutter client stays thin: calls `POST /v1/controlplane/wg/regenerate-awg` (no local bank)

## CPS format (official Amnezia / sing-box-lx)

amneziawg-go `newObfChain` accepts a **sequence of tags**, not one byte per tag:

- `<b 0xHEXBLOB>` — static bytes (**one** tag for the whole blob)
- `<r N>` / `<rc N>` / `<rd N>` — fresh entropy at send time
- `<t>` — 4-byte unix timestamp

Bank seed entries are almost always a single `<b 0x…>` per slot (frozen capture).
lx sugar masquerade (`id`/`ip`/`ib`) builds richer CPS at runtime (QUIC Initial +
ClientHello, STUN Binding, DNS query, SIP dialog) and is mutually exclusive with
explicit `i1`.

## Masquerade (lx) vs bank dumps

| | lx `ip=quic\|dns\|stun\|sip` | Bank manual `i1`–`i5` |
|--|------------------------------|------------------------|
| Fidelity | Structurally correct protocol builders | Real capture bytes |
| Entropy | Fresh every send via `<r>`/`<rc>`/`<t>` | Static unless we rewrite |
| Coverage | 4 protocols | + dtls, ntp, webrtc, quic_browser, … |
| Fingerprint risk | Low (generated) | High if dump replayed byte-identical |

**Recommendation:** keep lx sugar for masquerade modes; use the bank only for
`masquerade=none` (manual CPS). Our generator picks a bank variant, then rewrites
mutable regions (DNS TXID, STUN TXID, NTP timestamp, QUIC DCID/body, …) into
`<r>`/`<t>` tags so the dump size/shape stays while the fingerprint rotates.
