"""Vault API client for Hyperliquid."""

from decimal import Decimal
from typing import Any

from hlhandler.client.base import APIError, BaseClient
from hlhandler.config import NetworkConfig
from hlhandler.models import TradingSignal, VaultDetails, VaultInfo, VaultPosition
from hlhandler.signer import Signer


class VaultNotFoundError(APIError):
    """Vault not found."""

    pass


class LockupPeriodError(APIError):
    """Cannot withdraw during lockup period."""

    pass


class VaultClient(BaseClient):
    """Client for Hyperliquid Vault API."""

    def __init__(
        self,
        network: NetworkConfig,
        signer: Signer | None = None,
        **kwargs,
    ):
        """Initialize the vault client.

        Args:
            network: Network configuration.
            signer: Optional signer for authenticated operations.
        """
        super().__init__(network, **kwargs)
        self.signer = signer

    async def list_vaults(
        self,
        min_tvl: Decimal | None = None,
        min_apr: Decimal | None = None,
    ) -> list[VaultInfo]:
        """List public vaults.

        Args:
            min_tvl: Minimum TVL filter.
            min_apr: Minimum APR filter.

        Returns:
            List of VaultInfo.
        """
        result = await self._post("info", {"type": "vaults"})

        vaults = []
        for vault_data in result:
            vault = self._parse_vault_info(vault_data)

            # Apply filters
            if min_tvl is not None and vault.tvl < min_tvl:
                continue
            if min_apr is not None and vault.apr < min_apr:
                continue

            vaults.append(vault)

        return vaults

    async def get_vault_details(
        self,
        vault_address: str,
        user_address: str | None = None,
    ) -> VaultDetails:
        """Get detailed information about a vault.

        Args:
            vault_address: The vault address.
            user_address: Optional user address to get follower state.

        Returns:
            VaultDetails with portfolio and optionally follower state.

        Raises:
            VaultNotFoundError: If vault doesn't exist.
        """
        request: dict[str, Any] = {"type": "vaultDetails", "vault": vault_address}
        if user_address:
            request["user"] = user_address

        try:
            result = await self._post("info", request)
        except APIError as e:
            if "not found" in str(e).lower():
                raise VaultNotFoundError(f"Vault not found: {vault_address}")
            raise

        return self._parse_vault_details(result)

    async def deposit_to_vault(
        self,
        vault_address: str,
        amount_usd: Decimal,
    ) -> bool:
        """Deposit USD to a vault.

        Args:
            vault_address: The vault address.
            amount_usd: Amount to deposit in USD.

        Returns:
            True if successful.

        Raises:
            ValueError: If signer not configured.
        """
        if self.signer is None:
            raise ValueError("Signer required for vault operations")

        action = {
            "type": "vaultDeposit",
            "vault": vault_address,
            "usd": str(amount_usd),
        }

        payload = self.signer.sign_action(action)

        try:
            result = await self._post("exchange", payload)
            return result.get("status") == "ok"
        except Exception:
            return False

    async def withdraw_from_vault(
        self,
        vault_address: str,
        shares: Decimal,
    ) -> bool:
        """Withdraw from a vault by shares.

        Args:
            vault_address: The vault address.
            shares: Amount of shares to withdraw (0-1 for percentage).

        Returns:
            True if successful.

        Raises:
            ValueError: If signer not configured.
            LockupPeriodError: If in lockup period.
        """
        if self.signer is None:
            raise ValueError("Signer required for vault operations")

        action = {
            "type": "vaultWithdraw",
            "vault": vault_address,
            "shares": str(shares),
        }

        payload = self.signer.sign_action(action)

        try:
            result = await self._post("exchange", payload)
            return result.get("status") == "ok"
        except APIError as e:
            if "lockup" in str(e).lower():
                raise LockupPeriodError(str(e))
            return False
        except Exception:
            return False

    async def get_my_vault_positions(
        self,
        user_address: str | None = None,
    ) -> list[VaultPosition]:
        """Get user's positions in all vaults.

        Args:
            user_address: User address. Uses signer address if not provided.

        Returns:
            List of VaultPosition.
        """
        if user_address is None:
            if self.signer is None:
                raise ValueError("User address or signer required")
            user_address = self.signer.address

        result = await self._post(
            "info",
            {"type": "userVaultEquities", "user": user_address},
        )

        positions = []
        for pos_data in result:
            positions.append(
                VaultPosition(
                    vault=pos_data.get("vault", ""),
                    vault_name=pos_data.get("vaultName", ""),
                    shares=Decimal(str(pos_data.get("shares", "0"))),
                    deposited=Decimal(str(pos_data.get("deposited", "0"))),
                    current_value=Decimal(str(pos_data.get("currentValue", "0"))),
                )
            )

        return positions

    async def create_vault(
        self,
        name: str,
        description: str = "",
        is_public: bool = True,
        max_capacity: Decimal | None = None,
        lockup_period: int = 86400,  # 24 hours
        profit_share: Decimal = Decimal("10"),
    ) -> str:
        """Create a new vault.

        Args:
            name: Vault name.
            description: Vault description.
            is_public: Whether the vault is public.
            max_capacity: Maximum capacity in USD.
            lockup_period: Lockup period in seconds.
            profit_share: Leader's profit share percentage.

        Returns:
            New vault address.

        Raises:
            ValueError: If signer not configured.
        """
        if self.signer is None:
            raise ValueError("Signer required for vault operations")

        action: dict[str, Any] = {
            "type": "createVault",
            "name": name,
            "description": description,
            "isPublic": is_public,
            "lockupPeriod": lockup_period,
            "profitShare": str(profit_share),
        }

        if max_capacity is not None:
            action["maxCapacity"] = str(max_capacity)

        payload = self.signer.sign_action(action)
        result = await self._post("exchange", payload)

        if result.get("status") == "ok":
            return result.get("response", {}).get("vault", "")

        raise APIError(result.get("response", "Failed to create vault"))

    def _parse_vault_info(self, data: dict) -> VaultInfo:
        """Parse vault info from API response."""
        return VaultInfo(
            address=data.get("vault", ""),
            name=data.get("name", ""),
            leader=data.get("leader", ""),
            tvl=Decimal(str(data.get("tvl", "0"))),
            apr=Decimal(str(data.get("apr", "0"))),
            profit_share=Decimal(str(data.get("profitShare", "0"))),
            lockup_period=int(data.get("lockupPeriod", 0)),
            is_public=data.get("isPublic", True),
            followers=int(data.get("followers", 0)),
            max_capacity=self.to_decimal(data.get("maxCapacity")),
        )

    def _parse_vault_details(self, data: dict) -> VaultDetails:
        """Parse vault details from API response."""
        info = self._parse_vault_info(data)

        portfolio = data.get("portfolio", {})
        account_value = Decimal(str(portfolio.get("accountValue", "0")))
        positions = portfolio.get("positions", [])

        follower_state = data.get("followerState")

        return VaultDetails(
            info=info,
            account_value=account_value,
            positions=positions,
            follower_state=follower_state,
        )
