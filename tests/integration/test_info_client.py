"""Integration tests for Info client."""

from decimal import Decimal

import httpx
import pytest
import respx

from hyperhandler.client import AssetNotFoundError, InfoClient


@pytest.mark.integration
class TestInfoClient:
    """Integration tests for InfoClient."""

    @pytest.mark.asyncio
    async def test_get_meta_success(self, mock_api, testnet_config, mock_meta_response):
        """I-INF-01: get_meta success."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_meta_response))

        async with InfoClient(testnet_config) as client:
            result = await client.get_meta()

        assert "universe" in result
        assert len(result["universe"]) == 3
        assert result["universe"][0]["name"] == "BTC"

    @pytest.mark.asyncio
    async def test_get_meta_empty(self, mock_api, testnet_config):
        """I-INF-02: get_meta empty response."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json={"universe": []}))

        async with InfoClient(testnet_config) as client:
            result = await client.get_meta()

        assert result["universe"] == []

    @pytest.mark.asyncio
    async def test_get_account_state_success(self, mock_api, testnet_config, mock_account_state):
        """I-INF-03: get_account_state success."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_account_state))

        async with InfoClient(testnet_config) as client:
            result = await client.get_account_state("0x123")

        assert "marginSummary" in result
        assert result["marginSummary"]["accountValue"] == "10000.0"

    @pytest.mark.asyncio
    async def test_get_all_mids_success(self, mock_api, testnet_config, mock_mids_response):
        """I-INF-05: get_all_mids success."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_mids_response))

        async with InfoClient(testnet_config) as client:
            result = await client.get_all_mids()

        assert result["BTC"] == Decimal("67500.5")
        assert result["ETH"] == Decimal("3450.25")

    @pytest.mark.asyncio
    async def test_get_open_orders_empty(self, mock_api, testnet_config):
        """I-INF-06: get_open_orders empty."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=[]))

        async with InfoClient(testnet_config) as client:
            result = await client.get_open_orders("0x123")

        assert result == []

    @pytest.mark.asyncio
    async def test_get_open_orders_with_data(self, mock_api, testnet_config, mock_open_orders):
        """I-INF-07: get_open_orders with orders."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_open_orders))

        async with InfoClient(testnet_config) as client:
            result = await client.get_open_orders("0x123")

        assert len(result) == 2
        assert result[0].coin == "BTC"
        assert result[0].order_id == 123456
        assert result[0].is_buy is True

    @pytest.mark.asyncio
    async def test_get_asset_index_existing(self, mock_api, testnet_config, mock_meta_response):
        """I-INF-08: get_asset_index for existing asset."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_meta_response))

        async with InfoClient(testnet_config) as client:
            index = await client.get_asset_index("BTC")

        assert index == 0

    @pytest.mark.asyncio
    async def test_get_asset_index_nonexistent(self, mock_api, testnet_config, mock_meta_response):
        """I-INF-09: get_asset_index for non-existent asset."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_meta_response))

        async with InfoClient(testnet_config) as client:
            with pytest.raises(AssetNotFoundError):
                await client.get_asset_index("XYZ")

    @pytest.mark.asyncio
    async def test_get_positions(self, mock_api, testnet_config, mock_account_state):
        """get_positions returns parsed positions."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_account_state))

        async with InfoClient(testnet_config) as client:
            positions = await client.get_positions("0x123")

        assert len(positions) == 1
        assert positions[0].coin == "BTC"
        assert positions[0].size == Decimal("0.1")
        assert positions[0].is_long is True

    @pytest.mark.asyncio
    async def test_get_mid_price(self, mock_api, testnet_config, mock_mids_response):
        """get_mid_price returns correct price."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_mids_response))

        async with InfoClient(testnet_config) as client:
            price = await client.get_mid_price("ETH")

        assert price == Decimal("3450.25")

    @pytest.mark.asyncio
    async def test_get_mid_price_not_found(self, mock_api, testnet_config, mock_mids_response):
        """get_mid_price raises for unknown asset."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_mids_response))

        async with InfoClient(testnet_config) as client:
            with pytest.raises(AssetNotFoundError):
                await client.get_mid_price("XYZ")

    @pytest.mark.asyncio
    async def test_asset_index_caching(self, mock_api, testnet_config, mock_meta_response):
        """Asset index is cached after first call."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_meta_response))

        async with InfoClient(testnet_config) as client:
            # First call fetches meta
            await client.get_asset_index("BTC")
            # Second call uses cache
            index = await client.get_asset_index("ETH")

        assert index == 1
        # Should only have one call to API
        assert len(mock_api.calls) == 1
