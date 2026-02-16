# Integration Tests for Risk Management Module

**Version:** v0.7 (draft)
**Spec ID:** SPEC-005
**Created:** 2026-02-16 23:40
**Updated:** 2026-02-17 00:20

---

## Цель

Добавить интеграционные тесты для модуля риск-менеджмента, покрывающие:
- Взаимодействие `RiskManager` с `InfoClient` (mocked HTTP)
- Персистенцию в `Storage` (risk_decisions, trade_results)
- `TradeResultCollector` с API
- CLI команды `risk check/status/reset`

**Scope:**
- 42 интеграционных теста в 8 группах
- Новые фикстуры для risk-specific API responses
- Паттерн для множественных вызовов одного endpoint

**Out of Scope:**
- E2E тесты на реальном testnet
- Тесты vault trading с risk management

---

## Технические решения

### Множественные POST /info вызовы

Hyperliquid использует единый endpoint `/info` с разными `type` в body. Для тестов нужен routing по request body:

```python
def route_info_requests(request):
    """Route /info requests based on type field.

    IMPORTANT: Fail-fast on unknown types to catch regressions.
    """
    body = json.loads(request.content)
    req_type = body.get("type")

    responses = {
        "meta": mock_meta_response,
        "allMids": mock_mids_response,
        "clearinghouseState": mock_account_state,
        "metaAndAssetCtxs": mock_funding_response,
        "candleSnapshot": mock_candles_response,
        "userFills": mock_user_fills_response,
    }

    # Unknown request type = test bug, fail loudly
    assert req_type in responses, f"Unknown /info request type: {req_type}"
    return httpx.Response(200, json=responses[req_type])

# Использование:
mock_api.post("/info").mock(side_effect=route_info_requests)
```

### In-memory Storage

```python
@pytest.fixture
def memory_storage(tmp_path):
    """SQLite storage in temp directory."""
    db_path = tmp_path / "test.db"
    storage = Storage(db_path=db_path)
    return storage
```

---

## Файлы

### Создать

| Файл | Описание |
|------|----------|
| `tests/integration/test_risk_manager.py` | Интеграционные тесты RiskManager (группы A, B) |
| `tests/integration/test_risk_collector.py` | Тесты TradeResultCollector (группа D) |
| `tests/integration/test_risk_storage.py` | Тесты Storage integration (группа C) |
| `tests/integration/test_risk_cli.py` | Тесты CLI команд risk (группы E, F) |
| `tests/integration/test_risk_precision.py` | Тесты precision/rounding (группа G) |
| `tests/integration/test_risk_e2e.py` | E2E lifecycle тест (группа H) |

### Изменить

| Файл | Изменения |
|------|-----------|
| `tests/integration/conftest.py` | Добавить фикстуры для risk API responses |

---

## Новые фикстуры

### conftest.py additions

