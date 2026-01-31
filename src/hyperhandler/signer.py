"""EIP-712 signing for Hyperliquid API.

Implements the Hyperliquid L1 action signing scheme:
1. Pack action with msgpack
2. Combine with nonce, vault address, and expiration
3. Keccak256 hash
4. Create phantom agent with hash
5. Sign EIP-712 typed data
"""

import time
from typing import Any

import msgpack
from eth_account import Account
from eth_account.messages import encode_typed_data
from eth_utils import keccak, to_hex


# EIP-712 domain for Hyperliquid
EIP712_DOMAIN = {
    "name": "Exchange",
    "version": "1",
    "chainId": 1337,
    "verifyingContract": "0x0000000000000000000000000000000000000000",
}

# EIP-712 types for Agent
EIP712_TYPES = {
    "Agent": [
        {"name": "source", "type": "string"},
        {"name": "connectionId", "type": "bytes32"},
    ],
}


class Signer:
    """Signer for Hyperliquid API requests.

    Hyperliquid uses EIP-712 typed data signing with a custom scheme
    that creates a "phantom agent" from the action hash.
    """

    def __init__(self, private_key: str, is_mainnet: bool = True):
        """Initialize signer with private key.

        Args:
            private_key: Hex-encoded private key with 0x prefix.
            is_mainnet: True for mainnet, False for testnet.
        """
        self.account = Account.from_key(private_key)
        self.is_mainnet = is_mainnet

    @property
    def address(self) -> str:
        """Get the signer's Ethereum address."""
        return self.account.address

    def sign_action(
        self,
        action: dict[str, Any],
        nonce: int | None = None,
        vault_address: str | None = None,
        expires_after: int | None = None,
    ) -> dict:
        """Sign an action for the exchange API.

        Args:
            action: The action payload to sign.
            nonce: Optional nonce (timestamp in ms). If None, uses current time.
            vault_address: Optional vault address for vault trading.
            expires_after: Optional expiration timestamp.

        Returns:
            Complete request payload with signature.
        """
        if nonce is None:
            nonce = int(time.time() * 1000)

        # Create action hash
        action_hash = self._create_action_hash(action, vault_address, nonce, expires_after)

        # Create phantom agent
        phantom_agent = self._construct_phantom_agent(action_hash)

        # Create EIP-712 payload and sign
        signature = self._sign_l1_action(phantom_agent)

        # Build response payload
        payload = {
            "action": action,
            "nonce": nonce,
            "signature": signature,
        }

        if vault_address:
            payload["vaultAddress"] = vault_address

        return payload

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
        return self.sign_action(action, nonce, vault_address=vault_address)

    def _create_action_hash(
        self,
        action: dict[str, Any],
        vault_address: str | None,
        nonce: int,
        expires_after: int | None,
    ) -> bytes:
        """Create the action hash for signing.

        Format:
        - msgpack(action)
        - nonce (8 bytes, big-endian)
        - vault flag (1 byte): 0x00 if no vault, 0x01 if vault
        - vault address (20 bytes) if vault flag is 0x01
        - expiration flag + value if expires_after is set
        """
        # Pack action with msgpack
        data = msgpack.packb(action)

        # Add nonce (8 bytes, big-endian)
        data += nonce.to_bytes(8, "big")

        # Add vault address if present
        if vault_address is None:
            data += b"\x00"
        else:
            data += b"\x01"
            data += self._address_to_bytes(vault_address)

        # Add expiration if present
        if expires_after is not None:
            data += b"\x00"
            data += expires_after.to_bytes(8, "big")

        # Keccak256 hash
        return keccak(data)

    def _construct_phantom_agent(self, action_hash: bytes) -> dict[str, Any]:
        """Construct phantom agent from action hash.

        Args:
            action_hash: The keccak256 hash of the action data.

        Returns:
            Phantom agent dict with source and connectionId.
        """
        return {
            "source": "a" if self.is_mainnet else "b",
            "connectionId": action_hash,
        }

    def _sign_l1_action(self, phantom_agent: dict[str, Any]) -> dict[str, Any]:
        """Sign the L1 action using EIP-712.

        Args:
            phantom_agent: The phantom agent to sign.

        Returns:
            Signature components {r, s, v}.
        """
        # Create full EIP-712 message
        full_message = {
            "domain": EIP712_DOMAIN,
            "types": {
                "EIP712Domain": [
                    {"name": "name", "type": "string"},
                    {"name": "version", "type": "string"},
                    {"name": "chainId", "type": "uint256"},
                    {"name": "verifyingContract", "type": "address"},
                ],
                **EIP712_TYPES,
            },
            "primaryType": "Agent",
            "message": phantom_agent,
        }

        # Encode and sign
        structured_data = encode_typed_data(full_message=full_message)
        signed = self.account.sign_message(structured_data)

        return {
            "r": to_hex(signed.r),
            "s": to_hex(signed.s),
            "v": signed.v,
        }

    @staticmethod
    def _address_to_bytes(address: str) -> bytes:
        """Convert Ethereum address to bytes.

        Args:
            address: Hex address with or without 0x prefix.

        Returns:
            20-byte address.
        """
        if address.startswith("0x"):
            address = address[2:]
        return bytes.fromhex(address)


def get_nonce() -> int:
    """Get a nonce value (current timestamp in milliseconds)."""
    return int(time.time() * 1000)
