"""Trade result collection for circuit breaker tracking."""

from datetime import datetime, timezone
from decimal import Decimal
from typing import TYPE_CHECKING

from hyperhandler.models.order import Position
from hyperhandler.models.risk import TradeResult

if TYPE_CHECKING:
    from hyperhandler.client.info import InfoClient
    from hyperhandler.storage import Storage


class TradeResultCollector:
    """Collects closed trade results for circuit breaker tracking.

    Two collection strategies:
    1. On-close: When position is closed via CLI command
    2. Reconcile: Periodic sync from HL fills API
    """

    def __init__(self, storage: "Storage", network: str):
        self.storage = storage
        self.network = network
        self._recorded_fills: set[str] = set()

    async def collect_from_fills(
        self,
        info_client: "InfoClient",
        address: str,
        since_timestamp: datetime | None = None,
    ) -> list[TradeResult]:
        """Reconcile trade results from HL user fills.

        This matches fills to known positions and calculates PnL.
        Should be called periodically or before risk evaluation.

        Args:
            info_client: InfoClient for API calls.
            address: User's wallet address.
            since_timestamp: Only process fills after this time.

        Returns:
            List of newly recorded TradeResults.
        """
        fills = await info_client.get_user_fills(address, limit=100)

        results: list[TradeResult] = []
        for fill in fills:
            # Check if this fill closes a position
            closed_pnl = fill.get("closedPnl")
            if not closed_pnl:
                continue  # Not a closing fill

            # Generate unique fill ID
            fill_id = f"{fill.get('oid', '')}_{fill.get('time', '')}"
            if fill_id in self._recorded_fills:
                continue  # Already processed

            # Check timestamp filter
            fill_time = datetime.fromtimestamp(
                fill.get("time", 0) / 1000, tz=timezone.utc
            )
            if since_timestamp and fill_time < since_timestamp:
                continue

            # Extract entry price from startPosition if available
            start_pos = fill.get("startPosition", {})
            entry_px = start_pos.get("entryPx", fill.get("px", "0"))

            result = TradeResult(
                coin=fill.get("coin", ""),
                side="long" if fill.get("side") == "B" else "short",
                entry_price=Decimal(str(entry_px)),
                exit_price=Decimal(str(fill.get("px", "0"))),
                size=Decimal(str(fill.get("sz", "0"))),
                pnl=Decimal(str(closed_pnl)),
                fees=Decimal(str(fill.get("fee", "0"))),
                funding_paid=Decimal("0"),  # Not available in fills
                opened_at=datetime.fromtimestamp(
                    start_pos.get("time", fill.get("time", 0)) / 1000,
                    tz=timezone.utc,
                ),
                closed_at=fill_time,
            )

            # Save to storage
            result_id = self.storage.save_trade_result(result, self.network)
            result.id = result_id
            self._recorded_fills.add(fill_id)
            results.append(result)

        return results

    def record_close(
        self,
        position: Position,
        exit_price: Decimal,
        fees: Decimal,
        funding_paid: Decimal = Decimal("0"),
        signal_id: int | None = None,
    ) -> TradeResult:
        """Record a position close initiated via CLI.

        Called by ExchangeClient.close_position() or similar.

        Args:
            position: The position being closed.
            exit_price: Exit price.
            fees: Trading fees paid.
            funding_paid: Funding payments.
            signal_id: Associated signal ID if known.

        Returns:
            TradeResult with ID populated.
        """
        pnl = self._calculate_pnl(position, exit_price, fees, funding_paid)

        result = TradeResult(
            signal_id=signal_id,
            coin=position.coin,
            side="long" if position.is_long else "short",
            entry_price=position.entry_price,
            exit_price=exit_price,
            size=position.abs_size,
            pnl=pnl,
            fees=fees,
            funding_paid=funding_paid,
            opened_at=position.opened_at or datetime.now(timezone.utc),
            closed_at=datetime.now(timezone.utc),
        )

        result_id = self.storage.save_trade_result(result, self.network)
        result.id = result_id
        return result

    def get_recent_results(self, limit: int = 50) -> list[TradeResult]:
        """Get recent trade results from storage.

        Args:
            limit: Maximum results to return.

        Returns:
            List of TradeResults ordered by closed_at desc.
        """
        return self.storage.get_recent_trade_results(self.network, limit=limit)

    def _calculate_pnl(
        self,
        position: Position,
        exit_price: Decimal,
        fees: Decimal,
        funding_paid: Decimal,
    ) -> Decimal:
        """Calculate realized PnL.

        Args:
            position: Position being closed.
            exit_price: Exit price.
            fees: Trading fees.
            funding_paid: Funding payments.

        Returns:
            Net realized PnL.
        """
        if position.is_long:
            gross_pnl = (exit_price - position.entry_price) * position.abs_size
        else:
            gross_pnl = (position.entry_price - exit_price) * position.abs_size

        return gross_pnl - fees - funding_paid