```python
@pytest.fixture
def mock_candles_response():
    """15 candles for ATR calculation."""
    base_price = 67000
    candles = []
    for i in range(15):
        candles.append({
            "t": 1700000000000 + i * 3600000,  # hourly
            "o": str(base_price + i * 10),
            "h": str(base_price + i * 10 + 500),
            "l": str(base_price + i * 10 - 300),
            "c": str(base_price + i * 10 + 100),
        })
    return candles


@pytest.fixture
def mock_funding_response(mock_meta_response):
    """metaAndAssetCtxs response (2-element list)."""
    ctxs = [
        {"funding": "0.0001", "openInterest": "1000", "oraclePx": "67500"},
        {"funding": "0.00005", "openInterest": "500", "oraclePx": "3500"},
    ]
    return [mock_meta_response, ctxs]


@pytest.fixture
def mock_account_state_full():
    """Account state with all fields for risk (no positions)."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "0.0",  # No positions = no margin used
            "totalNtlPos": "0.0",
        },
        "withdrawable": "10000.0",  # Full balance available
        "assetPositions": [],
    }


@pytest.fixture
def mock_account_state_with_position():
    """Account state with existing BTC position."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "2000.0",
            "totalNtlPos": "10000.0",
        },
        "withdrawable": "5000.0",
        "assetPositions": [
            {
                "position": {
                    "coin": "BTC",
                    "szi": "0.1",
                    "entryPx": "67000.0",
                    "positionValue": "6700.0",
                    "unrealizedPnl": "50.0",
                    "leverage": {"value": 5, "type": "cross"},
                    "markPx": "67050.0",
                    "liquidationPx": "54000.0",
                    "marginUsed": "1340.0",
                    "cumFunding": {"allTime": "-12.5", "sinceOpen": "-2.1"},
                }
            }
        ],
    }


@pytest.fixture
def mock_user_fills_response():
    """User fills with closing and opening fills."""
    return [
        {
            "coin": "BTC",
            "oid": 123,
            "side": "B",
            "px": "67500.0",
            "sz": "0.1",
            "time": 1700000000000,
            "closedPnl": "50.0",
            "fee": "3.0",
            "startPosition": {
                "entryPx": "67000.0",
                "time": 1699900000000,
            },
        },
        {
            "coin": "ETH",
            "oid": 124,
            "side": "A",
            "px": "3500.0",
            "sz": "1.0",
            "time": 1700001000000,
            # No closedPnl — opening fill
        },
    ]


@pytest.fixture
def memory_storage(tmp_path):
    """In-memory SQLite storage for testing."""
    from hyperhandler.storage import Storage
    db_path = tmp_path / "test_risk.db"
    return Storage(db_path=db_path)


@pytest.fixture
def info_request_router(
    mock_meta_response,
    mock_mids_response,
    mock_account_state_full,
    mock_funding_response,
    mock_candles_response,
    mock_user_fills_response,
):
    """Router for multiple /info requests."""
    import json

    called_types: set[str] = set()

    def route(request):
        body = json.loads(request.content)
        req_type = body.get("type")

        responses = {
            "meta": mock_meta_response,
            "allMids": mock_mids_response,
            "clearinghouseState": mock_account_state_full,
            "metaAndAssetCtxs": mock_funding_response,
            "candleSnapshot": mock_candles_response,
            "userFills": mock_user_fills_response,
        }

        # Fail-fast: unknown request type is a test bug, not silent success
        assert req_type in responses, f"Unknown /info request type: {req_type}"
        called_types.add(req_type)
        return httpx.Response(200, json=responses[req_type])

    route.called_types = called_types  # Expose for assertions
    return route
```

---

## Тестовые сценарии

### Группа A: RiskManager MANUAL mode (7 тестов)

