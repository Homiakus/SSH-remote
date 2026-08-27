# Полный анализ документации: HydroPilot v5 + ECA-библиотека

Дата: 2026-05-04

---

## Часть 1. Обзор проекта

### 1.1 Структура проекта

```
./
├── go.mod              → модуль hydropilot-v5, Go 1.26.0
├── go.sum
├── cmd/hydropilot/main.go      → точка входа (CLI: server, workspace validate, healthcheck)
├── internal/
│   ├── app/app.go              → оркестрация: загрузка конфига, регистрация handler-ов,
│   │                              материализация workspace, запуск engine, scheduler, dashboard
│   ├── config/config.go        → конфигурация (TOML): app, http, uart, sqlite, scheduler, auth, ml_dosing, zones
│   ├── handlers/handlers.go    → 17 зарегистрированных Go-handler-ов
│   ├── workspace/workspace.go  → генерация data.toml + actions/*.toml из Go-структур
│   ├── dashboard/              → HTTP/SSE дашборд
│   ├── persistence/            → SQLite (measurements, alerts, uart traces, calibration, snapshots)
│   ├── scheduler/              → периодические тики для измерений
│   └── uart/                   → транспорт (mock/real) для общения с контроллером
├── pkg/mldosing/               → ML-библиотека расчёта доз (predictor, planner, safety guard)
├── rules/                      → сгенерированный workspace (data.toml + actions/*.toml)
├── config/hydropilot.toml      → конфигурация по умолчанию
├── scripts/                    → Python-скрипты (train_catboost.py, evaluate_model.py)
├── ECA-lib/                    → ECA-библиотека (go-library-fsm), подключается через go.mod replace
│   ├── engine.go, fact.go, action.go, ...   → ядро
│   ├── internal/{bus, store, index, compiler, agenda} → внутренние подсистемы
│   ├── docs/                   → 9 документов
│   └── README.md
└── docs/                       → проектная документация Hydropilot
    ├── README.md, api-reference.md, legacy_exclusions.md, ...
    ├── 04_handlers/            → документация handler-ов
    └── 02_modules/             → документация модулей
```

### 1.2 Технологический стек

| Компонент | Технология |
|-----------|-----------|
| Язык | Go 1.26.0 |
| ECA-движок | `go-library-fsm` (in-house, индексированный Event-Condition-Action runtime) |
| Конфигурация | TOML (BurntSushi/toml) |
| База данных | SQLite (modernc.org/sqlite, pure Go) |
| UART | go.bug.st/serial |
| Dashboard | HTTP/SSE (встроенный net/http) |
| ML | CatBoost (C API через cgo) или Python gRPC |
| Выражения | expr-lang/expr (парсинг/компиляция when-условий) |

---

## Часть 2. ECA-библиотека: полный анализ документации

### 2.1 Карта документации ECA-lib

