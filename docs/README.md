# HydroPilot ECA v5

Активная документация системы автоматизации гидропоники HydroPilot v5 на базе ECA-движка.

**Главный принцип**: Handler — исполнитель, Case — причина, Store — картина мира, Workspace — контракт между ними.

## Карта документации

### Основные документы

| Документ | Назначение |
|----------|-----------|
| [Единая техническая документация](hydropilot-v5-docs.md) | Полный справочник: архитектура, UART-протокол, Store slots, actions, handler-ы, HTTP API, SQLite, жизненный цикл, сценарии |
| [Полный анализ документации](full-analysis-eca-hydropilot.md) | Сопоставление документации и кода, расхождения, пробелы, рекомендации |

### ECA-библиотека

| Документ | Назначение |
|----------|-----------|
| [README](../ECA-lib/README.md) | Обзор ECA-библиотеки, быстрый старт |
| [API Reference](../ECA-lib/docs/api-reference.md) | Полный справочник публичного API |
| [Архитектура](../ECA-lib/docs/architecture.md) | Устройство runtime, pipeline, конкуррентная модель |
| [Handbook](../ECA-lib/docs/eca-handbook.md) | Пошаговое руководство пользователя |
| [Структуры данных](../ECA-lib/docs/data-structures.md) | Все структуры данных и их взаимодействие |
| [Паттерны](../ECA-lib/docs/programming-patterns.md) | Приёмы проектирования правил и handler-ов |
| [TOML Reference](../ECA-lib/docs/toml-reference.md) | Low-level формат правил |
| [Workspace Reference](../ECA-lib/docs/workspace-reference.md) | High-level workspace-формат |
| [FSM → ECA](../ECA-lib/docs/fsm-to-eca.md) | Миграция с конечных автоматов |

### Модули Hydropilot

| Документ | Назначение |
|----------|-----------|
| [UART Transport](02_modules/uart_transport.md) | Транспортный протокол обмена с контроллером |
| [UART Payload Catalog](02_modules/uart_payload_catalog.md) | Каталог команд контроллера |
| [Fault Taxonomy](02_modules/fault_taxonomy.md) | Классификация ошибок и коды |
| [Runtime Lifecycle](02_modules/runtime_lifecycle.md) | Startup, Runtime, Shutdown |
| [Config Hot-Reload](02_modules/config_hotreload.md) | Безопасное обновление конфигурации |
| [Plant Profiles](02_modules/plant_profiles.md) | Профили растений и стадии роста |
| [Deploy](02_modules/deploy.md) | Схема развёртывания (systemd, Ansible) |

### Handler-ы

| Документ | Назначение |
|----------|-----------|
| [Handlers MOC](04_handlers/handlers_moc.md) | Обзор всех 17 handler-ов и их контрактов |
| [send_uart_request](04_handlers/send_uart_request.md) | Отправка команд контроллеру |
| [parse_uart_response](04_handlers/parse_uart_response.md) | Разбор ответов контроллера |
| [calculate_ph_dose_plan](04_handlers/calculate_ph_dose_plan.md) | Расчёт дозы pH |
| [calculate_ec_dose_plan](04_handlers/calculate_ec_dose_plan.md) | Расчёт дозы EC |
| [set_value](04_handlers/set_value.md) | Универсальная запись в Store |
| [build_payload](04_handlers/build_payload.md) | Внутренний механизм сборки payload (не handler) |

### Архив

Архивные документы находятся в `_archive/` и используются только как источник миграции. Актуальный контракт описан в [legacy_exclusions.md](legacy_exclusions.md).

## Быстрый вход

1. **Понять архитектуру** → [hydropilot-v5-docs.md §1](hydropilot-v5-docs.md#1-архитектурный-обзор)
2. **Увидеть протокол** → [hydropilot-v5-docs.md §2](hydropilot-v5-docs.md#2-протокол-обмена-с-контроллером-uart)
3. **Найти переменную** → [hydropilot-v5-docs.md §3](hydropilot-v5-docs.md#3-справочник-сущностей-store-slots)
4. **Понять цепочки** → [hydropilot-v5-docs.md §12](hydropilot-v5-docs.md#12-сценарии-и-цепочки)