```python
@pytest.mark.integration
class TestRiskManagerManualIntegration:
    """Integration tests for RiskManager in MANUAL mode."""

    @pytest.mark.asyncio
    async def test_manual_mode_happy_path(self, mock_api, testnet_config, info_request_router, memory_storage):
        """I-RISK-A01: Valid signal approved in manual mode."""
        mock_api.post("/info").mock(side_effect=info_request_router)

        signal = TradingSignal(pair="BTC", side="long", order_type="market", size=Decimal("0.1"), leverage=5)
        manager = RiskManager(risk_level=RiskLevel.MEDIUM, risk_mode=RiskMode.MANUAL)

        async with InfoClient(testnet_config) as client:
            result = await manager.evaluate_signal(
                signal, client, TEST_ADDRESS, trade_history=[],
                storage=memory_storage, network="testnet"
            )

        assert isinstance(result, TradeOrder)
        assert result.risk_mode == RiskMode.MANUAL
        # Verify decision persisted
        # ...

    @pytest.mark.asyncio
    async def test_manual_mode_circuit_breaker_hard_stop(self, ...):
        """I-RISK-A02: Hard circuit breaker rejects signal."""
        # 5 consecutive losing trades in history

    @pytest.mark.asyncio
    async def test_manual_mode_stale_signal_rejected(self, ...):
        """I-RISK-A03: Signal with >1% entry deviation rejected."""
        # mock_mids returns 68000, signal.entry_price = 66000

    @pytest.mark.asyncio
    async def test_manual_mode_duplicate_position_rejected(self, ...):
        """I-RISK-A04: Duplicate same-side position rejected."""
        # Use mock_account_state_with_position (BTC long exists)

    @pytest.mark.asyncio
    async def test_manual_mode_insufficient_margin_rejected(self, ...):
        """I-RISK-A05: Insufficient withdrawable balance rejected."""
        # withdrawable = 100, required margin = 1000

    @pytest.mark.asyncio
    async def test_manual_mode_leverage_exceeds_coin_max(self, ...):
        """I-RISK-A06: Leverage above coin maxLeverage rejected."""
        # signal.leverage = 50, meta.maxLeverage = 20

    @pytest.mark.asyncio
    async def test_manual_mode_only_isolated_coin(self, ...):
        """I-RISK-A07: Signal for onlyIsolated coin auto-switches to isolated.

        Setup:
        - meta.universe has coin with onlyIsolated=True
        - Signal uses cross leverage mode

        Assertions:
        1. TradeOrder returned (not rejected)
        2. TradeOrder.margin_mode == "isolated"
        3. Leverage adjusted if needed for isolated mode
        """
```

### Группа B: RiskManager MANAGED mode (8 тестов)

```python
@pytest.mark.integration
class TestRiskManagerManagedIntegration:
    """Integration tests for RiskManager in MANAGED mode."""

    @pytest.mark.asyncio
    async def test_managed_mode_happy_path(self, mock_api, info_request_router, ...):
        """I-RISK-B01: ATR calculated, size/SL computed.

        Assertions:
        1. All required /info types called:
           assert {"meta", "allMids", "clearinghouseState", "candleSnapshot"}.issubset(
               info_request_router.called_types
           )
        2. TradeOrder returned with calculated values
        3. stop_loss_price is set based on ATR
        """
        # Verify candles fetched, ATR calculated, TradeOrder returned

    @pytest.mark.asyncio
    async def test_managed_mode_insufficient_candles(self, ...):
        """I-RISK-B02: Less than 2 candles → ATR_UNAVAILABLE."""
        # candleSnapshot returns only 1 candle

    @pytest.mark.asyncio
    async def test_managed_mode_risk_budget_exhausted(self, ...):
        """I-RISK-B03: Existing positions consume all budget."""
        # Multiple positions with high risk amounts

    @pytest.mark.asyncio
    async def test_managed_mode_soft_circuit_breaker(self, ...):
        """I-RISK-B04: Soft CB reduces position size."""
        # 3 consecutive losses → risk_multiplier = 0.5

    @pytest.mark.asyncio
    async def test_managed_mode_high_funding_rejected(self, mock_api, info_request_router, ...):
        """I-RISK-B05: High funding rate rejects signal.

        Setup:
        - funding_rate = 0.005 (0.5% per 8h = extreme)
        - Signal side = long (paying funding)

        Assertions:
        1. metaAndAssetCtxs called (verify via called_types)
        2. RiskReject returned with reason == RejectReason.HIGH_FUNDING_COST
        3. reason contains "funding"
        4. No /exchange calls made
        5. Decision persisted with rejected=True
        """
        # funding = 0.005 (0.5% per 8h)

    @pytest.mark.asyncio
    async def test_managed_mode_wide_atr_reduces_leverage(self, ...):
        """I-RISK-B06: Wide ATR stop triggers leverage reduction via select_leverage_for_stop."""
        # ATR = 5% of price → stop at 7.5% → effective leverage < requested
        # Verify RiskCalculator.select_leverage_for_stop() is applied

    @pytest.mark.asyncio
    async def test_managed_mode_correlation_group_penalty(self, mock_api, ...):
        """I-RISK-B07: Correlated positions apply risk penalty.

        Setup:
        - Existing BTC long (group: "btc-major")
        - New ETH long signal (group: "l1-alt", uncorrelated)
        - vs New SOL long signal (group: "l1-alt", correlated with ETH if exists)

        Scenario A (uncorrelated):
        - BTC + ETH = different groups → no penalty
        - Full position size allowed

        Scenario B (correlated):
        - ETH + SOL = same group "l1-alt"
        - correlation_factor applied → reduced size or reject

        Assertions:
        1. Uncorrelated: TradeOrder.size == calculated_size
        2. Correlated: TradeOrder.size < calculated_size OR
           RiskReject with reason == RejectReason.CORRELATION_LIMIT
        3. RiskDecision.correlation_groups populated correctly
        """

    @pytest.mark.asyncio
    async def test_managed_mode_cumulative_exposure_rejected(self, mock_api, ...):
        """I-RISK-B08: Cumulative exposure exceeds threshold → reject.

        Setup:
        - Existing BTC long: $5000 notional
        - Equity: $10000
        - New BTC long signal: $6000 notional
        - max_cumulative_exposure: 100% of equity

        Assertions:
        1. RiskReject with reason == RejectReason.RISK_BUDGET_EXCEEDED
        2. RiskReject.details contains current + proposed exposure
        3. Existing position NOT closed
        """
```