| Документ | Строк | Назначение | Качество |
|----------|-------|-----------|----------|
| README.md | 272 | Обзор, быстрый старт, карта документации | ОТЛИЧНО |
| architecture.md | 351 | Детальная архитектура runtime, pipeline, конкуррентная модель | ОТЛИЧНО |
| api-reference.md | 1175 | Полный справочник публичного API | ОТЛИЧНО |
| eca-handbook.md | 506 | Пошаговое руководство пользователя | ОТЛИЧНО |
| data-structures.md | 482 | Все структуры данных и их взаимодействие | ОТЛИЧНО |
| programming-patterns.md | 374 | 20+ паттернов проектирования, антипаттерны, чек-лист | ОТЛИЧНО |
| toml-reference.md | 228 | Low-level TOML-формат [[rules]] | ХОРОШО |
| workspace-reference.md | 422 | High-level workspace-формат (data.toml + actions/*.toml) | ОТЛИЧНО |
| fsm-to-eca.md | 266 | Миграция с FSM на ECA | ОТЛИЧНО |
| Гайд по проектированию... | 827 | Детальный русскоязычный гайд с примерами | ОТЛИЧНО |
| deep-research-report (8).md | ? | Исследовательский отчёт | НЕ ПРОВЕРЕН |

**Суммарно**: ~5,000+ строк документации, охватывающей все аспекты библиотеки от быстрого старта до внутренних структур данных.

### 2.2 Архитектура ECA-движка (краткая выжимка)

```
Вход: Set/SetMany/PublishEvent/FactAdapter.Upsert
  → workItem
  → bus.Router (partition workers)
  → applyPrelude (запись в store)
  → index.Index (выбор candidate rules по изменённым slot-ам)
  → compiler.CompiledRule.Evaluate (проверка условий)
  → agenda.Sort (сортировка по priority)
  → ActionHandler (выполнение действия)
  → ctx.Set/ctx.PublishEvent (новые изменения)
  → цикл повторяется только по затронутым slot-ам
```

**Ключевые подсистемы**:
- **store.Store** — string-table: имена → slot (uint32), значения + версии
- **index.Index** — slot → []ruleID (candidate index, не truth index)
- **compiler.CompiledRule** — стековая программа условия ([]instruction), DependencySlots, MaxStack
- **bus.Router** — один worker на partition, порядок внутри partition, параллелизм между partition
- **partitionScratch** — переиспользуемые буферы на каждый partition (index buffer, agenda, eval context, action context)
- **пулы** — eventFramePool, workItemPool (снижение аллокаций)

**Защиты от ошибок**:
- Signature-дедупликация (не даёт правилу срабатывать повторно при неизменных зависимостях)
- MaxFiringsPerEvent (защита от бесконечных каскадов)
- Cooldown (временной предохранитель)
- Output-контракт (UndeclaredOutputError при записи в незаявленные переменные)
- DataCatalog-валидация (типы, enum, min/max, pattern)

### 2.3 Два режима работы

| Режим | Описание | Формат | Когда использовать |
|-------|----------|--------|-------------------|
| Low-level | Правила как `[]RuleSpec` | Go-код или `[[rules]]` TOML | Правила генерируются кодом, полный контроль над `when` |
| Workspace | data.toml + actions/*.toml | TOML-файлы в директории | Нужна декларативная схема данных, inputs/outputs, cases, валидация |

### 2.4 Ключевые API (из api-reference.md)

**Engine**:
- `NewEngine(Config) (*Engine, error)`
- `Start(ctx)`, `Close()`
- `Set(ctx, variable, value)`, `SetMany(ctx, []StateChange)`
- `Get(variable) (any, bool)`
- `PublishEvent(ctx, *EventFrame)`
- `LoadRules([]RuleSpec)`, `LoadWorkspace(WorkspaceSpec)`
- `Stats() Stats`, `DebugSnapshot() map[string]any`
- `Store() Store`, `EventType(name)`, `EventField(path)`, `AcquireEvent(typeID)`

**ActionContext** (доступен внутри handler-а):
- `Context() context.Context`
- `Event() EventView`, `Rule() FiredRule`, `Action() ActionInvocation`
- `Get(VariableID) (any, bool)`, `Set(VariableID, any) error`
- `AcquireEvent(EventTypeID) *EventFrame`, `PublishEvent(*EventFrame) error`
- `Store() Store`

**ActionRegistry**:
- `Register(name, handler, validator)`, `MustRegister(name, handler)`
- `Lookup(name)`, `ValidateCall(call)`

**Паттерны (из programming-patterns.md)**:
1. Guard + Command
2. Derived State
3. Event Fanout
4. Action Status Chaining
5. Explicit Outputs
6. Stable Partition Key
7. Fact Adapter as Boundary
8. Small Action
9. Pure Predicate, Imperative Handler
10. Wide Data, Narrow Rules
11. State First, Event Second

---

## Часть 3. Hydropilot v5: анализ реализации

### 3.1 Жизненный цикл приложения (internal/app/app.go)

```
1. Загрузка конфига (config.Load)
2. Открытие SQLite (persistence.Open)
3. Инициализация UART транспорта (uart.NewTransport)
4. Создание ActionRegistry
5. Построение ML dosing service
6. Регистрация 17 handler-ов (handlers.RegisterAll)
7. Подготовка правил:
   a. Если есть rules/data.toml → использовать как есть
   b. Иначе → сгенерировать workspace.Materialize(tmp, cfg)
8. Загрузка workspace (workspace.Load → WorkspaceLoader.LoadDir)
9. Создание и запуск ECA engine (NewEngine → LoadWorkspace → Start)
10. Заполнение runtime начальными данными (seedRuntime)
11. Восстановление snapshot из SQLite (если есть)
12. Запуск scheduler (sched.Run)
13. Запуск HTTP dashboard
```

### 3.2 17 зарегистрированных handler-ов

| Handler | Категория | Назначение |
|---------|-----------|-----------|
| set_value | Store Writer | Универсальная запись в Store slot |
| set_bool | Store Writer | Запись boolean-флага |
| clear_flag | Store Writer | Сброс флагов |
| emit_event | Events | Публикация ECA-события |
| raise_alert | Alerts | Поднятие alert + запись fault.last.* |
| clear_alert | Alerts | Снятие alert |
| notify | Alerts | Фиксация флага уведомления оператора |
| noop | Diagnostic | Заглушка для chaining/dry-run |
| send_uart_request | UART | Отправка команды контроллеру, запись command.* |
| parse_uart_response | UART | Разбор ответа контроллера в parsed slots |
| store_measurement | Measurements | Перенос accepted parsed value в canonical slot + SQLite |
| validate_measurement | Measurements | Проверка parsed значения на диапазон |
| store_calibration | Calibration | Запись успешной калибровки + SQLite |
| apply_interlock | Safety | Включение safety.locked с причиной |
| calculate_ph_dose_plan | Dosing | Расчёт плана дозирования pH |
| calculate_ec_dose_plan | Dosing | Расчёт плана дозирования EC |

### 3.3 Генерация workspace (internal/workspace/workspace.go)

Workspace **не пишется вручную** — он генерируется кодом Go из конфигурации зон:

```go
workspace.Materialize(dir, cfg)
  → data.toml   (содержит ~200 переменных Store)
  → actions/*.toml (содержат cases с условиями)
```

Для каждой зоны генерируются правила:
- **Детекция отклонений** (detect_ph_low/high, detect_ec_low/high, detect_temp_high, detect_level_critical)
- **Очистка отклонений** (clear_ph_low/high)
- **Dose intents** (require_ph_up/down, require_nutrient, require_dilution)
- **Dose planning** (plan_ph_dose, plan_ec_dose, plan_dilution)
- **UART команды** (send_ph_up/down, send_nutrient, send_dilution, send_irrigation, light_on/off)
- **Датчики** (read_ph/ec/tds/temp/level, validate, store)
- **Алерты** (raise_ph_low/high, raise_ec_low/high, raise_level_critical, raise_uart_fault, raise_interlock_blocked)
- **Калибровка** (run_ph/ec, store_result, raise_expired, clear_expired)
- **Планировщик** (mark_zone_due, clear_zone_due, emit_tick)
- **Safety** (apply_interlock, clear_interlock, estop_on/off)
- **Оператор** (apply_mode_change, notify_safety, notify_command_failed, notify_calibration_expired)

### 3.4 DataCatalog (сгенерированные переменные Store)

Основные группы переменных:
- `system.*` — mode, ready, started_at, tick
- `uart.*` — ready, connected, circuit_open, last_error_code
- `command.*` — busy, current.payload/name, last.name/status/error_code/response_raw
- `safety.*` — locked, reason, estop.active/requested
- `alerts.*` — ph.low/high.active, ec.low/high.active, level.critical.active, uart.offline.active, interlock_blocked.active, measurement.failed.active, calibration.expired.active
- `operator.*` — notified.*, intent.*
- `fault.*` — active_count, last.{class, code, severity, message, at, resolution_policy}
- `runtime.*` — modules.{eca,scheduler,uart,persistence,dashboard}.ready/heartbeat_at/last_error
- `zone.N.*` — ph/ec/tds/temp/level.value, min/max limits, dose.intents/plans, parsed.*, sensor.*, calibration.*, plant.*, reagent.*

### 3.5 ML Dosing Library (pkg/mldosing/)

- **Predictor** — ML-модель или fallback (линейная формула)
- **Planner** — перебор доз с scoring
- **Safety Guard** — 10 правил из ТЗ
- **EventStore** — SQLite для dose_events и model_versions
- **TrainRunner** — запуск Python-скрипта обучения

---

## Часть 4. Сопоставление документации и кода

### 4.1 Найденные расхождения

| # | Расхождение | Серьёзность | Описание |
|---|------------|------------|----------|
| 1 | Упрощённый формат правил | СРЕДНЯЯ | `hydropilot_simplified_rules_toml.md` описывает формат `if`/`do`/`[params]`, которого **нет в коде**. Реальный код использует workspace-формат (data.toml + actions/*.toml с cases/conditions). Этот документ — proposal, не реализация. |
| 2 | Дублирование api-reference | НИЗКАЯ | `docs/api-reference.md` — почти полная копия `ECA-lib/docs/api-reference.md` (~1200 строк), но с Obsidian wiki-links вместо markdown-ссылок. |
| 3 | Obsidian-синтаксис | НИЗКАЯ | Проектные документы используют YAML-frontmatter, `[[wiki-links]]`, `dataview` блоки — это нестандартный markdown, требует Obsidian для полной функциональности. |
| 4 | Документы без файлов | СРЕДНЯЯ | `docs/README.md` ссылается на: `architecture_moc`, `modules_moc`, `store_moc`, `cases_moc`, `architecture_overview`, `handler_rule_store_workspace`, `data_catalog`, `dashboard_api`, `admin_diagnostics`, `calibration_chain`, `ph_correction_chain` и др. — многие из этих файлов **отсутствуют** в репозитории. |
| 5 | Неполный русский гайд | НИЗКАЯ | `Гайд по проектированию приложений на твоей ECA.md` (827 строк, обрывается на разделе 10) — содержит отсылки к разделам за пределами файла. |
| 6 | Разные модели документации | СРЕДНЯЯ | ECA-lib/docs/ использует чистый markdown с относительными ссылками. docs/ использует Obsidian-синтаксис. Две системы документации не связаны перекрёстными ссылками. |

### 4.2 Пробелы в документации

| # | Пробел | Влияние |
|---|--------|---------|
| 1 | Нет документации по workspace/workspace.go | Генерация TOML из Go-кода — неочевидный механизм. Нужен документ, объясняющий: как добавлять новые правила, как зоны влияют на генерацию cases. |
| 2 | Нет документации по dashboard | Только `Dashboard hydropilot.md` (концептуальный HMI-дизайн). Нет API-спецификации dashboard-эндпоинтов. |
| 3 | Нет документации по persistence | SQLite-схема не задокументирована отдельно. Упоминается в implementation_plan-ml.md, но нет единого документа. |
| 4 | Нет документации по scheduler | Упомянут в runtime_lifecycle.md, но отдельного разбора нет. |
| 5 | Нет диаграммы потоков данных | Отсутствует визуальная схема: sensor → UART → parse → validate → store → detect → plan → send → controller. Есть только в упрощённом формате правил (не реализован). |
| 6 | Нет runbook / troubleshooting guide | `eca-handbook.md` содержит секцию «если правило не срабатывает», но нет проектного troubleshooting. |

### 4.3 Что документировано хорошо

- **ECA-библиотека** — все 9+ документов на высшем уровне. Архитектура, API, структуры данных, паттерны, миграция с FSM, TOML/workspace форматы.
- **Handler-ы** — `handlers_moc.md` даёт полный обзор 17 handler-ов, их контрактов и категорий.
- **Конфигурация** — config.go полностью покрывает все настройки.
- **Жизненный цикл** — `runtime_lifecycle.md` чётко описывает startup/shutdown.
- **Legacy-миграция** — `legacy_exclusions.md` чётко разделяет старую и новую модель.

---

## Часть 5. Итоговая документация: HydroPilot на базе ECA-библиотеки

### 5.1 Концептуальная модель

HydroPilot v5 — это система автоматизации гидропоники, построенная на **ECA-движке** (Event-Condition-Action). В отличие от классических конечных автоматов (FSM), система не имеет единственного «текущего состояния». Вместо этого она работает с **blackboard-состоянием** — набором из ~200 именованных переменных (Store slots).

**Ключевой принцип**:
```
Данные описывают мир → Правила решают, когда реагировать → Handler-ы выполняют действия
```

**Поток данных**:
```
Сенсоры/Оператор → Set/SetMany → Store → Index (поиск candidate rules)
→ CompiledRule (проверка условий) → Agenda (приоритет) → Handler (действие)
→ ctx.Set (новые изменения) → цикл повторяется
```

### 5.2 Архитектурные слои

| Слой | Ответственность | Компоненты |
|------|----------------|-----------|
| **Входной** | Получение данных из внешнего мира | UART transport, HTTP API, scheduler ticks |
| **ECA Runtime** | Blackboard-состояние + правила + действия | Engine, Store, Index, Compiler, Router |
| **Handler-ы** | Исполнение действий | 17 Go-функций (set_value, send_uart_request, ...) |
| **Сервисный** | ML-дозирование, персистентность | pkg/mldosing, internal/persistence |
| **Пользовательский** | Дашборд, API | internal/dashboard (HTTP/SSE) |

### 5.3 Жизненный цикл правил

1. **Конфигурация загружается** из `config/hydropilot.toml` (зоны, лимиты, реагенты)
2. **Workspace генерируется** — из Go-структур создаются `data.toml` (каталог переменных) и `actions/*.toml` (правила с cases)
3. **Workspace загружается** в Engine через `LoadWorkspace()` — валидация, построение DataCatalog, компиляция правил в CompiledRule, построение Index
4. **Engine запускается** — bus начинает принимать work items
5. **Runtime заполняется** начальными значениями (seedRuntime: system.ready, uart.ready, zone limits, ...)
6. **Snapshot восстанавливается** из SQLite (если есть)
7. **Scheduler начинает** периодически отправлять tick
8. **Правила реагируют** на изменения Store

### 5.4 Приоритеты правил

```
1000 — E-Stop (аварийная остановка)
 950 — Safety lock
 900 — Alerts (поднятие тревог)
 800 — Detect derived state (обнаружение отклонений)
 750 — Clear derived state (возврат в норму)
 700 — Dose intents (намерения дозирования)
 650 — Dose planning (расчёт планов)
 600 — UART commands (отправка команд)
 570 — Validation (валидация измерений)
 550 — Store measurements (сохранение)
 500 — Cleanup / scheduler / operator mode
 520 — Operator mode change
 450 — Clear zone due
 300 — Notifications
 100 — Scheduler tick
```

### 5.5 Основные бизнес-цепочки

#### Измерительная цепочка
```
scheduler.tick → mark_zone_due → sensors.read_ph (UART) → parse_uart_response
→ validate_measurement → store_measurement → clear_zone_due
```

#### Цепочка коррекции pH
```
sensors.store_measurement (ph.value) → solution.detect_ph_low
→ solution.require_ph_up → solution.plan_ph_dose (ML или fallback)
→ uart.send_ph_up → solution.clear_dose_intent
```

#### Цепочка безопасности
```
operator.intent.estop_requested → safety.apply_interlock (safety.locked=true)
→ safety.estop_on (UART) → alerts.raise_interlock_blocked
→ operator.notify_safety
```

### 5.6 Handler Reference

| Handler | Inputs | Outputs | Side Effects |
|---------|--------|---------|-------------|
| `set_value` | `field`, `value` | Store[field] = value | — |
| `set_bool` | `field`, `value` | Store[field] = bool | — |
| `clear_flag` | `field` или `fields` | Store[*] = false | — |
| `emit_event` | `type`, `key?`, `partition_key?` | — | PublishEvent |
| `raise_alert` | `slot`, `code`, `zone?` | alert.active=true, fault.last.* | SQLite alert save |
| `clear_alert` | `slot` | alert.active=false | — |
| `notify` | `slot` | operator.notified.*=true | — |
| `noop` | — | — | — |
| `send_uart_request` | `payload` или `payload_template` | command.*, uart.last_* | UART transport |
| `parse_uart_response` | `zone`, (читает command.last.response_raw) | zone.N.parsed.*, zone.N.sensor.*.valid | — |
| `validate_measurement` | `zone`, `kind`, `minimum?`, `maximum?` | sensor.*.quality, sensor.*.valid, measurement.validation_* | — |
| `store_measurement` | `zone`, `kind` | zone.N.{kind}.value, measurement.updated_at | SQLite measurement save |
| `store_calibration` | `zone`, `kind`, `reference_value?` | calibration.*.status/slope/offset/expires_at | SQLite calibration save |
| `apply_interlock` | `reason?` | safety.locked=true, safety.reason | — |
| `calculate_ph_dose_plan` | `zone`, `volume_liters`, ... | dose.ph_up_ml, dose.ph_down_ml, dose.plan_ready | ML dosing service |
| `calculate_ec_dose_plan` | `zone`, `volume_liters`, ... | dose.nutrient_ml, dose.water_ml, dose.plan_ready | ML dosing service |

### 5.7 Store Reference (ключевые переменные)

**Системные**:
- `system.mode` — "manual", "auto", "service", "emergency"
- `system.ready` — runtime готов к работе
- `system.started_at` — время запуска

**UART/Команды**:
- `uart.ready`, `uart.connected`, `uart.circuit_open`
- `command.busy`, `command.current.payload`, `command.current.name`
- `command.last.name`, `command.last.status`, `command.last.response_raw`, `command.last.error_code`

**Безопасность**:
- `safety.locked`, `safety.reason`, `safety.estop.active`
- `operator.intent.estop_requested`, `operator.intent.estop_clear_requested`, `operator.intent.safety_acknowledged`

**Зона (zone.N)**:
- Значения: `ph.value`, `ec.value`, `tds.value`, `temp.value`, `level.value`
- Лимиты: `ph.min/max`, `ec.min/max`, `tds.min/max`, `temp.max`, `level.min/critical`
- Derived: `ph.low/high`, `ec.low/high`, `temp.high`, `level.critical_active`
- Дозирование: `dose.enabled`, `dose.ph_up_required`, `dose.plan_ready`, `dose.ph_up_ml`, `dose.ph_down_ml`, `dose.nutrient_ml`, `dose.water_ml`
- Сенсоры: `parsed.{ph,ec,tds,temp,level}.{value,status}`, `sensor.{kind}.{quality,valid}`
- Калибровка: `calibration.{ph,ec}.{status,last_success_at,expires_at,slope,offset,quality}`

---

## Часть 6. Карта документов и рекомендации

### 6.1 Актуальная структура документации

```
ECA-lib/                          — документация библиотеки (ПОЛНАЯ)
├── README.md                     — обзор, быстрый старт
├── docs/architecture.md          — архитектура runtime
├── docs/api-reference.md         — полный API-справочник
├── docs/eca-handbook.md          — руководство пользователя
├── docs/data-structures.md       — структуры данных
├── docs/programming-patterns.md  — паттерны проектирования
├── docs/toml-reference.md        — low-level TOML
├── docs/workspace-reference.md   — workspace-формат
├── docs/fsm-to-eca.md            — миграция с FSM
└── docs/Гайд по проектированию...md — русскоязычный гайд

docs/                             — проектная документация (ТРЕБУЕТ ДОРАБОТКИ)
├── README.md                     — MOC (map of content), Obsidian
├── api-reference.md              — дубликат ECA-lib/docs/api-reference.md (Obsidian-версия)
├── legacy_exclusions.md          — что исключено из v5
├── Dashboard hydropilot.md       — HMI/дашборд концепция
├── implementation_plan-ml.md     — план ML-дозирования
├── hydropilot_simplified_rules_toml.md — **proposal**, не реализован
├── 04_handlers/handlers_moc.md   — обзор handler-ов
├── 04_handlers/*.md              — индивидуальные handler-ы
├── 02_modules/*.md               — модули (uart_transport, runtime_lifecycle, ...)
└── _archive/                     — архивные документы (неактуальны)
```

### 6.2 Рекомендации

1. **Удалить или пометить** `hydropilot_simplified_rules_toml.md` как proposal/черновик — его формат не соответствует реальному коду
2. **Удалить** `docs/api-reference.md` (дубликат) или заменить на ссылку на `../ECA-lib/docs/api-reference.md`
3. **Добавить** документ `docs/workspace-generation.md` — объясняющий генерацию workspace из Go-кода
4. **Добавить** `docs/data-catalog.md` — полный список всех Store slots (сейчас есть только в коде workspace.go)
5. **Добавить** `docs/business-chains.md` — визуальные диаграммы основных цепочек
6. **Добавить** `docs/troubleshooting.md` — runbook для оператора
7. **Конвертировать** Obsidian-документы в стандартный markdown для переносимости
8. **Связать** документацию ECA-lib и проекта перекрёстными ссылками

---

## Часть 7. Выводы

### Сильные стороны
- ECA-библиотека документирована на отлично (9 документов, ~5000 строк)
- Код Hydropilot чистый и хорошо структурированный
- Чёткое разделение: handler-ы (императивный Go) vs правила (декларативный TOML)
- Workspace-генерация позволяет избежать ручного написания TOML для типовых правил
- ML-дозирование интегрировано через абстракцию Service/Predictor

### Зоны роста
- Проектная документация фрагментирована между Obsidian-форматом и markdown
- Часть документов ссылается на несуществующие файлы (Obsidian MOC)
- Упрощённый формат правил задокументирован, но не реализован
- Отсутствует единый troubleshooting guide
- Нет визуальной схемы потоков данных между компонентами
