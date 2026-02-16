# Risk Management Module

**Version:** v1.0 (implemented)
**Spec ID:** SPEC-003
**Created:** 2026-02-16 18:30
**Work Completed:** 2026-02-16 22:51

---

## Цель

Модуль управления рисками для копитрейдинга сигналов на Hyperliquid Perps. Принимает торговый сигнал, рассчитывает безопасный размер позиции с учётом состояния портфеля, и возвращает готовый к исполнению ордер или reject.

**Scope MVP:**
- Position sizing на основе ATR и risk budget
- ATR-based стоп-лоссы (нативные HL TP/SL ордера)
- Кумулятивный риск портфеля (cross margin awareness)
- Circuit breaker (consecutive losses, daily loss limit)
- Funding rate cost estimation
- Два режима: manual (validate-only) и managed (full sizing)

**Out of Scope (v1):**
- Take-profit стратегии / trailing stop
- Multi-account / vault trading
- WebSocket real-time prices
- Dynamic fee tier tracking

---

## Changelog

### v0.3
- Удалён неиспользуемый `atr_multiplier` из RiskProfile
- Добавлен clamp для confidence scaling (`min 0.3`)
- Добавлено поле `trigger` в CircuitBreakerStatus (вместо парсинга строк)
- Добавлена функция `estimate_liquidation_price` для новых позиций
- Добавлена обработка `onlyIsolated` coins
- Добавлен persist для RiskDecisionLog
- Добавлен запас в `get_candles` для пропусков
- Добавлен assert в `_round_down`

### v0.2
- Добавлены Risk Modes (MANUAL/MANAGED)
- Разделены SignalValidator (format) и RiskManager (risk)
- Добавлен Trade Result Collection
- Уточнены equity definitions

---

## Архитектурная интеграция

### Текущий Flow (без риск-менеджмента)

```
Signal JSON → TradingSignal → SignalValidator → InfoClient →
→ ExchangeClient.set_leverage() → place_order_from_signal() → Storage
```

### Новый Flow (с риск-менеджментом)

```
Signal JSON
     │
     ▼
┌─────────────────────────┐
│  1. PARSE               │
│     TradingSignal       │
└──────────┬──────────────┘
           ▼
┌─────────────────────────┐
│  2. SIGNAL VALIDATOR    │  ← Format-only checks (упрощённый)
│     - pair exists       │
│     - side/type valid   │
│     - size > 0          │
│     - leverage 1-50     │
└──────────┬──────────────┘
           ▼
┌─────────────────────────┐
│  3. RISK MANAGER        │  ← Новый компонент
│     evaluate_signal()   │
│     → TradeOrder        │
│     → RiskReject        │
└──────────┬──────────────┘
           ▼
┌─────────────────────────┐
│  4. ORDER BUILDER       │  ← Принимает TradeOrder (не raw signal)
│     build_from_trade()  │
└──────────┬──────────────┘
           ▼
┌─────────────────────────┐
│  5. EXCHANGE CLIENT     │
│     set_leverage()      │
│     place_order()       │
└──────────┬──────────────┘
           ▼
┌─────────────────────────┐
│  6. STORAGE             │
│     save_signal()       │
│     save_order()        │
│     save_risk_decision()│  ← Новое
└─────────────────────────┘
```

### Ключевые решения

| Вопрос | Решение |
|--------|---------|
| Кто владеет size/leverage/SL? | Зависит от `risk_mode`: manual → signal, managed → RiskManager |
| SignalValidator vs RiskManager | SignalValidator = format checks, RiskManager = risk limits |
| Equity для расчётов | `accountValue` (equity = balance + unrealizedPnL) |
| Margin checks | `withdrawable` (available balance) |
| Liquidation для новых позиций | `estimate_liquidation_price()` — расчёт до открытия |

---

## Risk Modes

```python
class RiskMode(str, Enum):
    """Risk management mode."""
    MANUAL = "manual"      # Signal has size/sl, RiskManager validates only
    MANAGED = "managed"    # RiskManager calculates size/sl from risk budget
```

### Mode: MANUAL

- Сигнал приходит с готовыми `size`, `leverage`, `stop_loss`
- RiskManager только проверяет лимиты:
  - Circuit breaker status
  - Cumulative risk budget
  - Max positions
  - Funding cost threshold
  - Stop vs estimated liquidation
- Если проверки пройдены → approve as-is
- Если нет → reject с причиной

**Use case:** Сигналы от стратегий с собственным risk management.

### Mode: MANAGED

- Сигнал приходит с `entry_price`, `confidence`, `horizon`
- `size` в сигнале игнорируется (или отсутствует)
- RiskManager рассчитывает:
  - ATR → stop_loss
  - Risk budget → size
  - Safe leverage
- CLI показывает diff: "Signal → Calculated"

**Use case:** Копитрейдинг influencer-сигналов.

### CLI Integration

```bash
# Manual mode (default) — validate only
hyperhandler exec --signal signal.json

# Managed mode — full risk sizing
hyperhandler exec --signal signal.json --risk-level medium

# Check what would be calculated (dry-run + managed)
hyperhandler risk check --signal signal.json --risk-level medium
```

---

## Файлы

### Создать

| Файл | Описание |
|------|----------|
| `src/hyperhandler/risk/__init__.py` | Экспорт публичного API |
| `src/hyperhandler/risk/manager.py` | `RiskManager` — основной класс |
| `src/hyperhandler/risk/calculator.py` | ATR, position sizing, leverage selection |
| `src/hyperhandler/risk/circuit_breaker.py` | Circuit breaker logic |
| `src/hyperhandler/risk/config.py` | Risk profiles и HL-specific config |
| `src/hyperhandler/risk/collector.py` | Trade result collection |
| `src/hyperhandler/models/risk.py` | Pydantic модели для risk management |
| `tests/unit/test_risk_calculator.py` | Unit тесты калькуляторов |
| `tests/unit/test_risk_manager.py` | Unit тесты RiskManager |
| `tests/unit/test_circuit_breaker.py` | Unit тесты circuit breaker |
| `tests/integration/test_risk_integration.py` | Integration тесты с mocked API |

### Изменить

| Файл | Изменения |
|------|-----------|
| `src/hyperhandler/models/signal.py` | Добавить `confidence`, `source`, `horizon` |
| `src/hyperhandler/models/order.py` | Расширить `Position` полями для risk tracking |
| `src/hyperhandler/models/validator.py` | Упростить до format-only checks |
| `src/hyperhandler/client/info.py` | Добавить `get_candles()`, `get_funding_rate()`, `get_asset_ctx()` |
| `src/hyperhandler/client/order_builder.py` | Добавить `build_from_trade_order()` |
| `src/hyperhandler/storage.py` | Добавить таблицы `trade_results`, `risk_decisions` |
| `src/hyperhandler/cli.py` | Добавить `--risk-level`, команды `risk check/status/reset` |
| `src/hyperhandler/config.py` | Добавить `RiskSettings` |

---

## Структуры данных

### Расширение TradingSignal

```python
# models/signal.py — добавить поля

class SignalHorizon(str, Enum):
    """Expected hold duration for ATR timeframe selection."""
    SCALP = "scalp"          # <4h, uses 15m candles
    INTRADAY = "intraday"    # 4h-24h, uses 1h candles
    SWING = "swing"          # 1d-7d, uses 4h candles
    POSITION = "position"    # >7d, uses 1d candles


class TradingSignal(BaseModel):
    # ... existing fields ...

    # New optional fields for risk management
    confidence: float | None = Field(
        default=None, ge=0.0, le=1.0,
        description="Signal confidence (0.0-1.0), affects position sizing"
    )
    source: str | None = Field(
        default=None,
        description="Signal source ID (influencer/strategy)"
    )
    horizon: SignalHorizon = Field(
        default=SignalHorizon.INTRADAY,
        description="Expected hold duration"
    )
```

### Расширение Position

```python
# models/order.py — расширить Position

@dataclass
class Position:
    # ... existing fields ...

    # New fields for risk tracking
    mark_price: Decimal | None = None
    funding_accrued: Decimal = field(default_factory=lambda: Decimal("0"))
    stop_loss_price: Decimal | None = None
    opened_at: datetime | None = None

    # Calculated risk fields (populated by RiskManager)
    risk_amount: Decimal | None = None      # $ at risk = size * |entry - stop|
    risk_pct: Decimal | None = None         # % of account value
    correlation_group: str | None = None    # "btc-major", "l1-alt", etc.
```

### Упрощение SignalValidator

```python
# models/validator.py — упростить до format checks

@dataclass
class ValidationConfig:
    """Configuration for signal validation (format only)."""
    allowed_pairs: list[str] = field(default_factory=list)  # Empty = all allowed
    max_leverage_hard: int = 50  # HL absolute max


class SignalValidator:
    """Validates trading signal FORMAT (not risk limits)."""

    def validate(self, signal: TradingSignal) -> ValidationResult:
        errors: list[str] = []

        # Format checks only
        if self.config.allowed_pairs:
            if signal.pair not in self.config.allowed_pairs:
                errors.append(f"Pair {signal.pair} not in allowed list")

        if signal.leverage > self.config.max_leverage_hard:
            errors.append(f"Leverage {signal.leverage} exceeds HL max {self.config.max_leverage_hard}")

        if signal.size <= 0:
            errors.append("Size must be positive")

        # Risk limits moved to RiskManager:
        # - max_position_size_usd → cumulative risk check
        # - max_leverage → profile.max_leverage
        # - require_stop_loss → managed mode auto-calculates
        # - min_order_size → position sizing check

        return ValidationResult(valid=len(errors) == 0, errors=errors)
```