### Группа C: Storage integration (6 тестов)

```python
@pytest.mark.integration
class TestRiskStorageIntegration:
    """Integration tests for risk data persistence."""

    def test_risk_decision_persisted(self, memory_storage):
        """I-RISK-C01: Decision log written to risk_decisions."""

    def test_risk_decision_not_persisted_without_storage(self, ...):
        """I-RISK-C02: No storage param → no persistence."""

    def test_trade_results_roundtrip(self, memory_storage):
        """I-RISK-C03: save/get trade_results works correctly."""

    def test_trade_results_ordering(self, memory_storage):
        """I-RISK-C04: Results ordered by closed_at desc."""

    def test_rejected_signal_no_order_records(self, memory_storage):
        """I-RISK-C05: Rejected signal → no order_request/order_response.

        Scenario:
        1. Signal fails risk check → RiskReject
        2. Query storage for order records

        Assertions:
        1. risk_decisions has 1 record with approved=False
        2. order_requests table has 0 records
        3. order_responses table has 0 records
        """

    def test_approved_signal_persistence_order(self, memory_storage):
        """I-RISK-C06: Approved signal → correct persistence order.

        Scenario:
        1. Signal passes risk check → TradeOrder
        2. Order executed successfully

        Assertions (verify timestamps):
        1. risk_decision saved FIRST
        2. order_request saved BEFORE exchange call
        3. order_response saved AFTER exchange response
        4. All records have same correlation_id
        """
```

### Группа D: TradeResultCollector (5 тестов)

```python
@pytest.mark.integration
class TestTradeResultCollectorIntegration:
    """Integration tests for TradeResultCollector."""

    @pytest.mark.asyncio
    async def test_collect_closing_fills(self, mock_api, testnet_config, memory_storage):
        """I-RISK-D01: Closing fills saved to storage."""

    @pytest.mark.asyncio
    async def test_skip_non_closing_fills(self, ...):
        """I-RISK-D02: Fills without closedPnl skipped."""

    @pytest.mark.asyncio
    async def test_deduplication(self, ...):
        """I-RISK-D03: Same fill not recorded twice.

        Dedup key: fill_id (oid field from API response).
        Test: call collect() twice with same fills → storage has 1 record.
        Assert: storage.get_trade_results() returns exactly 1 result.
        """

    @pytest.mark.asyncio
    async def test_since_timestamp_filter(self, ...):
        """I-RISK-D04: Fills before cutoff skipped."""

    @pytest.mark.asyncio
    @pytest.mark.skip(reason="Requires SPEC-003 amendment: partial fill aggregation")
    async def test_partial_close_then_final_close(self, mock_api, memory_storage, ...):
        """I-RISK-D05: Partial + final close → one trade_result.

        ⚠️ DEFERRED: Current TradeResultCollector works per-fill.
        This test requires additional spec for partial fill aggregation.
        See: SPEC-003 amendment needed.

        Setup:
        - userFills returns:
          - Fill 1: partial close (closedPnl=25, remaining position)
          - Fill 2: final close (closedPnl=25, position fully closed)

        Assertions:
        1. Only ONE trade_result created (not two)
        2. trade_result.pnl = sum of both closedPnl (50)
        3. trade_result.closed_at = timestamp of final fill
        4. Circuit breaker updated ONCE (not twice)

        Note: Requires grouping fills by (coin, startPosition.time).
        """
```

