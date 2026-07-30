# 04 — Adding an invariant

1. Выберите папку протокола (или добавьте `protocol.json` + запись в `index.json`).
2. Изучите inbound/outbound options в sing-box-lx для типа (`option/*`, docs-lx).
3. Скопируйте соседний `*.json`; задайте:
   - `tag`, `short_name`, `aliases` (если миграция)
   - `i18n.ru` (кратко: чем отличается от siblings)
   - `traits`, `demux_hints` (что видно **до** TLS terminate)
   - `scores` (см. [03-scores.md](03-scores.md))
   - `requirements` (`tls_profile`, `reality_assignment`, …)
   - `cred_fields`, `inbound_template`, `outbound_template`
4. Placeholders: `{{tag}}`, `{{listen}}`, `{{server}}`, `{{user.uuid}}`, …
5. `go test -tags with_controlplane ./internal/controlplane/presets/`
6. Проверьте API: `GET /v1/controlplane/presets/{tag}?lang=ru`
7. Materialize: set с новым tag или alias + activate

Не коммитьте `status: stable` без рабочих templates и smoke.
