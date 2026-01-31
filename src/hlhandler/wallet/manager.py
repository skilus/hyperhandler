"""Wallet manager for handling private keys and addresses."""

from dataclasses import dataclass

from eth_account import Account

from hlhandler.utils import normalize_private_key, validate_private_key
from hlhandler.wallet.providers.base import KeyProvider
from hlhandler.wallet.providers.env import EnvKeyProvider
from hlhandler.wallet.providers.keyring_provider import KeyringProvider
from hlhandler.wallet.providers.prompt import PromptKeyProvider


@dataclass
class KeyResult:
    """Result of a key lookup."""

    key: str
    provider: str

    @property
    def address(self) -> str:
        """Derive Ethereum address from the private key."""
        return Account.from_key(self.key).address


class WalletManager:
    """Manages private keys using a chain of providers.

    Providers are tried in order: Env -> Keyring -> Prompt
    """

    def __init__(
        self,
        providers: list[KeyProvider] | None = None,
        allow_prompt: bool = True,
    ):
        """Initialize the wallet manager.

        Args:
            providers: Custom list of providers. If None, uses defaults.
            allow_prompt: Whether to include the prompt provider.
        """
        if providers is not None:
            self._providers = providers
        else:
            self._providers = [
                EnvKeyProvider(),
                KeyringProvider(),
            ]
            if allow_prompt:
                self._providers.append(PromptKeyProvider())

        # Keep reference to keyring provider for save/remove operations
        self._keyring: KeyringProvider | None = None
        for p in self._providers:
            if isinstance(p, KeyringProvider):
                self._keyring = p
                break

    @property
    def providers(self) -> list[KeyProvider]:
        """Get the list of providers."""
        return self._providers

    def get_private_key(self, network: str) -> KeyResult | None:
        """Get private key from the first available provider.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            KeyResult with the key and provider name, or None if not found.

        Raises:
            ValueError: If the key is invalid.
        """
        for provider in self._providers:
            if not provider.is_available():
                continue

            key = provider.get_key(network)
            if key is not None:
                # Validate and normalize the key
                if not validate_private_key(key):
                    raise ValueError(
                        f"Invalid private key from {provider.name}: "
                        "must be 64 hex characters (32 bytes)"
                    )
                normalized = normalize_private_key(key)
                return KeyResult(key=normalized, provider=provider.name)

        return None

    def get_address(self, network: str) -> str | None:
        """Get the Ethereum address for a network.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Ethereum address or None if no key available.
        """
        result = self.get_private_key(network)
        if result:
            return result.address
        return None

    def save_to_keyring(self, network: str, key: str) -> None:
        """Save a private key to the system keyring.

        Args:
            network: Network name (mainnet, testnet).
            key: Private key to save.

        Raises:
            ValueError: If the key is invalid.
            RuntimeError: If keyring provider is not available.
        """
        if not validate_private_key(key):
            raise ValueError("Invalid private key: must be 64 hex characters (32 bytes)")

        if self._keyring is None:
            # Create a keyring provider if not in the chain
            self._keyring = KeyringProvider()

        if not self._keyring.is_available():
            raise RuntimeError("System keyring is not available")

        normalized = normalize_private_key(key)
        self._keyring.set_key(network, normalized)

    def remove_from_keyring(self, network: str) -> bool:
        """Remove a private key from the system keyring.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            True if the key was removed, False if it didn't exist.
        """
        if self._keyring is None:
            self._keyring = KeyringProvider()

        return self._keyring.delete_key(network)

    def check_providers(self, network: str) -> dict[str, dict]:
        """Check the status of all providers for a network.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Dict with provider status information.
        """
        status = {}
        for provider in self._providers:
            available = provider.is_available()
            has_key = False

            if available:
                # Check if provider has a key without triggering prompt
                if isinstance(provider, PromptKeyProvider):
                    # Don't trigger prompt, just check cache
                    has_key = network in provider._cache
                else:
                    has_key = provider.get_key(network) is not None

            status[provider.name] = {
                "available": available,
                "has_key": has_key,
            }

        return status