### Новые модели (models/risk.py)

```python
"""Risk management models."""

from decimal import Decimal
from enum import Enum
from datetime import datetime
import logging

from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class RiskLevel(str, Enum):
    """Risk tolerance level."""
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"


class RiskMode(str, Enum):
    """Risk management mode."""
    MANUAL = "manual"      # Validate signal as-is
    MANAGED = "managed"    # Calculate size/sl from risk budget


class RejectReason(str, Enum):
    """Reason for rejecting a signal."""
    CIRCUIT_BREAKER_SOFT = "circuit_breaker_soft"
    CIRCUIT_BREAKER_HARD = "circuit_breaker_hard"
    DAILY_LOSS_LIMIT = "daily_loss_limit"
    RISK_BUDGET_EXCEEDED = "risk_budget_exceeded"
    CORRELATION_LIMIT = "correlation_limit"
    MAX_POSITIONS_REACHED = "max_positions_reached"
    INSUFFICIENT_MARGIN = "insufficient_margin"
    POSITION_TOO_SMALL = "position_too_small"
    DUPLICATE_POSITION = "duplicate_position"
    INVALID_COIN = "invalid_coin"
    STALE_SIGNAL = "stale_signal"
    LEVERAGE_EXCEEDED = "leverage_exceeded"
    LIQUIDATION_TOO_CLOSE = "liquidation_too_close"
    HIGH_FUNDING_COST = "high_funding_cost"
    ATR_UNAVAILABLE = "atr_unavailable"


class CircuitBreakerTrigger(str, Enum):
    """What triggered the circuit breaker."""
    NONE = "none"
    DAILY_LOSS = "daily_loss"
    CONSECUTIVE = "consecutive"


class RiskReject(BaseModel):
    """Result when signal is rejected by risk manager."""
    reason: RejectReason
    details: str
    suggested_action: str = Field(
        description="wait | reduce_risk | close_positions | manual_reset"
    )


class StopLossResult(BaseModel):
    """Calculated stop-loss parameters."""
    price: Decimal
    distance: Decimal          # Absolute distance from entry
    distance_pct: Decimal      # Distance as % of entry
    atr_value: Decimal
    atr_multiplier: Decimal


class PositionSizeResult(BaseModel):
    """Calculated position size parameters."""
    size: Decimal              # In base currency, rounded to szDecimals
    notional: Decimal          # size * entry_price
    margin_required: Decimal
    risk_amount: Decimal       # $ at risk
    risk_pct: Decimal          # % of account
    commission_estimate: Decimal


class LeverageResult(BaseModel):
    """Selected leverage parameters."""
    leverage: int
    max_safe: int              # Based on stop distance
    max_coin: int              # HL max for this coin
    max_config: int            # User config max
    reason: str                # Why this leverage was selected


class CumulativeRiskResult(BaseModel):
    """Portfolio cumulative risk calculation."""
    raw_risk: Decimal          # Sum of all risk amounts
    adjusted_risk: Decimal     # With correlation penalty
    risk_pct: Decimal          # % of account
    available_budget: Decimal  # Remaining risk budget
    within_limit: bool
    correlation_groups: dict[str, list[str]]  # group -> coins


class FundingEstimate(BaseModel):
    """Estimated funding costs."""
    hourly_rate: Decimal
    hourly_cost: Decimal       # If paying
    hourly_income: Decimal     # If receiving
    projected_24h: Decimal
    funding_eats_risk_pct: Decimal


class CircuitBreakerStatus(BaseModel):
    """Circuit breaker state."""
    active: bool
    level: str = Field(description="NONE | SOFT | HARD")
    trigger: CircuitBreakerTrigger = CircuitBreakerTrigger.NONE  # v0.3: добавлено
    risk_multiplier: Decimal = Field(
        default=Decimal("1.0"),
        description="1.0 = normal, 0.5 = reduced, 0.0 = blocked"
    )
    reason: str | None = None
    consecutive_losses: int = 0
    daily_loss_pct: Decimal = Decimal("0")


class TradeOrder(BaseModel):
    """Output: ready-to-execute order with risk parameters."""
    # Order params
    coin: str
    asset_id: int
    side: str                  # "long" | "short"
    size: Decimal
    entry_price: Decimal
    leverage: int
    margin_mode: str = "cross"  # v0.3: auto-set to "isolated" for onlyIsolated coins

    # Risk params
    stop_loss: Decimal
    risk_amount: Decimal
    risk_pct: Decimal
    cumulative_risk_after: Decimal
    estimated_liquidation: Decimal  # v0.3: добавлено

    # Cost estimates
    estimated_commission: Decimal
    estimated_funding_24h: Decimal
    margin_required: Decimal

    # Mode tracking
    risk_mode: RiskMode
    size_source: str = Field(description="signal | calculated")
    sl_source: str = Field(description="signal | calculated")

    # Audit trail
    calculation_details: dict = Field(default_factory=dict)


class TradeResult(BaseModel):
    """Closed trade result for circuit breaker tracking."""
    id: int | None = None
    signal_id: int | None = None
    coin: str
    side: str
    entry_price: Decimal
    exit_price: Decimal
    size: Decimal
    pnl: Decimal               # Realized P&L in USDC
    fees: Decimal
    funding_paid: Decimal
    opened_at: datetime
    closed_at: datetime

    @property
    def is_loss(self) -> bool:
        """For circuit breaker: pnl < 0 is a loss."""
        return self.pnl < 0


class RiskDecisionLog(BaseModel):
    """Full audit log of risk decision."""
    timestamp: datetime
    risk_mode: RiskMode
    signal_source: str | None
    coin: str
    side: str
    decision: str              # "approved" | "rejected"
    reject_reason: RejectReason | None = None

    # Input vs Output (for diff display)
    input_size: Decimal | None = None
    input_leverage: int | None = None
    input_stop_loss: Decimal | None = None
    output_size: Decimal | None = None
    output_leverage: int | None = None
    output_stop_loss: Decimal | None = None

    # Market snapshot
    mark_price: Decimal
    atr_value: Decimal | None = None
    funding_rate: Decimal

    # Risk state
    risk_per_trade_pct: Decimal
    cumulative_risk_before_pct: Decimal
    cumulative_risk_after_pct: Decimal | None = None
    open_positions_count: int
    consecutive_losses: int
    daily_pnl_pct: Decimal

    # Account state (explicit field definitions)
    account_value: Decimal     # marginSummary.accountValue (equity)
    available_balance: Decimal # withdrawable (free margin)
    estimated_liquidation: Decimal | None = None  # v0.3

    def persist(self, storage: "Storage", network: str) -> None:
        """Save decision to storage and log."""
        storage.save_risk_decision(self, network)
        logger.info(
            f"Risk decision: {self.decision} | {self.coin} {self.side} | "
            f"risk={self.risk_per_trade_pct:.1%} | reason={self.reject_reason}"
        )
```

---

## Интерфейсы

### RiskManager (risk/manager.py)

