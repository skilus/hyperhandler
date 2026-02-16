"""End-to-end tests for complete risk lifecycle.

Group H from SPEC-005 - Critical tests.
"""

from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest

from hyperhandler.client.info import InfoClient
from hyperhandler.models import TradingSignal
from hyperhandler.models.risk import (
    RejectReason,
    RiskLevel,
    RiskMode,
    RiskReject,
    TradeOrder,
    TradeResult,
)
from hyperhandler.risk import RiskManager


@pytest.mark.integration
class TestRiskLifecycleE2E:
    """End-to-end test for complete risk lifecycle.

    This is the most important integration test.
    If this passes, the risk system works as designed.
    """

    @pytest.mark.asyncio
    async def test_circuit_breaker_full_cycle(
        self, mock_api, testnet_config, info_request_router, memory_storage, test_address
    ):
        """I-RISK-H01: Complete CB lifecycle: losses → block → reset → allow.

        Scenario:
        1. Execute 3 losing trades (record in storage)
        2. 4th trade attempt → RiskReject with CIRCUIT_BREAKER_HARD or DAILY_LOSS_LIMIT
        3. Wait for CB to reset (or use fresh history)
        4. Trade allowed again
        """
        mock_api.post("/info").mock(side_effect=info_request_router)

        now = datetime.now(timezone.utc)

        # Phase 1: Record 5 consecutive losses (triggers HARD CB in MEDIUM profile)
        trade_history = []
        for i in range(5):
            result = TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("67000"),
                exit_price=Decimal("66000"),
                size=Decimal("0.1"),
                pnl=Decimal("-100"),  # Loss
                fees=Decimal("5"),
                funding_paid=Decimal("0"),
                opened_at=now - timedelta(hours=i + 2),
                closed_at=now - timedelta(hours=i + 1),
            )
            trade_history.append(result)
            # Also save to storage
            memory_storage.save_trade_result(result, "testnet")

        signal = TradingSignal(
            pair="BTC",
            side="long",
            order_type="market",
            size=Decimal("0.01"),
            leverage=5,
            stop_loss=Decimal("66500"),
        )
        manager = RiskManager(risk_level=RiskLevel.MEDIUM, risk_mode=RiskMode.MANUAL)

        # Phase 2: Verify rejection
        async with InfoClient(testnet_config) as client:
            result = await manager.evaluate_signal(
                signal,
                client,
                test_address,
                trade_history=trade_history,
                storage=memory_storage,
                network="testnet",
            )

        assert isinstance(result, RiskReject)
        assert result.reason in (
            RejectReason.CIRCUIT_BREAKER_HARD,
            RejectReason.DAILY_LOSS_LIMIT,
        )

        # Phase 3: With no loss history → should be allowed
        async with InfoClient(testnet_config) as client:
            result = await manager.evaluate_signal(
                signal,
                client,
                test_address,
                trade_history=[],  # Fresh start
                storage=memory_storage,
                network="testnet",
            )

        assert isinstance(result, TradeOrder)

    @pytest.mark.asyncio
    async def test_concurrent_exec_no_race_condition(
        self, mock_api, testnet_config, info_request_router, memory_storage, test_address
    ):
        """I-RISK-H02: Concurrent exec calls → no race conditions.

        Scenario:
        - 2 exec calls launched almost simultaneously (asyncio.gather)
        - Both for different coins (BTC, ETH)
        - Both should succeed
        """
        import asyncio

        mock_api.post("/info").mock(side_effect=info_request_router)

        manager = RiskManager(risk_level=RiskLevel.HIGH, risk_mode=RiskMode.MANUAL)

        signals = [
            TradingSignal(
                pair="BTC",
                side="long",
                order_type="market",
                size=Decimal("0.005"),
                leverage=5,
                stop_loss=Decimal("66500"),
            ),
            TradingSignal(
                pair="ETH",
                side="short",
                order_type="market",
                size=Decimal("0.5"),
                leverage=5,
                stop_loss=Decimal("3600"),
            ),
        ]

        async def evaluate_signal(signal):
            async with InfoClient(testnet_config) as client:
                return await manager.evaluate_signal(
                    signal,
                    client,
                    test_address,
                    trade_history=[],
                    storage=memory_storage,
                    network="testnet",
                )

        # Execute concurrently
        results = await asyncio.gather(
            evaluate_signal(signals[0]),
            evaluate_signal(signals[1]),
        )

        # Both should succeed
        assert len(results) == 2
        assert all(isinstance(r, TradeOrder) for r in results)

        # Verify storage has 2 decisions
        with memory_storage._connection() as conn:
            cursor = conn.execute(
                "SELECT COUNT(*) FROM risk_decisions WHERE network = ?",
                ("testnet",),
            )
            count = cursor.fetchone()[0]

        assert count == 2
