"""Order-related models."""

from dataclasses import dataclass, field
from datetime import datetime
from decimal import Decimal
from enum import Enum


class OrderStatus(str, Enum):
    """Order status enum."""

    PENDING = "pending"
    OPEN = "open"
    FILLED = "filled"
    PARTIALLY_FILLED = "partially_filled"
    CANCELLED = "cancelled"
    REJECTED = "rejected"


@dataclass
class OrderResult:
    """Result of an order placement."""

    success: bool
    order_id: int | None = None
    filled_size: Decimal = field(default_factory=lambda: Decimal("0"))
    avg_price: Decimal | None = None
    error: str | None = None
    status: OrderStatus = OrderStatus.PENDING

    @property
    def is_filled(self) -> bool:
        """Check if order is fully filled."""
        return self.status == OrderStatus.FILLED

    @property
    def is_partial(self) -> bool:
        """Check if order is partially filled."""
        return self.status == OrderStatus.PARTIALLY_FILLED


@dataclass
class OpenOrder:
    """Representation of an open order."""

    coin: str
    order_id: int
    side: str  # "B" for buy, "S" for sell
    price: Decimal
    size: Decimal
    timestamp: int
    order_type: str = "limit"
    reduce_only: bool = False

    @property
    def is_buy(self) -> bool:
        """Check if this is a buy order."""
        return self.side == "B"


@dataclass
class Position:
    """Representation of an open position."""

    coin: str
    size: Decimal  # Positive for long, negative for short
    entry_price: Decimal
    position_value: Decimal
    unrealized_pnl: Decimal
    leverage: int
    leverage_type: str  # "cross" or "isolated"
    liquidation_price: Decimal | None = None

    # Risk tracking fields
    mark_price: Decimal | None = None
    funding_accrued: Decimal = field(default_factory=lambda: Decimal("0"))
    stop_loss_price: Decimal | None = None
    opened_at: datetime | None = None

    # Calculated risk fields (populated by RiskManager)
    risk_amount: Decimal | None = None  # $ at risk = size * |entry - stop|
    risk_pct: Decimal | None = None  # % of account value
    correlation_group: str | None = None  # "btc-major", "l1-alt", etc.

    @property
    def is_long(self) -> bool:
        """Check if this is a long position."""
        return self.size > 0

    @property
    def is_short(self) -> bool:
        """Check if this is a short position."""
        return self.size < 0

    @property
    def abs_size(self) -> Decimal:
        """Get absolute position size."""
        return abs(self.size)
