"""EIP-712 signing for Hyperliquid API."""

import hashlib
import json
import time
from typing import Any

from eth_account import Account
from eth_account.messages import encode_defunct


class Signer:
    """Signer for Hyperliquid API requests.

    Hyperliquid uses a custom signing scheme where the action payload
    is hashed and signed using personal_sign (EIP-191).
    """

    def __init__(self, private_key: str):
        """Initialize signer with private key.

        Args:
            private_key: Hex-encoded private key with 0x prefix.
        """
        self.account = Account.from_key(private_key)

    @property
    def address(self) -> str:
        """Get the signer's Ethereum address."""
        return self.account.address

    def sign_action(self, action: dict[str, Any], nonce: int | None = None) -> dict:
        """Sign an action for the exchange API.

        Args:
            action: The action payload to sign.
            nonce: Optional nonce (timestamp in ms). If None, uses current time.

        Returns:
            Complete request payload with signature.
        """
        if nonce is None:
            nonce = int(time.time() * 1000)

        # Create the message to sign
        # Hyperliquid hashes the action JSON + nonce
        message_hash = self._create_message_hash(action, nonce)
        signature = self._sign_hash(message_hash)

        return {
            "action": action,
            "nonce": nonce,
            "signature": signature,
        }

    def sign_action_for_vault(
        self,
        action: dict[str, Any],
        vault_address: str,
        nonce: int | None = None,
    ) -> dict:
        """Sign an action for vault trading.

        Args:
            action: The action payload to sign.
            vault_address: The vault address to trade on behalf of.
            nonce: Optional nonce (timestamp in ms).

        Returns:
            Complete request payload with signature and vault address.
        """
        payload = self.sign_action(action, nonce)
        payload["vaultAddress"] = vault_address
        return payload

    def _create_message_hash(self, action: dict[str, Any], nonce: int) -> bytes:
        """Create the message hash to sign.

        Hyperliquid uses a specific format combining action and nonce.
        """
        # Serialize action to canonical JSON
        action_str = json.dumps(action, separators=(",", ":"), sort_keys=True)

        # Combine with nonce
        message = f"{action_str}{nonce}"

        # Hash the message
        return hashlib.sha256(message.encode()).digest()

    def _sign_hash(self, message_hash: bytes) -> dict[str, Any]:
        """Sign a message hash and return signature components.

        Args:
            message_hash: The hash to sign.

        Returns:
            Dict with r, s, v signature components.
        """
        # Use personal_sign (EIP-191)
        signable = encode_defunct(primitive=message_hash)
        signed = self.account.sign_message(signable)

        return {
            "r": hex(signed.r),
            "s": hex(signed.s),
            "v": signed.v,
        }


def get_nonce() -> int:
    """Get a nonce value (current timestamp in milliseconds)."""
    return int(time.time() * 1000)
