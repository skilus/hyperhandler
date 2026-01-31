"""System keyring key provider."""

import keyring
from keyring.errors import KeyringError

from hyperhandler.wallet.providers.base import KeyProvider


class KeyringProvider(KeyProvider):
    """Key provider that uses the system keyring.

    Stores keys securely using the OS keychain (macOS Keychain,
    Windows Credential Locker, Linux Secret Service).
    """

    SERVICE_NAME = "hyperhandler"

    @property
    def name(self) -> str:
        return "keyring"

    def _get_username(self, network: str) -> str:
        """Get the keyring username for a network."""
        return f"private_key_{network}"

    def get_key(self, network: str) -> str | None:
        """Get private key from system keyring.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Private key or None if not stored.
        """
        try:
            return keyring.get_password(self.SERVICE_NAME, self._get_username(network))
        except KeyringError:
            return None

    def set_key(self, network: str, key: str) -> None:
        """Store a private key in the system keyring.

        Args:
            network: Network name (mainnet, testnet).
            key: Private key to store.

        Raises:
            KeyringError: If the keyring operation fails.
        """
        keyring.set_password(self.SERVICE_NAME, self._get_username(network), key)

    def delete_key(self, network: str) -> bool:
        """Delete a private key from the system keyring.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            True if the key was deleted, False if it didn't exist.
        """
        try:
            keyring.delete_password(self.SERVICE_NAME, self._get_username(network))
            return True
        except KeyringError:
            return False

    def is_available(self) -> bool:
        """Check if the system keyring is available."""
        try:
            # Try to get a non-existent key to test keyring availability
            keyring.get_password(self.SERVICE_NAME, "__test__")
            return True
        except KeyringError:
            return False

    def has_key(self, network: str) -> bool:
        """Check if a key is stored for the given network."""
        return self.get_key(network) is not None
