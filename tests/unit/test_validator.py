"""Tests for signal validator."""

from decimal import Decimal

import pytest

from hlhandler.models import (
    OrderSide,
    OrderType,
    SignalValidator,
    TradingSignal,
    ValidationConfig,
)


@pytest.fixture
def default_validator():
    """Create validator with default config."""
    return SignalValidator()


@pytest.fixture
def strict_validator():
    """Create validator with strict config."""
    return SignalValidator(
        ValidationConfig(
            max_position_size_usd=Decimal("5000"),
            max_leverage=10,
            require_stop_loss=True,
            allowed_pairs=["BTC", "ETH"],
            min_order_size=Decimal("0.01"),
        )
    )


@pytest.fixture
def valid_signal():
    """Create a valid signal for testing."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.LONG,
        order_type=OrderType.LIMIT,
        entry_price=Decimal("67500"),
        size=Decimal("0.1"),
        leverage=5,
        stop_loss=Decimal("66000"),
    )


class TestSignalValidator:
    """Tests for SignalValidator."""

    def test_valid_signal_passes(self, default_validator, valid_signal):
        """U-VAL-01: Size within limit passes."""
        result = default_validator.validate(valid_signal, current_price=Decimal("67500"))
        assert result.valid is True

    def test_size_exceeds_limit(self, strict_validator):
        """U-VAL-02: Size exceeds limit fails."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("1.0"),  # $67,500 > $5,000 limit
            stop_loss=Decimal("66000"),
        )
        result = strict_validator.validate(signal)
        assert result.valid is False
        assert any("exceeds maximum" in e for e in result.errors)

    def test_leverage_within_limit(self, strict_validator, valid_signal):
        """U-VAL-03: Leverage within limit passes."""
        result = strict_validator.validate(valid_signal)
        assert not any("Leverage" in e for e in result.errors)

    def test_leverage_exceeds_limit(self, strict_validator):
        """U-VAL-04: Leverage exceeds limit fails."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("100"),
            size=Decimal("0.01"),
            leverage=15,  # > 10 limit
            stop_loss=Decimal("90"),
        )
        result = strict_validator.validate(signal)
        assert result.valid is False
        assert any("Leverage" in e for e in result.errors)

    def test_require_stop_loss_enabled_without_sl(self, strict_validator):
        """U-VAL-05: SL required but not provided fails."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.01"),
        )
        result = strict_validator.validate(signal, current_price=Decimal("100"))
        assert result.valid is False
        assert any("Stop-loss is required" in e for e in result.errors)

    def test_require_stop_loss_disabled_without_sl(self, default_validator):
        """U-VAL-06: SL optional when not required."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.01"),
        )
        result = default_validator.validate(signal, current_price=Decimal("100"))
        # Should have warning but not error
        assert result.valid is True
        assert any("No stop-loss" in w for w in result.warnings)

    def test_pair_in_whitelist(self, strict_validator, valid_signal):
        """U-VAL-07: Pair in whitelist passes."""
        result = strict_validator.validate(valid_signal)
        assert not any("not in allowed list" in e for e in result.errors)

    def test_pair_not_in_whitelist(self, strict_validator):
        """U-VAL-08: Pair not in whitelist fails."""
        signal = TradingSignal(
            pair="SOL",  # Not in ["BTC", "ETH"]
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.01"),
            stop_loss=Decimal("90"),
        )
        result = strict_validator.validate(signal, current_price=Decimal("100"))
        assert result.valid is False
        assert any("not in allowed list" in e for e in result.errors)

    def test_empty_whitelist_allows_all(self, default_validator):
        """U-VAL-09: Empty whitelist allows all pairs."""
        signal = TradingSignal(
            pair="OBSCURE",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.01"),
        )
        result = default_validator.validate(signal, current_price=Decimal("100"))
        assert not any("not in allowed list" in e for e in result.errors)

    def test_min_order_size(self, strict_validator):
        """U-VAL-10: Size below minimum fails."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.001"),  # < 0.01 minimum
            stop_loss=Decimal("90"),
        )
        result = strict_validator.validate(signal, current_price=Decimal("100"))
        assert result.valid is False
        assert any("below minimum" in e for e in result.errors)

    def test_high_leverage_warning(self, default_validator):
        """High leverage generates warning."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.01"),
            leverage=15,
        )
        result = default_validator.validate(signal, current_price=Decimal("100"))
        assert any("High leverage" in w for w in result.warnings)

    def test_validate_or_raise_success(self, default_validator, valid_signal):
        """validate_or_raise doesn't raise for valid signal."""
        default_validator.validate_or_raise(valid_signal)

    def test_validate_or_raise_failure(self, strict_validator):
        """validate_or_raise raises for invalid signal."""
        signal = TradingSignal(
            pair="SOL",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.001"),
        )
        with pytest.raises(ValueError, match="validation failed"):
            strict_validator.validate_or_raise(signal, current_price=Decimal("100"))