```python
"""Risk manager - main entry point."""

import logging
from decimal import Decimal
from datetime import datetime, timezone
from typing import TYPE_CHECKING

from hyperhandler.models import TradingSignal, Position
from hyperhandler.models.risk import (
    RiskLevel, RiskMode, TradeOrder, RiskReject, RiskDecisionLog,
    CircuitBreakerStatus, TradeResult, RejectReason,
)
from hyperhandler.risk.config import RiskProfile, HLConfig
from hyperhandler.risk.calculator import RiskCalculator
from hyperhandler.risk.circuit_breaker import CircuitBreaker

if TYPE_CHECKING:
    from hyperhandler.client.info import InfoClient
    from hyperhandler.storage import Storage

logger = logging.getLogger(__name__)


class RiskManager:
    """
    Stateless risk evaluator.
    All data comes through arguments — no internal state.
    Allows testing and backtesting through single interface.
    """

    def __init__(
        self,
        risk_level: RiskLevel = RiskLevel.MEDIUM,
        risk_mode: RiskMode = RiskMode.MANUAL,
        hl_config: HLConfig | None = None,
    ):
        self.risk_level = risk_level
        self.risk_mode = risk_mode
        self.profile = RiskProfile.get(risk_level)
        self.hl_config = hl_config or HLConfig()
        self.calculator = RiskCalculator(self.profile, self.hl_config)
        self.circuit_breaker = CircuitBreaker(self.profile)
        self._last_decision_log: RiskDecisionLog | None = None

    async def evaluate_signal(
        self,
        signal: TradingSignal,
        info_client: "InfoClient",
        address: str,
        trade_history: list[TradeResult] | None = None,
        storage: "Storage | None" = None,
        network: str = "mainnet",
    ) -> TradeOrder | RiskReject:
        """
        Main entry point. Evaluates signal and returns order or reject.

        Args:
            signal: Trading signal to evaluate
            info_client: HL info client for market data
            address: User's wallet address
            trade_history: Recent closed trades for circuit breaker
            storage: Optional storage for persisting decision log
            network: Network name for storage

        Returns:
            TradeOrder if approved, RiskReject if rejected
        """
        # Fetch all required data
        account_state = await info_client.get_account_state(address)
        positions = await info_client.get_positions(address)
        asset_meta = await info_client.get_asset_info(signal.pair)
        mark_price = await info_client.get_mid_price(signal.pair)
        funding_rate = await info_client.get_funding_rate(signal.pair)

        # Get candles for ATR (only in MANAGED mode or if no SL in signal)
        candles = []
        if self.risk_mode == RiskMode.MANAGED or signal.stop_loss is None:
            interval = self._get_candle_interval(signal.horizon)
            candles = await info_client.get_candles(signal.pair, interval)

        # Extract account metrics
        account_value = Decimal(str(account_state["marginSummary"]["accountValue"]))
        available_balance = Decimal(str(account_state.get("withdrawable", 0)))

        result = self.evaluate_signal_with_data(
            signal=signal,
            account_value=account_value,
            available_balance=available_balance,
            open_positions=positions,
            asset_meta=asset_meta,
            candles=candles,
            funding_rate=funding_rate,
            mark_price=mark_price,
            trade_history=trade_history,
        )

        # Persist decision log if storage provided
        if storage and self._last_decision_log:
            self._last_decision_log.persist(storage, network)

        return result

    def evaluate_signal_with_data(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        candles: list[dict],
        funding_rate: Decimal,
        mark_price: Decimal,
        trade_history: list[TradeResult] | None = None,
    ) -> TradeOrder | RiskReject:
        """
        Pure function version for testing/backtesting.
        All data provided as arguments.

        Behavior depends on self.risk_mode:
        - MANUAL: validate signal as-is, reject if limits exceeded
        - MANAGED: calculate optimal size/sl/leverage from risk budget
        """
        trade_history = trade_history or []

        # 1. Circuit breaker check (both modes)
        cb_status = self.circuit_breaker.check(trade_history, account_value)
        cb_reject = self.circuit_breaker.get_reject(cb_status)
        if cb_reject:
            self._log_decision(
                signal, None, cb_reject, mark_price, funding_rate,
                account_value, available_balance, open_positions, cb_status
            )
            return cb_reject

        # 2. Validate signal vs market
        entry_price = signal.entry_price or mark_price
        deviation = abs(entry_price - mark_price) / mark_price
        if deviation > self.hl_config.max_entry_deviation:
            reject = RiskReject(
                reason=RejectReason.STALE_SIGNAL,
                details=f"Entry deviation {deviation:.1%} > {self.hl_config.max_entry_deviation:.1%}",
                suggested_action="wait",
            )
            return reject

        # 3. Check duplicate position
        for pos in open_positions:
            if pos.coin == signal.pair:
                is_same_side = (pos.is_long and signal.side.value == "long") or \
                               (pos.is_short and signal.side.value == "short")
                if is_same_side:
                    return RiskReject(
                        reason=RejectReason.DUPLICATE_POSITION,
                        details=f"Already have {signal.side.value} position on {signal.pair}",
                        suggested_action="wait",
                    )

        # 4. Mode-specific processing
        if self.risk_mode == RiskMode.MANAGED:
            return self._evaluate_managed(
                signal, account_value, available_balance, open_positions,
                asset_meta, candles, funding_rate, mark_price, cb_status,
            )
        else:
            return self._evaluate_manual(
                signal, account_value, available_balance, open_positions,
                asset_meta, funding_rate, mark_price, cb_status,
            )

    def _evaluate_manual(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        funding_rate: Decimal,
        mark_price: Decimal,
        cb_status: CircuitBreakerStatus,
    ) -> TradeOrder | RiskReject:
        """MANUAL mode: validate signal parameters against risk limits."""

        entry_price = signal.entry_price or mark_price
        sz_decimals = asset_meta.get("szDecimals", 0)
        max_leverage_coin = asset_meta.get("maxLeverage", 50)
        only_isolated = asset_meta.get("onlyIsolated", False)  # v0.3

        # Check leverage
        if signal.leverage > self.profile.max_leverage:
            return RiskReject(
                reason=RejectReason.LEVERAGE_EXCEEDED,
                details=f"Leverage {signal.leverage} > profile max {self.profile.max_leverage}",
                suggested_action="reduce_risk",
            )

        if signal.leverage > max_leverage_coin:
            return RiskReject(
                reason=RejectReason.LEVERAGE_EXCEEDED,
                details=f"Leverage {signal.leverage} > coin max {max_leverage_coin}",
                suggested_action="reduce_risk",
            )

        # Check max positions
        if len(open_positions) >= self.profile.max_open_positions:
            return RiskReject(
                reason=RejectReason.MAX_POSITIONS_REACHED,
                details=f"Already have {len(open_positions)} positions (max {self.profile.max_open_positions})",
                suggested_action="close_positions",
            )

        # v0.3: Estimate liquidation price for new position
        estimated_liq = self.calculator.estimate_liquidation_price(
            entry_price, signal.leverage, signal.side.value
        )

        # Calculate risk from signal's SL
        if signal.stop_loss:
            stop_distance = abs(entry_price - signal.stop_loss)
            risk_amount = signal.size * stop_distance

            # v0.3: Validate stop vs estimated liquidation
            if not self.calculator.validate_stop_vs_liquidation(
                signal.stop_loss, estimated_liq, entry_price, signal.side.value
            ):
                return RiskReject(
                    reason=RejectReason.LIQUIDATION_TOO_CLOSE,
                    details=f"Stop-loss {signal.stop_loss} beyond estimated liquidation {estimated_liq:.2f}",
                    suggested_action="reduce_risk",
                )
        else:
            # No SL = max risk (full position value)
            risk_amount = signal.size * entry_price

        risk_pct = risk_amount / account_value if account_value > 0 else Decimal("1")

        # Check cumulative risk
        cum_risk = self.calculator.calculate_cumulative_risk(
            open_positions, risk_amount, signal.pair, account_value
        )
        if not cum_risk.within_limit:
            return RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details=f"Cumulative risk {cum_risk.risk_pct:.1%} > max {self.profile.max_cumulative_risk:.1%}",
                suggested_action="reduce_risk",
            )

        # Check margin
        margin_required = (signal.size * entry_price) / Decimal(str(signal.leverage))
        if margin_required > available_balance:
            return RiskReject(
                reason=RejectReason.INSUFFICIENT_MARGIN,
                details=f"Required ${margin_required:.2f} > available ${available_balance:.2f}",
                suggested_action="reduce_risk",
            )

        # Check min order
        notional = signal.size * entry_price
        if notional < self.hl_config.min_order_value:
            return RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details=f"Order value ${notional:.2f} < min ${self.hl_config.min_order_value}",
                suggested_action="wait",
            )

        # Funding cost check
        if signal.stop_loss:
            funding = self.calculator.estimate_funding_cost(
                signal.size, entry_price, signal.side.value, funding_rate, risk_amount
            )
            if funding.funding_eats_risk_pct > self.profile.max_funding_risk_pct:
                return RiskReject(
                    reason=RejectReason.HIGH_FUNDING_COST,
                    details=f"Funding cost {funding.funding_eats_risk_pct:.0%} of risk > {self.profile.max_funding_risk_pct:.0%}",
                    suggested_action="wait",
                )

        # All checks passed
        asset_id = self.calculator._get_asset_id_from_meta(asset_meta)

        # v0.3: Auto-switch margin mode for onlyIsolated coins
        margin_mode = "isolated" if only_isolated else "cross"

        order = TradeOrder(
            coin=signal.pair,
            asset_id=asset_id,
            side=signal.side.value,
            size=signal.size,
            entry_price=entry_price,
            leverage=signal.leverage,
            margin_mode=margin_mode,
            stop_loss=signal.stop_loss or Decimal("0"),
            risk_amount=risk_amount,
            risk_pct=risk_pct,
            cumulative_risk_after=cum_risk.risk_pct,
            estimated_liquidation=estimated_liq,
            estimated_commission=notional * self.hl_config.taker_fee * 2,
            estimated_funding_24h=Decimal("0"),
            margin_required=margin_required,
            risk_mode=RiskMode.MANUAL,
            size_source="signal",
            sl_source="signal" if signal.stop_loss else "none",
            calculation_details={
                "mode": "manual",
                "cb_status": cb_status.level,
                "only_isolated": only_isolated,
            },
        )

        self._log_decision(
            signal, order, None, mark_price, funding_rate,
            account_value, available_balance, open_positions, cb_status
        )

        return order

    def _evaluate_managed(
        self,
        signal: TradingSignal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        asset_meta: dict,
        candles: list[dict],
        funding_rate: Decimal,
        mark_price: Decimal,
        cb_status: CircuitBreakerStatus,
    ) -> TradeOrder | RiskReject:
        """MANAGED mode: calculate optimal position from risk budget."""

        entry_price = signal.entry_price or mark_price
        sz_decimals = asset_meta.get("szDecimals", 0)
        max_leverage_coin = asset_meta.get("maxLeverage", 50)
        only_isolated = asset_meta.get("onlyIsolated", False)  # v0.3

        # Check max positions
        if len(open_positions) >= self.profile.max_open_positions:
            return RiskReject(
                reason=RejectReason.MAX_POSITIONS_REACHED,
                details=f"Already have {len(open_positions)} positions",
                suggested_action="close_positions",
            )

        # Calculate ATR
        try:
            atr = self.calculator.calculate_atr(candles)
        except ValueError as e:
            return RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details=str(e),
                suggested_action="wait",
            )

        if atr <= 0:
            return RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details="ATR is zero (no volatility)",
                suggested_action="wait",
            )

        # Calculate stop-loss
        stop_result = self.calculator.calculate_stop_loss(
            entry_price, signal.side.value, atr, signal.horizon
        )

        # Select leverage
        leverage_result = self.calculator.select_leverage(
            stop_result.distance_pct, max_leverage_coin
        )

        # v0.3: Estimate liquidation and validate stop
        estimated_liq = self.calculator.estimate_liquidation_price(
            entry_price, leverage_result.leverage, signal.side.value
        )

        if not self.calculator.validate_stop_vs_liquidation(
            stop_result.price, estimated_liq, entry_price, signal.side.value
        ):
            # Reduce leverage to make stop valid
            leverage_result = self.calculator.select_leverage_for_stop(
                stop_result.price, entry_price, signal.side.value, max_leverage_coin
            )
            estimated_liq = self.calculator.estimate_liquidation_price(
                entry_price, leverage_result.leverage, signal.side.value
            )

        # Check cumulative risk budget BEFORE sizing
        # Use available budget to constrain position size
        cum_risk_preview = self.calculator.calculate_cumulative_risk(
            open_positions, Decimal("0"), signal.pair, account_value
        )
        max_new_risk = cum_risk_preview.available_budget

        if max_new_risk <= 0:
            return RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details="No risk budget available",
                suggested_action="close_positions",
            )

        # Calculate position size with budget constraint
        size_result = self.calculator.calculate_position_size(
            account_value=account_value,
            available_balance=available_balance,
            entry_price=entry_price,
            stop_price=stop_result.price,
            leverage=leverage_result.leverage,
            sz_decimals=sz_decimals,
            confidence=signal.confidence,
            risk_multiplier=cb_status.risk_multiplier,
            max_risk_amount=max_new_risk,  # v0.3: pass budget constraint
        )

        if isinstance(size_result, RiskReject):
            return size_result

        # Final cumulative risk check with actual size
        cum_risk = self.calculator.calculate_cumulative_risk(
            open_positions, size_result.risk_amount, signal.pair, account_value
        )
        if not cum_risk.within_limit:
            return RiskReject(
                reason=RejectReason.RISK_BUDGET_EXCEEDED,
                details=f"Cumulative risk {cum_risk.risk_pct:.1%} > max",
                suggested_action="reduce_risk",
            )

        # Funding cost check
        funding = self.calculator.estimate_funding_cost(
            size_result.size, entry_price, signal.side.value,
            funding_rate, size_result.risk_amount
        )
        if funding.funding_eats_risk_pct > self.profile.max_funding_risk_pct:
            return RiskReject(
                reason=RejectReason.HIGH_FUNDING_COST,
                details=f"Funding {funding.funding_eats_risk_pct:.0%} > {self.profile.max_funding_risk_pct:.0%}",
                suggested_action="wait",
            )

        asset_id = self.calculator._get_asset_id_from_meta(asset_meta)

        # v0.3: Auto-switch margin mode for onlyIsolated coins
        margin_mode = "isolated" if only_isolated else "cross"

        order = TradeOrder(
            coin=signal.pair,
            asset_id=asset_id,
            side=signal.side.value,
            size=size_result.size,
            entry_price=entry_price,
            leverage=leverage_result.leverage,
            margin_mode=margin_mode,
            stop_loss=stop_result.price,
            risk_amount=size_result.risk_amount,
            risk_pct=size_result.risk_pct,
            cumulative_risk_after=cum_risk.risk_pct,
            estimated_liquidation=estimated_liq,
            estimated_commission=size_result.commission_estimate,
            estimated_funding_24h=funding.projected_24h,
            margin_required=size_result.margin_required,
            risk_mode=RiskMode.MANAGED,
            size_source="calculated",
            sl_source="calculated",
            calculation_details={
                "mode": "managed",
                "atr": str(atr),
                "atr_multiplier": str(stop_result.atr_multiplier),
                "stop_distance_pct": str(stop_result.distance_pct),
                "leverage_reason": leverage_result.reason,
                "confidence": signal.confidence,
                "cb_multiplier": str(cb_status.risk_multiplier),
                "only_isolated": only_isolated,
            },
        )

        self._log_decision(
            signal, order, None, mark_price, funding_rate,
            account_value, available_balance, open_positions, cb_status, atr
        )

        return order

    def _log_decision(
        self,
        signal: TradingSignal,
        order: TradeOrder | None,
        reject: RiskReject | None,
        mark_price: Decimal,
        funding_rate: Decimal,
        account_value: Decimal,
        available_balance: Decimal,
        open_positions: list[Position],
        cb_status: CircuitBreakerStatus,
        atr_value: Decimal | None = None,
    ) -> None:
        """Create decision log for audit trail."""
        cum_risk_before = sum(p.risk_amount or Decimal("0") for p in open_positions)
        cum_risk_before_pct = cum_risk_before / account_value if account_value > 0 else Decimal("0")

        self._last_decision_log = RiskDecisionLog(
            timestamp=datetime.now(timezone.utc),
            risk_mode=self.risk_mode,
            signal_source=signal.source,
            coin=signal.pair,
            side=signal.side.value,
            decision="approved" if order else "rejected",
            reject_reason=reject.reason if reject else None,
            input_size=signal.size,
            input_leverage=signal.leverage,
            input_stop_loss=signal.stop_loss,
            output_size=order.size if order else None,
            output_leverage=order.leverage if order else None,
            output_stop_loss=order.stop_loss if order else None,
            mark_price=mark_price,
            atr_value=atr_value,
            funding_rate=funding_rate,
            risk_per_trade_pct=self.profile.risk_per_trade,
            cumulative_risk_before_pct=cum_risk_before_pct,
            cumulative_risk_after_pct=order.cumulative_risk_after if order else None,
            open_positions_count=len(open_positions),
            consecutive_losses=cb_status.consecutive_losses,
            daily_pnl_pct=cb_status.daily_loss_pct,
            account_value=account_value,
            available_balance=available_balance,
            estimated_liquidation=order.estimated_liquidation if order else None,
        )

    def _get_candle_interval(self, horizon: "SignalHorizon") -> str:
        from hyperhandler.risk.calculator import ATR_SETTINGS
        return ATR_SETTINGS[horizon]["interval"]

    def get_decision_log(self) -> RiskDecisionLog | None:
        """Get the last evaluation's decision log."""
        return self._last_decision_log
```

