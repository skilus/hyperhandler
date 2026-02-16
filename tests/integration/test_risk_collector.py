"""Integration tests for TradeResultCollector.

Group D from SPEC-005.
"""

from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest

from hyperhandler.client.info import InfoClient
from hyperhandler.risk.collector import TradeResultCollector


@pytest.mark.integration
class TestTradeResultCollectorIntegration:
    """Integration tests for TradeResultCollector."""

    @pytest.mark.asyncio
    async def test_collect_closing_fills(
        self, mock_api, testnet_config, memory_storage, test_address
    ):
        """I-RISK-D01: Closing fills saved to storage.

        Setup:
        - userFills returns fill with closedPnl

        Assertions:
        1. TradeResult saved to storage
        2. PnL matches closedPnl from fill
        3. Entry price from startPosition
        """
        import httpx

        mock_fills = [
            {
                "coin": "BTC",
                "oid": 123,
                "side": "B",
                "px": "67500.0",
                "sz": "0.1",
                "time": 1700000000000,
                "closedPnl": "50.0",
                "fee": "3.0",
                "startPosition": {
                    "entryPx": "67000.0",
                    "time": 1699900000000,
                },
            }
        ]

        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_fills))

        collector = TradeResultCollector(memory_storage, "testnet")

        async with InfoClient(testnet_config) as client:
            results = await collector.collect_from_fills(client, test_address)

        assert len(results) == 1
        assert results[0].coin == "BTC"
        assert results[0].pnl == Decimal("50.0")
        assert results[0].entry_price == Decimal("67000.0")
        assert results[0].exit_price == Decimal("67500.0")
        assert results[0].size == Decimal("0.1")

        # Verify saved in storage
        saved = memory_storage.get_recent_trade_results(network="testnet", limit=10)
        assert len(saved) == 1
        assert saved[0].pnl == Decimal("50.0")

    @pytest.mark.asyncio
    async def test_skip_non_closing_fills(
        self, mock_api, testnet_config, memory_storage, test_address
    ):
        """I-RISK-D02: Fills without closedPnl skipped.

        Setup:
        - userFills returns 2 fills:
          - Fill 1: has closedPnl (closing)
          - Fill 2: no closedPnl (opening)

        Assertions:
        1. Only closing fill saved
        2. Storage has 1 record
        """
        import httpx

        mock_fills = [
            {
                "coin": "BTC",
                "oid": 123,
                "side": "B",
                "px": "67500.0",
                "sz": "0.1",
                "time": 1700000000000,
                "closedPnl": "50.0",
                "fee": "3.0",
                "startPosition": {
                    "entryPx": "67000.0",
                    "time": 1699900000000,
                },
            },
            {
                "coin": "ETH",
                "oid": 124,
                "side": "A",
                "px": "3500.0",
                "sz": "1.0",
                "time": 1700001000000,
                # No closedPnl - opening fill
            },
        ]

        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_fills))

        collector = TradeResultCollector(memory_storage, "testnet")

        async with InfoClient(testnet_config) as client:
            results = await collector.collect_from_fills(client, test_address)

        # Only BTC fill should be collected (has closedPnl)
        assert len(results) == 1
        assert results[0].coin == "BTC"

        # Storage has only 1 record
        saved = memory_storage.get_recent_trade_results(network="testnet", limit=10)
        assert len(saved) == 1

    @pytest.mark.asyncio
    async def test_deduplication(
        self, mock_api, testnet_config, memory_storage, test_address
    ):
        """I-RISK-D03: Same fill not recorded twice.

        Dedup key: fill_id = oid_time
        Test: call collect() twice with same fills → storage has 1 record.
        """
        import httpx

        mock_fills = [
            {
                "coin": "BTC",
                "oid": 123,
                "side": "B",
                "px": "67500.0",
                "sz": "0.1",
                "time": 1700000000000,
                "closedPnl": "50.0",
                "fee": "3.0",
                "startPosition": {
                    "entryPx": "67000.0",
                    "time": 1699900000000,
                },
            }
        ]

        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_fills))

        collector = TradeResultCollector(memory_storage, "testnet")

        # First call
        async with InfoClient(testnet_config) as client:
            results1 = await collector.collect_from_fills(client, test_address)

        # Second call with same fills
        async with InfoClient(testnet_config) as client:
            results2 = await collector.collect_from_fills(client, test_address)

        # First call should return 1 result
        assert len(results1) == 1

        # Second call should return 0 (deduplicated)
        assert len(results2) == 0

        # Storage should have exactly 1 record
        saved = memory_storage.get_recent_trade_results(network="testnet", limit=10)
        assert len(saved) == 1

    @pytest.mark.asyncio
    async def test_since_timestamp_filter(
        self, mock_api, testnet_config, memory_storage, test_address
    ):
        """I-RISK-D04: Fills before cutoff skipped.

        Setup:
        - userFills returns 2 fills:
          - Fill 1: timestamp before since_timestamp
          - Fill 2: timestamp after since_timestamp

        Assertions:
        1. Only fill after cutoff collected
        """
        import httpx

        # Fill 1: old fill (before cutoff)
        # Fill 2: recent fill (after cutoff)
        old_time = 1699900000000  # ~2023-11-13
        recent_time = 1700100000000  # ~2023-11-16
        cutoff_time = datetime.fromtimestamp(1700000000000 / 1000, tz=timezone.utc)

        mock_fills = [
            {
                "coin": "BTC",
                "oid": 100,
                "side": "B",
                "px": "66000.0",
                "sz": "0.1",
                "time": old_time,
                "closedPnl": "30.0",
                "fee": "2.0",
                "startPosition": {
                    "entryPx": "65700.0",
                    "time": old_time - 100000,
                },
            },
            {
                "coin": "ETH",
                "oid": 101,
                "side": "A",
                "px": "3600.0",
                "sz": "0.5",
                "time": recent_time,
                "closedPnl": "-20.0",
                "fee": "1.5",
                "startPosition": {
                    "entryPx": "3650.0",
                    "time": recent_time - 100000,
                },
            },
        ]

        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_fills))

        collector = TradeResultCollector(memory_storage, "testnet")

        async with InfoClient(testnet_config) as client:
            results = await collector.collect_from_fills(
                client, test_address, since_timestamp=cutoff_time
            )

        # Only ETH fill should be collected (after cutoff)
        assert len(results) == 1
        assert results[0].coin == "ETH"
        assert results[0].pnl == Decimal("-20.0")

    @pytest.mark.asyncio
    @pytest.mark.skip(reason="Requires SPEC-003 amendment: partial fill aggregation")
    async def test_partial_close_then_final_close(
        self, mock_api, testnet_config, memory_storage, test_address
    ):
        """I-RISK-D05: Partial + final close → one trade_result.

        ⚠️ DEFERRED: Current TradeResultCollector works per-fill.
        This test requires additional spec for partial fill aggregation.
        See: SPEC-003 amendment needed.

        Setup:
        - userFills returns:
          - Fill 1: partial close (closedPnl=25, remaining position)
          - Fill 2: final close (closedPnl=25, position fully closed)

        Assertions:
        1. Only ONE trade_result created (not two)
        2. trade_result.pnl = sum of both closedPnl (50)
        3. trade_result.closed_at = timestamp of final fill
        4. Circuit breaker updated ONCE (not twice)

        Note: Requires grouping fills by (coin, startPosition.time).
        """
        pass
