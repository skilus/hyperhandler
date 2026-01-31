"""Vault-related models."""

from dataclasses import dataclass
from decimal import Decimal


@dataclass
class VaultInfo:
    """Information about a vault."""

    address: str
    name: str
    leader: str
    tvl: Decimal
    apr: Decimal
    profit_share: Decimal
    lockup_period: int  # seconds
    is_public: bool
    followers: int = 0
    max_capacity: Decimal | None = None

    @property
    def lockup_hours(self) -> float:
        """Get lockup period in hours."""
        return self.lockup_period / 3600


@dataclass
class VaultPosition:
    """User's position in a vault."""

    vault: str
    vault_name: str
    shares: Decimal
    deposited: Decimal
    current_value: Decimal

    @property
    def pnl(self) -> Decimal:
        """Calculate absolute PnL."""
        return self.current_value - self.deposited

    @property
    def pnl_percent(self) -> Decimal:
        """Calculate PnL percentage."""
        if self.deposited == 0:
            return Decimal("0")
        return (self.pnl / self.deposited) * 100


@dataclass
class VaultDetails:
    """Detailed information about a vault including portfolio."""

    info: VaultInfo
    account_value: Decimal
    positions: list[dict]
    follower_state: dict | None = None  # User's state if following this vault