### RiskCalculator (risk/calculator.py)

```python
"""Risk calculations: ATR, position sizing, leverage."""

from decimal import Decimal, ROUND_DOWN

from hyperhandler.models import Position
from hyperhandler.models.signal import SignalHorizon
from hyperhandler.models.risk import (
    StopLossResult, PositionSizeResult, LeverageResult,
    CumulativeRiskResult, FundingEstimate, RiskReject, RejectReason,
)
from hyperhandler.risk.config import RiskProfile, HLConfig


# Correlation groups for cumulative risk
CORRELATION_MAP: dict[str, list[str]] = {
    "btc-major": ["BTC", "ETH"],
    "l1-alt": ["SOL", "AVAX", "SUI", "APT", "SEI"],
    "defi": ["AAVE", "UNI", "MKR", "CRV", "DYDX"],
    "meme": ["DOGE", "SHIB", "PEPE", "WIF", "BONK"],
    "ai": ["FET", "RNDR", "TAO", "NEAR"],
}

# ATR settings per horizon (multiplier moved here from RiskProfile)
ATR_SETTINGS: dict[SignalHorizon, dict] = {
    SignalHorizon.SCALP: {"interval": "15m", "period": 14, "multiplier": Decimal("1.2")},
    SignalHorizon.INTRADAY: {"interval": "1h", "period": 14, "multiplier": Decimal("1.5")},
    SignalHorizon.SWING: {"interval": "4h", "period": 14, "multiplier": Decimal("2.0")},
    SignalHorizon.POSITION: {"interval": "1d", "period": 14, "multiplier": Decimal("2.5")},
}

# v0.3: Confidence scaling bounds
MIN_CONFIDENCE_FACTOR = Decimal("0.3")
MAX_CONFIDENCE_FACTOR = Decimal("1.0")

# v0.3: HL approximate maintenance margin
HL_MAINTENANCE_MARGIN = Decimal("0.005")  # 0.5%


class RiskCalculator:
    """Pure calculation functions for risk management."""

    def __init__(self, profile: RiskProfile, hl_config: HLConfig):
        self.profile = profile
        self.hl_config = hl_config

    def calculate_atr(
        self,
        candles: list[dict],
        period: int = 14,
    ) -> Decimal:
        """
        Calculate ATR using EMA method (pure Python, no numpy).

        Args:
            candles: HL candles with {o, h, l, c} fields
            period: ATR period (default 14)

        Returns:
            ATR value as Decimal

        Raises:
            ValueError: If insufficient candles
        """
        if len(candles) < 2:
            raise ValueError("Need at least 2 candles for ATR")

        true_ranges: list[Decimal] = []
        for i in range(1, len(candles)):
            high = Decimal(str(candles[i]["h"]))
            low = Decimal(str(candles[i]["l"]))
            prev_close = Decimal(str(candles[i - 1]["c"]))

            tr = max(
                high - low,
                abs(high - prev_close),
                abs(low - prev_close),
            )
            true_ranges.append(tr)

        if len(true_ranges) < period:
            return sum(true_ranges) / len(true_ranges)

        # EMA calculation
        alpha = Decimal("2") / (Decimal(str(period)) + Decimal("1"))
        ema = true_ranges[0]
        for tr in true_ranges[1:]:
            ema = alpha * tr + (Decimal("1") - alpha) * ema

        return ema

    def calculate_stop_loss(
        self,
        entry_price: Decimal,
        side: str,
        atr: Decimal,
        horizon: SignalHorizon,
    ) -> StopLossResult:
        """Calculate ATR-based stop-loss price."""
        settings = ATR_SETTINGS[horizon]
        multiplier = settings["multiplier"]

        stop_distance = atr * multiplier
        buffer = stop_distance * self.hl_config.slippage_buffer
        stop_with_buffer = stop_distance + buffer

        if side == "long":
            stop_price = entry_price - stop_with_buffer
        else:
            stop_price = entry_price + stop_with_buffer

        return StopLossResult(
            price=stop_price,
            distance=stop_with_buffer,
            distance_pct=stop_with_buffer / entry_price,
            atr_value=atr,
            atr_multiplier=multiplier,
        )

    def estimate_liquidation_price(
        self,
        entry_price: Decimal,
        leverage: int,
        side: str,
    ) -> Decimal:
        """
        Estimate liquidation price for a NEW position (before it's opened).

        v0.3: Critical for validating stop-loss on positions that don't exist yet.

        Simplified formula (HL cross margin):
        liq_distance ≈ 1/leverage - maintenance_margin

        Args:
            entry_price: Expected entry price
            leverage: Selected leverage
            side: "long" or "short"

        Returns:
            Estimated liquidation price
        """
        liq_distance_pct = (Decimal("1") / Decimal(str(leverage))) - HL_MAINTENANCE_MARGIN
        liq_distance_pct = max(Decimal("0.01"), liq_distance_pct)  # Floor at 1%

        if side == "long":
            return entry_price * (Decimal("1") - liq_distance_pct)
        else:
            return entry_price * (Decimal("1") + liq_distance_pct)

    def validate_stop_vs_liquidation(
        self,
        stop_price: Decimal,
        liquidation_price: Decimal,
        entry_price: Decimal,
        side: str,
    ) -> bool:
        """
        Validate stop-loss is closer to entry than liquidation.

        v0.3: Now works with estimated liq price for new positions.

        Returns:
            True if valid, False if stop is beyond liquidation
        """
        if side == "long":
            # For long: stop must be ABOVE liquidation
            if stop_price <= liquidation_price:
                return False
        else:
            # For short: stop must be BELOW liquidation
            if stop_price >= liquidation_price:
                return False

        # Check safety buffer (stop should be at least 2% away from liq)
        liq_buffer = abs(stop_price - liquidation_price) / entry_price
        return liq_buffer >= self.hl_config.liq_safety_buffer

    def select_leverage(
        self,
        stop_distance_pct: Decimal,
        max_leverage_coin: int,
    ) -> LeverageResult:
        """Select leverage so liquidation is beyond stop-loss."""
        SAFETY_FACTOR = Decimal("1.5")

        if stop_distance_pct <= 0:
            max_safe = self.profile.max_leverage
        else:
            max_safe = int(Decimal("1") / (stop_distance_pct * SAFETY_FACTOR))
            max_safe = max(1, max_safe)

        leverage = min(
            max_safe,
            max_leverage_coin,
            self.profile.max_leverage,
        )

        reason = []
        if leverage == max_safe:
            reason.append("safe_for_stop")
        if leverage == max_leverage_coin:
            reason.append("coin_max")
        if leverage == self.profile.max_leverage:
            reason.append("config_max")

        return LeverageResult(
            leverage=leverage,
            max_safe=max_safe,
            max_coin=max_leverage_coin,
            max_config=self.profile.max_leverage,
            reason="+".join(reason) if reason else "default",
        )

    def select_leverage_for_stop(
        self,
        stop_price: Decimal,
        entry_price: Decimal,
        side: str,
        max_leverage_coin: int,
    ) -> LeverageResult:
        """
        Select leverage that makes the given stop valid (liq beyond stop).

        v0.3: Used when ATR-based stop requires lower leverage.
        """
        stop_distance_pct = abs(entry_price - stop_price) / entry_price

        # Liquidation must be further than stop + safety buffer
        required_liq_distance = stop_distance_pct + self.hl_config.liq_safety_buffer

        # liq_distance ≈ 1/leverage - maintenance
        # leverage ≈ 1 / (liq_distance + maintenance)
        max_safe = int(Decimal("1") / (required_liq_distance + HL_MAINTENANCE_MARGIN))
        max_safe = max(1, max_safe)

        leverage = min(max_safe, max_leverage_coin, self.profile.max_leverage)

        return LeverageResult(
            leverage=leverage,
            max_safe=max_safe,
            max_coin=max_leverage_coin,
            max_config=self.profile.max_leverage,
            reason="adjusted_for_stop",
        )

    def calculate_position_size(
        self,
        account_value: Decimal,
        available_balance: Decimal,
        entry_price: Decimal,
        stop_price: Decimal,
        leverage: int,
        sz_decimals: int,
        confidence: float | None = None,
        risk_multiplier: Decimal = Decimal("1.0"),
        max_risk_amount: Decimal | None = None,  # v0.3: budget constraint
    ) -> PositionSizeResult | RiskReject:
        """Calculate position size based on risk budget."""

        # v0.3: Clamp confidence factor
        if confidence is not None:
            confidence_factor = max(
                MIN_CONFIDENCE_FACTOR,
                min(MAX_CONFIDENCE_FACTOR, Decimal(str(confidence)))
            )
        else:
            confidence_factor = MAX_CONFIDENCE_FACTOR

        risk_pct = self.profile.risk_per_trade * confidence_factor * risk_multiplier
        risk_amount = account_value * risk_pct

        # v0.3: Apply budget constraint
        if max_risk_amount is not None and risk_amount > max_risk_amount:
            risk_amount = max_risk_amount

        stop_distance = abs(entry_price - stop_price)
        if stop_distance == 0:
            return RiskReject(
                reason=RejectReason.ATR_UNAVAILABLE,
                details="Stop distance is zero",
                suggested_action="wait",
            )

        raw_size = risk_amount / stop_distance
        notional = raw_size * entry_price
        commission = notional * self.hl_config.taker_fee * Decimal("2")

        adjusted_risk = risk_amount - commission
        if adjusted_risk <= 0:
            return RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details="Risk amount doesn't cover commission",
                suggested_action="wait",
            )

        adjusted_size = adjusted_risk / stop_distance

        margin_required = (adjusted_size * entry_price) / Decimal(str(leverage))
        if margin_required > available_balance:
            max_size = (available_balance * Decimal(str(leverage))) / entry_price
            adjusted_size = min(adjusted_size, max_size)
            margin_required = (adjusted_size * entry_price) / Decimal(str(leverage))

        adjusted_size = self._round_down(adjusted_size, sz_decimals)

        final_notional = adjusted_size * entry_price
        final_risk = adjusted_size * stop_distance
        final_commission = final_notional * self.hl_config.taker_fee * Decimal("2")

        if final_notional < self.hl_config.min_order_value:
            return RiskReject(
                reason=RejectReason.POSITION_TOO_SMALL,
                details=f"Order ${final_notional:.2f} < min ${self.hl_config.min_order_value}",
                suggested_action="wait",
            )

        return PositionSizeResult(
            size=adjusted_size,
            notional=final_notional,
            margin_required=margin_required,
            risk_amount=final_risk,
            risk_pct=final_risk / account_value if account_value > 0 else Decimal("0"),
            commission_estimate=final_commission,
        )

    def calculate_cumulative_risk(
        self,
        open_positions: list[Position],
        new_risk_amount: Decimal,
        new_coin: str,
        account_value: Decimal,
    ) -> CumulativeRiskResult:
        """Calculate cumulative portfolio risk with correlation adjustment."""
        groups: dict[str, list[str]] = {}
        for pos in open_positions:
            group = self._get_correlation_group(pos.coin)
            if group not in groups:
                groups[group] = []
            groups[group].append(pos.coin)

        # Only add new coin if we're actually adding risk
        if new_risk_amount > 0:
            new_group = self._get_correlation_group(new_coin)
            if new_group not in groups:
                groups[new_group] = []
            if new_coin not in groups[new_group]:
                groups[new_group].append(new_coin)

        total_risk = sum(
            pos.risk_amount or Decimal("0")
            for pos in open_positions
        )

        adjusted_risk = Decimal("0")
        for group_name, coins in groups.items():
            group_positions = [p for p in open_positions if p.coin in coins]
            group_risk = sum(p.risk_amount or Decimal("0") for p in group_positions)

            n = len(coins)
            if n > 1:
                penalty = Decimal("1") + Decimal(str(n - 1)) * self.profile.correlation_factor
                group_risk = group_risk * penalty

            adjusted_risk += group_risk

        cascade_buffer = Decimal("0")
        for group_name, coins in groups.items():
            if len(coins) > 1:
                group_positions = [p for p in open_positions if p.coin in coins]
                group_risk = sum(p.risk_amount or Decimal("0") for p in group_positions)
                cascade_buffer += group_risk * Decimal("0.1")

        total_adjusted = adjusted_risk + cascade_buffer + new_risk_amount
        max_risk = account_value * self.profile.max_cumulative_risk

        return CumulativeRiskResult(
            raw_risk=total_risk + new_risk_amount,
            adjusted_risk=total_adjusted,
            risk_pct=total_adjusted / account_value if account_value > 0 else Decimal("0"),
            available_budget=max(Decimal("0"), max_risk - adjusted_risk - cascade_buffer),
            within_limit=total_adjusted <= max_risk,
            correlation_groups=groups,
        )

    def estimate_funding_cost(
        self,
        size: Decimal,
        entry_price: Decimal,
        side: str,
        funding_rate: Decimal,
        risk_amount: Decimal,
        hold_hours: int = 24,
    ) -> FundingEstimate:
        """Estimate funding costs/income."""
        notional = size * entry_price
        hourly_payment = notional * funding_rate

        if side == "long":
            projected_cost = hourly_payment * Decimal(str(hold_hours))
        else:
            projected_cost = -hourly_payment * Decimal(str(hold_hours))

        funding_eats_risk_pct = (
            max(Decimal("0"), projected_cost) / risk_amount
            if risk_amount > 0 else Decimal("0")
        )

        return FundingEstimate(
            hourly_rate=funding_rate,
            hourly_cost=abs(hourly_payment) if projected_cost > 0 else Decimal("0"),
            hourly_income=abs(hourly_payment) if projected_cost < 0 else Decimal("0"),
            projected_24h=projected_cost,
            funding_eats_risk_pct=funding_eats_risk_pct,
        )

    def _round_down(self, value: Decimal, decimals: int) -> Decimal:
        """Round down to specified decimals (HL requirement)."""
        assert decimals >= 0, f"decimals must be >= 0, got {decimals}"  # v0.3

        if decimals == 0:
            return value.to_integral_value(rounding=ROUND_DOWN)
        quantize_str = "0." + "0" * decimals
        return value.quantize(Decimal(quantize_str), rounding=ROUND_DOWN)

    def _get_correlation_group(self, coin: str) -> str:
        for group, coins in CORRELATION_MAP.items():
            if coin in coins:
                return group
        return f"independent-{coin}"

    def _get_asset_id_from_meta(self, asset_meta: dict) -> int:
        return asset_meta.get("_asset_id", 0)
```

