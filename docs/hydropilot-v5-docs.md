# HydroPilot v5 — Техническая документация

Версия: 5.0.0 | Дата: 2026-05-04

---

## Оглавление

1. [Архитектурный обзор](#1-архитектурный-обзор)
2. [Протокол обмена с контроллером (UART)](#2-протокол-обмена-с-контроллером-uart)
3. [Справочник сущностей (Store Slots)](#3-справочник-сущностей-store-slots)
4. [Справочник действий (Actions)](#4-справочник-действий-actions)
5. [Справочник handler-ов](#5-справочник-handler-ов)
6. [Справочник модулей](#6-справочник-модулей)
7. [HTTP API (Dashboard)](#7-http-api-dashboard)
8. [База данных (SQLite)](#8-база-данных-sqlite)
9. [ECA-движок: устройство и API](#9-eca-движок-устройство-и-api)
10. [Конфигурация](#10-конфигурация)
11. [Жизненный цикл приложения](#11-жизненный-цикл-приложения)
12. [Сценарии и цепочки](#12-сценарии-и-цепочки)

---

## 1. Архитектурный обзор

### 1.1 Ментальная модель

HydroPilot v5 — система автоматизации гидропоники на базе **ECA-движка** (Event-Condition-Action). Система не имеет единственного «текущего состояния». Вместо неё — **blackboard-состояние** из ~200 именованных переменных.

```
ДАННЫЕ описывают мир → ПРАВИЛА решают, когда реагировать → HANDLER-Ы выполняют действия
```

### 1.2 Слои системы

```
┌──────────────────────────────────────────────────┐
│  Dashboard (HTTP/SSE)  │  Scheduler (тики)        │  ← интерфейсы
├──────────────────────────────────────────────────┤
│  ECA Engine (blackboard + index + compiler)       │  ← ядро принятия решений
├──────────────────────────────────────────────────┤
│  17 Handler-ов (set_value, send_uart, ...)       │  ← исполнители действий
├──────────────────────────────────────────────────┤
│  UART Transport  │  SQLite Persistence  │  ML Dosing │ ← инфраструктура
├──────────────────────────────────────────────────┤
│  Контроллер (плата Arduino/STM32)                 │  ← внешний мир
└──────────────────────────────────────────────────┘
```

### 1.3 Поток данных

```
Сенсоры / Оператор / Таймер
  │
  ▼
engine.Set / engine.SetMany / engine.PublishEvent
  │
  ▼
Store (blackboard: имя → slot → значение + версия)
  │
  ▼
Index (slot → candidate rules)    ← будит только затронутые правила
  │
  ▼
CompiledRule.Evaluate (стековая программа условия)
  │
  ▼
Agenda.Sort (приоритет ↓)
  │
  ▼
ActionHandler (ctx.Set / ctx.PublishEvent)
  │
  ▼ (новые изменения)
  цикл повторяется только по затронутым slot-ам
```

### 1.4 Подсистемы ECA-движка

| Подсистема | Назначение | Ключевая структура |
|-----------|-----------|-------------------|
| Store | Хранилище blackboard-переменных | slot → {value, version} |
| Index | Индекс зависимостей | slot → []ruleID |
| Compiler | Компиляция when-выражений | CompiledRule {Program []instruction, DependencySlots} |
| Router | Маршрутизация по partition | PartitionKey → worker goroutine |
| Agenda | Сортировка matched правил | priority desc, ruleID asc |
| Pools | Переиспользование объектов | EventFrame pool, WorkItem pool |

### 1.5 Модель конкуррентности

- Один **worker** на каждый partition
- Внутри partition обработка **последовательная**
- Между разными partition — **параллельная**
- State-обновления (`Set/SetMany`) идут в partition `"state"`
- События (`PublishEvent`) маршрутизируются по `frame.PartitionKey`

### 1.6 Защиты от некорректного поведения

| Механизм | Что предотвращает |
|----------|------------------|
| Signature-дедупликация | Повторный firing при неизменных зависимостях |
| MaxFiringsPerEvent (128) | Бесконечные каскады |
| Cooldown | Слишком частый firing одного правила |
| Output-контракт | Запись handler-ом в незаявленные переменные |
| DataCatalog-валидация | Значения неверного типа / вне диапазона |

---

## 2. Протокол обмена с контроллером (UART)

### 2.1 Транспортный уровень

**Физический уровень**: UART (RS-232/TTL), настраиваемая скорость (по умолчанию 115200 бод).

**Кадровый формат** (текстовый, с разделителями):

```
<ID>:<TYPE>:<PAYLOAD>*<CRC><TERM>
```

| Поле | Описание |
|------|---------|
| `ID` | Уникальный номер транзакции (uint64, от 1) |
| `TYPE` | Тип кадра: `CMD`, `OK`, `RESULT`, `ERROR` |
| `PAYLOAD` | Полезная нагрузка (формат зависит от TYPE) |
| `*<CRC>` | 4-значный hex checksum (опционально) |
| `<TERM>` | Символ конца кадра (`\n`) |

**Алгоритм CRC**: простая сумма байт (uint16 sum всех символов тела до `*`).

### 2.2 Протокол транзакции

```
 ┌──────────┐                    ┌──────────┐
 │ HydroPilot│                    │  Плата   │
 └────┬─────┘                    └────┬─────┘
      │                               │
      │  CMD: 7:CMD:1:SENSOR:PH:READ:0*ABCD\n
      │──────────────────────────────>│
      │                               │
      │  7:OK:ACCEPTED*XXXX\n         │
      │<──────────────────────────────│
      │                               │  (плата выполняет операцию)
      │  7:RESULT:1:RESP:OK:SENSOR:PH:6.10*YYYY\n
      │<──────────────────────────────│
      │                               │
```

**Этапы**:
1. HydroPilot отправляет кадр `CMD`
2. Плата подтверждает приём кадром `OK` (должен совпадать `ID`)
3. Плата выполняет операцию
4. Плата возвращает `RESULT` (успех) или `ERROR` (ошибка)

**Таймауты** (настраиваемые):
- `OKTimeout`: ожидание `OK` (по умолчанию 500ms)
- `ResultTimeout`: ожидание `RESULT/ERROR` (по умолчанию 2000ms)
- `MaxRetries`: максимум повторов при таймауте/ошибке (по умолчанию 3)

**Повторы**: при таймауте `OK` или `RESULT` — повтор с тем же `ID` (для идемпотентности).

### 2.3 Исходящие команды (CMD)

Формат поля `PAYLOAD` в команде:

```
<zone>:<domain>:<subject>:<verb>[:arg1[:arg2...]]
```

**Домены команд**:

#### SENSOR — чтение датчиков

| Команда | Ответ |
|---------|-------|
| `Z:SENSOR:PH:READ:0` | `Z:RESP:OK:SENSOR:PH:<value>` |
| `Z:SENSOR:EC:READ:0` | `Z:RESP:OK:SENSOR:EC:<value>` |
| `Z:SENSOR:TDS:READ:0` | `Z:RESP:OK:SENSOR:TDS:<value>` |
| `Z:SENSOR:TEMP:READ:0` | `Z:RESP:OK:SENSOR:TEMP:<value>` |
| `Z:SENSOR:LEVEL:READ:0` | `Z:RESP:OK:SENSOR:LEVEL:<value>` |

#### ACT — исполнительные механизмы

| Команда | Назначение |
|---------|-----------|
| `Z:ACT:LIGHT:SET:ON` | Включить свет |
| `Z:ACT:LIGHT:SET:OFF` | Выключить свет |
| `Z:ACT:IRRIGATION:RUN:<ch>:<sec>` | Полив |
| `Z:ACT:DOSE:RUN:<pump_id>:<ml>` | Дозирование реагента |
| `Z:ACT:TANK:FILL:FULL` | Наполнить бак |

#### CAL — калибровка

| Команда | Назначение |
|---------|-----------|
| `Z:CAL:PH:RUN:<buffer>` | Калибровка pH (буфер) |
| `Z:CAL:EC:RUN:<reference>` | Калибровка EC (референс) |

#### SYS — системные

| Команда | Назначение |
|---------|-----------|
| `0:SYS:PING:GET` | Проверка связи |
| `0:SYS:VERSION:GET` | Версия прошивки |
| `0:SYS:STATUS:GET` | Состояние контроллера |
| `0:SYS:ESTOP:SET:ON` | Аварийный останов |
| `0:SYS:ESTOP:SET:OFF` | Сброс E-Stop |

#### AXIS — сервисные (оси)

| Команда | Назначение |
|---------|-----------|
| `0:AXIS:X:MOVE:<steps>` | Сдвиг оси X |
| `0:AXIS:Y:JOG:<steps>` | Ручной сдвиг Y |
| `0:AXIS:Z:ZERO` | Обнуление Z |
| `0:AXIS:X:POS` | Позиция X |
| `0:AXIS:ALL:POS` | Все позиции |

### 2.4 Входящие ответы (RESULT)

Формат поля `PAYLOAD` в ответе `RESULT`:

```
<zone>:RESP:OK:<domain>:<subject>:<value>
<zone>:RESP:ERR:<error_code>
```

**Примеры**:
- `1:RESP:OK:SENSOR:PH:6.10` — успешное чтение pH
- `1:RESP:OK:ACT:DOSE:PH_UP_1:5.0` — дозирование выполнено
- `0:RESP:ERR:CRC` — ошибка контрольной суммы
- `0:RESP:ERR:PUMP` — отказ насоса

### 2.5 UART Transport (Go-реализация)

**Интерфейс**: `Transport` с методом `Execute(ctx, payload) (Result, error)`.

**Режимы**:
- `mock` — эмуляция (возвращает фиктивные ответы, без реального порта)
- `real` — работа с физическим последовательным портом (`go.bug.st/serial`)

**Результат** (`Result`):
- `Status` — `"ok"`, `"failed"`, `"timeout"`, `"blocked"`
- `ErrorCode` — `"UART_TIMEOUT"`, `"UART_BUSY"`, `"UART_CLOSED"`, `"CRC"`, `"FRAME"`, ...
- `Raw` — сырая строка ответа
- `Latency` — время транзакции

**Важно**: в каждый момент времени активна только ОДНА транзакция. Параллельные вызовы `Execute` невозможны (возвращается `"blocked"`).

---

## 3. Справочник сущностей (Store Slots)

Все переменные хранятся в **blackboard** (Store) ECA-движка. Формат имён: `категория.подкатегория.поле`.

### 3.1 Системные (system.*)

| Slot | Тип | По умолчанию | Назначение |
|------|-----|-------------|-----------|
| `system.mode` | string | `"manual"` | Режим: manual, auto, service, emergency |
| `system.ready` | bool | false | Runtime готов |
| `system.started_at` | string | — | Время запуска (RFC3339Nano) |
| `system.tick` | number | 0 | Счётчик тиков |

### 3.2 UART и команды (uart.*, command.*, firmware.*)

| Slot | Тип | Назначение |
|------|-----|-----------|
| `uart.ready` | bool | Транспорт готов |
| `uart.connected` | bool | Соединение установлено |
| `uart.circuit_open` | bool | Circuit breaker разомкнут |
| `uart.last_error_code` | string | Последний код ошибки |
| `firmware.busy` | bool | Контроллер занят |
| `firmware.version` | string | Версия прошивки |
| `command.busy` | bool | Команда выполняется |
| `command.current.payload` | string | Текущий payload |
| `command.current.name` | string | Имя текущего action |
| `command.last.name` | string | Имя последнего action |
| `command.last.status` | string | Статус: ok, failed, timeout |
| `command.last.response_raw` | string | Сырой ответ |
| `command.last.error_code` | string | Код ошибки |

### 3.3 Безопасность (safety.*)

| Slot | Тип | Назначение |
|------|-----|-----------|
| `safety.locked` | bool | Система заблокирована |
| `safety.reason` | string | Причина блокировки |
| `safety.estop.requested` | bool | Запрошен E-Stop |
| `safety.estop.active` | bool | E-Stop активен |

### 3.4 Алерты (alerts.*)

| Slot | Тип | Назначение |
|------|-----|-----------|
| `alerts.ph.low.active` | bool | pH ниже минимума |
| `alerts.ph.high.active` | bool | pH выше максимума |
| `alerts.ec.low.active` | bool | EC ниже минимума |
| `alerts.ec.high.active` | bool | EC выше максимума |
| `alerts.level.critical.active` | bool | Критический уровень |
| `alerts.uart.offline.active` | bool | UART не отвечает |
| `alerts.interlock_blocked.active` | bool | Interlock активен |
| `alerts.measurement.failed.active` | bool | Измерение отклонено |
| `alerts.calibration.expired.active` | bool | Калибровка просрочена |

### 3.5 Оператор (operator.*)

| Slot | Тип | Назначение |
|------|-----|-----------|
| `operator.intent.estop_requested` | bool | Оператор запросил E-Stop |
| `operator.intent.estop_clear_requested` | bool | Снять E-Stop |
| `operator.intent.safety_acknowledged` | bool | Оператор подтвердил safety |
| `operator.intent.mode_requested` | string | Запрошенный режим |
| `operator.notified.ph_low` | bool | Уведомлён о pH LOW |
| `operator.notified.safety` | bool | Уведомлён о safety |
| `operator.notified.command_failed` | bool | Уведомлён о сбое команды |
| `operator.notified.calibration_expired` | bool | Уведомлён о калибровке |

### 3.6 Fault (fault.*)

| Slot | Тип | Назначение |
|------|-----|-----------|
| `fault.active_count` | int | Число активных fault |
| `fault.last.class` | string | Класс: transport, controller, safety, validation, config, runtime |
| `fault.last.code` | string | Код: CRC, FRAME, TIMEOUT, BUSY, INTERLOCK_BLOCKED, ... |
| `fault.last.severity` | string | info, warning, error, critical |
| `fault.last.message` | string | Сообщение для оператора |
| `fault.last.at` | string | Время возникновения |
| `fault.last.resolution_policy` | string | auto_on_success, manual, restart_required |

### 3.7 Runtime-модули (runtime.modules.*)

Для каждого модуля (`eca`, `scheduler`, `uart`, `persistence`, `dashboard`):

| Slot | Тип | Назначение |
|------|-----|-----------|
| `runtime.modules.<name>.ready` | bool | Модуль готов |
| `runtime.modules.<name>.heartbeat_at` | string | Последний heartbeat |
| `runtime.modules.<name>.last_error` | string | Последняя ошибка |

Дополнительно: `runtime.shutdown_requested`, `runtime.last_snapshot_at`.

### 3.8 Зона (zone.N.*)

Базовые измерения:

| Slot | Тип | Назначение |
|------|-----|-----------|
| `zone.N.ph.value` | number | Текущий pH |
| `zone.N.ec.value` | number | Текущий EC |
| `zone.N.tds.value` | number | Текущий TDS |
| `zone.N.temp.value` | number | Текущая температура |
| `zone.N.level.value` | number | Текущий уровень |
| `zone.N.measurement.updated_at` | string | Время последнего измерения |
| `zone.N.measurement.required` | bool | Требуется измерение |
| `zone.N.control.required` | bool | Требуется контроль раствора |
| `zone.N.controller.heartbeat_at` | string | Heartbeat контроллера |
| `zone.N.controller.heartbeat_status` | string | Статус heartbeat |

Лимиты:

| Slot | Тип |
|------|-----|
| `zone.N.ph.min` / `.max` | number |
| `zone.N.ec.min` / `.max` | number |
| `zone.N.tds.min` / `.max` | number |
| `zone.N.temp.max` | number |
| `zone.N.level.min` / `.critical` | number |

Derived state (производные флаги):

| Slot | Тип | Условие |
|------|-----|--------|
| `zone.N.ph.low` | bool | ph.value < ph.min |
| `zone.N.ph.high` | bool | ph.value > ph.max |
| `zone.N.ec.low` | bool | ec.value < ec.min |
| `zone.N.ec.high` | bool | ec.value > ec.max |
| `zone.N.temp.high` | bool | temp.value > temp.max |
| `zone.N.level.low` | bool | level.value < level.min |
| `zone.N.level.critical_active` | bool | level.value < level.critical |

Dose intent (намерения дозирования):

| Slot | Тип |
|------|-----|
| `zone.N.dose.enabled` | bool |
| `zone.N.dose.dilution_available` | bool |
| `zone.N.dose.ph_up_required` | bool |
| `zone.N.dose.ph_down_required` | bool |
| `zone.N.dose.nutrient_required` | bool |
| `zone.N.dose.dilution_required` | bool |
| `zone.N.dose.plan_ready` | bool |

Dose plan (рассчитанные дозы):

| Slot | Тип |
|------|-----|
| `zone.N.dose.ph_up_ml` | number |
| `zone.N.dose.ph_down_ml` | number |
| `zone.N.dose.nutrient_ml` | number |
| `zone.N.dose.water_ml` | number |
| `zone.N.dose.dilution_ml` | number |
| `zone.N.dose.cooldown_until` | string |
| `zone.N.dose.water_reagent_id` | string |

Parsed sensor data (разобранные ответы UART):

| Slot | Тип | Пример |
|------|-----|--------|
| `zone.N.parsed.ph.value` | number | 6.10 |
| `zone.N.parsed.ph.status` | string | "ok" |
| `zone.N.parsed.ec.value` | number | 1.50 |
| `zone.N.parsed.ec.status` | string | "ok" |
| `zone.N.parsed.tds.value` | number | 750 |
| `zone.N.parsed.temp.value` | number | 24.5 |
| `zone.N.parsed.level.value` | number | 70.0 |

Sensor validation:

| Slot | Тип |
|------|-----|
| `zone.N.sensor.<ph\|ec\|tds\|temp\|level>.quality` | string ("accepted" / "rejected") |
| `zone.N.sensor.<kind>.valid` | bool |
| `zone.N.sensor.<kind>.last_error_code` | string |
| `zone.N.measurement.validation_status` | string |
| `zone.N.measurement.validation_error` | string |

Calibration:

| Slot | Тип |
|------|-----|
| `zone.N.calibration.<ph\|ec>.status` | string ("idle" / "valid" / "expired") |
| `zone.N.calibration.<kind>.buffer_value` / `.reference_value` | number |
| `zone.N.calibration.<kind>.last_run_at` / `.last_success_at` | string |
| `zone.N.calibration.<kind>.expires_at` | string |
| `zone.N.calibration.<kind>.slope` | number |
| `zone.N.calibration.<kind>.offset` | number |
| `zone.N.calibration.<kind>.quality` | string |

Operator intents (для зоны):

| Slot | Тип |
|------|-----|
| `zone.N.operator.intent.measurement_requested` | bool |
| `zone.N.operator.intent.control_requested` | bool |
| `zone.N.operator.intent.manual_dose_requested` | bool |
| `zone.N.operator.intent.manual_dose_reagent` | string |
| `zone.N.operator.intent.manual_dose_ml` | number |
| `zone.N.operator.intent.irrigation_requested` | bool |
| `zone.N.operator.intent.light_requested` | string ("on"/"off") |
| `zone.N.operator.intent.calibration.ph_requested` | bool |
| `zone.N.operator.intent.calibration.ec_requested` | bool |

Reagents (привязка реагентов к зоне):

| Slot | Тип |
|------|-----|
| `zone.N.reagent.ph_up_id` / `.ph_down_id` | string |
| `zone.N.reagent.nutrient_id` / `.water_id` | string |
| `zone.N.reagent.<role>.pump_id` | string |
| `zone.N.reagent.<role>.max_ml_per_action` | number |
| `zone.N.reagent.<role>.cooldown_sec` | int |

Plant profiles:

| Slot | Тип |
|------|-----|
| `zone.N.plant.profile_id` | string |
| `zone.N.plant.stage_id` | string |
| `zone.N.plant.stage_started_at` | string |
| `zone.N.plant.stage_day` | int |
| `zone.N.plant.target.ph_min` / `.ph_max` | number |
| `zone.N.plant.target.ec_min` / `.ec_max` | number |
| `zone.N.plant.target.tds_min` / `.tds_max` | number |
| `zone.N.plant.target.temp_max` | number |
| `zone.N.plant.light.on_at` / `.off_at` | string |

Дополнительно: `zone.N.irrigation.enabled` (bool).

---

## 4. Справочник действий (Actions)

Действия (actions) — это логические имена операций в workspace. Каждое действие имеет один или несколько **cases** — вариантов срабатывания с конкретными условиями, inputs и outputs.

### 4.1 Приоритеты

```
1000 — E-Stop (аварийное)
 950 — Safety lock
 900 — Alerts (поднятие тревог) / Level critical
 850 — UART fault
 820 — Calibration expired
 800 — Detect derived state (ph.low/high, ec.low/high, ...)
 750 — Clear derived state
 700 — Dose intents (ph_up_required, ...)
 650 — Dose planning
 620 — Calibration run
 600 — UART commands
 590 — Parse UART response
 580 — Store calibration
 570 — Validate measurement
 550 — Store measurement
 520 — Operator mode change
 500 — Cleanup / scheduler / clear intent
 450 — Clear zone due
 300 — Notifications
 100 — Scheduler tick
```

### 4.2 Список всех actions

#### Безопасность

| Action | Handler | Cases |
|--------|---------|-------|
| `safety.apply_interlock` | apply_interlock | estop_requested |
| `safety.clear_interlock` | clear_flag | safety_acknowledged |
| `safety.estop_on` | send_uart_request | estop_on_requested |
| `safety.estop_off` | send_uart_request | estop_clear_requested |
| `safety.raise_level_critical` | raise_alert | zone_N_level_critical_alert |

#### Алерты

| Action | Handler | Cases |
|--------|---------|-------|
| `alerts.raise_ph_low` | raise_alert | ph_low_raise_alert |
| `alerts.raise_ph_high` | raise_alert | ph_high_raise_alert |
| `alerts.raise_ec_low` | raise_alert | ec_low_raise_alert |
| `alerts.raise_ec_high` | raise_alert | ec_high_raise_alert |
| `alerts.raise_uart_fault` | raise_alert | command_failed, command_timeout |
| `alerts.raise_interlock_blocked` | raise_alert | interlock_blocked |
| `alerts.raise_measurement_failed` | raise_alert | measurement_rejected |
| `alerts.clear` | clear_alert | clear_uart_alert |
| `alerts.clear_measurement_alert` | clear_alert | measurement_ok |

#### Датчики и измерения

| Action | Handler | Cases |
|--------|---------|-------|
| `uart.read_ph` | send_uart_request | read_ph_when_required |
| `uart.read_ec` | send_uart_request | read_ec_when_required |
| `uart.read_temp` | send_uart_request | read_temp_when_required |
| `sensors.read_tds` | send_uart_request | read_tds_when_required |
| `sensors.read_level` | send_uart_request | read_level_when_required |
| `sensors.validate_measurement` | validate_measurement | validate_{ph,ec,tds,temp,level} |
| `sensors.store_measurement` | store_measurement | store_{ph,ec,tds,temp,level} |

#### Анализ раствора

| Action | Handler | Cases |
|--------|---------|-------|
| `solution.detect_ph_low` | set_bool | ph_below_min |
| `solution.detect_ph_high` | set_bool | ph_above_max |
| `solution.clear_ph_low` | clear_flag | ph_low_back_to_normal |
| `solution.clear_ph_high` | clear_flag | ph_high_back_to_normal |
| `solution.detect_ec_low` | set_bool | ec_below_min |
| `solution.detect_ec_high` | set_bool | ec_above_max |
| `solution.detect_temp_high` | set_bool | temp_above_max |
| `solution.detect_level_critical` | set_bool | level_below_critical |
| `solution.require_ph_up` | set_bool | ph_low_requires_dose |
| `solution.require_ph_down` | set_bool | ph_high_requires_dose |
| `solution.require_nutrient` | set_bool | ec_low_requires_nutrient |
| `solution.require_dilution` | set_bool | ec_high_requires_dilution |
| `solution.plan_ph_dose` | calculate_ph_dose_plan | ph_up_dose_plan_required, ph_down_dose_plan_required |
| `solution.plan_ec_dose` | calculate_ec_dose_plan | ec_dose_plan_required |
| `solution.plan_dilution` | calculate_ec_dose_plan | dilution_plan_required |
| `solution.clear_dose_intent` | clear_flag | dose_sent_clear_intent |

#### UART actuator commands

| Action | Handler | Cases |
|--------|---------|-------|
| `uart.send_ph_up` | send_uart_request | send_ph_up_when_ready |
| `uart.send_ph_down` | send_uart_request | send_ph_down_when_ready |
| `uart.send_nutrient` | send_uart_request | send_nutrient_when_ready |
| `uart.send_dilution` | send_uart_request | send_dilution_when_ready |
| `uart.send_irrigation` | send_uart_request | send_irrigation_when_requested |
| `uart.light_on` | send_uart_request | light_on_requested |
| `uart.light_off` | send_uart_request | light_off_requested |

#### Калибровка

| Action | Handler | Cases |
|--------|---------|-------|
| `calibration.run_ph` | send_uart_request | zone_N_calibration_ph_requested |
| `calibration.run_ec` | send_uart_request | zone_N_calibration_ec_requested |
| `calibration.store_result` | store_calibration | zone_N_store_{ph,ec}_calibration |
| `calibration.raise_expired` | raise_alert | zone_N_calibration_expired_alert |
| `calibration.clear_expired` | clear_alert | zone_N_calibration_fresh_clear_alert |

#### Планировщик и зоны

| Action | Handler | Cases |
|--------|---------|-------|
| `scheduler.emit_tick` | emit_event | system_ready_tick |
| `scheduler.mark_zone_due` | set_bool | mark_zone_N_measurement_due |
| `scheduler.clear_zone_due` | clear_flag | clear_zone_N_due_after_measurement |
| `zone.request_measurement` | set_bool | operator_zone_N_measurement_request |
| `zone.request_control` | set_bool | operator_zone_N_control_request |

#### Оператор

| Action | Handler | Cases |
|--------|---------|-------|
| `operator.apply_mode_change` | set_value | mode_requested |
| `operator.notify_safety` | notify | notify_safety |
| `operator.notify_command_failed` | notify | notify_failed_command, notify_timeout_command |
| `operator.notify_calibration_expired` | notify | notify_calibration_expired |

---

## 5. Справочник handler-ов

Handler — Go-функция, вызываемая ECA-движком при firing правила. Зарегистрирована в `ActionRegistry`. Не принимает бизнес-решений — только исполняет действие.

### 5.1 Store Writers

#### `set_value`
Записывает произвольное значение в Store slot.

- **Params**: `field` (string), `value` (any)
- **Writes**: `Store[field] = value`

#### `set_bool`
Записывает boolean-флаг.

- **Params**: `field` (string), `value` (bool)
- **Writes**: `Store[field] = true/false`

#### `clear_flag`
Сбрасывает один или несколько флагов.

- **Params**: `field` (string) или `fields` ([]string), `value` (any, по умолчанию false)
- **Writes**: `Store[*] = false`

### 5.2 Events

#### `emit_event`
Публикует ECA-событие через runtime engine.

- **Params**: `type` (string), `key`? (string), `partition_key`? (string)
- **Side effects**: `ctx.PublishEvent(frame)`
- **Deps**: `EngineRef`

### 5.3 Alerts & Notifications

#### `raise_alert`
Поднимает alert и записывает `fault.last.*`.

- **Params**: `slot` (string), `code` (string), `zone`? (int)
- **Writes**: alert.active=true, fault.last.code, fault.last.at
- **Side effects**: SQLite alert save (если Persistence настроен)

#### `clear_alert`
Снимает alert.

- **Params**: `slot` (string)
- **Writes**: alert.active=false

#### `notify`
Фиксирует факт уведомления оператора.

- **Params**: `slot` (string)
- **Writes**: operator.notified.*=true

### 5.4 UART & Response Processing

#### `send_uart_request`
Отправляет команду контроллеру по UART.

- **Params**: `payload` (string) ИЛИ `payload_template` (string с `{slot}` токенами)
- **Reads**: Store (при template-подстановке)
- **Writes**: `command.busy`, `command.current.*`, `command.last.*`
- **Side effects**: `deps.UART.Execute()`, `deps.Persistence.SaveUARTTrace()`
- **Deps**: `UART`, `Persistence?`, `Clock`

#### `parse_uart_response`
Разбирает `command.last.response_raw` в parsed sensor slots.

- **Params**: `zone`? (int, default из ответа)
- **Reads**: `command.last.response_raw`
- **Writes**: `zone.N.parsed.<kind>.value`, `.status`, `zone.N.sensor.<kind>.valid`
- **При ошибке**: `command.last.status = "failed"`, возвращает nil (не error!)

### 5.5 Measurements

#### `validate_measurement`
Проверяет parsed значение на допустимый диапазон.

- **Params**: `zone`, `kind`, `minimum`?, `maximum`?
- **Reads**: `zone.N.parsed.<kind>.value`
- **Writes**: `zone.N.sensor.<kind>.quality` ("accepted"/"rejected"), `.valid`, `measurement.validation_status`, `measurement.validation_error`

#### `store_measurement`
Переносит accepted parsed value в canonical measurement slot + SQLite.

- **Params**: `zone`, `kind`, `value`?
- **Reads**: `zone.N.parsed.<kind>.value`, `zone.N.sensor.<kind>.valid`
- **Writes**: `zone.N.<kind>.value`, `zone.N.measurement.updated_at`
- **Side effects**: `deps.Persistence.SaveMeasurement()`

### 5.6 Safety

#### `apply_interlock`
Включает safety lock с причиной.

- **Params**: `reason`? (string, default "interlock")
- **Writes**: `safety.locked = true`, `safety.reason`

### 5.7 Calibration

#### `store_calibration`
Записывает результат калибровки.

- **Params**: `zone`, `kind`, `reference_value`?
- **Writes**: `calibration.<kind>.status = "valid"`, `.last_success_at`, `.expires_at` (+30d), `.slope = 1.0`, `.offset = 0.0`, `.quality = "good"`
- **Side effects**: `deps.Persistence.SaveCalibration()`

### 5.8 Dosing Calculators

#### `calculate_ph_dose_plan`
Рассчитывает план дозирования pH (через ML или fallback-формулу).

- **Params**: `zone`, `volume_liters`, `min_dose_ml`, `max_dose_ml`, ...
- **Reads**: `zone.N.ph.value`, `.ph.min`, `.ph.max`, `.ec.value`, `.tds.value`, `.temp.value`, `.level.value`
- **Writes**: `zone.N.dose.ph_up_ml` / `.ph_down_ml`, `zone.N.dose.plan_ready`
- **Side effects**: `deps.Dosing.Plan()` — ML dosing service

#### `calculate_ec_dose_plan`
Рассчитывает план дозирования EC (nutrient/dilution).

- **Аналогично**: пишет `zone.N.dose.nutrient_ml` / `.water_ml` / `.dilution_ml`

### 5.9 Diagnostic

#### `noop`
Пустой handler, возвращает nil. Используется для chaining/dry-run.

### 5.10 Контракт handler-а

Handler должен:
- Быть коротким и делать одну вещь
- Быть по возможности идемпотентным
- Читать только нужные данные через `ctx.Get()`
- Писать ТОЛЬКО в declared outputs (в workspace-режиме — `UndeclaredOutputError` при нарушении)
- Явно возвращать ошибку при неудаче side effect
- НЕ принимать бизнес-решений (условия, безопасность, режим — в rules/cases)

---

## 6. Справочник модулей

### 6.1 UART Transport (`internal/uart/`)

**Назначение**: транспортный уровень обмена с контроллером.

**Интерфейс**: `Transport { Execute(ctx, payload) (Result, error); Ready() bool; Close() error }`

**Реализации**:
- `TransactionTransport` — реальный порт (go.bug.st/serial)
- `MockTransport` — эмуляция (заглушка с фиктивными ответами)

**Особенности**:
- Только одна активная транзакция в момент времени
- Буферизация входящих байт через `bufio.Reader`
- Поддержка CRC (checksum uint16)
- Повторы при таймауте (до `MaxRetries`)
- Хранение recent IDs для детекции дубликатов

### 6.2 Scheduler (`internal/scheduler/`)

**Назначение**: периодическая генерация тиков для запуска измерений.

**Поведение**:
- Тикер с интервалом `Scheduler.TickInterval` (по умолчанию 30s)
- На каждом тике: `scheduler.last_tick_at`, `runtime.modules.scheduler.heartbeat_at`, `zone.N.measurement.required=true`, `zone.N.control.required=true`
- Отправляет изменения через `engine.SetMany()`

### 6.3 Persistence (`internal/persistence/`)

**Назначение**: долговременное хранение данных в SQLite.

**Методы**:
- `SaveMeasurement`, `RecentMeasurements`
- `SaveDoseEvent`
- `SaveAlert`
- `SaveCalibration`
- `SaveSnapshot`, `LatestSnapshot`
- `SaveUARTTrace`
- `SaveLog`
- `SaveHeartbeat`

**Режим**: WAL, foreign keys ON, single connection (MaxOpenConns=1).

### 6.4 Dashboard (`internal/dashboard/`)

**Назначение**: HTTP API и web-интерфейс оператора. Подробно: [§7 HTTP API](#7-http-api-dashboard).

### 6.5 ML Dosing (`pkg/mldosing/`)

**Назначение**: расчёт безопасной дозы реагентов с ML-прогнозом или fallback-формулой.

**Компоненты**:
- `Predictor` — ML-модель (CatBoost C API через cgo) или Fallback (линейная формула)
- `Planner` — перебор доз с scoring
- `Guard` — 10 правил безопасности (sensor stability, direction check, max dose, hourly limit, mixing time, ...)
- `EventStore` — SQLite для dose_events и model_versions

**Scoring formula**:
```
score = 3.0 * |ph_pred - target| / tolerance
      + 1.5 * |tds_pred - target| / tolerance
      + 0.05 * dose_ml
      + penalties (overshoot, TDS ceiling, dose > 90% max)
```

**Fallback-формула**: `dose_ml = k * |error| * volume * safety_factor`

### 6.6 Workspace (`internal/workspace/`)

**Назначение**: кодогенерация data.toml и actions/*.toml из конфигурации зон.

**Методы**:
- `Materialize(dir, cfg)` — создаёт файлы `data.toml` и `actions/*.toml`
- `Load(dir, registry)` — загружает workspace через `WorkspaceLoader`

**Генерируемые сущности**:
- ~200 переменных в data.toml
- ~50+ action-файлов с cases для каждой зоны

---

## 7. HTTP API (Dashboard)

Базовый URL: `http://localhost:<port>`

### 7.1 Аутентификация

**Роли**: `admin` > `operator` > `viewer`

**POST /api/v2/login**
```json
{"user_id": "operator", "password": "operator"}
→ {"user_id": "operator", "role": "operator", "csrf_token": "..."}
```
Устанавливает cookie `hydropilot_session`.

**POST /api/v2/logout** — удаляет сессию.

**GET /api/v2/session** — возвращает текущую сессию.

### 7.2 Read-only (роль: viewer)

| Метод | Путь | Возвращает |
|-------|------|-----------|
| GET | `/health`, `/healthz`, `/api/v2/health` | Статус системы |
| GET | `/api/v2/dashboard/snapshot` | system + zones + stats |
| GET | `/api/v2/system` | system.mode, uart, firmware, scheduler, runtime stats |
| GET | `/api/v2/zones` | Список зон с ph/ec/tds/temp/level |
| GET | `/api/v2/measurements` | Последние 50 измерений из SQLite |
| GET | `/api/v2/alerts` | Активные alert-флаги |
| GET | `/api/v2/calibrations` | Калибровки (заглушка) |
| GET | `/api/v2/calibrations/latest` | Последние калибровки |
| GET | `/api/v2/actions/recent` | action status variables |
| GET | `/api/v2/events/stream` | SSE-поток событий |

### 7.3 Mutations (роль: operator, CSRF required)

| Метод | Путь | Назначение |
|-------|------|-----------|
| POST | `/api/v2/intents/system/mode` | `{"mode": "auto"}` → `operator.intent.mode_requested` |
| POST | `/api/v2/intents/estop` | `{"active": true}` → estop_requested / estop_clear_requested |
| POST | `/api/v2/intents/zones/<id>/measure` | `zone.N.measurement.required = true` |
| POST | `/api/v2/intents/zones/<id>/control` | `zone.N.control.required = true` |
| POST | `/api/v2/intents/zones/<id>/dose` | `{"reagent": "...", "ml": 5.0}` — ручное дозирование |
| POST | `/api/v2/intents/zones/<id>/irrigation` | Запрос полива |
| POST | `/api/v2/intents/zones/<id>/light` | `{"state": "on"/"off"}` |
| POST | `/api/v2/alerts/ack` | `operator.intent.safety_acknowledged` |
| POST | `/api/v2/calibration` | `{"zone_id": 1, "kind": "ph"}` → calibration intent |

### 7.4 Admin (роль: admin)

| Метод | Путь | Назначение |
|-------|------|-----------|
| GET | `/api/v2/store/slots?prefix=zone.1` | Все Store slots |
| GET | `/api/v2/config/snapshot` | Текущая конфигурация |
| POST | `/api/v2/config/draft` | Черновик конфига (заглушка) |
| POST | `/api/v2/config/diff` | Diff конфига (заглушка) |
| POST | `/api/v2/config/apply` | Применить конфиг (заглушка) |
| GET | `/api/v2/admin/logs` | Логи (заглушка) |
| GET | `/api/v2/admin/uart-traces` | UART traces |
| GET | `/api/v2/admin/uart-console` | UART console (read-only) |
| POST | `/api/v2/admin/uart-console/request` | Прямой UART — ЗАПРЕЩЁН |
| GET | `/api/v2/admin/runtime` | `engine.Stats()` |
| GET | `/api/v2/admin/faults` | `fault.last.code` |

### 7.5 Защита мутаций

- CSRF-токен в заголовке `X-CSRF-Token`
- Session cookie `hydropilot_session` (HttpOnly, SameSite=Strict)
- RBAC: viewer < operator < admin

---

## 8. База данных (SQLite)

Файл: `hydropilot-v5.db` (WAL режим).

### 8.1 Таблицы

| Таблица | Назначение | Ключевые поля |
|---------|-----------|--------------|
| `measurements` | История измерений | zone_id, sensor_type, value, created_at |
| `dose_events` | События дозирования (ML) | zone_id, reagent_type, dose_ml, ph_before/after, predicted_delta_*, model_version |
| `alerts_history` | История alert-ов | alert_code, zone_id, raised_at, cleared_at, acknowledged |
| `calibrations` | История калибровок | zone_id, kind, reference_value, slope, offset, quality |
| `store_snapshots` | Снимки Store для восстановления | snapshot (JSON), created_at |
| `uart_traces` | Трассировка UART-обмена | action, case_name, payload, response_raw, status, latency_ms |
| `logs` | Системный журнал | level, source, message, fields (JSON) |
| `runtime_heartbeats` | Heartbeat модулей | module_name, ready, last_error, observed_at |
| `model_versions` | Версии ML-моделей | version, model_path, metrics (JSON), status |
| `pump_calibrations` | Калибровка насосов | pump_id, reagent_type, ml_per_sec |

### 8.2 Схема (ключевые индексы)

```sql
CREATE INDEX idx_measurements_zone_sensor_time ON measurements(zone_id, sensor_type, created_at);
CREATE INDEX idx_uart_traces_time ON uart_traces(created_at);
CREATE INDEX idx_logs_time ON logs(created_at);
```

---

## 9. ECA-движок: устройство и API

Полная документация: `ECA-lib/docs/`. Здесь — краткий справочник.

### 9.1 Ключевые типы

```go
// Engine — главный runtime-объект
type Engine struct { ... }

// Config — параметры движка
type Config struct {
    ActionRegistry     *ActionRegistry
    EventCatalog       EventCatalog
    PartitionQueueSize int  // default: 64
    MaxFiringsPerEvent int  // default: 128
    Clock              func() time.Time
}

// RuleSpec — low-level правило
type RuleSpec struct {
    Name, When         string
    Priority           int
    Then               ActionCall
    DependsOn, Inputs, Outputs []VariableID
    Cooldown           time.Duration
    PartitionSelector  string  // "" или "event"
}

// ActionContext — интерфейс handler-а к runtime
type ActionContext interface {
    Context() context.Context
    Event() EventView
    Rule() FiredRule
    Action() ActionInvocation
    Get(VariableID) (any, bool)
    Set(VariableID, any) error
    AcquireEvent(EventTypeID) *EventFrame
    PublishEvent(*EventFrame) error
    Store() Store
}
```

### 9.2 Жизненный цикл Engine

```go
registry := eca.NewActionRegistry()
registry.MustRegister("my_action", myHandler)

engine, _ := eca.NewEngine(eca.Config{ActionRegistry: registry})
defer engine.Close()

// Способ 1: Low-level правила
engine.LoadRules([]eca.RuleSpec{{Name: "r1", When: "x > 0", Then: eca.ActionCall{Name: "my_action"}}})

// Способ 2: Workspace
loader := eca.NewWorkspaceLoader(registry)
workspace, report, _ := loader.LoadDir("rules")
engine.LoadWorkspace(workspace)

engine.Start(ctx)
engine.Set(ctx, "x", 42)
```

### 9.3 Workspace-формат

**data.toml:**
```toml
version = 1

[[data]]
name = "temp"
type = "number"

[[data]]
name = "alerts.hot"
type = "bool"
default = false
```

**actions/heat_alarm.toml:**
```toml
name = "heat_alarm"
handler = "mark_hot"

[[cases]]
name = "over_limit"
priority = 100
match = "all"
inputs = ["temp", "limit", "machine.enabled"]
outputs = ["alerts.hot"]

[[cases.conditions]]
kind = "compare_data"
left = "temp"
op = "gt"
right = "limit"
```

**Виды условий (cases.conditions)**:
- `kind = "data"` — сравнение переменной с константой (op: eq, ne, gt, gte, lt, lte, exists, missing)
- `kind = "compare_data"` — сравнение двух переменных (left op right)
- `kind = "action"` — статус другого action (fired, succeeded, failed)

### 9.4 When-выражения (low-level)

Поддерживаемые операторы: `&&`, `||`, `!`, `==`, `!=`, `>`, `>=`, `<`, `<=`

Доступны:
- State-переменные напрямую: `temp > 100`
- Nested state: `state.machine.enabled == true`
- Event metadata: `event.type`, `event.key`, `event.partition_key`
- Event fields: `event.payload.seq`, `event.meta.source`
- Литералы: `nil`, `true`, `false`, строки, числа

### 9.5 Generated action statuses

Для каждого action автоматически создаются virtual-переменные:
- `actions.<name>.fired`
- `actions.<name>.succeeded`
- `actions.<name>.failed`

### 9.6 Диагностика

```go
stats := engine.Stats()
// RulesLoaded, Published, Processed, Fired,
// EvaluationErrors, ActionErrors, CycleStops,
// Partitions, ActiveRules

snapshot := engine.DebugSnapshot() // map[string]any всего Store
```

---

## 10. Конфигурация

Файл: `config/hydropilot.toml` (TOML).

### 10.1 Секция `[app]`

```toml
[app]
name = "HydroPilot"
version = "5.0.0"
```

### 10.2 Секция `[http]`

```toml
[http]
addr = ":8080"
```

### 10.3 Секция `[uart]`

```toml
[uart]
mode = "mock"             # "mock" или "real"
port = "COM4"             # для real mode
baud_rate = 115200
timeout_ms = 2000         # общий таймаут
ok_timeout_ms = 500       # таймаут OK
result_timeout_ms = 2000  # таймаут RESULT/ERROR
max_retries = 3
retry_backoff_ms = 50
frame_terminator = "\n"
crc_enabled = true
mock_latency_ms = 1       # для mock mode
recent_id_capacity = 64
```

### 10.4 Секция `[sqlite]`

```toml
[sqlite]
path = "hydropilot-v5.db"
```

### 10.5 Секция `[scheduler]`

```toml
[scheduler]
enabled = true
tick_interval_ms = 30000         # интервал тиков
measurement_every_ms = 300000    # интервал измерений
control_every_ms = 600000        # интервал контроля
```

### 10.6 Секция `[auth]`

```toml
[auth]
session_ttl_min = 60

[[auth.users]]
id = "admin"
role = "admin"
password = "admin"
```

### 10.7 Секция `[ml_dosing]`

```toml
[ml_dosing]
enabled = false
backend = "catboost_cgo"    # или "grpc", "fallback"
model_path = ""
library_path = ""
ph_deadband = 0.03
ph_tolerance = 0.08
ec_tolerance = 0.1
safety_factor = 0.6
dose_step_ml = 0.1
max_cycles_per_run = 5
```

### 10.8 Секция `[[zones]]`

```toml
[[zones]]
id = 1
name = "Zone 1"
volume_liters = 80
ph_min = 5.8
ph_max = 6.5
ec_min = 1.2
ec_max = 2.0
tds_min = 600
tds_max = 1000
temp_max = 30
level_min = 20
level_critical = 10
dose_enabled = true
dilution_available = true
irrigation_enabled = true
ph_up_reagent_id = "PH_UP_1"
ph_down_reagent_id = "PH_DOWN_1"
nutrient_reagent_id = "NUTRIENT_A"
water_reagent_id = "WATER_1"
ph_up_pump_id = "PH_UP_1"
ph_down_pump_id = "PH_DOWN_1"
nutrient_pump_id = "NUTRIENT_A"
water_pump_id = "WATER_1"
max_dose_ml = 50
max_hourly_dose_ml = 200
min_dose_ml = 1
pump_ml_per_sec = 1
measurement_interval_ms = 300000
control_interval_ms = 600000
```

---

## 11. Жизненный цикл приложения

### 11.1 Startup

```
1. Загрузка конфигурации (config.Load)
2. Открытие SQLite (persistence.Open) + миграция схемы
3. Инициализация UART транспорта (uart.NewTransport)
4. Создание ActionRegistry
5. Построение ML dosing service
6. Регистрация 17 handler-ов (handlers.RegisterAll)
7. Подготовка workspace:
   a. Если rules/data.toml существует → использовать как есть
   b. Иначе → workspace.Materialize(tmp, cfg) — кодогенерация
8. Загрузка workspace (WorkspaceLoader.LoadDir → engine.LoadWorkspace)
9. Создание Engine (eca.NewEngine) + Start()
10. Заполнение runtime начальными данными (seedRuntime):
    system.ready, system.started_at, uart.ready, uart.connected,
    runtime.modules.*.ready, zone.N.dose.enabled, zone.N.reagent.*
11. Восстановление snapshot из SQLite (LatestSnapshot → SetMany)
12. Запуск scheduler (sched.Run)
13. Запуск HTTP dashboard (dashboard.Serve)
```

### 11.2 Runtime

- **Scheduler** периодически выставляет `zone.N.measurement.required = true`
- Это запускает каскад: измерение → разбор → валидация → сохранение → анализ → дозирование
- **Dashboard** обслуживает HTTP-запросы и SSE-поток
- **UART transport** обрабатывает команды последовательно

### 11.3 Shutdown

```
1. SIGTERM/SIGINT → ctx отмена
2. Прекращение приёма мутирующих HTTP-запросов
3. Ожидание завершения текущих action (graceful)
4. Сохранение финального snapshot (engine.DebugSnapshot → SQLite)
5. Закрытие: scheduler, UART, persistence, engine
6. system.ready = false (неявно, через Close)
```

---

## 12. Сценарии и цепочки

### 12.1 Полный измерительный цикл

```
scheduler.tick
  → scheduler.last_tick_at = now
  → zone.1.measurement.required = true
  → zone.1.control.required = true

scheduler.mark_zone_due (priority 500)
  when: zone.1.measurement.required == true
  → (уже установлено тиком)

uart.read_ph (priority 600)
  when: zone.1.measurement.required == true && uart.ready && !safety.locked
  → send_uart_request("1:SENSOR:PH:READ:0")
  → command.busy = true
  → UART: 1:CMD:1:SENSOR:PH:READ:0 → OK → RESULT: 1:RESP:OK:SENSOR:PH:6.10
  → command.last.status = "ok", command.last.response_raw = "1:RESP:OK:SENSOR:PH:6.10"

(uart.read_ec → uart.read_tds → uart.read_temp → uart.read_level — аналогично, цепочкой)

parse_uart_response (priority 590)
  when: command.last.status == "ok"
  → parse "1:RESP:OK:SENSOR:PH:6.10" → zone.1.parsed.ph.value = 6.10, .status = "ok", valid = true

sensors.validate_ph (priority 570)
  when: zone.1.parsed.ph.status == "ok"
  → проверка: 0 ≤ 6.10 ≤ 14 → accepted, valid = true

sensors.store_ph_measurement (priority 550)
  when: zone.1.sensor.ph.valid == true
  → zone.1.ph.value = 6.10, measurement.updated_at = now
  → SQLite: INSERT INTO measurements

scheduler.clear_zone_due (priority 450)
  when: zone.1.measurement.required == true
  → zone.1.measurement.required = false, zone.1.control.required = false
```

### 12.2 Цепочка коррекции pH (пониженный pH → pH Up)

```
(после сохранения измерения: zone.1.ph.value = 5.42, ph.min = 5.8)

solution.detect_ph_low (priority 800)
  when: zone.1.ph.value < zone.1.ph.min
  → zone.1.ph.low = true

alerts.raise_ph_low (priority 900)
  when: zone.1.ph.low == true && alerts.ph.low.active == false
  → alerts.ph.low.active = true, fault.last.code = "PH_LOW", fault.last.at = now
  → SQLite: INSERT INTO alerts_history

solution.require_ph_up (priority 700)
  when: zone.1.ph.low == true && system.mode == "auto" && dose.enabled && !safety.locked
  → zone.1.dose.ph_up_required = true

solution.plan_ph_dose (priority 650)
  when: zone.1.dose.ph_up_required == true && zone.1.dose.plan_ready == false
  → calculate_ph_dose_plan: target = (5.8+6.5)/2 = 6.15, error = 0.73
  → fallback: dose = 0.02 * 0.73 * 80 * 0.6 = 0.70 → clamp(1, 50) = 1.0 ml
  → zone.1.dose.ph_up_ml = 1.0, zone.1.dose.plan_ready = true

uart.send_ph_up (priority 600)
  when: zone.1.dose.ph_up_required == true && zone.1.dose.plan_ready == true && uart.ready
  → send_uart_request("1:ACT:DOSE:RUN:PH_UP_1:1.0")
  → UART: команда → OK → RESULT

solution.clear_dose_intent (priority 500)
  when: actions.uart.send_ph_up.succeeded == true
  → reset: ph_up_required, ph_down_required, nutrient_required, dilution_required, plan_ready = false
```

### 12.3 Цепочка безопасности (E-Stop)

```
operator.intent.estop_requested = true  (через HTTP API)

safety.apply_interlock (priority 950)
  when: operator.intent.estop_requested == true
  → safety.locked = true, safety.reason = "estop"

safety.estop_on (priority 1000)
  when: operator.intent.estop_requested == true && uart.ready
  → send_uart_request("0:SYS:ESTOP:SET:ON")
  → UART: команда → OK → RESULT

alerts.raise_interlock_blocked (priority 900)
  when: safety.locked == true
  → alerts.interlock_blocked.active = true, fault.last.code = "INTERLOCK_BLOCKED"

operator.notify_safety (priority 300)
  when: safety.locked == true && operator.notified.safety == false
  → operator.notified.safety = true
```

### 12.4 Цепочка калибровки

```
operator.intent.calibration.ph_requested = true (через HTTP API)

calibration.run_ph (priority 620)
  when: zone.1.operator.intent.calibration.ph_requested == true && uart.ready
  → send_uart_request("1:CAL:PH:RUN:7.0")
  → UART: команда → OK → RESULT

calibration.store_result (priority 580)
  when: actions.calibration.run_ph.succeeded == true
  → zone.1.calibration.ph.status = "valid", .last_success_at = now, .expires_at = now+30d
  → SQLite: INSERT INTO calibrations

calibration.clear_expired (priority 500)
  when: calibration.ph.status == "valid" && alerts.calibration.expired.active == true
  → alerts.calibration.expired.active = false
```

---

## Ссылки на документацию ECA-библиотеки

- `ECA-lib/README.md` — обзор и быстрый старт
- `ECA-lib/docs/architecture.md` — архитектура runtime
- `ECA-lib/docs/api-reference.md` — полный API-справочник
- `ECA-lib/docs/eca-handbook.md` — руководство пользователя
- `ECA-lib/docs/data-structures.md` — структуры данных
- `ECA-lib/docs/programming-patterns.md` — паттерны проектирования
- `ECA-lib/docs/toml-reference.md` — low-level TOML-формат
- `ECA-lib/docs/workspace-reference.md` — workspace-формат
- `ECA-lib/docs/fsm-to-eca.md` — миграция с FSM
