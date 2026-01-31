"""Integration tests for Vault client."""

from decimal import Decimal

import httpx
import pytest
import respx

from hyperhandler.client import LockupPeriodError, VaultClient, VaultNotFoundError
from hyperhandler.signer import Signer


TEST_PRIVATE_KEY = "0x" + "a" * 64


@pytest.fixture
def signer():
    """Create test signer."""
    return Signer(TEST_PRIVATE_KEY)


@pytest.fixture
def mock_vaults_response():
    """Mock vaults list response."""
    return [
        {
            "vault": "0xvault1",
            "name": "Alpha Vault",
            "leader": "0xleader1",
            "tvl": "1000000.0",
            "apr": "25.5",
            "profitShare": "10.0",
            "lockupPeriod": 86400,
            "isPublic": True,
            "followers": 100,
        },
        {
            "vault": "0xvault2",
            "name": "Beta Vault",
            "leader": "0xleader2",
            "tvl": "500000.0",
            "apr": "15.0",
            "profitShare": "20.0",
            "lockupPeriod": 43200,
            "isPublic": True,
            "followers": 50,
        },
    ]


@pytest.fixture
def mock_vault_details():
    """Mock vault details response."""
    return {
        "vault": "0xvault1",
        "name": "Alpha Vault",
        "leader": "0xleader1",
        "tvl": "1000000.0",
        "apr": "25.5",
        "profitShare": "10.0",
        "lockupPeriod": 86400,
        "isPublic": True,
        "followers": 100,
        "portfolio": {
            "accountValue": "1000000.0",
            "positions": [{"coin": "BTC", "szi": "1.0"}],
        },
        "followerState": {
            "shares": "0.05",
            "deposited": "50000.0",
            "currentValue": "55000.0",
        },
    }


@pytest.fixture
def mock_user_vault_positions():
    """Mock user vault positions response."""
    return [
        {
            "vault": "0xvault1",
            "vaultName": "Alpha Vault",
            "shares": "0.05",
            "deposited": "50000.0",
            "currentValue": "55000.0",
        }
    ]


@pytest.mark.integration
@pytest.mark.vault
class TestVaultClient:
    """Integration tests for VaultClient."""

    @pytest.mark.asyncio
    async def test_list_vaults_success(self, mock_api, testnet_config, mock_vaults_response):
        """I-VLT-01: list_vaults success."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_vaults_response))

        async with VaultClient(testnet_config) as client:
            vaults = await client.list_vaults()

        assert len(vaults) == 2
        assert vaults[0].name == "Alpha Vault"
        assert vaults[0].tvl == Decimal("1000000.0")

    @pytest.mark.asyncio
    async def test_list_vaults_with_filter(self, mock_api, testnet_config, mock_vaults_response):
        """I-VLT-02: list_vaults with min_tvl filter."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_vaults_response))

        async with VaultClient(testnet_config) as client:
            vaults = await client.list_vaults(min_tvl=Decimal("600000"))

        assert len(vaults) == 1
        assert vaults[0].name == "Alpha Vault"

    @pytest.mark.asyncio
    async def test_get_vault_details_success(self, mock_api, testnet_config, mock_vault_details):
        """I-VLT-03: get_vault_details success."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_vault_details))

        async with VaultClient(testnet_config) as client:
            details = await client.get_vault_details("0xvault1")

        assert details.info.name == "Alpha Vault"
        assert details.account_value == Decimal("1000000.0")
        assert details.follower_state is not None

    @pytest.mark.asyncio
    async def test_get_vault_details_not_found(self, mock_api, testnet_config):
        """I-VLT-04: get_vault_details not found."""
        mock_api.post("/info").mock(
            return_value=httpx.Response(200, json={"status": "err", "response": "Vault not found"})
        )

        async with VaultClient(testnet_config) as client:
            with pytest.raises(VaultNotFoundError):
                await client.get_vault_details("0xnonexistent")

    @pytest.mark.asyncio
    async def test_deposit_to_vault_success(self, mock_api, testnet_config, signer):
        """I-VLT-05: deposit_to_vault success."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        async with VaultClient(testnet_config, signer=signer) as client:
            result = await client.deposit_to_vault("0xvault1", Decimal("1000"))

        assert result is True

    @pytest.mark.asyncio
    async def test_deposit_without_signer_fails(self, mock_api, testnet_config):
        """Deposit without signer raises error."""
        async with VaultClient(testnet_config) as client:
            with pytest.raises(ValueError, match="Signer required"):
                await client.deposit_to_vault("0xvault1", Decimal("1000"))

    @pytest.mark.asyncio
    async def test_withdraw_from_vault_success(self, mock_api, testnet_config, signer):
        """I-VLT-07: withdraw_from_vault success."""
        mock_api.post("/exchange").mock(return_value=httpx.Response(200, json={"status": "ok"}))

        async with VaultClient(testnet_config, signer=signer) as client:
            result = await client.withdraw_from_vault("0xvault1", Decimal("0.5"))

        assert result is True

    @pytest.mark.asyncio
    async def test_withdraw_lockup_period_error(self, mock_api, testnet_config, signer):
        """I-VLT-08: withdraw during lockup period fails."""
        mock_api.post("/exchange").mock(
            return_value=httpx.Response(200, json={"status": "err", "response": "Lockup period active"})
        )

        async with VaultClient(testnet_config, signer=signer) as client:
            with pytest.raises(LockupPeriodError):
                await client.withdraw_from_vault("0xvault1", Decimal("0.5"))

    @pytest.mark.asyncio
    async def test_get_my_vault_positions(self, mock_api, testnet_config, signer, mock_user_vault_positions):
        """I-VLT-07: get_my_vault_positions."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_user_vault_positions))

        async with VaultClient(testnet_config, signer=signer) as client:
            positions = await client.get_my_vault_positions()

        assert len(positions) == 1
        assert positions[0].vault == "0xvault1"
        assert positions[0].pnl == Decimal("5000.0")
        assert positions[0].pnl_percent == Decimal("10")

    @pytest.mark.asyncio
    async def test_list_vaults_with_apr_filter(self, mock_api, testnet_config, mock_vaults_response):
        """list_vaults with min_apr filter."""
        mock_api.post("/info").mock(return_value=httpx.Response(200, json=mock_vaults_response))

        async with VaultClient(testnet_config) as client:
            vaults = await client.list_vaults(min_apr=Decimal("20"))

        assert len(vaults) == 1
        assert vaults[0].apr >= Decimal("20")