### Trade Result Collector (risk/collector.py)

```python
"""Trade result collection for circuit breaker."""

from decimal import Decimal
from datetime import datetime, timezone
from typing import TYPE_CHECKING

from hyperhandler.models.risk import TradeResult
from hyperhandler.models.order import Position

if TYPE_CHECKING:
    from hyperhandler.client.info import InfoClient
    from hyperhandler.storage import Storage


class TradeResultCollector:
    """
    Collects closed trade results for circuit breaker tracking.

    Two collection strategies:
    1. On-close: When position is closed via CLI command
    2. Reconcile: Periodic sync from HL fills API
    """

    def __init__(self, storage: "Storage", network: str):
        self.storage = storage
        self.network = network

    async def collect_from_fills(
        self,
        info_client: "InfoClient",
        address: str,
        since_timestamp: datetime | None = None,
    ) -> list[TradeResult]:
        """
        Reconcile trade results from HL user fills.

        This matches fills to known positions and calculates PnL.
        Should be called periodically or before risk evaluation.
        """
        fills = await info_client.get_user_fills(address, limit=100)

        results: list[TradeResult] = []
        for fill in fills:
            # Check if this fill closes a position
            if not fill.get("closedPnl"):
                continue  # Not a closing fill

            # Check if already recorded
            fill_id = f"{fill['oid']}_{fill['time']}"
            if self._is_recorded(fill_id):
                continue

            result = TradeResult(
                coin=fill["coin"],
                side="long" if fill["side"] == "B" else "short",
                entry_price=Decimal(str(fill.get("startPosition", {}).get("entryPx", 0))),
                exit_price=Decimal(str(fill["px"])),
                size=Decimal(str(fill["sz"])),
                pnl=Decimal(str(fill["closedPnl"])),
                fees=Decimal(str(fill.get("fee", 0))),
                funding_paid=Decimal("0"),  # Not available in fills
                opened_at=datetime.fromtimestamp(
                    fill.get("startPosition", {}).get("time", 0) / 1000,
                    tz=timezone.utc
                ),
                closed_at=datetime.fromtimestamp(fill["time"] / 1000, tz=timezone.utc),
            )

            self.storage.save_trade_result(result, self.network)
            results.append(result)

        return results

    def record_close(
        self,
        position: Position,
        exit_price: Decimal,
        fees: Decimal,
        funding_paid: Decimal = Decimal("0"),
        signal_id: int | None = None,
    ) -> TradeResult:
        """
        Record a position close initiated via CLI.

        Called by ExchangeClient.close_position() or similar.
        """
        pnl = self._calculate_pnl(position, exit_price, fees, funding_paid)

        result = TradeResult(
            signal_id=signal_id,
            coin=position.coin,
            side="long" if position.is_long else "short",
            entry_price=position.entry_price,
            exit_price=exit_price,
            size=position.abs_size,
            pnl=pnl,
            fees=fees,
            funding_paid=funding_paid,
            opened_at=position.opened_at or datetime.now(timezone.utc),
            closed_at=datetime.now(timezone.utc),
        )

        self.storage.save_trade_result(result, self.network)
        return result

    def _calculate_pnl(
        self,
        position: Position,
        exit_price: Decimal,
        fees: Decimal,
        funding_paid: Decimal,
    ) -> Decimal:
        """Calculate realized PnL."""
        if position.is_long:
            gross_pnl = (exit_price - position.entry_price) * position.abs_size
        else:
            gross_pnl = (position.entry_price - exit_price) * position.abs_size

        return gross_pnl - fees - funding_paid

    def _is_recorded(self, fill_id: str) -> bool:
        """Check if fill was already recorded (deduplication)."""
        # Implementation uses storage to check
        return False  # Simplified; real impl checks DB
```

