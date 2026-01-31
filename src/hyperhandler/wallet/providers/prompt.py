"""Interactive prompt key provider."""

import getpass
import sys

from hyperhandler.wallet.providers.base import KeyProvider


class PromptKeyProvider(KeyProvider):
    """Key provider that prompts the user interactively.

    Caches the key for the duration of the session.
    """

    def __init__(self):
        self._cache: dict[str, str] = {}

    @property
    def name(self) -> str:
        return "prompt"

    def get_key(self, network: str) -> str | None:
        """Get private key by prompting the user.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Private key or None if user cancels.
        """
        # Return cached key if available
        if network in self._cache:
            return self._cache[network]

        # Only prompt if we have a TTY
        if not self.is_available():
            return None

        try:
            key = getpass.getpass(f"Enter private key for {network}: ")
            if key:
                self._cache[network] = key
                return key
        except (KeyboardInterrupt, EOFError):
            print()  # Newline after interrupt
            return None

        return None

    def is_available(self) -> bool:
        """Check if interactive input is available."""
        return sys.stdin.isatty()

    def clear_cache(self) -> None:
        """Clear all cached keys."""
        self._cache.clear()

    def clear_network(self, network: str) -> None:
        """Clear cached key for a specific network."""
        self._cache.pop(network, None)
