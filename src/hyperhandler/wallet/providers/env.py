"""Environment variable key provider."""

import os

from hyperhandler.wallet.providers.base import KeyProvider


class EnvKeyProvider(KeyProvider):
    """Key provider that reads from environment variables.

    Checks in order:
    1. HL_{NETWORK}_PRIVATE_KEY (e.g., HL_MAINNET_PRIVATE_KEY)
    2. HL_PRIVATE_KEY (fallback for any network)
    """

    @property
    def name(self) -> str:
        return "environment"

    def get_key(self, network: str) -> str | None:
        """Get private key from environment variables.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Private key or None if not set.
        """
        # Try network-specific variable first
        network_upper = network.upper()
        network_key = os.environ.get(f"HL_{network_upper}_PRIVATE_KEY")
        if network_key:
            return network_key

        # Fall back to generic variable
        return os.environ.get("HL_PRIVATE_KEY")

    def is_available(self) -> bool:
        """Environment provider is always available."""
        return True

    def has_key(self, network: str) -> bool:
        """Check if a key is available for the given network."""
        return self.get_key(network) is not None