### CircuitBreaker (risk/circuit_breaker.py)

```python
"""Circuit breaker for consecutive losses and daily limits."""

from decimal import Decimal
from datetime import datetime, timezone

from hyperhandler.models.risk import (
    CircuitBreakerStatus, CircuitBreakerTrigger,
    TradeResult, RiskReject, RejectReason,
)
from hyperhandler.risk.config import RiskProfile


class CircuitBreaker:
    """Tracks losses and enforces trading limits."""

    def __init__(self, profile: RiskProfile):
        self.profile = profile

    def check(
        self,
        trade_history: list[TradeResult],
        account_value: Decimal,
    ) -> CircuitBreakerStatus:
        """Check circuit breaker status."""
        consecutive_losses = 0
        for trade in reversed(trade_history):
            if trade.is_loss:  # v0.3: use is_loss property
                consecutive_losses += 1
            else:
                break

        today_start = self._get_utc_day_start()
        today_trades = [
            t for t in trade_history
            if t.closed_at >= today_start
        ]
        daily_pnl = sum(t.pnl for t in today_trades)
        daily_loss_pct = abs(min(Decimal("0"), daily_pnl)) / account_value if account_value > 0 else Decimal("0")

        # v0.3: Use trigger enum instead of parsing strings
        if daily_loss_pct >= self.profile.daily_loss_limit:
            return CircuitBreakerStatus(
                active=True,
                level="HARD",
                trigger=CircuitBreakerTrigger.DAILY_LOSS,
                risk_multiplier=Decimal("0"),
                reason=f"Daily loss {daily_loss_pct:.1%} >= {self.profile.daily_loss_limit:.1%}",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        if consecutive_losses >= self.profile.hard_stop_losses:
            return CircuitBreakerStatus(
                active=True,
                level="HARD",
                trigger=CircuitBreakerTrigger.CONSECUTIVE,
                risk_multiplier=Decimal("0"),
                reason=f"{consecutive_losses} consecutive losses (hard: {self.profile.hard_stop_losses})",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        if consecutive_losses >= self.profile.soft_stop_losses:
            return CircuitBreakerStatus(
                active=True,
                level="SOFT",
                trigger=CircuitBreakerTrigger.CONSECUTIVE,
                risk_multiplier=Decimal("0.5"),
                reason=f"{consecutive_losses} consecutive losses (soft: {self.profile.soft_stop_losses})",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        return CircuitBreakerStatus(
            active=False,
            level="NONE",
            trigger=CircuitBreakerTrigger.NONE,
            risk_multiplier=Decimal("1.0"),
            consecutive_losses=consecutive_losses,
            daily_loss_pct=daily_loss_pct,
        )

    def get_reject(self, status: CircuitBreakerStatus) -> RiskReject | None:
        """Get reject if circuit breaker is active at HARD level."""
        if not status.active:
            return None

        if status.level == "HARD":
            # v0.3: Match on trigger enum instead of parsing reason string
            if status.trigger == CircuitBreakerTrigger.DAILY_LOSS:
                return RiskReject(
                    reason=RejectReason.DAILY_LOSS_LIMIT,
                    details=status.reason or "",
                    suggested_action="wait",
                )
            elif status.trigger == CircuitBreakerTrigger.CONSECUTIVE:
                return RiskReject(
                    reason=RejectReason.CIRCUIT_BREAKER_HARD,
                    details=status.reason or "",
                    suggested_action="manual_reset",
                )

        return None  # SOFT level doesn't reject, just reduces risk

    def _get_utc_day_start(self) -> datetime:
        now = datetime.now(timezone.utc)
        return now.replace(hour=0, minute=0, second=0, microsecond=0)
```

