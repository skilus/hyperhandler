"""Info API client for Hyperliquid."""

from decimal import Decimal

from hyperhandler.client.base import AssetNotFoundError, BaseClient
from hyperhandler.config import NetworkConfig
from hyperhandler.models import OpenOrder, Position


class InfoClient(BaseClient):
    """Client for Hyperliquid Info API (public data)."""

    def __init__(self, network: NetworkConfig, **kwargs):
        super().__init__(network, **kwargs)
        self._meta_cache: dict | None = None
        self._asset_index_cache: dict[str, int] = {}

    async def get_meta(self) -> dict:
        """Get market metadata.

        Returns:
            Dict with universe (list of assets) and other metadata.
        """
        result = await self._post("info", {"type": "meta"})
        self._meta_cache = result

        # Build asset index cache
        if "universe" in result:
            for i, asset in enumerate(result["universe"]):
                self._asset_index_cache[asset["name"]] = i

        return result

    async def get_asset_index(self, symbol: str) -> int:
        """Get the asset index for a symbol.

        Args:
            symbol: Asset symbol (e.g., "BTC", "ETH").

        Returns:
            Asset index for use in orders.

        Raises:
            AssetNotFoundError: If the symbol is not found.
        """
        # Use cache if available
        if symbol in self._asset_index_cache:
            return self._asset_index_cache[symbol]

        # Fetch metadata if not cached
        if self._meta_cache is None:
            await self.get_meta()

        if symbol not in self._asset_index_cache:
            raise AssetNotFoundError(f"Asset not found: {symbol}")

        return self._asset_index_cache[symbol]

    async def get_asset_info(self, symbol: str) -> dict:
        """Get asset information.

        Args:
            symbol: Asset symbol.

        Returns:
            Asset info dict with szDecimals, maxLeverage, etc.
        """
        if self._meta_cache is None:
            await self.get_meta()

        for asset in self._meta_cache.get("universe", []):
            if asset["name"] == symbol:
                return asset

        raise AssetNotFoundError(f"Asset not found: {symbol}")

    async def get_all_mids(self) -> dict[str, Decimal]:
        """Get current mid prices for all assets.

        Returns:
            Dict mapping symbol to mid price.
        """
        result = await self._post("info", {"type": "allMids"})
        return {k: Decimal(str(v)) for k, v in result.items()}

    async def get_mid_price(self, symbol: str) -> Decimal:
        """Get current mid price for a symbol.

        Args:
            symbol: Asset symbol.

        Returns:
            Current mid price.

        Raises:
            AssetNotFoundError: If the symbol is not found.
        """
        mids = await self.get_all_mids()
        if symbol not in mids:
            raise AssetNotFoundError(f"Price not found for: {symbol}")
        return mids[symbol]

    async def get_account_state(self, address: str) -> dict:
        """Get account state including margin and positions.

        Args:
            address: Ethereum address.

        Returns:
            Account state dict.
        """
        result = await self._post(
            "info",
            {"type": "clearinghouseState", "user": address},
        )
        return result

    async def get_open_orders(self, address: str) -> list[OpenOrder]:
        """Get open orders for an address.

        Args:
            address: Ethereum address.

        Returns:
            List of open orders.
        """
        result = await self._post(
            "info",
            {"type": "openOrders", "user": address},
        )

        orders = []
        for order_data in result:
            orders.append(
                OpenOrder(
                    coin=order_data["coin"],
                    order_id=order_data["oid"],
                    side=order_data["side"],
                    price=Decimal(str(order_data["limitPx"])),
                    size=Decimal(str(order_data["sz"])),
                    timestamp=order_data["timestamp"],
                )
            )
        return orders

    async def get_positions(self, address: str) -> list[Position]:
        """Get open positions for an address.

        Args:
            address: Ethereum address.

        Returns:
            List of positions.
        """
        state = await self.get_account_state(address)
        positions = []

        for asset_pos in state.get("assetPositions", []):
            pos = asset_pos.get("position", {})
            if not pos:
                continue

            # Skip zero positions
            size = Decimal(str(pos.get("szi", "0")))
            if size == 0:
                continue

            leverage_info = pos.get("leverage", {})

            positions.append(
                Position(
                    coin=pos["coin"],
                    size=size,
                    entry_price=Decimal(str(pos.get("entryPx", "0"))),
                    position_value=Decimal(str(pos.get("positionValue", "0"))),
                    unrealized_pnl=Decimal(str(pos.get("unrealizedPnl", "0"))),
                    leverage=int(leverage_info.get("value", 1)),
                    leverage_type=leverage_info.get("type", "cross"),
                    liquidation_price=self.to_decimal(pos.get("liquidationPx")),
                )
            )

        return positions

    async def get_margin_summary(self, address: str) -> dict:
        """Get margin summary for an address.

        Args:
            address: Ethereum address.

        Returns:
            Margin summary dict.
        """
        state = await self.get_account_state(address)
        return state.get("marginSummary", {})

    async def get_user_fills(self, address: str, limit: int = 100) -> list[dict]:
        """Get recent fills for an address.

        Args:
            address: Ethereum address.
            limit: Maximum number of fills to return.

        Returns:
            List of fill records.
        """
        result = await self._post(
            "info",
            {"type": "userFills", "user": address},
        )
        return result[:limit] if isinstance(result, list) else []
