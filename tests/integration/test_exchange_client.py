"""Integration tests for Exchange client."""

from decimal import Decimal

import httpx
import pytest
import respx

from hyperhandler.client import ExchangeClient
from hyperhandler.models import OrderStatus, OrderType, TradingSignal, OrderSide
from hyperhandler.signer import Signer


TEST_PRIVATE_KEY = "0x" + "a" * 64


@pytest.fixture
def signer():
    """Create test signer."""
    return Signer(TEST_PRIVATE_KEY)


@pytest.mark.integration
class TestExchangeClient:
    """Integration tests for ExchangeClient."""

    @pytest.mark.asyncio
    async def test_place_order_success(self, mock_api, testnet_config, signer, mock_order_success):
        """I-EXC-01: place_order success."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json=mock_order_success))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.place_order(
                asset_index=0,
                is_buy=True,
                size=Decimal("0.1"),
                price=Decimal("67500"),
                order_type=OrderType.LIMIT,
            )

        assert result.success is True
        assert result.order_id == 999999
        assert result.status == OrderStatus.FILLED

    @pytest.mark.asyncio
    async def test_place_order_insufficient_margin(self, mock_api, testnet_config, signer, mock_order_error):
        """I-EXC-02: place_order insufficient margin."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json=mock_order_error))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.place_order(
                asset_index=0,
                is_buy=True,
                size=Decimal("100"),
                price=Decimal("67500"),
            )

        assert result.success is False
        assert "margin" in result.error.lower()

    @pytest.mark.asyncio
    async def test_place_order_resting(self, mock_api, testnet_config, signer, mock_order_resting):
        """I-EXC-03: place_order creates resting order."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json=mock_order_resting))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.place_order(
                asset_index=0,
                is_buy=True,
                size=Decimal("0.1"),
                price=Decimal("65000"),  # Below market
            )

        assert result.success is True
        assert result.order_id == 888888
        assert result.status == OrderStatus.OPEN

    @pytest.mark.asyncio
    async def test_place_order_from_signal(self, mock_api, testnet_config, signer, mock_order_success):
        """I-EXC-04: place_order_from_signal."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json=mock_order_success))

        signal = TradingSignal(
            pair="BTC",
            side=OrderSide.LONG,
            order_type=OrderType.LIMIT,
            entry_price=Decimal("67500"),
            size=Decimal("0.1"),
            stop_loss=Decimal("66000"),
            take_profit=Decimal("70000"),
        )

        async with ExchangeClient(testnet_config, signer) as client:
            results = await client.place_order_from_signal(
                signal=signal,
                asset_index=0,
            )

        assert len(results) == 3  # Entry + SL + TP
        assert results[0].success is True

    @pytest.mark.asyncio
    async def test_cancel_order_success(self, mock_api, testnet_config, signer):
        """I-EXC-05: cancel_order success."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.cancel_order(asset_index=0, order_id=123456)

        assert result is True

    @pytest.mark.asyncio
    async def test_cancel_order_not_found(self, mock_api, testnet_config, signer):
        """I-EXC-06: cancel_order not found."""
        mock_api.post("/exchange").mock(
            return_value=httpx.Response(200, json={"status": "err", "response": "Order not found"})
        )

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.cancel_order(asset_index=0, order_id=999999)

        assert result is False

    @pytest.mark.asyncio
    async def test_set_leverage_success(self, mock_api, testnet_config, signer):
        """I-EXC-07: set_leverage success."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.set_leverage(asset_index=0, leverage=10)

        assert result is True

    @pytest.mark.asyncio
    async def test_set_leverage_invalid(self, mock_api, testnet_config, signer):
        """I-EXC-08: set_leverage invalid value."""
        mock_api.post("/exchange").mock(
            return_value=httpx.Response(200, json={"status": "err", "response": "Invalid leverage"})
        )

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.set_leverage(asset_index=0, leverage=100)

        assert result is False

    @pytest.mark.asyncio
    async def test_request_includes_signature(self, mock_api, testnet_config, signer):
        """I-EXC-09: Request includes signature."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        async with ExchangeClient(testnet_config, signer) as client:
            await client.set_leverage(asset_index=0, leverage=5)

        # Check the request
        call = mock_api.calls[0]
        request_json = call.request.content.decode()
        assert "signature" in request_json
        assert "nonce" in request_json

    @pytest.mark.asyncio
    async def test_vault_address_in_request(self, mock_api, testnet_config, signer):
        """I-EXC-11: vaultAddress included when trading for vault."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        vault = "0x1234567890123456789012345678901234567890"

        async with ExchangeClient(testnet_config, signer) as client:
            await client.set_leverage(asset_index=0, leverage=5, vault_address=vault)

        call = mock_api.calls[0]
        request_json = call.request.content.decode()
        assert vault in request_json

    @pytest.mark.asyncio
    async def test_close_position(self, mock_api, testnet_config, signer, mock_order_success):
        """close_position works correctly."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json=mock_order_success))

        async with ExchangeClient(testnet_config, signer) as client:
            result = await client.close_position(
                asset_index=0,
                size=Decimal("0.1"),
                is_long=True,
                price=Decimal("67500"),
            )

        assert result.success is True
