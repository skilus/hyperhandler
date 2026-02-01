"""Order builder for converting signals to API payloads."""

from decimal import ROUND_HALF_UP, Decimal
from typing import Any

from hyperhandler.models import OrderType, TradingSignal


class OrderBuilder:
    """Builds order payloads from trading signals."""

    # Default price decimals (6 for perps, 8 for spot)
    DEFAULT_PERP_PRICE_DECIMALS = 6
    DEFAULT_SPOT_PRICE_DECIMALS = 8

    def __init__(self, slippage: Decimal = Decimal("0.005")):
        """Initialize the order builder.

        Args:
            slippage: Default slippage for market orders (0.005 = 0.5%).
        """
        self.slippage = slippage

    def build_order_payload(
        self,
        signal: TradingSignal,
        asset_index: int,
        current_price: Decimal | None = None,
        sz_decimals: int = 0,
    ) -> dict[str, Any]:
        """Build an order payload from a trading signal.

        Args:
            signal: The trading signal.
            asset_index: The asset index from the API.
            current_price: Current market price (required for market orders).
            sz_decimals: Size decimals for the asset (affects price rounding).

        Returns:
            Order action payload ready for signing.
        """
        orders = []

        # Build entry order
        entry_order = self._build_entry_order(
            signal, asset_index, current_price, sz_decimals
        )
        orders.append(entry_order)

        # Build SL/TP orders if specified
        sl_order = self._build_sl_order(signal, asset_index)
        if sl_order:
            orders.append(sl_order)

        tp_order = self._build_tp_order(signal, asset_index)
        if tp_order:
            orders.append(tp_order)

        # Determine grouping
        grouping = "na"
        if sl_order or tp_order:
            grouping = "normalTpsl"

        return {
            "type": "order",
            "orders": orders,
            "grouping": grouping,
        }

    def _build_entry_order(
        self,
        signal: TradingSignal,
        asset_index: int,
        current_price: Decimal | None = None,
        sz_decimals: int = 0,
    ) -> dict[str, Any]:
        """Build the entry order."""
        is_buy = signal.is_buy

        # Determine price
        if signal.is_market:
            if current_price is None:
                raise ValueError("Current price required for market orders")
            # Apply slippage and round to tick size
            price = self._slippage_price(current_price, is_buy, sz_decimals)
            tif = "Ioc"  # Immediate-or-cancel for market orders
        else:
            if signal.entry_price is None:
                raise ValueError("Entry price required for limit orders")
            price = signal.entry_price
            tif = "Gtc"  # Good-til-canceled for limit orders

        return {
            "a": asset_index,
            "b": is_buy,
            "p": self._format_price(price),
            "s": self._format_size(signal.size),
            "r": False,  # Not reduce-only for entry
            "t": {"limit": {"tif": tif}},
        }

    def _build_sl_order(
        self,
        signal: TradingSignal,
        asset_index: int,
    ) -> dict[str, Any] | None:
        """Build the stop-loss order."""
        if signal.stop_loss is None:
            return None

        # SL closes the position, so direction is opposite
        is_buy = not signal.is_buy

        # For SL, we use a trigger order
        # The execution price should be slightly worse than trigger
        if is_buy:
            exec_price = signal.stop_loss * (1 + self.slippage)
        else:
            exec_price = signal.stop_loss * (1 - self.slippage)

        # Key order matters for msgpack hashing!
        return {
            "a": asset_index,
            "b": is_buy,
            "p": self._format_price(exec_price),
            "s": self._format_size(signal.size),
            "r": True,  # Reduce-only
            "t": {
                "trigger": {
                    "isMarket": True,
                    "triggerPx": self._format_price(signal.stop_loss),
                    "tpsl": "sl",
                }
            },
        }

    def _build_tp_order(
        self,
        signal: TradingSignal,
        asset_index: int,
    ) -> dict[str, Any] | None:
        """Build the take-profit order."""
        if signal.take_profit is None:
            return None

        # TP closes the position, so direction is opposite
        is_buy = not signal.is_buy

        # For TP, we use a trigger order
        # The execution price should be slightly worse than trigger
        if is_buy:
            exec_price = signal.take_profit * (1 + self.slippage)
        else:
            exec_price = signal.take_profit * (1 - self.slippage)

        # Key order matters for msgpack hashing!
        return {
            "a": asset_index,
            "b": is_buy,
            "p": self._format_price(exec_price),
            "s": self._format_size(signal.size),
            "r": True,  # Reduce-only
            "t": {
                "trigger": {
                    "isMarket": True,
                    "triggerPx": self._format_price(signal.take_profit),
                    "tpsl": "tp",
                }
            },
        }

    def build_cancel_payload(self, asset_index: int, order_id: int) -> dict[str, Any]:
        """Build a cancel order payload.

        Args:
            asset_index: The asset index.
            order_id: The order ID to cancel.

        Returns:
            Cancel action payload.
        """
        return {
            "type": "cancel",
            "cancels": [{"a": asset_index, "o": order_id}],
        }

    def build_leverage_payload(
        self,
        asset_index: int,
        leverage: int,
        is_cross: bool = True,
    ) -> dict[str, Any]:
        """Build a leverage update payload.

        Args:
            asset_index: The asset index.
            leverage: The leverage value.
            is_cross: True for cross margin, False for isolated.

        Returns:
            Leverage update action payload.
        """
        return {
            "type": "updateLeverage",
            "asset": asset_index,
            "isCross": is_cross,
            "leverage": leverage,
        }

    def _slippage_price(
        self,
        price: Decimal,
        is_buy: bool,
        sz_decimals: int,
        is_spot: bool = False,
    ) -> Decimal:
        """Calculate price with slippage and proper rounding.

        Follows the Hyperliquid SDK logic:
        1. Apply slippage
        2. Format to 5 significant figures
        3. Round to (6 - szDecimals) decimal places for perps

        Args:
            price: Current market price.
            is_buy: True for buy orders (add slippage).
            sz_decimals: Size decimals for the asset.
            is_spot: True for spot assets.

        Returns:
            Price with slippage, properly rounded for the API.
        """
        # Apply slippage
        if is_buy:
            px = price * (1 + self.slippage)
        else:
            px = price * (1 - self.slippage)

        # Convert to float for 5 significant figures formatting
        px_float = float(px)

        # Format to 5 significant figures
        sig_fig_str = f"{px_float:.5g}"
        px_rounded = float(sig_fig_str)

        # Calculate decimal places: 6 for perps, 8 for spot, minus szDecimals
        max_decimals = (
            self.DEFAULT_SPOT_PRICE_DECIMALS
            if is_spot
            else self.DEFAULT_PERP_PRICE_DECIMALS
        )
        decimal_places = max_decimals - sz_decimals

        # Round to the appropriate number of decimal places
        final_price = round(px_rounded, decimal_places)

        return Decimal(str(final_price))

    @staticmethod
    def _format_price(price: Decimal) -> str:
        """Format price for API.

        Hyperliquid expects normalized string prices without trailing zeros.
        E.g. "1700" not "1700.0" or "1700.00000"
        """
        # Round to 8 decimals, then normalize to remove trailing zeros
        rounded = price.quantize(Decimal("0.00000001"))
        normalized = rounded.normalize()
        return f"{normalized:f}"

    @staticmethod
    def _format_size(size: Decimal) -> str:
        """Format size for API.

        Hyperliquid expects normalized string sizes without trailing zeros.
        """
        rounded = size.quantize(Decimal("0.00000001"))
        normalized = rounded.normalize()
        return f"{normalized:f}"
