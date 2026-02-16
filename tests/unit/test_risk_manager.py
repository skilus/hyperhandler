"""Unit tests for RiskManager."""

from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest

from hyperhandler.models.order import Position
from hyperhandler.models.risk import (
    RejectReason,
    RiskLevel,
    RiskMode,
    RiskReject,
    TradeOrder,
    TradeResult,
)
from hyperhandler.models.signal import OrderSide, OrderType, SignalHorizon, TradingSignal
from hyperhandler.risk import RiskManager


@pytest.fixture
def sample_signal() -> TradingSignal:
    """Create a sample trading signal."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.LONG,
        order_type=OrderType.LIMIT,
        size=Decimal("0.1"),
        leverage=10,
        entry_price=Decimal("50000"),
        stop_loss=Decimal("49000"),
        confidence=0.8,
        horizon=SignalHorizon.INTRADAY,
    )


@pytest.fixture
def sample_candles() -> list[dict]:
    """Generate sample candles for ATR calculation."""
    candles = []
    for i in range(20):
        candles.append({
            "o": "50000",
            "h": "50500",
            "l": "49500",
            "c": "50100",
        })
    return candles


@pytest.fixture
def asset_meta() -> dict:
    """Sample asset metadata."""
    return {
        "_asset_id": 0,
        "name": "BTC",
        "szDecimals": 5,
        "maxLeverage": 50,
        "onlyIsolated": False,
    }


class TestRiskManagerManualMode:
    """Tests for MANUAL mode (validate-only)."""

    def test_manual_mode_approves_valid_signal(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Valid signal should be approved in MANUAL mode."""
        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, TradeOrder)
        assert result.coin == "BTC"
        assert result.size == sample_signal.size
        assert result.leverage == sample_signal.leverage
        assert result.stop_loss == sample_signal.stop_loss
        assert result.risk_mode == RiskMode.MANUAL
        assert result.size_source == "signal"
        assert result.sl_source == "signal"

    def test_manual_mode_rejects_leverage_exceeded(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when leverage exceeds profile max."""
        sample_signal.leverage = 15  # MEDIUM max is 10

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.LEVERAGE_EXCEEDED

    def test_manual_mode_rejects_max_positions(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when max positions reached."""
        # MEDIUM profile max is 5 positions
        positions = [
            Position(
                coin=f"COIN{i}",
                size=Decimal("1"),
                entry_price=Decimal("100"),
                position_value=Decimal("100"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
            )
            for i in range(5)
        ]

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=positions,
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.MAX_POSITIONS_REACHED

    def test_manual_mode_rejects_duplicate_position(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when trying to open duplicate position."""
        existing_position = Position(
            coin="BTC",
            size=Decimal("0.5"),  # Positive = long
            entry_price=Decimal("48000"),
            position_value=Decimal("24000"),
            unrealized_pnl=Decimal("1000"),
            leverage=10,
            leverage_type="cross",
        )

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,  # Also long BTC
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[existing_position],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.DUPLICATE_POSITION

    def test_manual_mode_rejects_insufficient_margin(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when margin is insufficient."""
        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("100"),  # Very limited
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.INSUFFICIENT_MARGIN

    def test_manual_mode_rejects_stale_signal(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when entry price deviates too much from mark."""
        sample_signal.entry_price = Decimal("51000")  # 2% deviation

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.STALE_SIGNAL

    def test_manual_mode_respects_only_isolated(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should set margin_mode to isolated for onlyIsolated coins."""
        asset_meta["onlyIsolated"] = True

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, TradeOrder)
        assert result.margin_mode == "isolated"


class TestRiskManagerManagedMode:
    """Tests for MANAGED mode (full position sizing)."""

    def test_managed_mode_calculates_size(
        self, sample_signal: TradingSignal, asset_meta: dict, sample_candles: list[dict]
    ):
        """MANAGED mode should calculate position size."""
        # Remove size from signal (will be calculated)
        sample_signal.stop_loss = None  # Will be calculated from ATR

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANAGED,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=sample_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, TradeOrder)
        assert result.risk_mode == RiskMode.MANAGED
        assert result.size_source == "calculated"
        assert result.sl_source == "calculated"
        assert result.stop_loss > 0
        # Size should be based on 2% risk (MEDIUM profile)
        assert result.risk_pct > Decimal("0")
        assert result.risk_pct <= Decimal("0.025")  # ~2% with some tolerance

    def test_managed_mode_uses_confidence_scaling(
        self, sample_signal: TradingSignal, asset_meta: dict, sample_candles: list[dict]
    ):
        """Lower confidence should reduce position size."""
        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANAGED,
        )

        # Full confidence
        sample_signal.confidence = 1.0
        result_full = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=sample_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        # Half confidence
        sample_signal.confidence = 0.5
        result_half = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=sample_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result_full, TradeOrder)
        assert isinstance(result_half, TradeOrder)
        assert result_half.size < result_full.size

    def test_managed_mode_rejects_insufficient_candles(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when not enough candles for ATR."""
        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANAGED,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[{"o": "50000", "h": "50500", "l": "49500", "c": "50100"}],  # Only 1
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.ATR_UNAVAILABLE

    def test_managed_mode_adjusts_leverage(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should adjust leverage based on ATR stop distance."""
        # Create candles with high volatility
        high_vol_candles = []
        for i in range(20):
            high_vol_candles.append({
                "o": "50000",
                "h": "55000",  # 10% range
                "l": "45000",
                "c": "50000",
            })

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANAGED,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=high_vol_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, TradeOrder)
        # High volatility should result in lower leverage
        assert result.leverage < sample_signal.leverage


