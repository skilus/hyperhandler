# Risk Management Resilience Hardening

**Version:** v1.0 (approved)
**Spec ID:** SPEC-006
**Created:** 2026-03-31 12:00
**Work Started:** 2026-03-31 12:00

---

## Цель / Goal

Устранить выявленные при аудите уязвимости в системе управления рисками, которые позволяют обходить защитные механизмы (кумулятивный лимит, circuit breaker) или принимать решения на некорректных данных.

## Контекст / Context

Аудит выявил 10 проблем (3 CRITICAL/HIGH, 5 MEDIUM, 2 LOW). Спека покрывает все 10, сгруппированные по приоритету.

---

## Группа A: Critical / High (блокеры)

### A1. Обогащение `Position.risk_amount` перед оценкой

**Проблема:** Позиции из HL API приходят с `risk_amount=None`, кумулятивный риск считается как 0.

**Решение:**
- В `RiskManager.evaluate_signal` после получения позиций — вызвать `_enrich_positions(positions, info_client)`
- Для каждой позиции с `risk_amount is None`:
  - Если есть `stop_loss_price`: `risk_amount = abs_size * |entry - stop|`
  - Если есть `liquidation_price`: `risk_amount = abs_size * |entry - liq| * 0.9`
  - Fallback: `risk_amount = position_value / leverage` (грубая оценка = initial margin)
- Заполнить `correlation_group` через `_get_correlation_group`
- В `evaluate_signal_with_data` — добавить guard: если хотя бы одна позиция имеет `risk_amount=None`, логировать warning (не reject, чтобы не блокировать торговлю)

**Файлы:** `risk/manager.py`

### A2. Guard на `account_value <= 0`

**Проблема:** При нулевом или отрицательном equity торговля продолжается.

**Решение:**
- В начале `evaluate_signal_with_data`, сразу после circuit breaker:
```python
if account_value <= Decimal("0"):
    return RiskReject(
        reason=RejectReason.INSUFFICIENT_MARGIN,
        details=f"Account value ${account_value} <= 0",
        suggested_action="wait",
    )
```

**Файлы:** `risk/manager.py`

### A3. Автозагрузка trade_history из storage

**Проблема:** CB обходится если caller не передал trade_history.

**Решение:**
- В `evaluate_signal`, если `trade_history is None` и `storage is not None`:
```python
if trade_history is None and storage is not None:
    collector = TradeResultCollector(storage, network)
    trade_history = collector.get_recent_results(limit=50)
```
- Если `trade_history is None` и `storage is None` — логировать `logger.warning("No trade history available, circuit breaker disabled")`

**Файлы:** `risk/manager.py`

---

## Группа B: Medium (защита от неконсистентности)

### B1. Проверка `max_signal_age`

**Проблема:** Конфиг `max_signal_age_seconds=300` существует, но не используется.

**Решение:**
- В `evaluate_signal_with_data` после deviation check:
```python
if signal.timestamp:
    age = (datetime.now(timezone.utc) - signal.timestamp).total_seconds()
    if age > self.hl_config.max_signal_age_seconds:
        return RiskReject(reason=RejectReason.STALE_SIGNAL, ...)
```
- Если `signal.timestamp is None` — пропустить (обратная совместимость)

**Файлы:** `risk/manager.py`

### B2. Корреляционный штраф для новой позиции

**Проблема:** `new_risk_amount` добавляется в `total_adjusted` без корреляционного штрафа.

**Решение:**
- В `calculate_cumulative_risk` вместо `total_adjusted = adjusted_risk + cascade_buffer + new_risk_amount`:
```python
# Apply correlation penalty to new position too
new_group = self._get_correlation_group(new_coin)
new_risk_adjusted = new_risk_amount
if new_group in groups and len(groups[new_group]) > 1:
    penalty = Decimal("1") + self.profile.correlation_factor
    new_risk_adjusted = new_risk_amount * penalty

total_adjusted = adjusted_risk + cascade_buffer + new_risk_adjusted
```

**Файлы:** `risk/calculator.py`

### B3. Предупреждение о хедж-сигнале

**Проблема:** Противоположный сигнал на существующую позицию проходит без предупреждения.

**Решение:**
- В блоке проверки дубликатов, если `not is_same_side` (противоположная сторона):
  - Добавить в `calculation_details` результирующего `TradeOrder`: `"warning": "will_flip_position", "existing_side": pos.side, "existing_size": str(pos.abs_size)`
  - Логировать `logger.warning(f"Signal will flip {pos.coin} position from {existing_side} to {signal.side}")`