### Группа E: CLI risk commands (4 теста)

```python
@pytest.mark.integration
class TestRiskCLIIntegration:
    """Integration tests for risk CLI commands."""

    def test_risk_check_approved(self, runner, tmp_path, mock_api):
        """I-RISK-E01: risk check with valid signal → approved."""

    def test_risk_check_rejected(self, runner, tmp_path, mock_api):
        """I-RISK-E02: risk check with stale signal → rejected."""

    def test_risk_status_shows_info(self, runner, mock_api):
        """I-RISK-E03: risk status displays account and CB state."""

    def test_risk_reset_clears_cb(self, runner, memory_storage):
        """I-RISK-E04: risk reset adds virtual win."""
```

### Группа F: exec with --risk-level (5 тестов, +1 CLI diff)

```python
@pytest.mark.integration
class TestExecWithRiskLevel:
    """Integration tests for exec command with risk level.

    Key contract: MANAGED mode uses RiskManager.calculated_order for execution,
    not the original signal values.
    """

    @pytest.mark.asyncio
    async def test_exec_with_risk_level_full(self, mock_api, ...):
        """I-RISK-F01: exec --risk-level medium executes order.

        Assertions (order matters):
        1. set_leverage called BEFORE place_order
        2. place_order payload uses calculated_order values (size, sl_price)
        3. Exit code 0
        """
        # Verify call order via mock_api.calls index
        # assert mock_api.calls[N].request contains set_leverage action
        # assert mock_api.calls[N+1].request contains place_order action

    @pytest.mark.asyncio
    async def test_exec_with_risk_level_dry_run(self, mock_api, ...):
        """I-RISK-F02: exec --risk-level medium --dry-run shows order.

        Assertions:
        1. place_order NOT called (mock_api has no /exchange calls)
        2. Signer NOT used (no signature in any request)
        3. Output contains "dry run" or similar indicator
        4. Output shows calculated values (Size, Stop-Loss, Risk %)
        """
        # assert not any("/exchange" in str(call) for call in mock_api.calls)

    @pytest.mark.asyncio
    async def test_exec_without_risk_level_uses_manual(self, ...):
        """I-RISK-F03: exec without --risk-level uses MANUAL mode.

        Assertions:
        1. Signal passed through with original size/leverage
        2. No ATR calculation (no candleSnapshot request)
        3. place_order uses signal values directly
        """

    @pytest.mark.asyncio
    async def test_exec_with_risk_level_rejected_shows_reason(self, ...):
        """I-RISK-F04: exec --risk-level with rejected signal shows rejection reason.

        Assertions:
        1. Exit code != 0
        2. Output contains RiskReject.reason text
        3. place_order NOT called
        """

    @pytest.mark.asyncio
    async def test_exec_managed_shows_diff_table(self, ...):
        """I-RISK-F05: MANAGED exec prints diff table with calculated values.

        Assertions (check stdout contains):
        1. "Size" row with original → calculated
        2. "Leverage" row
        3. "Stop-Loss" row with price
        4. "Risk %" row

        Consistency check (parse output):
        - Extract displayed risk_pct and max_loss from output
        - Verify: max_loss ≈ equity * risk_pct (within $1 tolerance)
        - This prevents diff table from lying
        """
        # assert "Size" in result.output
        # assert "Stop-Loss" in result.output
        # assert "Risk" in result.output
        # Parse and verify: displayed_max_loss ≈ equity * displayed_risk_pct
```

