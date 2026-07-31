# 09 — Custom presets (`{protocol}_custom`)

## Модель

Один инвариант на протокол: `tag = {protocol}_custom` (исключения: `hy2_custom`, `wg_custom` — короткие алиасы каталога).

Поля:

- `custom_preset: true`
- `status: lab` до пилота, затем `stable`
- полный `param_meta` / `param_fields` / `optional_param_fields`
- один `inbound_template` / `outbound_template` (или `endpoint_template`) с `{{param.*}}`
- materialize генерирует только creds/keys/paths; выборы пользователя — из `bindings[].params`

## Schema → UI

Клиент рисует форму из `params_schema` (version 2):

| Поле schema | UI |
|-------------|-----|
| `type` / `widget` | text / select / toggle / port |
| `enum` + `enum_labels` | bottom sheet / radio |
| `ui_group` + `ui_order` | секции и сортировка |
| `visible_when` | скрыть поле |
| `conflicts_with` | banner + server reject `cp_param_conflict` |
| `requires` | server reject `cp_param_requires` |
| `required_guide` | banner со steps+urls |
| `default` | prefill до validate |

Неизвестные ключи клиент игнорирует (forward-compat).

## Валидация

`paramvalidate.Validate` вызывается из `sets/from-presets` / install path до materialize. Коды: `cp_param_required|type|enum|range|conflict|requires`.

Defaults из `param_meta.default` подставляются перед validate.

## Пилот

`vless_custom` — эталон конструктора. Остальные протоколы копируют каркас (минимальный custom уже в каталоге).
