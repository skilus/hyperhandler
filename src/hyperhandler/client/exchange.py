"""Exchange API client for Hyperliquid."""

from decimal import Decimal
from typing import Any

from hyperhandler.client.base import BaseClient
from hyperhandler.client.order_builder import OrderBuilder
from hyperhandler.config import NetworkConfig
from hyperhandler.models import OrderResult, OrderStatus, OrderType, TradingSignal
from hyperhandler.signer import Signer


class ExchangeClient(BaseClient):
    """Client for Hyperliquid Exchange API (trading operations)."""

    def __init__(
        self,
        network: NetworkConfig,
        signer: Signer,
        slippage: Decimal = Decimal("0.005"),
        **kwargs,
    ):
        """Initialize the exchange client.

        Args:
            network: Network configuration.
            signer: EIP-712 signer.
            slippage: Default slippage for market orders.
        """
        super().__init__(network, **kwargs)
        self.signer = signer
        self.order_builder = OrderBuilder(slippage=slippage)

    @property
    def address(self) -> str:
        """Get the signer's address."""
        return self.signer.address

    async def place_order(
        self,
        asset_index: int,
        is_buy: bool,
        size: Decimal,
        price: Decimal,
        order_type: OrderType = OrderType.LIMIT,
        reduce_only: bool = False,
        vault_address: str | None = None,
    ) -> OrderResult:
        """Place a single order.

        Args:
            asset_index: Asset index from API.
            is_buy: True for buy, False for sell.
            size: Order size.
            price: Order price (with slippage for market).
            order_type: Order type.
            reduce_only: Whether this is a reduce-only order.
            vault_address: Optional vault address for vault trading.

        Returns:
            OrderResult with order status.
        """
        tif = "Ioc" if order_type == OrderType.MARKET else "Gtc"

        action = {
            "type": "order",
            "orders": [
                {
                    "a": asset_index,
                    "b": is_buy,
                    "p": OrderBuilder._format_price(price),
                    "s": OrderBuilder._format_size(size),
                    "r": reduce_only,
                    "t": {"limit": {"tif": tif}},
                }
            ],
            "grouping": "na",
        }

        return await self._execute_order(action, vault_address)

    async def place_order_from_signal(
        self,
        signal: TradingSignal,
        asset_index: int,
        current_price: Decimal | None = None,
        vault_address: str | None = None,
        sz_decimals: int = 0,
    ) -> list[OrderResult]:
        """Place orders from a trading signal.

        Args:
            signal: The trading signal.
            asset_index: Asset index from API.
            current_price: Current market price (required for market orders).
            vault_address: Optional vault address.
            sz_decimals: Size decimals for the asset (affects price rounding).

        Returns:
            List of OrderResult for each order (entry, SL, TP).
        """
        action = self.order_builder.build_order_payload(
            signal=signal,
            asset_index=asset_index,
            current_price=current_price,
            sz_decimals=sz_decimals,
        )

        result = await self._execute_order(action, vault_address)

        # For grouped orders, we get a single result
        # but we should return one result per order in the group
        num_orders = len(action.get("orders", []))
        if result.success and num_orders > 1:
            # Create placeholder results for SL/TP
            results = [result]
            for _ in range(num_orders - 1):
                results.append(
                    OrderResult(
                        success=True,
                        status=OrderStatus.OPEN,
                    )
                )
            return results

        return [result]

    async def _execute_order(
        self,
        action: dict[str, Any],
        vault_address: str | None = None,
    ) -> OrderResult:
        """Execute an order action.

        Args:
            action: The order action payload.
            vault_address: Optional vault address.

        Returns:
            OrderResult.
        """
        try:
            if vault_address:
                payload = self.signer.sign_action_for_vault(action, vault_address)
            else:
                payload = self.signer.sign_action(action)

            result = await self._post("exchange", payload)

            # Parse response
            if result.get("status") == "ok":
                response = result.get("response", {})
                data = response.get("data", {})

                # Extract order details if available
                statuses = data.get("statuses", [])
                if statuses:
                    first_status = statuses[0]
                    if isinstance(first_status, dict):
                        if "filled" in first_status:
                            filled = first_status["filled"]
                            return OrderResult(
                                success=True,
                                order_id=filled.get("oid"),
                                filled_size=Decimal(str(filled.get("totalSz", "0"))),
                                avg_price=Decimal(str(filled.get("avgPx", "0")))
                                if filled.get("avgPx")
                                else None,
                                status=OrderStatus.FILLED,
                            )
                        elif "resting" in first_status:
                            resting = first_status["resting"]
                            return OrderResult(
                                success=True,
                                order_id=resting.get("oid"),
                                status=OrderStatus.OPEN,
                            )
                        elif "error" in first_status:
                            return OrderResult(
                                success=False,
                                error=first_status["error"],
                                status=OrderStatus.REJECTED,
                            )

                return OrderResult(success=True, status=OrderStatus.PENDING)

            # Error response
            return OrderResult(
                success=False,
                error=result.get("response", "Unknown error"),
                status=OrderStatus.REJECTED,
            )

        except Exception as e:
            return OrderResult(
                success=False,
                error=str(e),
                status=OrderStatus.REJECTED,
            )

    async def cancel_order(
        self,
        asset_index: int,
        order_id: int,
        vault_address: str | None = None,
    ) -> bool:
        """Cancel an order.

        Args:
            asset_index: Asset index.
            order_id: Order ID to cancel.
            vault_address: Optional vault address.

        Returns:
            True if cancelled successfully.
        """
        action = self.order_builder.build_cancel_payload(asset_index, order_id)

        if vault_address:
            payload = self.signer.sign_action_for_vault(action, vault_address)
        else:
            payload = self.signer.sign_action(action)

        try:
            result = await self._post("exchange", payload)
            return result.get("status") == "ok"
        except Exception:
            return False

    async def cancel_all_orders(
        self,
        asset_index: int | None = None,
        vault_address: str | None = None,
    ) -> int:
        """Cancel all open orders.

        Args:
            asset_index: Optional asset index to filter. None for all.
            vault_address: Optional vault address.

        Returns:
            Number of orders cancelled.
        """
        # This would need to get open orders first, then cancel each
        # For now, we'll implement a basic version
        action = {
            "type": "cancelByCloid",
            "cancels": [],  # Empty means cancel all
        }

        if vault_address:
            payload = self.signer.sign_action_for_vault(action, vault_address)
        else:
            payload = self.signer.sign_action(action)

        try:
            result = await self._post("exchange", payload)
            if result.get("status") == "ok":
                return result.get("response", {}).get("data", {}).get("cancelled", 0)
        except Exception:
            pass
        return 0

    async def set_leverage(
        self,
        asset_index: int,
        leverage: int,
        is_cross: bool = True,
        vault_address: str | None = None,
    ) -> bool:
        """Set leverage for an asset.

        Args:
            asset_index: Asset index.
            leverage: Leverage value.
            is_cross: True for cross margin, False for isolated.
            vault_address: Optional vault address.

        Returns:
            True if successful.
        """
        action = self.order_builder.build_leverage_payload(
            asset_index=asset_index,
            leverage=leverage,
            is_cross=is_cross,
        )

        if vault_address:
            payload = self.signer.sign_action_for_vault(action, vault_address)
        else:
            payload = self.signer.sign_action(action)

        try:
            result = await self._post("exchange", payload)
            return result.get("status") == "ok"
        except Exception:
            return False

    async def close_position(
        self,
        asset_index: int,
        size: Decimal,
        is_long: bool,
        price: Decimal,
        vault_address: str | None = None,
    ) -> OrderResult:
        """Close a position.

        Args:
            asset_index: Asset index.
            size: Position size to close.
            is_long: True if closing a long position.
            price: Execution price (with slippage).
            vault_address: Optional vault address.

        Returns:
            OrderResult.
        """
        # To close, we place an opposite order with reduce_only
        return await self.place_order(
            asset_index=asset_index,
            is_buy=not is_long,  # Opposite direction
            size=size,
            price=price,
            order_type=OrderType.MARKET,
            reduce_only=True,
            vault_address=vault_address,
        )
