"""Tests for order builder."""

from decimal import Decimal

import pytest

from hyperhandler.client import OrderBuilder
from hyperhandler.models import OrderSide, OrderType, TradingSignal


@pytest.fixture
def builder():
    """Create order builder with default slippage."""
    return OrderBuilder(slippage=Decimal("0.005"))


@pytest.fixture
def long_limit_signal():
    """Create a long limit signal."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.LONG,
        order_type=OrderType.LIMIT,
        entry_price=Decimal("67500"),
        size=Decimal("0.1"),
        leverage=5,
    )


@pytest.fixture
def short_limit_signal():
    """Create a short limit signal."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.SHORT,
        order_type=OrderType.LIMIT,
        entry_price=Decimal("67500"),
        size=Decimal("0.1"),
        leverage=5,
    )


@pytest.fixture
def long_market_signal():
    """Create a long market signal."""
    return TradingSignal(
        pair="ETH",
        side=OrderSide.LONG,
        order_type=OrderType.MARKET,
        size=Decimal("1.0"),
        leverage=10,
    )


class TestOrderBuilder:
    """Tests for OrderBuilder."""

    def test_limit_long_order(self, builder, long_limit_signal):
        """U-BLD-01: Limit long order."""
        payload = builder.build_order_payload(long_limit_signal, asset_index=0)

        orders = payload["orders"]
        assert len(orders) == 1

        order = orders[0]
        assert order["b"] is True  # buy
        assert order["t"]["limit"]["tif"] == "Gtc"
        assert order["r"] is False  # not reduce-only

    def test_limit_short_order(self, builder, short_limit_signal):
        """U-BLD-02: Limit short order."""
        payload = builder.build_order_payload(short_limit_signal, asset_index=0)

        order = payload["orders"][0]
        assert order["b"] is False  # sell
        assert order["t"]["limit"]["tif"] == "Gtc"

    def test_market_long_order(self, builder, long_market_signal):
        """U-BLD-03: Market long order."""
        payload = builder.build_order_payload(
            long_market_signal,
            asset_index=1,
            current_price=Decimal("3500"),
        )

        order = payload["orders"][0]
        assert order["b"] is True
        assert order["t"]["limit"]["tif"] == "Ioc"

    def test_market_short_order(self, builder):
        """U-BLD-04: Market short order."""
        signal = TradingSignal(
            pair="ETH",
            side=OrderSide.SHORT,
            order_type=OrderType.MARKET,
            size=Decimal("1.0"),
        )
        payload = builder.build_order_payload(
            signal,
            asset_index=1,
            current_price=Decimal("3500"),
        )

        order = payload["orders"][0]
        assert order["b"] is False
        assert order["t"]["limit"]["tif"] == "Ioc"

    def test_sl_for_long_position(self, builder):
        """U-BLD-05: SL for long position."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            stop_loss=Decimal("66000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        assert len(payload["orders"]) == 2
        sl_order = payload["orders"][1]

        assert sl_order["b"] is False  # Sell to close long
        assert sl_order["r"] is True  # Reduce-only
        assert sl_order["t"]["trigger"]["tpsl"] == "sl"
        assert sl_order["t"]["trigger"]["triggerPx"] == "66000"

    def test_sl_for_short_position(self, builder):
        """U-BLD-06: SL for short position."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.SHORT,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            stop_loss=Decimal("69000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        sl_order = payload["orders"][1]
        assert sl_order["b"] is True  # Buy to close short
        assert sl_order["t"]["trigger"]["tpsl"] == "sl"

    def test_tp_for_long_position(self, builder):
        """U-BLD-07: TP for long position."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            take_profit=Decimal("70000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        tp_order = payload["orders"][1]
        assert tp_order["b"] is False  # Sell to close long
        assert tp_order["t"]["trigger"]["tpsl"] == "tp"
        assert tp_order["t"]["trigger"]["triggerPx"] == "70000"

    def test_tp_for_short_position(self, builder):
        """U-BLD-08: TP for short position."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.SHORT,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            take_profit=Decimal("65000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        tp_order = payload["orders"][1]
        assert tp_order["b"] is True  # Buy to close short
        assert tp_order["t"]["trigger"]["tpsl"] == "tp"

    def test_entry_with_sl_and_tp(self, builder):
        """U-BLD-09: Entry + SL + TP creates 3 orders with normalTpsl grouping."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            stop_loss=Decimal("66000"),
            take_profit=Decimal("70000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        assert payload["grouping"] == "normalTpsl"
        assert len(payload["orders"]) == 3

    def test_entry_only_no_tpsl(self, builder, long_limit_signal):
        """U-BLD-10: Entry only (no SL/TP) has 'na' grouping."""
        payload = builder.build_order_payload(long_limit_signal, asset_index=0)

        assert payload["grouping"] == "na"
        assert len(payload["orders"]) == 1

    def test_slippage_price_long(self, builder, long_market_signal):
        """U-BLD-11: Slippage price for long is higher."""
        payload = builder.build_order_payload(
            long_market_signal,
            asset_index=1,
            current_price=Decimal("100"),
        )

        order = payload["orders"][0]
        price = Decimal(order["p"])
        assert price == Decimal("100.50000")  # 100 * 1.005

    def test_slippage_price_short(self, builder):
        """U-BLD-12: Slippage price for short is lower."""
        signal = TradingSignal(
            pair="ETH",
            side=OrderSide.SHORT,
            order_type=OrderType.MARKET,
            size=Decimal("1.0"),
        )
        payload = builder.build_order_payload(
            signal,
            asset_index=1,
            current_price=Decimal("100"),
        )

        order = payload["orders"][0]
        price = Decimal(order["p"])
        assert price == Decimal("99.50000")  # 100 * 0.995

    def test_asset_index_mapping(self, builder, long_limit_signal):
        """U-BLD-13: Asset index is correctly set."""
        payload = builder.build_order_payload(long_limit_signal, asset_index=5)

        order = payload["orders"][0]
        assert order["a"] == 5

    def test_reduce_only_for_sl_tp(self, builder):
        """U-BLD-14: SL/TP orders have reduce_only=True."""
        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            stop_loss=Decimal("66000"),
            take_profit=Decimal("70000"),
        )
        payload = builder.build_order_payload(signal, asset_index=0)

        entry = payload["orders"][0]
        sl = payload["orders"][1]
        tp = payload["orders"][2]

        assert entry["r"] is False
        assert sl["r"] is True
        assert tp["r"] is True

    def test_cancel_payload(self, builder):
        """Cancel payload is correctly built."""
        payload = builder.build_cancel_payload(asset_index=0, order_id=123456)

        assert payload["type"] == "cancel"
        assert payload["cancels"][0]["a"] == 0
        assert payload["cancels"][0]["o"] == 123456

    def test_leverage_payload(self, builder):
        """Leverage payload is correctly built."""
        payload = builder.build_leverage_payload(
            asset_index=0,
            leverage=10,
            is_cross=True,
        )

        assert payload["type"] == "updateLeverage"
        assert payload["asset"] == 0
        assert payload["leverage"] == 10
        assert payload["isCross"] is True

    def test_market_order_without_price_fails(self, builder, long_market_signal):
        """Market order without current_price raises error."""
        with pytest.raises(ValueError, match="Current price required"):
            builder.build_order_payload(long_market_signal, asset_index=1)