### RiskProfile (risk/config.py)

```python
"""Risk profiles and HL-specific configuration."""

from decimal import Decimal
from dataclasses import dataclass

from hyperhandler.models.risk import RiskLevel


@dataclass
class RiskProfile:
    """Risk management parameters for a risk level."""

    risk_per_trade: Decimal
    max_cumulative_risk: Decimal
    daily_loss_limit: Decimal
    max_open_positions: int
    max_leverage: int
    correlation_factor: Decimal
    soft_stop_losses: int
    hard_stop_losses: int
    max_funding_risk_pct: Decimal
    # v0.3: Removed atr_multiplier - now in ATR_SETTINGS per horizon

    @classmethod
    def get(cls, level: RiskLevel) -> "RiskProfile":
        return RISK_PROFILES[level]


RISK_PROFILES: dict[RiskLevel, RiskProfile] = {
    RiskLevel.LOW: RiskProfile(
        risk_per_trade=Decimal("0.01"),
        max_cumulative_risk=Decimal("0.04"),
        daily_loss_limit=Decimal("0.02"),
        max_open_positions=3,
        max_leverage=5,
        correlation_factor=Decimal("0.4"),
        soft_stop_losses=2,
        hard_stop_losses=4,
        max_funding_risk_pct=Decimal("0.3"),
    ),
    RiskLevel.MEDIUM: RiskProfile(
        risk_per_trade=Decimal("0.02"),
        max_cumulative_risk=Decimal("0.06"),
        daily_loss_limit=Decimal("0.03"),
        max_open_positions=5,
        max_leverage=10,
        correlation_factor=Decimal("0.3"),
        soft_stop_losses=3,
        hard_stop_losses=5,
        max_funding_risk_pct=Decimal("0.5"),
    ),
    RiskLevel.HIGH: RiskProfile(
        risk_per_trade=Decimal("0.03"),
        max_cumulative_risk=Decimal("0.10"),
        daily_loss_limit=Decimal("0.05"),
        max_open_positions=8,
        max_leverage=20,
        correlation_factor=Decimal("0.25"),
        soft_stop_losses=3,
        hard_stop_losses=6,
        max_funding_risk_pct=Decimal("0.7"),
    ),
}


@dataclass
class HLConfig:
    """Hyperliquid-specific configuration."""

    # Fees (Tier 0, no staking discount)
    taker_fee: Decimal = Decimal("0.00045")
    maker_fee: Decimal = Decimal("0.00015")

    # Order constraints
    min_order_value: Decimal = Decimal("10.0")
    max_slippage: Decimal = Decimal("0.01")
    slippage_buffer: Decimal = Decimal("0.005")

    # Margin
    default_margin_mode: str = "cross"
    liq_safety_buffer: Decimal = Decimal("0.02")

    # Signal validation
    max_entry_deviation: Decimal = Decimal("0.01")
    max_signal_age_seconds: int = 300
```

---

## Расширение InfoClient

```python
# client/info.py — добавить методы

import time

async def get_candles(
    self,
    symbol: str,
    interval: str = "1h",
    lookback: int = 100,
) -> list[dict]:
    """Get candlestick data for ATR calculation.

    v0.3: Request 20% extra candles to handle gaps/maintenance.
    """
    # Request extra candles in case of gaps
    request_lookback = int(lookback * 1.2)

    end_time = int(time.time() * 1000)
    result = await self._post(
        "info",
        {
            "type": "candleSnapshot",
            "req": {
                "coin": symbol,
                "interval": interval,
                "startTime": end_time - (request_lookback * self._interval_to_ms(interval)),
                "endTime": end_time,
            },
        },
    )

    # Trim to requested lookback
    if len(result) > lookback:
        result = result[-lookback:]

    return result

async def get_asset_ctx(self, symbol: str) -> dict:
    """Get asset context including funding rate."""
    result = await self._post("info", {"type": "metaAndAssetCtxs"})
    meta = result[0]
    ctxs = result[1]

    for i, asset in enumerate(meta.get("universe", [])):
        if asset["name"] == symbol:
            return ctxs[i]

    raise AssetNotFoundError(f"Asset context not found: {symbol}")

async def get_funding_rate(self, symbol: str) -> Decimal:
    """Get current hourly funding rate."""
    ctx = await self.get_asset_ctx(symbol)
    return Decimal(str(ctx.get("funding", "0")))

def _interval_to_ms(self, interval: str) -> int:
    multipliers = {
        "1m": 60_000,
        "15m": 900_000,
        "1h": 3_600_000,
        "4h": 14_400_000,
        "1d": 86_400_000,
    }
    return multipliers.get(interval, 3_600_000)
```

---

## Расширение Storage

```python
# storage.py — добавить в _init_db()

CREATE TABLE IF NOT EXISTS trade_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    signal_id INTEGER,
    network TEXT NOT NULL,
    coin TEXT NOT NULL,
    side TEXT NOT NULL,
    entry_price TEXT NOT NULL,
    exit_price TEXT NOT NULL,
    size TEXT NOT NULL,
    pnl TEXT NOT NULL,
    fees TEXT NOT NULL,
    funding_paid TEXT NOT NULL,
    opened_at TIMESTAMP NOT NULL,
    closed_at TIMESTAMP NOT NULL,
    FOREIGN KEY (signal_id) REFERENCES signals(id)
);

CREATE TABLE IF NOT EXISTS risk_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    network TEXT NOT NULL,
    signal_id INTEGER,
    risk_mode TEXT NOT NULL,
    decision TEXT NOT NULL,
    reject_reason TEXT,
    coin TEXT NOT NULL,
    side TEXT NOT NULL,
    input_size TEXT,
    output_size TEXT,
    risk_pct TEXT,
    estimated_liq TEXT,
    details_json TEXT,
    FOREIGN KEY (signal_id) REFERENCES signals(id)
);

CREATE INDEX IF NOT EXISTS idx_trade_results_closed ON trade_results(closed_at);
CREATE INDEX IF NOT EXISTS idx_trade_results_network ON trade_results(network);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_network ON risk_decisions(network);
```

```python
# storage.py — добавить методы

def save_risk_decision(self, decision: "RiskDecisionLog", network: str) -> int:
    """Save risk decision for audit trail."""
    import json

    with self._connection() as conn:
        cursor = conn.execute(
            """
            INSERT INTO risk_decisions (
                network, risk_mode, decision, reject_reason, coin, side,
                input_size, output_size, risk_pct, estimated_liq, details_json
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                network,
                decision.risk_mode.value,
                decision.decision,
                decision.reject_reason.value if decision.reject_reason else None,
                decision.coin,
                decision.side,
                str(decision.input_size) if decision.input_size else None,
                str(decision.output_size) if decision.output_size else None,
                str(decision.cumulative_risk_after_pct) if decision.cumulative_risk_after_pct else None,
                str(decision.estimated_liquidation) if decision.estimated_liquidation else None,
                json.dumps({
                    "mark_price": str(decision.mark_price),
                    "funding_rate": str(decision.funding_rate),
                    "atr_value": str(decision.atr_value) if decision.atr_value else None,
                    "consecutive_losses": decision.consecutive_losses,
                    "daily_pnl_pct": str(decision.daily_pnl_pct),
                }),
            ),
        )
        return cursor.lastrowid or 0
```