### Группа G: Precision & Rounding (5 тестов)

```python
@pytest.mark.integration
class TestRiskPrecisionIntegration:
    """Integration tests for size/price precision and exchange constraints."""

    @pytest.mark.asyncio
    async def test_calculated_size_respects_min_sz(self, ...):
        """I-RISK-G01: Calculated size >= minSz from meta.

        Setup:
        - Small equity (100 USD)
        - Risk 1% → raw size = 0.00001 BTC
        - minSz = 0.001

        Assertions:
        1. If raw_size < minSz → RiskReject with reason == RejectReason.POSITION_TOO_SMALL
        2. Never return TradeOrder with size < minSz
        """

    @pytest.mark.asyncio
    async def test_calculated_size_respects_sz_decimals(self, ...):
        """I-RISK-G02: Calculated size rounded to szDecimals.

        Setup:
        - szDecimals = 4 (from meta)
        - Calculated raw size = 0.123456789

        Assertions:
        1. TradeOrder.size has at most 4 decimal places
        2. Rounding is DOWN (conservative)
        """

    @pytest.mark.asyncio
    async def test_risk_drift_within_tolerance(self, ...):
        """I-RISK-G03: Actual risk % after rounding stays within tolerance.

        Setup:
        - target risk = 2%
        - size rounded → actual risk may differ

        Assertions:
        1. abs(actual_risk_pct - target_risk_pct) <= 0.1%
        2. If drift > tolerance → log warning (not reject)

        Formula:
        risk_amount = size * stop_distance  # in USDC
        actual_risk_pct = risk_amount / equity
        """

    @pytest.mark.asyncio
    async def test_tiny_equity_rejects_below_min_sz(self, ...):
        """I-RISK-G04: Very small equity → size < minSz → reject.

        Setup:
        - Equity: $50
        - Risk: 2% = $1 max loss
        - BTC price: $67000
        - minSz: 0.001 ($67)

        Assertions:
        1. Calculated size < minSz
        2. RiskReject with reason == RejectReason.POSITION_TOO_SMALL
        3. details explains why (equity too small)
        """

    @pytest.mark.asyncio
    async def test_large_equity_leverage_cap_applied(self, ...):
        """I-RISK-G05: Very large equity → leverage capped to coin max.

        Setup:
        - Equity: $1,000,000
        - Signal leverage: 50x
        - Coin maxLeverage: 20x

        Assertions:
        1. TradeOrder.leverage == 20 (capped)
        2. Size adjusted for lower leverage
        3. Risk % maintained despite leverage change
        """
```

### Группа H: E2E Risk Lifecycle (2 теста, критические)

