"""Tests for EIP-712 signer."""

import time

import pytest

from hyperhandler.signer import Signer, get_nonce


# Test private key (DO NOT use in production!)
TEST_PRIVATE_KEY = "0x" + "a" * 64


@pytest.fixture
def signer():
    """Create a signer with test key."""
    return Signer(TEST_PRIVATE_KEY)


class TestSigner:
    """Tests for Signer."""

    def test_valid_signature(self, signer):
        """U-SGN-01: Valid signature for order."""
        action = {
            "type": "order",
            "orders": [
                {"a": 0, "b": True, "p": "67500", "s": "0.1", "r": False}
            ],
            "grouping": "na",
        }

        payload = signer.sign_action(action)

        assert "action" in payload
        assert "nonce" in payload
        assert "signature" in payload

        sig = payload["signature"]
        assert "r" in sig
        assert "s" in sig
        assert "v" in sig

    def test_deterministic_signature(self, signer):
        """U-SGN-02: Same payload produces same signature."""
        action = {"type": "test", "data": "value"}
        nonce = 1699999999999

        payload1 = signer.sign_action(action, nonce=nonce)
        payload2 = signer.sign_action(action, nonce=nonce)

        assert payload1["signature"] == payload2["signature"]

    def test_different_payloads_different_signatures(self, signer):
        """U-SGN-03: Different payloads produce different signatures."""
        action1 = {"type": "order", "data": "value1"}
        action2 = {"type": "order", "data": "value2"}
        nonce = 1699999999999

        payload1 = signer.sign_action(action1, nonce=nonce)
        payload2 = signer.sign_action(action2, nonce=nonce)

        assert payload1["signature"] != payload2["signature"]

    def test_invalid_private_key(self):
        """U-SGN-04: Invalid private key raises error."""
        with pytest.raises(Exception):
            Signer("invalid_key")

    def test_nonce_is_recent_timestamp(self, signer):
        """U-SGN-05: Nonce is close to current time."""
        action = {"type": "test"}
        before = int(time.time() * 1000)

        payload = signer.sign_action(action)

        after = int(time.time() * 1000)
        assert before <= payload["nonce"] <= after

    def test_get_nonce(self):
        """get_nonce returns current timestamp in ms."""
        before = int(time.time() * 1000)
        nonce = get_nonce()
        after = int(time.time() * 1000)

        assert before <= nonce <= after

    def test_signer_address(self, signer):
        """Signer returns correct address."""
        address = signer.address
        assert address.startswith("0x")
        assert len(address) == 42

    def test_sign_action_for_vault(self, signer):
        """sign_action_for_vault includes vault address."""
        action = {"type": "order", "orders": []}
        vault = "0x1234567890123456789012345678901234567890"

        payload = signer.sign_action_for_vault(action, vault)

        assert payload["vaultAddress"] == vault
        assert "signature" in payload

    def test_custom_nonce(self, signer):
        """Custom nonce is used when provided."""
        action = {"type": "test"}
        custom_nonce = 1234567890000

        payload = signer.sign_action(action, nonce=custom_nonce)

        assert payload["nonce"] == custom_nonce

    def test_signature_components(self, signer):
        """Signature has valid hex components."""
        action = {"type": "test"}
        payload = signer.sign_action(action)

        sig = payload["signature"]

        # r and s should be hex strings
        assert sig["r"].startswith("0x")
        assert sig["s"].startswith("0x")

        # v should be 27 or 28
        assert sig["v"] in [27, 28]

    def test_sign_action_with_expiration(self, signer):
        """U-SGN-11: Sign action with expires_after parameter."""
        action = {"type": "order", "orders": []}
        nonce = int(time.time() * 1000)
        expires = nonce + 60000  # 60 seconds from now

        payload = signer.sign_action(action, nonce=nonce, expires_after=expires)

        assert payload["expiresAfter"] == expires
        assert payload["nonce"] == nonce
        assert "signature" in payload

    def test_sign_action_with_vault_and_expiration(self, signer):
        """U-SGN-12: Sign action with both vault and expiration."""
        action = {"type": "order", "orders": []}
        vault = "0x1234567890123456789012345678901234567890"
        nonce = int(time.time() * 1000)
        expires = nonce + 60000

        payload = signer.sign_action(
            action, nonce=nonce, vault_address=vault, expires_after=expires
        )

        assert payload["vaultAddress"] == vault
        assert payload["expiresAfter"] == expires
        assert "signature" in payload
