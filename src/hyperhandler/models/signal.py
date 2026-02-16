"""Trading signal model."""

from decimal import Decimal
from enum import Enum

from pydantic import BaseModel, Field, field_validator, model_validator


class OrderSide(str, Enum):
    """Order side enum."""

    LONG = "long"
    SHORT = "short"


class OrderType(str, Enum):
    """Order type enum."""

    MARKET = "market"
    LIMIT = "limit"


class SignalHorizon(str, Enum):
    """Expected hold duration for ATR timeframe selection."""

    SCALP = "scalp"  # <4h, uses 15m candles
    INTRADAY = "intraday"  # 4h-24h, uses 1h candles
    SWING = "swing"  # 1d-7d, uses 4h candles
    POSITION = "position"  # >7d, uses 1d candles


class TradingSignal(BaseModel):
    """Trading signal model with validation."""

    pair: str = Field(..., description="Asset symbol (BTC, ETH, SOL)")
    side: OrderSide = Field(..., description="Trade direction")
    order_type: OrderType = Field(..., description="Order type")
    size: Decimal = Field(..., gt=0, description="Position size")
    leverage: int = Field(default=5, ge=1, le=50, description="Leverage")
    entry_price: Decimal | None = Field(default=None, description="Entry price for limit orders")
    stop_loss: Decimal | None = Field(default=None, description="Stop-loss price")
    take_profit: Decimal | None = Field(default=None, description="Take-profit price")

    # Risk management fields
    confidence: float | None = Field(
        default=None,
        ge=0.0,
        le=1.0,
        description="Signal confidence (0.0-1.0), affects position sizing",
    )
    source: str | None = Field(
        default=None,
        description="Signal source ID (influencer/strategy)",
    )
    horizon: SignalHorizon = Field(
        default=SignalHorizon.INTRADAY,
        description="Expected hold duration for ATR calculation",
    )

    @field_validator("pair")
    @classmethod
    def normalize_pair(cls, v: str) -> str:
        """Normalize pair symbol to uppercase without suffixes."""
        return v.upper().replace("-USD", "").replace("-PERP", "").replace("/USD", "")

    @model_validator(mode="after")
    def validate_prices(self) -> "TradingSignal":
        """Validate entry_price, stop_loss, and take_profit logic."""
        # Require entry_price for limit orders
        if self.order_type == OrderType.LIMIT and self.entry_price is None:
            raise ValueError("entry_price is required for limit orders")

        # Get reference price for SL/TP validation
        ref_price = self.entry_price
        if ref_price is None:
            # For market orders, we can't validate SL/TP without knowing market price
            return self

        # Validate stop_loss position
        if self.stop_loss is not None:
            if self.side == OrderSide.LONG and self.stop_loss >= ref_price:
                raise ValueError("stop_loss must be below entry_price for long positions")
            if self.side == OrderSide.SHORT and self.stop_loss <= ref_price:
                raise ValueError("stop_loss must be above entry_price for short positions")

        # Validate take_profit position
        if self.take_profit is not None:
            if self.side == OrderSide.LONG and self.take_profit <= ref_price:
                raise ValueError("take_profit must be above entry_price for long positions")
            if self.side == OrderSide.SHORT and self.take_profit >= ref_price:
                raise ValueError("take_profit must be below entry_price for short positions")

        return self

    @property
    def is_buy(self) -> bool:
        """Return True if this is a buy order."""
        return self.side == OrderSide.LONG

    @property
    def is_market(self) -> bool:
        """Return True if this is a market order."""
        return self.order_type == OrderType.MARKET
