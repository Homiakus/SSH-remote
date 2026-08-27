# HydroPilot v5 — Документация API

> Этот документ — ссылка на канонический API-справочник ECA-библиотеки. HydroPilot v5 использует библиотеку `go-library-fsm` (ECA-lib) как ядро runtime.

## Канонический источник

Полный API-справочник находится в файле:

**[../ECA-lib/docs/api-reference.md](../ECA-lib/docs/api-reference.md)**

Он содержит:
- Раздел 1-2: Быстрый старт и базовая модель
- Раздел 3: Runtime API (Config, Engine, Start/Close, LoadRules/LoadWorkspace, Stats)
- Раздел 4: State API (Set, SetMany, Get, Store, DebugSnapshot)
- Раздел 5: Rules и actions (RuleSpec, ActionCall, ActionHandler, ActionContext)
- Раздел 6: Low-level TOML loader (Loader, LoadReport)
- Раздел 7: Event API (EventCatalog, EventFrame, PublishEvent)
- Раздел 8: Workspace API (WorkspaceLoader, WorkspaceSpec, DataCatalog, Actions, Cases, Conditions)
- Раздел 9: FactAdapter
- Раздел 10: Ошибки
- Раздел 11: Статистика и мониторинг

## HydroPilot-специфичное

HydroPilot дополняет ECA-библиотеку следующими компонентами (документированы отдельно):

- **Handler-ы**: 17 зарегистрированных Go-функций в [handlers_moc.md](04_handlers/handlers_moc.md)
- **UART-протокол**: обмен сигналами с контроллером в [uart_transport.md](02_modules/uart_transport.md) и [uart_payload_catalog.md](02_modules/uart_payload_catalog.md)
- **Dashboard HTTP API**: эндпоинты в [основной документации](hydropilot-v5-docs.md#7-http-api-dashboard)
- **Store slots**: полный каталог переменных в [основной документации](hydropilot-v5-docs.md#3-справочник-сущностей-store-slots)
- **Actions**: полный список в [основной документации](hydropilot-v5-docs.md#4-справочник-действий-actions)
