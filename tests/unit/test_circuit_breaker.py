"""Unit tests for CircuitBreaker."""

from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest

from hyperhandler.models.risk import (
    CircuitBreakerTrigger,
    RejectReason,
    RiskLevel,
    TradeResult,
)
from hyperhandler.risk import RISK_PROFILES
from hyperhandler.risk.circuit_breaker import CircuitBreaker


@pytest.fixture
def circuit_breaker() -> CircuitBreaker:
    """Create circuit breaker with MEDIUM profile."""
    profile = RISK_PROFILES[RiskLevel.MEDIUM]
    return CircuitBreaker(profile)


@pytest.fixture
def winning_trade() -> TradeResult:
    """Create a winning trade."""
    now = datetime.now(timezone.utc)
    return TradeResult(
        coin="BTC",
        side="long",
        entry_price=Decimal("50000"),
        exit_price=Decimal("51000"),
        size=Decimal("0.1"),
        pnl=Decimal("100"),  # Win
        fees=Decimal("5"),
        funding_paid=Decimal("0"),
        opened_at=now - timedelta(hours=2),
        closed_at=now - timedelta(hours=1),
    )


@pytest.fixture
def losing_trade() -> TradeResult:
    """Create a small losing trade (won't trigger daily loss alone)."""
    now = datetime.now(timezone.utc)
    return TradeResult(
        coin="BTC",
        side="long",
        entry_price=Decimal("50000"),
        exit_price=Decimal("49500"),
        size=Decimal("0.02"),
        pnl=Decimal("-10"),  # Small loss: 0.1% of 10000
        fees=Decimal("1"),
        funding_paid=Decimal("0"),
        opened_at=now - timedelta(hours=2),
        closed_at=now - timedelta(hours=1),
    )


