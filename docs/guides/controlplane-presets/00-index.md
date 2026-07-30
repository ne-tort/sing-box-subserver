# Controlplane presets (operator)

Файловый каталог протоколов и инвариантов для embedded controlplane.

Отдельно от design-ladder (`docs/controlplane/`). Здесь — как устроен каталог, API и как добавлять инварианты.

| File | Content |
|------|---------|
| [01-structure.md](01-structure.md) | Папки, naming, status |
| [02-api.md](02-api.md) | HTTP `protocols` / `presets` + `lang` |
| [03-scores.md](03-scores.md) | Шкалы DPI / speed |
| [04-adding-invariant.md](04-adding-invariant.md) | Чеклист изучения синтаксиса |
| [05-priority.md](05-priority.md) | Приоритет протоколов и матрица variants |
| [06-invariant-matrix.md](06-invariant-matrix.md) | Docker+iperf чеклист / harness |
| [07-demux-groups.md](07-demux-groups.md) | Demux-группы: каталог, API, SNI/Reality, Docker matrix |

Данные: `internal/controlplane/presets/data/`.
Harness: `scripts/invariant_matrix/`, `scripts/demux_groups_matrix/`.
