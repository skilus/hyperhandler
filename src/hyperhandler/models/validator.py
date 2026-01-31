"""Signal validation logic."""

from dataclasses import dataclass, field
from decimal import Decimal

from hyperhandler.models.signal import TradingSignal


@dataclass
class ValidationConfig:
    """Configuration for signal validation."""

    max_position_size_usd: Decimal = Decimal("10000")
    max_leverage: int = 20
    min_order_size: Decimal = Decimal("0.0001")
    require_stop_loss: bool = False
    allowed_pairs: list[str] = field(default_factory=list)  # Empty = all allowed


@dataclass
class ValidationResult:
    """Result of signal validation."""

    valid: bool
    errors: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)


class SignalValidator:
    """Validates trading signals against configured limits."""

    def __init__(self, config: ValidationConfig | None = None):
        self.config = config or ValidationConfig()

    def validate(
        self,
        signal: TradingSignal,
        current_price: Decimal | None = None,
    ) -> ValidationResult:
        """Validate a trading signal.

        Args:
            signal: The trading signal to validate.
            current_price: Current market price for size validation.

        Returns:
            ValidationResult with errors and warnings.
        """
        errors: list[str] = []
        warnings: list[str] = []

        # Check allowed pairs
        if self.config.allowed_pairs:
            if signal.pair not in self.config.allowed_pairs:
                errors.append(
                    f"Pair {signal.pair} not in allowed list: {self.config.allowed_pairs}"
                )

        # Check leverage
        if signal.leverage > self.config.max_leverage:
            errors.append(
                f"Leverage {signal.leverage} exceeds maximum {self.config.max_leverage}"
            )

        # Check minimum order size
        if signal.size < self.config.min_order_size:
            errors.append(
                f"Size {signal.size} below minimum {self.config.min_order_size}"
            )

        # Check position size in USD (if price available)
        price = current_price or signal.entry_price
        if price is not None:
            position_usd = signal.size * price
            if position_usd > self.config.max_position_size_usd:
                errors.append(
                    f"Position size ${position_usd:.2f} exceeds maximum "
                    f"${self.config.max_position_size_usd}"
                )

        # Check stop-loss requirement
        if self.config.require_stop_loss and signal.stop_loss is None:
            errors.append("Stop-loss is required but not provided")

        # Warnings for risky setups
        if signal.leverage > 10:
            warnings.append(f"High leverage ({signal.leverage}x) - increased risk")

        if signal.stop_loss is None:
            warnings.append("No stop-loss set - position has unlimited downside risk")

        return ValidationResult(
            valid=len(errors) == 0,
            errors=errors,
            warnings=warnings,
        )

    def validate_or_raise(
        self,
        signal: TradingSignal,
        current_price: Decimal | None = None,
    ) -> None:
        """Validate signal and raise exception if invalid.

        Args:
            signal: The trading signal to validate.
            current_price: Current market price for size validation.

        Raises:
            ValueError: If validation fails.
        """
        result = self.validate(signal, current_price)
        if not result.valid:
            raise ValueError(f"Signal validation failed: {'; '.join(result.errors)}")
