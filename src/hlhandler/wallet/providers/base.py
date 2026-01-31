"""Base class for key providers."""

from abc import ABC, abstractmethod


class KeyProvider(ABC):
    """Abstract base class for private key providers."""

    @property
    @abstractmethod
    def name(self) -> str:
        """Human-readable name of this provider."""
        ...

    @abstractmethod
    def get_key(self, network: str) -> str | None:
        """Get the private key for a network.

        Args:
            network: Network name (mainnet, testnet).

        Returns:
            Private key string or None if not available.
        """
        ...

    @abstractmethod
    def is_available(self) -> bool:
        """Check if this provider is available.

        Returns:
            True if the provider can be used.
        """
        ...