class TestCircuitBreakerCheck:
    """Tests for circuit breaker status check."""

    def test_no_trades_no_trigger(self, circuit_breaker: CircuitBreaker):
        """No trades = no circuit breaker."""
        status = circuit_breaker.check([], account_value=Decimal("10000"))

        assert status.active is False
        assert status.level == "NONE"
        assert status.trigger == CircuitBreakerTrigger.NONE
        assert status.risk_multiplier == Decimal("1.0")
        assert status.consecutive_losses == 0

    def test_winning_trades_no_trigger(
        self, circuit_breaker: CircuitBreaker, winning_trade: TradeResult
    ):
        """Winning trades don't trigger circuit breaker."""
        trades = [winning_trade] * 5

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        assert status.active is False
        assert status.level == "NONE"
        assert status.consecutive_losses == 0

    def test_soft_stop_consecutive_losses(
        self, circuit_breaker: CircuitBreaker, losing_trade: TradeResult
    ):
        """3 consecutive losses triggers SOFT stop (MEDIUM profile)."""
        trades = [losing_trade] * 3

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        assert status.active is True
        assert status.level == "SOFT"
        assert status.trigger == CircuitBreakerTrigger.CONSECUTIVE
        assert status.risk_multiplier == Decimal("0.5")
        assert status.consecutive_losses == 3

    def test_hard_stop_consecutive_losses(
        self, circuit_breaker: CircuitBreaker, losing_trade: TradeResult
    ):
        """5 consecutive losses triggers HARD stop (MEDIUM profile)."""
        trades = [losing_trade] * 5

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        assert status.active is True
        assert status.level == "HARD"
        assert status.trigger == CircuitBreakerTrigger.CONSECUTIVE
        assert status.risk_multiplier == Decimal("0")
        assert status.consecutive_losses == 5

    def test_winning_trade_resets_consecutive(
        self,
        circuit_breaker: CircuitBreaker,
        winning_trade: TradeResult,
        losing_trade: TradeResult,
    ):
        """A winning trade resets the consecutive loss count."""
        # 2 losses, then 1 win, then 2 losses
        trades = [
            losing_trade,
            losing_trade,
            winning_trade,  # Resets count
            losing_trade,
            losing_trade,
        ]

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        # Only 2 consecutive losses (after the win)
        assert status.active is False
        assert status.consecutive_losses == 2

    def test_daily_loss_limit_trigger(self, circuit_breaker: CircuitBreaker):
        """Daily loss >= 3% triggers HARD stop (MEDIUM profile)."""
        now = datetime.now(timezone.utc)

        # Create trades with total daily loss of 3.5%
        trades = [
            TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("50000"),
                exit_price=Decimal("49000"),
                size=Decimal("0.7"),
                pnl=Decimal("-350"),  # 3.5% of 10000
                fees=Decimal("5"),
                funding_paid=Decimal("0"),
                opened_at=now - timedelta(hours=2),
                closed_at=now - timedelta(minutes=30),  # Today
            )
        ]

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        assert status.active is True
        assert status.level == "HARD"
        assert status.trigger == CircuitBreakerTrigger.DAILY_LOSS
        assert status.risk_multiplier == Decimal("0")
        assert status.daily_loss_pct >= Decimal("0.03")

    def test_daily_loss_ignores_yesterday(self, circuit_breaker: CircuitBreaker):
        """Trades from yesterday don't count toward daily loss."""
        now = datetime.now(timezone.utc)
        yesterday = now - timedelta(days=1)

        trades = [
            TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("50000"),
                exit_price=Decimal("49000"),
                size=Decimal("1"),
                pnl=Decimal("-1000"),  # 10% loss
                fees=Decimal("5"),
                funding_paid=Decimal("0"),
                opened_at=yesterday - timedelta(hours=2),
                closed_at=yesterday,  # Yesterday
            )
        ]

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        # Should not trigger because loss was yesterday
        assert status.trigger != CircuitBreakerTrigger.DAILY_LOSS
        # But still counts as consecutive loss
        assert status.consecutive_losses == 1

    def test_daily_loss_takes_priority(
        self, circuit_breaker: CircuitBreaker, losing_trade: TradeResult
    ):
        """Daily loss limit check happens before consecutive check."""
        now = datetime.now(timezone.utc)

        # 2 losses but with high daily loss (> 3%)
        big_loss = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("48000"),
            size=Decimal("0.5"),
            pnl=Decimal("-500"),  # 5% loss
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=now - timedelta(hours=2),
            closed_at=now - timedelta(minutes=30),
        )

        status = circuit_breaker.check([big_loss], account_value=Decimal("10000"))

        # Daily loss should trigger first (even with only 1 trade)
        assert status.level == "HARD"
        assert status.trigger == CircuitBreakerTrigger.DAILY_LOSS

    def test_consecutive_loss_count_from_end(
        self, circuit_breaker: CircuitBreaker,
        winning_trade: TradeResult,
        losing_trade: TradeResult,
    ):
        """Consecutive losses counted from most recent trades."""
        # Win, then 3 losses (most recent)
        trades = [
            winning_trade,
            losing_trade,
            losing_trade,
            losing_trade,
        ]

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))

        assert status.consecutive_losses == 3
        assert status.level == "SOFT"


class TestGetReject:
    """Tests for get_reject method."""

    def test_get_reject_not_active(self, circuit_breaker: CircuitBreaker):
        """No reject when circuit breaker not active."""
        status = circuit_breaker.check([], account_value=Decimal("10000"))

        reject = circuit_breaker.get_reject(status)

        assert reject is None

    def test_get_reject_soft_no_reject(
        self, circuit_breaker: CircuitBreaker, losing_trade: TradeResult
    ):
        """SOFT level doesn't reject (just reduces risk)."""
        trades = [losing_trade] * 3  # Triggers SOFT

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))
        reject = circuit_breaker.get_reject(status)

        assert status.level == "SOFT"
        assert reject is None  # SOFT doesn't reject

    def test_get_reject_hard_consecutive(
        self, circuit_breaker: CircuitBreaker, losing_trade: TradeResult
    ):
        """HARD level from consecutive losses returns proper reject."""
        trades = [losing_trade] * 5  # Triggers HARD

        status = circuit_breaker.check(trades, account_value=Decimal("10000"))
        reject = circuit_breaker.get_reject(status)

        assert reject is not None
        assert reject.reason == RejectReason.CIRCUIT_BREAKER_HARD
        assert reject.suggested_action == "manual_reset"

    def test_get_reject_hard_daily_loss(self, circuit_breaker: CircuitBreaker):
        """HARD level from daily loss returns proper reject."""
        now = datetime.now(timezone.utc)

        big_loss = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("48000"),
            size=Decimal("0.5"),
            pnl=Decimal("-500"),  # 5% > 3% daily limit
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=now - timedelta(hours=2),
            closed_at=now - timedelta(minutes=30),
        )

        status = circuit_breaker.check([big_loss], account_value=Decimal("10000"))
        reject = circuit_breaker.get_reject(status)

        assert reject is not None
        assert reject.reason == RejectReason.DAILY_LOSS_LIMIT
        assert reject.suggested_action == "wait"