---

## CLI Integration

### Обновлённая команда exec

```python
@app.command()
def exec(
    signal_file: Annotated[Path | None, typer.Option("--signal", "-s")] = None,
    network: NetworkOption = "mainnet",
    vault: VaultOption = None,
    dry_run: Annotated[bool, typer.Option("--dry-run")] = False,
    risk_level: Annotated[str | None, typer.Option("--risk-level", "-r",
        help="Risk level (low/medium/high). Enables managed mode.")] = None,
) -> None:
    """Execute a trading signal."""
    from hyperhandler.models.risk import RiskLevel, RiskMode
    from hyperhandler.risk import RiskManager

    # ... parse signal ...

    # Determine risk mode
    if risk_level:
        risk_mode = RiskMode.MANAGED
        level = RiskLevel(risk_level)
    else:
        risk_mode = RiskMode.MANUAL
        level = RiskLevel.MEDIUM  # Default for validation

    # Create risk manager
    risk_manager = RiskManager(risk_level=level, risk_mode=risk_mode)

    async def execute():
        async with InfoClient(network_config) as info_client:
            # Get trade history for circuit breaker
            trade_history = storage.get_recent_trade_results(network, limit=50)

            # Evaluate signal (v0.3: pass storage for decision logging)
            result = await risk_manager.evaluate_signal(
                signal, info_client, signer.address, trade_history,
                storage=storage, network=network,
            )

            if isinstance(result, RiskReject):
                return None, result

            # Show diff in managed mode
            if risk_mode == RiskMode.MANAGED:
                _print_risk_diff(signal, result)

            if dry_run:
                return result, None

            # Execute using TradeOrder
            async with ExchangeClient(network_config, signer) as exchange_client:
                await exchange_client.set_leverage(result.asset_id, result.leverage)
                # ... place order from TradeOrder ...

            return result, None

    # ... rest of execution ...


def _print_risk_diff(signal: TradingSignal, order: TradeOrder) -> None:
    """Print diff between signal and calculated values."""
    console.print("\n[bold]Risk-Managed Adjustments:[/bold]")

    table = Table(show_header=True)
    table.add_column("Parameter", style="cyan")
    table.add_column("Signal", style="yellow")
    table.add_column("Calculated", style="green")

    table.add_row("Size", str(signal.size), str(order.size))
    table.add_row("Leverage", f"{signal.leverage}x", f"{order.leverage}x")
    table.add_row(
        "Stop-Loss",
        str(signal.stop_loss) if signal.stop_loss else "-",
        f"{order.stop_loss:.2f}"
    )
    table.add_row("Est. Liquidation", "-", f"{order.estimated_liquidation:.2f}")
    table.add_row("Risk %", "-", f"{order.risk_pct:.2%}")
    table.add_row("Margin", "-", f"${order.margin_required:.2f}")
    table.add_row("Margin Mode", "-", order.margin_mode)

    console.print(table)
```

### Новые команды risk

```python
risk_app = typer.Typer(name="risk", help="Risk management commands")
app.add_typer(risk_app, name="risk")


@risk_app.command("check")
def risk_check(
    signal_file: Annotated[Path, typer.Option("--signal", "-s")],
    network: NetworkOption = "testnet",
    risk_level: Annotated[str, typer.Option("--risk-level", "-r")] = "medium",
) -> None:
    """Check signal against risk rules without executing."""
    # Similar to exec --dry-run but always uses managed mode
    # Shows full calculation details


@risk_app.command("status")
def risk_status(
    network: NetworkOption = "testnet",
) -> None:
    """Show current risk status."""
    # Shows:
    # - Account value / available balance
    # - Open positions with risk amounts
    # - Cumulative risk %
    # - Circuit breaker status
    # - Daily P&L


@risk_app.command("reset")
def risk_reset(
    network: NetworkOption = "testnet",
    confirm: Annotated[bool, typer.Option("--yes", "-y")] = False,
) -> None:
    """Reset circuit breaker (manual override)."""
    # Adds a "virtual win" to trade_results to reset consecutive losses
```

---

## Edge Cases

| Case | Handling |
|------|----------|
| ATR = 0 (no volatility) | Reject(ATR_UNAVAILABLE) |
| Coin not in HL universe | Reject(INVALID_COIN) |
| `onlyIsolated` coin | Auto-switch margin_mode to "isolated" |
| Mark price deviates > 1% from signal entry | Reject(STALE_SIGNAL) |
| SL beyond estimated liquidation | Reject(LIQUIDATION_TOO_CLOSE) |
| Funding rate > threshold | Reject(HIGH_FUNDING_COST) |
| Duplicate position (same coin+side) | Reject(DUPLICATE_POSITION) |
| Opposite position on same coin | Reject with "close existing first" |
| Available balance = 0 | Reject(INSUFFICIENT_MARGIN) |
| sz_decimals rounding → size = 0 | Reject(POSITION_TOO_SMALL) |
| No trade history for CB | CB inactive (fresh start) |
| MANUAL mode, no SL in signal | Warn but allow (risk = full position) |
| Confidence < 0.3 | Clamp to 0.3 (min 30% of normal risk) |
| Candle gaps from maintenance | Request 20% extra, trim |

---

## Последовательность реализации

### Phase 1 — Models & Config (3 дня)
1. Создать `src/hyperhandler/models/risk.py`
2. Расширить `TradingSignal` (SignalHorizon, confidence, source)
3. Расширить `Position` (risk fields)
4. Упростить `SignalValidator` (format-only)
5. Создать `src/hyperhandler/risk/config.py`

### Phase 2 — Calculator (3 дня)
6. Создать `src/hyperhandler/risk/calculator.py`
7. Unit тесты ATR, position sizing, leverage selection
8. Unit тесты cumulative risk, funding
9. Unit тесты estimate_liquidation, confidence clamp

### Phase 3 — Circuit Breaker & Storage (2 дня)
10. Создать `src/hyperhandler/risk/circuit_breaker.py`
11. Расширить Storage (trade_results, risk_decisions)
12. Создать `src/hyperhandler/risk/collector.py`
13. Unit тесты circuit breaker с trigger enum

### Phase 4 — InfoClient Extensions (2 дня)
14. Добавить `get_candles()` в InfoClient (с запасом)
15. Добавить `get_asset_ctx()`, `get_funding_rate()`
16. Integration тесты с mocked API

### Phase 5 — RiskManager (3 дня)
17. Создать `src/hyperhandler/risk/manager.py`
18. Реализовать MANUAL mode с estimated liq
19. Реализовать MANAGED mode с budget constraint
20. Создать `src/hyperhandler/risk/__init__.py`
21. Integration тесты RiskManager

### Phase 6 — CLI Integration (2 дня)
22. Добавить `--risk-level` в exec
23. Добавить diff display для managed mode
24. Добавить команды `risk check/status/reset`
25. Добавить decision log persist

### Phase 7 — Documentation (1 день)
26. Обновить README.md
27. Обновить ARCHITECTURE.md
28. Обновить Claude Skill

**Итого: ~16 рабочих дней (~3 недели)**

---

## Критерии приёмки

- [ ] Все unit тесты проходят (`pytest tests/unit/test_risk*.py -v`)
- [ ] Integration тесты проходят (`pytest tests/integration/test_risk*.py -v`)
- [ ] MANUAL mode: валидирует сигнал без изменений
- [ ] MANAGED mode: рассчитывает size/sl/leverage
- [ ] Estimated liquidation рассчитывается для новых позиций
- [ ] Confidence clamping работает (min 0.3)
- [ ] Circuit breaker использует trigger enum (не парсинг строк)
- [ ] onlyIsolated coins автоматически получают isolated margin
- [ ] CLI показывает diff "Signal → Calculated" в managed mode
- [ ] Circuit breaker блокирует после N losses
- [ ] Trade results записываются при закрытии позиций
- [ ] Risk decisions логируются и сохраняются
- [ ] Storage migration работает на существующей БД
- [ ] Нет регрессий в существующих тестах
- [ ] Coverage > 80% для модуля risk
- [ ] Документация обновлена

---

## Dependencies

Новые зависимости **не требуются**.

---

## Глоссарий

| Термин | Определение |
|--------|-------------|
| `accountValue` | Equity = USDC balance + unrealized PnL всех позиций |
| `withdrawable` | Свободная маржа, доступная для новых позиций |
| `risk_amount` | $ под риском = size × |entry - stop_loss| |
| `risk_pct` | risk_amount / accountValue |
| `cumulative_risk` | Сумма risk_amount всех позиций с корреляционной поправкой |
| `estimated_liquidation` | Расчётная цена ликвидации для новой позиции |
| MANUAL mode | RiskManager только валидирует, не меняет параметры |
| MANAGED mode | RiskManager рассчитывает оптимальные параметры |
| `onlyIsolated` | Монета которая поддерживает только isolated margin |
