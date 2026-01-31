"""Tests for data models."""

from decimal import Decimal

import pytest

from hlhandler.models import (
    OpenOrder,
    OrderResult,
    OrderSide,
    OrderStatus,
    OrderType,
    Position,
    TradingSignal,
    VaultInfo,
    VaultPosition,
)


class TestTradingSignal:
    """Tests for TradingSignal model."""

    def test_valid_long_limit_signal(self):
        """U-SIG-01: Valid long limit signal."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
        )
        assert signal.pair == "BTC"
        assert signal.side == OrderSide.LONG
        assert signal.is_buy is True

    def test_valid_short_market_signal(self):
        """U-SIG-02: Valid short market signal."""
        signal = TradingSignal(
            pair="ETH",
            side=OrderSide.SHORT,
            order_type=OrderType.MARKET,
            size=Decimal("1.0"),
        )
        assert signal.pair == "ETH"
        assert signal.side == OrderSide.SHORT
        assert signal.is_buy is False
        assert signal.is_market is True

    def test_normalize_pair_usd_suffix(self):
        """U-SIG-03: Normalize pair BTC-USD."""
        signal = TradingSignal(
            pair="BTC-USD",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.1"),
        )
        assert signal.pair == "BTC"

    def test_normalize_pair_lowercase(self):
        """U-SIG-04: Normalize pair to uppercase."""
        signal = TradingSignal(
            pair="btc",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.1"),
        )
        assert signal.pair == "BTC"

    def test_normalize_pair_perp_suffix(self):
        """Normalize pair with -PERP suffix."""
        signal = TradingSignal(
            pair="ETH-PERP",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.1"),
        )
        assert signal.pair == "ETH"

    def test_limit_without_entry_price_fails(self):
        """U-SIG-05: Limit without entry_price raises error."""
        with pytest.raises(ValueError, match="entry_price is required"):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.LIMIT,
                size=Decimal("0.1"),
            )

    def test_negative_size_fails(self):
        """U-SIG-06: Negative size raises error."""
        with pytest.raises(ValueError):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.MARKET,
                size=Decimal("-0.1"),
            )

    def test_zero_size_fails(self):
        """U-SIG-07: Zero size raises error."""
        with pytest.raises(ValueError):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.MARKET,
                size=Decimal("0"),
            )

    def test_invalid_side_fails(self):
        """U-SIG-08: Invalid side raises error."""
        with pytest.raises(ValueError):
            TradingSignal(
                pair="BTC",
                side="buy",  # type: ignore
                order_type=OrderType.MARKET,
                size=Decimal("0.1"),
            )

    def test_default_leverage(self):
        """U-SIG-09: Default leverage is 5."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.MARKET,
            size=Decimal("0.1"),
        )
        assert signal.leverage == 5

    def test_sl_above_entry_for_long_fails(self):
        """U-SIG-10: SL above entry for long raises error."""
        with pytest.raises(ValueError, match="stop_loss must be below"):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.LIMIT,
                entry_price=Decimal("100"),
                stop_loss=Decimal("110"),
                size=Decimal("0.1"),
            )

    def test_sl_below_entry_for_short_fails(self):
        """U-SIG-11: SL below entry for short raises error."""
        with pytest.raises(ValueError, match="stop_loss must be above"):
            TradingSignal(
                pair="BTC",
                side=OrderSide.SHORT,
                order_type=OrderType.LIMIT,
                entry_price=Decimal("100"),
                stop_loss=Decimal("90"),
                size=Decimal("0.1"),
            )

    def test_tp_below_entry_for_long_fails(self):
        """U-SIG-12: TP below entry for long raises error."""
        with pytest.raises(ValueError, match="take_profit must be above"):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.LIMIT,
                entry_price=Decimal("100"),
                take_profit=Decimal("90"),
                size=Decimal("0.1"),
            )

    def test_tp_above_entry_for_short_fails(self):
        """U-SIG-13: TP above entry for short raises error."""
        with pytest.raises(ValueError, match="take_profit must be below"):
            TradingSignal(
                pair="BTC",
                side=OrderSide.SHORT,
                order_type=OrderType.LIMIT,
                entry_price=Decimal("100"),
                take_profit=Decimal("110"),
                size=Decimal("0.1"),
            )

    def test_leverage_exceeds_max_fails(self):
        """U-SIG-14: Leverage exceeds maximum raises error."""
        with pytest.raises(ValueError):
            TradingSignal(
                pair="BTC",
                side=OrderSide.LONG,
                order_type=OrderType.MARKET,
                size=Decimal("0.1"),
                leverage=100,
            )

    def test_full_signal_with_all_fields(self):
        """U-SIG-15: Full signal with all fields."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            leverage=10,
            stop_loss=Decimal("66000"),
            take_profit=Decimal("70000"),
        )
        assert signal.pair == "BTC"
        assert signal.leverage == 10
        assert signal.stop_loss == Decimal("66000")
        assert signal.take_profit == Decimal("70000")


class TestOrderResult:
    """Tests for OrderResult model."""

    def test_successful_result(self):
        """U-ORD-01: Successful result."""
        result = OrderResult(
            success=True,
            order_id=123,
            filled_size=Decimal("0.1"),
            avg_price=Decimal("67500"),
            status=OrderStatus.FILLED,
        )
        assert result.success is True
        assert result.order_id == 123
        assert result.is_filled is True

    def test_failed_result_with_error(self):
        """U-ORD-02: Failed result with error."""
        result = OrderResult(
            success=False,
            error="Insufficient margin",
            status=OrderStatus.REJECTED,
        )
        assert result.success is False
        assert result.error == "Insufficient margin"

    def test_partial_fill(self):
        """U-ORD-03: Partial fill."""
        result = OrderResult(
            success=True,
            order_id=456,
            filled_size=Decimal("0.05"),
            status=OrderStatus.PARTIALLY_FILLED,
        )
        assert result.is_partial is True
        assert result.filled_size == Decimal("0.05")


class TestVaultModels:
    """Tests for vault models."""

    def test_vault_info(self):
        """U-VLT-01: Valid VaultInfo."""
        info = VaultInfo(
            address="0x123",
            name="Test Vault",
            leader="0x456",
            tvl=Decimal("1000000"),
            apr=Decimal("25.5"),
            profit_share=Decimal("10"),
            lockup_period=86400,
            is_public=True,
            followers=100,
        )
        assert info.lockup_hours == 24.0
        assert info.followers == 100

    def test_vault_position_pnl_calculation(self):
        """U-VLT-04: PnL percentage calculation."""
        position = VaultPosition(
            vault="0x123",
            vault_name="Test Vault",
            shares=Decimal("0.1"),
            deposited=Decimal("1000"),
            current_value=Decimal("1100"),
        )
        assert position.pnl == Decimal("100")
        assert position.pnl_percent == Decimal("10")

    def test_vault_position_negative_pnl(self):
        """VaultPosition with negative PnL."""
        position = VaultPosition(
            vault="0x123",
            vault_name="Test Vault",
            shares=Decimal("0.1"),
            deposited=Decimal("1000"),
            current_value=Decimal("900"),
        )
        assert position.pnl == Decimal("-100")
        assert position.pnl_percent == Decimal("-10")


class TestPosition:
    """Tests for Position model."""

    def test_long_position(self):
        """Long position properties."""
        pos = Position(
            coin="BTC",
            size=Decimal("0.1"),
            entry_price=Decimal("67500"),
            position_value=Decimal("6750"),
            unrealized_pnl=Decimal("100"),
            leverage=5,
            leverage_type="cross",
        )
        assert pos.is_long is True
        assert pos.is_short is False
        assert pos.abs_size == Decimal("0.1")

    def test_short_position(self):
        """Short position properties."""
        pos = Position(
            coin="ETH",
            size=Decimal("-1.0"),
            entry_price=Decimal("3500"),
            position_value=Decimal("3500"),
            unrealized_pnl=Decimal("-50"),
            leverage=10,
            leverage_type="isolated",
        )
        assert pos.is_long is False
        assert pos.is_short is True
        assert pos.abs_size == Decimal("1.0")


class TestOpenOrder:
    """Tests for OpenOrder model."""

    def test_buy_order(self):
        """Buy order properties."""
        order = OpenOrder(
            coin="BTC",
            order_id=123,
            side="B",
            price=Decimal("67000"),
            size=Decimal("0.1"),
            timestamp=1699999999999,
        )
        assert order.is_buy is True

    def test_sell_order(self):
        """Sell order properties."""
        order = OpenOrder(
            coin="BTC",
            order_id=456,
            side="S",
            price=Decimal("68000"),
            size=Decimal("0.1"),
            timestamp=1699999999999,
        )
        assert order.is_buy is False