class TestTradeResultIsLoss:
    """Tests for TradeResult.is_loss property."""

    def test_is_loss_negative_pnl(self):
        """Negative PnL = loss."""
        trade = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("49000"),
            size=Decimal("0.1"),
            pnl=Decimal("-100"),
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=datetime.now(timezone.utc),
            closed_at=datetime.now(timezone.utc),
        )

        assert trade.is_loss is True

    def test_is_loss_positive_pnl(self):
        """Positive PnL = not a loss."""
        trade = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("51000"),
            size=Decimal("0.1"),
            pnl=Decimal("100"),
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=datetime.now(timezone.utc),
            closed_at=datetime.now(timezone.utc),
        )

        assert trade.is_loss is False

    def test_is_loss_zero_pnl(self):
        """Zero PnL = not a loss."""
        trade = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("50000"),
            size=Decimal("0.1"),
            pnl=Decimal("0"),
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=datetime.now(timezone.utc),
            closed_at=datetime.now(timezone.utc),
        )

        assert trade.is_loss is False


class TestRiskProfiles:
    """Tests for different risk profile behavior."""

    def test_low_profile_stricter_limits(self):
        """LOW profile has stricter consecutive loss limits."""
        low_profile = RISK_PROFILES[RiskLevel.LOW]
        cb = CircuitBreaker(low_profile)

        now = datetime.now(timezone.utc)
        # Small loss that won't trigger daily limit (0.1% each)
        loss = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("49500"),
            size=Decimal("0.02"),
            pnl=Decimal("-10"),  # 0.1% of 10000
            fees=Decimal("1"),
            funding_paid=Decimal("0"),
            opened_at=now - timedelta(hours=2),
            closed_at=now - timedelta(hours=1),
        )

        # LOW: soft_stop_losses=2, hard_stop_losses=4
        status_2 = cb.check([loss, loss], account_value=Decimal("10000"))
        status_4 = cb.check([loss] * 4, account_value=Decimal("10000"))

        assert status_2.level == "SOFT"
        assert status_4.level == "HARD"

    def test_high_profile_looser_limits(self):
        """HIGH profile has looser consecutive loss limits."""
        high_profile = RISK_PROFILES[RiskLevel.HIGH]
        cb = CircuitBreaker(high_profile)

        now = datetime.now(timezone.utc)
        # Small loss that won't trigger daily limit (0.5% each)
        loss = TradeResult(
            coin="BTC",
            side="long",
            entry_price=Decimal("50000"),
            exit_price=Decimal("49000"),
            size=Decimal("0.1"),
            pnl=Decimal("-50"),  # 0.5% of 10000
            fees=Decimal("5"),
            funding_paid=Decimal("0"),
            opened_at=now - timedelta(hours=2),
            closed_at=now - timedelta(hours=1),
        )

        # HIGH: soft_stop_losses=3, hard_stop_losses=6, daily_loss_limit=5%
        status_3 = cb.check([loss] * 3, account_value=Decimal("10000"))  # 1.5% daily
        status_5 = cb.check([loss] * 5, account_value=Decimal("10000"))  # 2.5% daily
        status_6 = cb.check([loss] * 6, account_value=Decimal("10000"))  # 3% daily

        assert status_3.level == "SOFT"
        assert status_5.level == "SOFT"  # Still soft at 5
        assert status_6.level == "HARD"
