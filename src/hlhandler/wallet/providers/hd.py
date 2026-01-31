"""HD Wallet provider using BIP-39 mnemonic."""

from dataclasses import dataclass

import keyring
from eth_account import Account

from hlhandler.wallet.providers.base import KeyProvider


@dataclass
class KeyResult:
    """Result of a key lookup."""

    key: str
    provider: str
    address: str

# Enable HD wallet features
Account.enable_unaudited_hdwallet_features()

# Keyring service name for mnemonics
MNEMONIC_SERVICE = "hlhandler-mnemonic"

# Default derivation path (Ethereum standard BIP-44)
DEFAULT_PATH = "m/44'/60'/0'/0"


class HDWalletProvider(KeyProvider):
    """HD Wallet provider using BIP-39 seed phrases.

    Stores the mnemonic in system keyring and derives keys using BIP-44 path.
    Default path: m/44'/60'/0'/0/{index}
    """

    def __init__(self, derivation_path: str = DEFAULT_PATH):
        """Initialize HD wallet provider.

        Args:
            derivation_path: Base derivation path (without account index).
        """
        self.derivation_path = derivation_path

    @property
    def name(self) -> str:
        return "hdwallet"

    def is_available(self) -> bool:
        """HD wallet is available if keyring is accessible."""
        try:
            keyring.get_keyring()
            return True
        except Exception:
            return False

    def get_key(self, network: str, account_index: int = 0) -> KeyResult | None:
        """Get derived private key for network.

        Args:
            network: Network name (used as keyring key).
            account_index: Account index for derivation (default 0).

        Returns:
            KeyResult with derived private key or None.
        """
        mnemonic = self._get_mnemonic(network)
        if not mnemonic:
            return None

        try:
            path = f"{self.derivation_path}/{account_index}"
            account = Account.from_mnemonic(mnemonic, account_path=path)
            return KeyResult(
                key=account.key.hex(),
                provider=self.name,
                address=account.address,
            )
        except Exception:
            return None

    def has_key(self, network: str) -> bool:
        """Check if mnemonic exists for network."""
        return self._get_mnemonic(network) is not None

    def _get_mnemonic(self, network: str) -> str | None:
        """Get mnemonic from keyring."""
        try:
            return keyring.get_password(MNEMONIC_SERVICE, network)
        except Exception:
            return None

    @staticmethod
    def generate_mnemonic(num_words: int = 12) -> str:
        """Generate a new BIP-39 mnemonic.

        Args:
            num_words: Number of words (12 or 24).

        Returns:
            Space-separated mnemonic phrase.
        """
        if num_words == 12:
            entropy_bits = 128
        elif num_words == 24:
            entropy_bits = 256
        else:
            raise ValueError("num_words must be 12 or 24")

        _, mnemonic = Account.create_with_mnemonic(
            num_words=num_words,
            passphrase="",
        )
        return mnemonic

    @staticmethod
    def validate_mnemonic(mnemonic: str) -> bool:
        """Validate a mnemonic phrase.

        Args:
            mnemonic: Space-separated mnemonic phrase.

        Returns:
            True if valid.
        """
        words = mnemonic.strip().split()
        if len(words) not in (12, 24):
            return False

        try:
            Account.from_mnemonic(mnemonic)
            return True
        except Exception:
            return False

    def save_mnemonic(self, network: str, mnemonic: str) -> None:
        """Save mnemonic to keyring.

        Args:
            network: Network name.
            mnemonic: Mnemonic phrase to save.

        Raises:
            ValueError: If mnemonic is invalid.
        """
        if not self.validate_mnemonic(mnemonic):
            raise ValueError("Invalid mnemonic phrase")

        keyring.set_password(MNEMONIC_SERVICE, network, mnemonic)

    def delete_mnemonic(self, network: str) -> bool:
        """Delete mnemonic from keyring.

        Args:
            network: Network name.

        Returns:
            True if deleted, False if not found.
        """
        try:
            keyring.delete_password(MNEMONIC_SERVICE, network)
            return True
        except keyring.errors.PasswordDeleteError:
            return False

    def list_addresses(
        self,
        network: str,
        count: int = 5,
        start_index: int = 0,
    ) -> list[tuple[int, str]]:
        """List derived addresses for network.

        Args:
            network: Network name.
            count: Number of addresses to derive.
            start_index: Starting account index.

        Returns:
            List of (index, address) tuples.
        """
        mnemonic = self._get_mnemonic(network)
        if not mnemonic:
            return []

        addresses = []
        for i in range(start_index, start_index + count):
            path = f"{self.derivation_path}/{i}"
            account = Account.from_mnemonic(mnemonic, account_path=path)
            addresses.append((i, account.address))

        return addresses
