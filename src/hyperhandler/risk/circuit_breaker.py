"""Circuit breaker for consecutive losses and daily limits."""

from datetime import datetime, timezone
from decimal import Decimal

from hyperhandler.models.risk import (
    CircuitBreakerStatus,
    CircuitBreakerTrigger,
    RejectReason,
    RiskReject,
    TradeResult,
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
        """Check circuit breaker status based on trade history.

        Args:
            trade_history: List of recent closed trades
            account_value: Current account equity

        Returns:
            CircuitBreakerStatus with current state
        """
        # Count consecutive losses (most recent first)
        consecutive_losses = 0
        for trade in reversed(trade_history):
            if trade.is_loss:
                consecutive_losses += 1
            else:
                break

        # Calculate daily P&L
        today_start = self._get_utc_day_start()
        today_trades = [t for t in trade_history if t.closed_at >= today_start]
        daily_pnl = sum(t.pnl for t in today_trades)
        daily_loss_pct = (
            abs(min(Decimal("0"), daily_pnl)) / account_value
            if account_value > 0
            else Decimal("0")
        )

        # Check daily loss limit (HARD stop)
        if daily_loss_pct >= self.profile.daily_loss_limit:
            return CircuitBreakerStatus(
                active=True,
                level="HARD",
                trigger=CircuitBreakerTrigger.DAILY_LOSS,
                risk_multiplier=Decimal("0"),
                reason=f"Daily loss {daily_loss_pct:.1%} >= limit {self.profile.daily_loss_limit:.1%}",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        # Check hard stop (consecutive losses)
        if consecutive_losses >= self.profile.hard_stop_losses:
            return CircuitBreakerStatus(
                active=True,
                level="HARD",
                trigger=CircuitBreakerTrigger.CONSECUTIVE,
                risk_multiplier=Decimal("0"),
                reason=f"{consecutive_losses} consecutive losses (hard limit: {self.profile.hard_stop_losses})",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        # Check soft stop (reduced risk)
        if consecutive_losses >= self.profile.soft_stop_losses:
            return CircuitBreakerStatus(
                active=True,
                level="SOFT",
                trigger=CircuitBreakerTrigger.CONSECUTIVE,
                risk_multiplier=Decimal("0.5"),
                reason=f"{consecutive_losses} consecutive losses (soft limit: {self.profile.soft_stop_losses})",
                consecutive_losses=consecutive_losses,
                daily_loss_pct=daily_loss_pct,
            )

        # No circuit breaker active
        return CircuitBreakerStatus(
            active=False,
            level="NONE",
            trigger=CircuitBreakerTrigger.NONE,
            risk_multiplier=Decimal("1.0"),
            consecutive_losses=consecutive_losses,
            daily_loss_pct=daily_loss_pct,
        )

    def get_reject(self, status: CircuitBreakerStatus) -> RiskReject | None:
        """Get reject reason if circuit breaker is at HARD level.

        Args:
            status: Current circuit breaker status

        Returns:
            RiskReject if HARD stop, None otherwise
        """
        if not status.active:
            return None

        if status.level == "HARD":
            if status.trigger == CircuitBreakerTrigger.DAILY_LOSS:
                return RiskReject(
                    reason=RejectReason.DAILY_LOSS_LIMIT,
                    details=status.reason or "Daily loss limit reached",
                    suggested_action="wait",
                )
            elif status.trigger == CircuitBreakerTrigger.CONSECUTIVE:
                return RiskReject(
                    reason=RejectReason.CIRCUIT_BREAKER_HARD,
                    details=status.reason or "Too many consecutive losses",
                    suggested_action="manual_reset",
                )

        # SOFT level doesn't reject, just reduces risk via multiplier
        return None

    def _get_utc_day_start(self) -> datetime:
        """Get start of current UTC day."""
        now = datetime.now(timezone.utc)
        return now.replace(hour=0, minute=0, second=0, microsecond=0)