class TestRiskManagerCircuitBreaker:
    """Tests for circuit breaker integration."""

    def test_circuit_breaker_hard_stop_rejects(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when circuit breaker is at HARD level."""
        now = datetime.now(timezone.utc)

        # Create 5 consecutive losses (MEDIUM hard_stop_losses=5)
        trade_history = [
            TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("50000"),
                exit_price=Decimal("49500"),
                size=Decimal("0.02"),
                pnl=Decimal("-10"),
                fees=Decimal("1"),
                funding_paid=Decimal("0"),
                opened_at=now - timedelta(hours=i + 2),
                closed_at=now - timedelta(hours=i + 1),
            )
            for i in range(5)
        ]

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
            trade_history=trade_history,
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.CIRCUIT_BREAKER_HARD

    def test_circuit_breaker_soft_reduces_size(
        self, sample_signal: TradingSignal, asset_meta: dict, sample_candles: list[dict]
    ):
        """SOFT circuit breaker should reduce position size via multiplier."""
        now = datetime.now(timezone.utc)

        # Create 3 consecutive losses (MEDIUM soft_stop_losses=3)
        trade_history = [
            TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("50000"),
                exit_price=Decimal("49500"),
                size=Decimal("0.02"),
                pnl=Decimal("-10"),
                fees=Decimal("1"),
                funding_paid=Decimal("0"),
                opened_at=now - timedelta(hours=i + 2),
                closed_at=now - timedelta(hours=i + 1),
            )
            for i in range(3)
        ]

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANAGED,
        )

        # Result with no losses
        result_normal = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=sample_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
            trade_history=[],
        )

        # Result with 3 losses (SOFT)
        result_soft = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=sample_candles,
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
            trade_history=trade_history,
        )

        assert isinstance(result_normal, TradeOrder)
        assert isinstance(result_soft, TradeOrder)
        # SOFT multiplier is 0.5, so size should be roughly half
        assert result_soft.size < result_normal.size


class TestRiskManagerDecisionLog:
    """Tests for decision logging."""

    def test_decision_log_created_on_approve(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Decision log should be created on approval."""
        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, TradeOrder)
        log = manager.get_decision_log()
        assert log is not None
        assert log.decision == "approved"
        assert log.coin == "BTC"
        assert log.reject_reason is None
        assert log.output_size == result.size

    def test_decision_log_created_on_reject(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Decision log should be created on rejection."""
        sample_signal.leverage = 25  # Exceeds MEDIUM max of 10

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=[],
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        log = manager.get_decision_log()
        assert log is not None
        assert log.decision == "rejected"
        assert log.reject_reason == RejectReason.LEVERAGE_EXCEEDED
        assert log.output_size is None


class TestRiskManagerCumulativeRisk:
    """Tests for cumulative risk budget."""

    def test_cumulative_risk_budget_enforced(
        self, sample_signal: TradingSignal, asset_meta: dict
    ):
        """Should reject when cumulative risk exceeds budget."""
        # Create existing positions that use most of the risk budget
        existing_positions = [
            Position(
                coin="ETH",
                size=Decimal("5"),
                entry_price=Decimal("3000"),
                position_value=Decimal("15000"),
                unrealized_pnl=Decimal("0"),
                leverage=10,
                leverage_type="cross",
                risk_amount=Decimal("500"),  # 5% of 10000
            ),
        ]

        # Signal that would push cumulative risk over 6% limit
        sample_signal.size = Decimal("0.5")  # Large position

        manager = RiskManager(
            risk_level=RiskLevel.MEDIUM,
            risk_mode=RiskMode.MANUAL,
        )

        result = manager.evaluate_signal_with_data(
            signal=sample_signal,
            account_value=Decimal("10000"),
            available_balance=Decimal("5000"),
            open_positions=existing_positions,
            asset_meta=asset_meta,
            candles=[],
            funding_rate=Decimal("0.0001"),
            mark_price=Decimal("50000"),
        )

        assert isinstance(result, RiskReject)
        assert result.reason == RejectReason.RISK_BUDGET_EXCEEDED
