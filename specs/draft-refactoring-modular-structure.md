# Модульная структура hyperhandler + Risk Management

**Version:** v0.1 (draft)
**Spec ID:** SPEC-004
**Created:** 2026-02-16 19:00

---

## Цель

Рефакторинг структуры проекта для интеграции Risk Management модуля с сохранением обратной совместимости.

---

## Текущая структура (as-is)

```
src/hyperhandler/
├── cli.py                    # Монолит: все команды
├── config.py                 # YAML + env
├── signer.py                 # EIP-712
├── storage.py                # SQLite (signals, orders)
├── utils.py
│
├── models/
│   ├── signal.py             # TradingSignal
│   ├── order.py              # OrderResult, Position
│   ├── vault.py              # VaultInfo
│   └── validator.py          # SignalValidator (format + risk limits смешаны)
│
├── client/
│   ├── base.py               # BaseClient (retry, errors)
│   ├── info.py               # InfoClient (read-only)
│   ├── exchange.py           # ExchangeClient (trading)
│   ├── vault.py              # VaultClient
│   └── order_builder.py      # Signal → API payload
│
└── wallet/
    ├── manager.py
    └── providers/
```

## Проблемы текущей структуры

1. **`cli.py` — монолит.** Все команды в одном файле. С добавлением risk commands
   (check, status, reset) файл станет неуправляемым.

2. **`validator.py` смешивает format и risk.** `max_position_size_usd`, `max_leverage`,
   `require_stop_loss` — это risk limits, а не format validation.

3. **`storage.py` — один файл на всё.** Сейчас 2 таблицы, после risk будет 4.
   Нужно предусмотреть миграции.

4. **`order_builder.py` не знает о TradeOrder.** Сейчас строит payload из
   TradingSignal напрямую. Нужен второй путь: из TradeOrder.

---

## Целевая структура (to-be)

```
src/hyperhandler/
├── __init__.py
├── config.py                     # + RiskSettings
├── signer.py                     # Без изменений
├── storage.py                    # + trade_results, risk_decisions + migrations
├── utils.py                      # Без изменений
│
├── models/
│   ├── __init__.py
│   ├── signal.py                 # TradingSignal + SignalHorizon, confidence, source
│   ├── order.py                  # OrderResult, Position + risk fields
│   ├── vault.py                  # Без изменений
│   ├── validator.py              # Упрощённый: только format checks
│   └── risk.py                   # ★ NEW: RiskLevel, TradeOrder, RiskReject, etc.
│
├── client/
│   ├── __init__.py
│   ├── base.py                   # Без изменений
│   ├── info.py                   # + get_candles(), get_asset_ctx(), get_funding_rate()
│   ├── exchange.py               # Без изменений
│   ├── vault.py                  # Без изменений
│   └── order_builder.py          # + build_from_trade_order()
│
├── risk/                         # ★ NEW MODULE
│   ├── __init__.py               # Публичный API: RiskManager, RiskCalculator
│   ├── manager.py                # RiskManager (orchestrator)
│   ├── calculator.py             # ATR, position sizing, leverage, cumulative risk
│   ├── circuit_breaker.py        # CircuitBreaker
│   ├── collector.py              # TradeResultCollector
│   └── config.py                 # RiskProfile, HLConfig, RISK_PROFILES
│
├── cli/                          # ★ REFACTOR (Phase 2, post-MVP)
│   ├── __init__.py
│   ├── risk_commands.py          # risk check, risk status, risk reset
│   └── ... (остальные команды — follow-up)
│
└── wallet/                       # Без изменений
    ├── __init__.py
    ├── manager.py
    └── providers/
```

---

## Ключевые решения

### 1. CLI split: минимальный подход

**Для MVP:** Оставить `cli.py` + добавить `cli/risk_commands.py`.
**Post-MVP:** Полный split если cli.py > 800 строк.

```python
# cli.py — добавить в конец
from hyperhandler.cli.risk_commands import risk_app
app.add_typer(risk_app, name="risk")
```

### 2. `risk/` как отдельный пакет