- Не блокировать — это валидная операция, но с аудитом

**Файлы:** `risk/manager.py`

### B4. Параллельные API-запросы

**Проблема:** Последовательные запросы дают неконсистентный snapshot.

**Решение:**
- В `evaluate_signal` заменить последовательные вызовы на `asyncio.gather`:
```python
account_state, positions, asset_meta, mark_price, funding_rate = await asyncio.gather(
    info_client.get_account_state(address),
    info_client.get_positions(address),
    info_client.get_asset_info(signal.pair),
    info_client.get_mid_price(signal.pair),
    info_client.get_funding_rate(signal.pair),
)
```
- Запрос свечей (`get_candles`) — отдельно после, т.к. зависит от условия

**Файлы:** `risk/manager.py`

### B5. Cross-margin ликвидация — disclaimer

**Проблема:** `estimate_liquidation_price` не учитывает портфельный эффект cross margin.

**Решение:**
- Использовать реальную `liquidation_price` из позиции HL API когда доступна (для существующих позиций)
- Для новой позиции — оставить формулу, но добавить в `calculation_details`: `"liq_source": "estimated_isolated"`
- В `_evaluate_manual` и `_evaluate_managed`, если `margin_mode == "cross"` и есть другие cross-позиции — увеличить `liq_safety_buffer` на 1% (с 2% до 3%)

**Файлы:** `risk/calculator.py`, `risk/manager.py`

---

## Группа C: Low (улучшения)

### C1. ATR quality warning

**Проблема:** ATR на 2-3 свечах тихо деградирует.

**Решение:**
- `calculate_atr` возвращает `ATRResult(value: Decimal, quality: str, candles_used: int)`
- В `_evaluate_managed` если `quality == "partial"` — добавить в `calculation_details`: `"atr_quality": "partial", "atr_candles": N`
- Если `candles_used < 5` — reject с `ATR_UNAVAILABLE` ("Only N candles available, need at least 5")

**Файлы:** `risk/calculator.py`, `risk/manager.py`, `models/risk.py` (новый `ATRResult`)

### C2. Funding check без SL (MANUAL)

**Проблема:** Фандинг не проверяется если `signal.stop_loss is None`.

**Решение:**
- В `_evaluate_manual`, убрать `if signal.stop_loss:` обёртку вокруг funding check
- Использовать уже вычисленный `risk_amount` (который при отсутствии SL = `size * entry_price`)

**Файлы:** `risk/manager.py`

---

## Файлы / Files

| Файл | Изменения |
|---|---|
| `src/hyperhandler/risk/manager.py` | A1, A2, A3, B1, B3, B4, B5, C2 |
| `src/hyperhandler/risk/calculator.py` | B2, B5, C1 |
| `src/hyperhandler/models/risk.py` | C1 (ATRResult) |
| `tests/unit/test_risk_*.py` | Тесты на все фиксы |
| `tests/integration/test_risk_integration.py` | Обновление интеграционных тестов |

## Порядок реализации

1. **A1 + A2 + A3** — критические фиксы, одним коммитом
2. **B2 + B1 + C2** — чистые изменения логики, без новых зависимостей
3. **B4** — asyncio.gather (рефакторинг evaluate_signal)
4. **B3 + B5** — warnings и disclaimers
5. **C1** — ATRResult (изменение интерфейса calculator)
6. Обновление тестов на каждом шаге

## Критерии приёмки / Acceptance Criteria

- [ ] Позиции с `risk_amount=None` обогащаются перед кумулятивной проверкой
- [ ] `account_value <= 0` → reject
- [ ] `trade_history=None` при наличии storage → автозагрузка из storage
- [ ] `max_signal_age_seconds` проверяется для сигналов с timestamp
- [ ] Новая позиция получает корреляционный штраф в `calculate_cumulative_risk`
- [ ] Хедж-сигнал логируется с warning
- [ ] API-запросы в `evaluate_signal` выполняются параллельно
- [ ] ATR с < 5 свечами → reject; partial quality отражается в decision log
- [ ] Funding проверяется в MANUAL независимо от наличия SL
- [ ] Все существующие тесты проходят
- [ ] Новые тесты на каждый fix (минимум 15 тестов)
