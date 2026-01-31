"""Tests for wallet management."""

import os

import pytest

from hlhandler.utils import mask_key, normalize_private_key, validate_private_key
from hlhandler.wallet import WalletManager
from hlhandler.wallet.providers import EnvKeyProvider, KeyringProvider


class TestNormalizePrivateKey:
    """Tests for normalize_private_key function."""

    def test_with_0x_prefix(self):
        key = "0x" + "a" * 64
        assert normalize_private_key(key) == "0x" + "a" * 64

    def test_without_0x_prefix(self):
        key = "a" * 64
        assert normalize_private_key(key) == "0x" + "a" * 64

    def test_uppercase_normalized_to_lowercase(self):
        key = "0x" + "A" * 64
        assert normalize_private_key(key) == "0x" + "a" * 64

    def test_with_whitespace(self):
        key = "  0x" + "a" * 64 + "  "
        assert normalize_private_key(key) == "0x" + "a" * 64

    def test_invalid_length_short(self):
        with pytest.raises(ValueError, match="64 hex characters"):
            normalize_private_key("0x" + "a" * 63)

    def test_invalid_length_long(self):
        with pytest.raises(ValueError, match="64 hex characters"):
            normalize_private_key("0x" + "a" * 65)

    def test_invalid_hex(self):
        with pytest.raises(ValueError, match="64 hex characters"):
            normalize_private_key("0x" + "g" * 64)


class TestValidatePrivateKey:
    """Tests for validate_private_key function."""

    def test_valid_key(self):
        assert validate_private_key("0x" + "a" * 64) is True

    def test_valid_key_no_prefix(self):
        assert validate_private_key("a" * 64) is True

    def test_invalid_key(self):
        assert validate_private_key("invalid") is False

    def test_empty_key(self):
        assert validate_private_key("") is False


class TestMaskKey:
    """Tests for mask_key function."""

    def test_mask_key(self):
        key = "0x" + "a" * 64
        masked = mask_key(key)
        assert masked.startswith("0x")
        assert "..." in masked
        assert len(masked) < len(key)

    def test_mask_short_key(self):
        key = "short"
        masked = mask_key(key)
        assert masked == "*****"


class TestEnvKeyProvider:
    """Tests for EnvKeyProvider."""

    def test_network_specific_key(self, clean_env):
        provider = EnvKeyProvider()
        os.environ["HL_MAINNET_PRIVATE_KEY"] = "0x" + "b" * 64

        key = provider.get_key("mainnet")
        assert key == "0x" + "b" * 64

    def test_generic_key_fallback(self, clean_env):
        provider = EnvKeyProvider()
        os.environ["HL_PRIVATE_KEY"] = "0x" + "c" * 64

        key = provider.get_key("mainnet")
        assert key == "0x" + "c" * 64

    def test_network_specific_takes_priority(self, clean_env):
        provider = EnvKeyProvider()
        os.environ["HL_MAINNET_PRIVATE_KEY"] = "0x" + "d" * 64
        os.environ["HL_PRIVATE_KEY"] = "0x" + "e" * 64

        key = provider.get_key("mainnet")
        assert key == "0x" + "d" * 64

    def test_no_key_returns_none(self, clean_env):
        provider = EnvKeyProvider()
        assert provider.get_key("mainnet") is None

    def test_is_always_available(self):
        provider = EnvKeyProvider()
        assert provider.is_available() is True


class TestKeyringProvider:
    """Tests for KeyringProvider."""

    def test_set_and_get_key(self, mock_keyring):
        provider = KeyringProvider()
        test_key = "0x" + "f" * 64

        provider.set_key("mainnet", test_key)
        assert provider.get_key("mainnet") == test_key

    def test_get_nonexistent_key(self, mock_keyring):
        provider = KeyringProvider()
        assert provider.get_key("mainnet") is None

    def test_delete_key(self, mock_keyring):
        provider = KeyringProvider()
        test_key = "0x" + "f" * 64

        provider.set_key("mainnet", test_key)
        assert provider.delete_key("mainnet") is True
        assert provider.get_key("mainnet") is None

    def test_delete_nonexistent_key(self, mock_keyring):
        provider = KeyringProvider()
        assert provider.delete_key("mainnet") is False

    def test_has_key(self, mock_keyring):
        provider = KeyringProvider()
        assert provider.has_key("mainnet") is False

        provider.set_key("mainnet", "0x" + "f" * 64)
        assert provider.has_key("mainnet") is True


class TestWalletManager:
    """Tests for WalletManager."""

    def test_get_key_from_env(self, wallet_manager, test_key):
        os.environ["HL_MAINNET_PRIVATE_KEY"] = test_key

        result = wallet_manager.get_private_key("mainnet")
        assert result is not None
        assert result.key == test_key
        assert result.provider == "environment"

    def test_get_key_from_keyring(self, wallet_manager, test_key, mock_keyring):
        mock_keyring[("hlhandler", "private_key_mainnet")] = test_key

        result = wallet_manager.get_private_key("mainnet")
        assert result is not None
        assert result.key == test_key
        assert result.provider == "keyring"

    def test_env_takes_priority_over_keyring(self, wallet_manager, mock_keyring):
        env_key = "0x" + "1" * 64
        keyring_key = "0x" + "2" * 64

        os.environ["HL_MAINNET_PRIVATE_KEY"] = env_key
        mock_keyring[("hlhandler", "private_key_mainnet")] = keyring_key

        result = wallet_manager.get_private_key("mainnet")
        assert result.key == env_key
        assert result.provider == "environment"

    def test_no_key_returns_none(self, wallet_manager):
        result = wallet_manager.get_private_key("mainnet")
        assert result is None

    def test_invalid_key_raises_error(self, wallet_manager):
        os.environ["HL_MAINNET_PRIVATE_KEY"] = "invalid"

        with pytest.raises(ValueError, match="Invalid private key"):
            wallet_manager.get_private_key("mainnet")

    def test_get_address(self, wallet_manager, test_key):
        os.environ["HL_MAINNET_PRIVATE_KEY"] = test_key

        address = wallet_manager.get_address("mainnet")
        assert address is not None
        assert address.startswith("0x")
        assert len(address) == 42

    def test_save_to_keyring(self, wallet_manager, test_key, mock_keyring):
        wallet_manager.save_to_keyring("mainnet", test_key)

        assert mock_keyring[("hlhandler", "private_key_mainnet")] == test_key

    def test_save_invalid_key_raises_error(self, wallet_manager):
        with pytest.raises(ValueError, match="Invalid private key"):
            wallet_manager.save_to_keyring("mainnet", "invalid")

    def test_remove_from_keyring(self, wallet_manager, test_key, mock_keyring):
        mock_keyring[("hlhandler", "private_key_mainnet")] = test_key

        result = wallet_manager.remove_from_keyring("mainnet")
        assert result is True
        assert ("hlhandler", "private_key_mainnet") not in mock_keyring

    def test_check_providers(self, wallet_manager, test_key):
        os.environ["HL_MAINNET_PRIVATE_KEY"] = test_key

        status = wallet_manager.check_providers("mainnet")

        assert "environment" in status
        assert status["environment"]["available"] is True
        assert status["environment"]["has_key"] is True

        assert "keyring" in status
        assert status["keyring"]["available"] is True
        assert status["keyring"]["has_key"] is False

    def test_key_result_address(self, wallet_manager, test_key):
        os.environ["HL_MAINNET_PRIVATE_KEY"] = test_key

        result = wallet_manager.get_private_key("mainnet")
        assert result is not None

        # Address should be derived correctly
        address = result.address
        assert address.startswith("0x")
        assert len(address) == 42