Зависимости (однонаправленные):

```
cli.py
  │
  ▼
risk/manager.py ──────► models/risk.py
  │                     models/signal.py
  │                     models/order.py
  ▼
risk/calculator.py ───► risk/config.py
risk/circuit_breaker.py
risk/collector.py
  │
  ▼
client/info.py          # Market data
storage.py              # Persistence
```

**Важно:** `risk/` зависит от `models/` и `client/`, но НЕ наоборот.

### 3. `models/risk.py` — НЕ в `risk/models.py`

Все Pydantic модели в `models/`. TradeOrder используется в risk/, cli/, order_builder.
Размещение в `risk/` создаст циклическую зависимость.

### 4. Storage migrations

```python
# storage.py
SCHEMA_VERSION = 2  # Bump при изменениях

def _check_schema_version(self):
    """Auto-migrate if needed."""
    current = self._get_schema_version()
    if current < SCHEMA_VERSION:
        self._migrate(current, SCHEMA_VERSION)
```

### 5. `order_builder.py` — Union интерфейс

```python
def build_order_payload(
    self,
    source: TradingSignal | TradeOrder,
    ...
) -> dict:
    if isinstance(source, TradeOrder):
        return self._build_from_trade_order(source)
    return self._build_from_signal(source)
```

---

## Что НЕ меняется

- `signer.py` — без изменений
- `wallet/` — без изменений
- `client/base.py`, `exchange.py`, `vault.py` — без изменений
- `utils.py` — без изменений

---

## Тестовая структура

```
tests/
├── unit/
│   ├── test_risk_calculator.py    # NEW
│   ├── test_risk_manager.py       # NEW
│   ├── test_circuit_breaker.py    # NEW
│   └── ...
├── integration/
│   ├── test_risk_integration.py   # NEW
│   └── ...
```

---

## Порядок реализации

### Phase 0: Подготовка
1. `mkdir -p src/hyperhandler/cli`
2. `touch src/hyperhandler/cli/__init__.py`
3. `touch src/hyperhandler/cli/risk_commands.py`

### Phase 1: Models & Config (из risk spec)
4. Создать `models/risk.py`
5. Расширить `models/signal.py`
6. Расширить `models/order.py`
7. Создать `risk/__init__.py`, `risk/config.py`
8. Упростить `validator.py`

### Phase 2-4: Calculator, CB, InfoClient (из risk spec)
9. Создать `risk/calculator.py`
10. Создать `risk/circuit_breaker.py`
11. Создать `risk/collector.py`
12. Расширить `client/info.py`
13. Расширить `storage.py` + migrations

### Phase 5: Integration (из risk spec)
14. Создать `risk/manager.py`
15. Добавить `build_from_trade_order()` в order_builder
16. Заполнить `cli/risk_commands.py`
17. Обновить `cli.py` (exec с --risk-level)

### Phase 6: CLI split (post-MVP, optional)
18. Вынести остальные команды если cli.py > 800 строк

---

## Dependency Graph

```
                    cli.py
                   ╱      ╲
                  ╱        ╲
              exec()    risk_commands.py
              │  ╲          │
              │   ╲         │
              ▼    ╲        ▼
          client/   ╲   risk/
         ╱  │  ╲    ╲  ╱  │  ╲
        ╱   │   ╲    ╲╱   │   ╲
   info  exchange  order  manager  calculator  circuit_breaker
     │      │    builder    │         │              │
     ▼      ▼       ▼       ▼         ▼              ▼
                  models/
              ╱    │    ╲
           signal order  risk
              │
              ▼
           config.py        storage.py        signer.py
```

**Правило:** стрелки только вниз. Нет циклов.

---

## Критерии приёмки

- [ ] Dependency graph без циклов
- [ ] `models/risk.py` содержит все risk-related модели
- [ ] `risk/` модуль изолирован и тестируем отдельно
- [ ] Storage migrations работают на существующей БД
- [ ] CLI backwards compatible (старые команды работают)
- [ ] Нет регрессий в существующих тестах
