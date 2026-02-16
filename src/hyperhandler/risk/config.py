"""Risk profiles and Hyperliquid-specific configuration."""

from dataclasses import dataclass
from decimal import Decimal

from hyperhandler.models.risk import RiskLevel
from hyperhandler.models.signal import SignalHorizon


@dataclass
class RiskProfile:
    """Risk management parameters for a risk level."""

    risk_per_trade: Decimal  # % of account per trade
    max_cumulative_risk: Decimal  # Max total risk across positions
    daily_loss_limit: Decimal  # Max daily loss before hard stop
    max_open_positions: int
    max_leverage: int
    correlation_factor: Decimal  # Penalty for correlated positions
    soft_stop_losses: int  # Consecutive losses for soft CB (reduced risk)
    hard_stop_losses: int  # Consecutive losses for hard CB (blocked)
    max_funding_risk_pct: Decimal  # Max funding cost as % of risk

    @classmethod
    def get(cls, level: RiskLevel) -> "RiskProfile":
        """Get profile by risk level."""
        return RISK_PROFILES[level]


RISK_PROFILES: dict[RiskLevel, RiskProfile] = {
    RiskLevel.LOW: RiskProfile(
        risk_per_trade=Decimal("0.01"),  # 1%
        max_cumulative_risk=Decimal("0.04"),  # 4%
        daily_loss_limit=Decimal("0.02"),  # 2%
        max_open_positions=3,
        max_leverage=5,
        correlation_factor=Decimal("0.4"),
        soft_stop_losses=2,
        hard_stop_losses=4,
        max_funding_risk_pct=Decimal("0.3"),  # 30%
    ),
    RiskLevel.MEDIUM: RiskProfile(
        risk_per_trade=Decimal("0.02"),  # 2%
        max_cumulative_risk=Decimal("0.06"),  # 6%
        daily_loss_limit=Decimal("0.03"),  # 3%
        max_open_positions=5,
        max_leverage=10,
        correlation_factor=Decimal("0.3"),
        soft_stop_losses=3,
        hard_stop_losses=5,
        max_funding_risk_pct=Decimal("0.5"),  # 50%
    ),
    RiskLevel.HIGH: RiskProfile(
        risk_per_trade=Decimal("0.03"),  # 3%
        max_cumulative_risk=Decimal("0.10"),  # 10%
        daily_loss_limit=Decimal("0.05"),  # 5%
        max_open_positions=8,
        max_leverage=20,
        correlation_factor=Decimal("0.25"),
        soft_stop_losses=3,
        hard_stop_losses=6,
        max_funding_risk_pct=Decimal("0.7"),  # 70%
    ),
}


@dataclass
class HLConfig:
    """Hyperliquid-specific configuration."""

    # Fees (Tier 0, no staking discount)
    taker_fee: Decimal = Decimal("0.00045")  # 0.045%
    maker_fee: Decimal = Decimal("0.00015")  # 0.015%

    # Order constraints
    min_order_value: Decimal = Decimal("10.0")  # $10 minimum
    max_slippage: Decimal = Decimal("0.01")  # 1%
    slippage_buffer: Decimal = Decimal("0.005")  # 0.5%

    # Margin
    default_margin_mode: str = "cross"
    liq_safety_buffer: Decimal = Decimal("0.02")  # 2% buffer from liquidation

    # Signal validation
    max_entry_deviation: Decimal = Decimal("0.01")  # 1% max price deviation
    max_signal_age_seconds: int = 300  # 5 minutes


# ATR settings per horizon
ATR_SETTINGS: dict[SignalHorizon, dict] = {
    SignalHorizon.SCALP: {
        "interval": "15m",
        "period": 14,
        "multiplier": Decimal("1.2"),
    },
    SignalHorizon.INTRADAY: {
        "interval": "1h",
        "period": 14,
        "multiplier": Decimal("1.5"),
    },
    SignalHorizon.SWING: {
        "interval": "4h",
        "period": 14,
        "multiplier": Decimal("2.0"),
    },
    SignalHorizon.POSITION: {
        "interval": "1d",
        "period": 14,
        "multiplier": Decimal("2.5"),
    },
}


# Correlation groups for cumulative risk calculation
CORRELATION_MAP: dict[str, list[str]] = {
    "btc-major": ["BTC", "ETH"],
    "l1-alt": ["SOL", "AVAX", "SUI", "APT", "SEI"],
    "defi": ["AAVE", "UNI", "MKR", "CRV", "DYDX"],
    "meme": ["DOGE", "SHIB", "PEPE", "WIF", "BONK"],
    "ai": ["FET", "RNDR", "TAO", "NEAR"],
}


# Confidence scaling bounds
MIN_CONFIDENCE_FACTOR = Decimal("0.3")  # Minimum 30% of normal risk
MAX_CONFIDENCE_FACTOR = Decimal("1.0")

# HL approximate maintenance margin
HL_MAINTENANCE_MARGIN = Decimal("0.005")  # 0.5%
