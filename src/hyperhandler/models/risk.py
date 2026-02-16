"""Risk management models."""

from datetime import datetime
from decimal import Decimal
from enum import Enum
from typing import TYPE_CHECKING

from pydantic import BaseModel, Field

if TYPE_CHECKING:
    from hyperhandler.storage import Storage


class RiskLevel(str, Enum):
    """Risk tolerance level."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"


class RiskMode(str, Enum):
    """Risk management mode."""

    MANUAL = "manual"  # Validate signal as-is
    MANAGED = "managed"  # Calculate size/sl from risk budget


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
    distance: Decimal  # Absolute distance from entry
    distance_pct: Decimal  # Distance as % of entry
    atr_value: Decimal
    atr_multiplier: Decimal


class PositionSizeResult(BaseModel):
    """Calculated position size parameters."""

    size: Decimal  # In base currency, rounded to szDecimals
    notional: Decimal  # size * entry_price
    margin_required: Decimal
    risk_amount: Decimal  # $ at risk
    risk_pct: Decimal  # % of account
    commission_estimate: Decimal


class LeverageResult(BaseModel):
    """Selected leverage parameters."""

    leverage: int
    max_safe: int  # Based on stop distance
    max_coin: int  # HL max for this coin
    max_config: int  # User config max
    reason: str  # Why this leverage was selected


class CumulativeRiskResult(BaseModel):
    """Portfolio cumulative risk calculation."""

    raw_risk: Decimal  # Sum of all risk amounts
    adjusted_risk: Decimal  # With correlation penalty
    risk_pct: Decimal  # % of account
    available_budget: Decimal  # Remaining risk budget
    within_limit: bool
    correlation_groups: dict[str, list[str]]  # group -> coins


class FundingEstimate(BaseModel):
    """Estimated funding costs."""

    hourly_rate: Decimal
    hourly_cost: Decimal  # If paying
    hourly_income: Decimal  # If receiving
    projected_24h: Decimal
    funding_eats_risk_pct: Decimal


class CircuitBreakerStatus(BaseModel):
    """Circuit breaker state."""

    active: bool
    level: str = Field(description="NONE | SOFT | HARD")
    trigger: CircuitBreakerTrigger = CircuitBreakerTrigger.NONE
    risk_multiplier: Decimal = Field(
        default=Decimal("1.0"),
        description="1.0 = normal, 0.5 = reduced, 0.0 = blocked",
    )
    reason: str | None = None
    consecutive_losses: int = 0
    daily_loss_pct: Decimal = Decimal("0")


class TradeOrder(BaseModel):
    """Output: ready-to-execute order with risk parameters."""

    # Order params
    coin: str
    asset_id: int
    side: str  # "long" | "short"
    size: Decimal
    entry_price: Decimal
    leverage: int
    margin_mode: str = "cross"  # auto-set to "isolated" for onlyIsolated coins

    # Risk params
    stop_loss: Decimal
    risk_amount: Decimal
    risk_pct: Decimal
    cumulative_risk_after: Decimal
    estimated_liquidation: Decimal

    # Cost estimates
    estimated_commission: Decimal
    estimated_funding_24h: Decimal
    margin_required: Decimal

    # Mode tracking
    risk_mode: RiskMode
    size_source: str = Field(description="signal | calculated")
    sl_source: str = Field(description="signal | calculated | none")

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
    pnl: Decimal  # Realized P&L in USDC
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
    decision: str  # "approved" | "rejected"
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

    # Account state
    account_value: Decimal  # marginSummary.accountValue (equity)
    available_balance: Decimal  # withdrawable (free margin)
    estimated_liquidation: Decimal | None = None

    def persist(self, storage: "Storage", network: str) -> None:
        """Save decision to storage."""
        import logging

        storage.save_risk_decision(self, network)
        logging.getLogger(__name__).info(
            f"Risk decision: {self.decision} | {self.coin} {self.side} | "
            f"risk={self.risk_per_trade_pct:.2%} | reason={self.reject_reason}"
        )