```python
@pytest.mark.integration
class TestRiskLifecycleE2E:
    """End-to-end test for complete risk lifecycle.

    This is the most important integration test.
    If this passes, the risk system works as designed.
    """

    @pytest.mark.asyncio
    async def test_circuit_breaker_full_cycle(self, mock_api, memory_storage, ...):
        """I-RISK-H01: Complete CB lifecycle: losses → block → reset → allow.

        Scenario:
        1. Execute 3 losing trades (record in storage)
        2. 4th trade attempt → RiskReject with CIRCUIT_BREAKER_HARD
        3. Call risk reset (add virtual win)
        4. 5th trade attempt → TradeOrder (allowed)

        Assertions at each step:
        Step 1: 3x TradeResult with is_win=False saved
        Step 2: evaluate_signal returns RiskReject
                RiskReject.code == "CIRCUIT_BREAKER_HARD"
        Step 3: storage has virtual_win record
                CB state reset
        Step 4: evaluate_signal returns TradeOrder

        This test validates:
        - CB detection logic
        - Storage persistence
        - Reset mechanism
        - State recovery
        """
        # Phase 1: Record losses (is_win computed from pnl < 0)
        for i in range(3):
            storage.save_trade_result(TradeResult(
                coin="BTC",
                pnl=Decimal("-50"),  # is_win = False (computed property)
                closed_at=datetime.now() - timedelta(hours=3-i),
            ))

        # Phase 2: Verify rejection
        signal = TradingSignal(pair="BTC", side="long", ...)
        result = await manager.evaluate_signal(signal, client, ...)
        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.CIRCUIT_BREAKER_HARD

        # Phase 3: Reset
        storage.add_virtual_win(coin="BTC", amount=Decimal("100"))

        # Phase 4: Verify recovery
        result = await manager.evaluate_signal(signal, client, ...)
        assert isinstance(result, TradeOrder)

    @pytest.mark.asyncio
    async def test_concurrent_exec_no_race_condition(self, mock_api, memory_storage, ...):
        """I-RISK-H02: Concurrent exec calls → no race conditions.

        Scenario:
        - 2 exec calls launched almost simultaneously (asyncio.gather)
        - Both for different coins (BTC, ETH)
        - Both should succeed

        Assertions:
        1. Both orders placed (2 /exchange calls)
        2. Storage has 2 distinct risk_decisions
        3. Storage has 2 distinct order_records
        4. No duplicate entries
        5. Circuit breaker state consistent

        SQLite safety:
        - Precondition: storage fixture enables WAL mode
        - No "database is locked" errors

        Note: If WAL not enabled, mark @pytest.mark.xfail until storage fix.
        """
        # signals = [btc_signal, eth_signal]
        # results = await asyncio.gather(
        #     exec_signal(signals[0], ...),
        #     exec_signal(signals[1], ...),
        # )
        # assert len(storage.get_risk_decisions()) == 2
```

---

## Последовательность реализации

### Phase 1: Fixtures (1 день)
1. Добавить фикстуры в `tests/integration/conftest.py`
2. Реализовать `info_request_router`
3. Добавить `memory_storage` fixture

### Phase 2: RiskManager tests (2 дня)
4. Создать `tests/integration/test_risk_manager.py`
5. Реализовать группу A (MANUAL mode) — 7 тестов
6. Реализовать группу B (MANAGED mode) — 8 тестов

### Phase 3: Storage & Collector (1 день)
7. Создать `tests/integration/test_risk_storage.py` — 6 тестов
8. Создать `tests/integration/test_risk_collector.py` — 4 теста

### Phase 4: CLI tests (1 день)
9. Создать `tests/integration/test_risk_cli.py`
10. Реализовать группы E и F — 9 тестов

### Phase 5: Precision & E2E (1 день)
11. Создать `tests/integration/test_risk_precision.py` — 5 тестов
12. Создать `tests/integration/test_risk_e2e.py` — 2 теста (критические)

---

## Критерии приёмки

- [ ] Все 42 интеграционных теста проходят
- [ ] **I-RISK-H01 (E2E lifecycle) — обязательно зелёный перед релизом**
- [ ] `pytest tests/integration/test_risk*.py -v` — 0 failures
- [ ] Coverage модуля `risk/` > 85%
- [ ] Паттерн `info_request_router` работает для множественных вызовов
- [ ] Storage тесты используют изолированную in-memory БД
- [ ] CLI тесты не требуют реальных credentials
- [ ] Нет регрессий в существующих тестах (`pytest tests/ -v`)
- [ ] Документация тестов (docstrings с I-RISK-XX IDs)

---

## Зависимости

Новые зависимости **не требуются** — `respx`, `pytest-asyncio` уже установлены.
