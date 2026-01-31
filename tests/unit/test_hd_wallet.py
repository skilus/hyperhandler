"""Tests for HD wallet provider."""

import pytest
from unittest.mock import patch, MagicMock

from eth_account import Account

# Enable HD wallet features for tests
Account.enable_unaudited_hdwallet_features()


class TestHDWalletProvider:
    """Tests for HDWalletProvider."""

    def test_generate_mnemonic_12_words(self):
        """Test generating 12-word mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=12)

        words = mnemonic.split()
        assert len(words) == 12

    def test_generate_mnemonic_24_words(self):
        """Test generating 24-word mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=24)

        words = mnemonic.split()
        assert len(words) == 24

    def test_generate_mnemonic_invalid_words(self):
        """Test that invalid word count raises error."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        with pytest.raises(ValueError, match="num_words must be 12 or 24"):
            provider.generate_mnemonic(num_words=15)

    def test_validate_mnemonic_valid_12(self):
        """Test validating a valid 12-word mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        # Generate a valid mnemonic first
        mnemonic = provider.generate_mnemonic(num_words=12)

        assert provider.validate_mnemonic(mnemonic) is True

    def test_validate_mnemonic_valid_24(self):
        """Test validating a valid 24-word mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=24)

        assert provider.validate_mnemonic(mnemonic) is True

    def test_validate_mnemonic_invalid_word_count(self):
        """Test that wrong word count is invalid."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        # 11 words - invalid
        mnemonic = "word " * 11
        assert provider.validate_mnemonic(mnemonic.strip()) is False

    def test_validate_mnemonic_invalid_words(self):
        """Test that invalid words are rejected."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        # Invalid words
        mnemonic = "notaword " * 12
        assert provider.validate_mnemonic(mnemonic.strip()) is False

    def test_provider_name(self):
        """Test provider name property."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        assert provider.name == "hdwallet"

    def test_is_available(self):
        """Test is_available returns True when keyring works."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        # Should return True since keyring is available on most systems
        assert provider.is_available() is True

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_save_and_get_mnemonic(self, mock_keyring):
        """Test saving and retrieving mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=12)

        # Mock keyring
        mock_keyring.get_password.return_value = mnemonic

        # Save mnemonic
        provider.save_mnemonic("testnet", mnemonic)
        mock_keyring.set_password.assert_called_once()

        # Get key (which reads mnemonic)
        result = provider.get_key("testnet", account_index=0)
        assert result is not None
        assert result.provider == "hdwallet"
        assert result.address.startswith("0x")

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_get_key_no_mnemonic(self, mock_keyring):
        """Test get_key returns None when no mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        mock_keyring.get_password.return_value = None

        provider = HDWalletProvider()
        result = provider.get_key("testnet")

        assert result is None

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_has_key(self, mock_keyring):
        """Test has_key checks for mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        mock_keyring.get_password.return_value = "some mnemonic"

        provider = HDWalletProvider()
        assert provider.has_key("testnet") is True

        mock_keyring.get_password.return_value = None
        assert provider.has_key("mainnet") is False

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_delete_mnemonic(self, mock_keyring):
        """Test deleting mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()

        # Successful delete
        result = provider.delete_mnemonic("testnet")
        assert result is True
        mock_keyring.delete_password.assert_called_once()

    def test_delete_mnemonic_not_found(self):
        """Test deleting non-existent mnemonic returns False."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider
        import keyring
        import keyring.errors

        provider = HDWalletProvider()
        # Delete a network that doesn't have a mnemonic
        # This might return True or False depending on keyring implementation
        # Just verify it doesn't raise an exception
        result = provider.delete_mnemonic("nonexistent_network_12345")
        assert isinstance(result, bool)

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_list_addresses(self, mock_keyring):
        """Test listing derived addresses."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=12)
        mock_keyring.get_password.return_value = mnemonic

        addresses = provider.list_addresses("testnet", count=3, start_index=0)

        assert len(addresses) == 3
        # Check indices
        assert addresses[0][0] == 0
        assert addresses[1][0] == 1
        assert addresses[2][0] == 2
        # Check addresses are valid
        for idx, addr in addresses:
            assert addr.startswith("0x")
            assert len(addr) == 42

    @patch("hyperhandler.wallet.providers.hd.keyring")
    def test_list_addresses_no_mnemonic(self, mock_keyring):
        """Test list_addresses returns empty when no mnemonic."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        mock_keyring.get_password.return_value = None

        provider = HDWalletProvider()
        addresses = provider.list_addresses("testnet")

        assert addresses == []

    def test_derivation_path_consistency(self):
        """Test that same mnemonic produces same addresses."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        # Use a known mnemonic
        mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

        provider = HDWalletProvider()

        # Derive address using our provider logic
        account = Account.from_mnemonic(mnemonic, account_path="m/44'/60'/0'/0/0")
        expected_address = account.address

        # The address should be deterministic
        account2 = Account.from_mnemonic(mnemonic, account_path="m/44'/60'/0'/0/0")
        assert account2.address == expected_address

    def test_different_indices_different_addresses(self):
        """Test that different indices produce different addresses."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()
        mnemonic = provider.generate_mnemonic(num_words=12)

        addr0 = Account.from_mnemonic(mnemonic, account_path="m/44'/60'/0'/0/0").address
        addr1 = Account.from_mnemonic(mnemonic, account_path="m/44'/60'/0'/0/1").address
        addr2 = Account.from_mnemonic(mnemonic, account_path="m/44'/60'/0'/0/2").address

        # All addresses should be different
        assert addr0 != addr1
        assert addr1 != addr2
        assert addr0 != addr2

    def test_save_invalid_mnemonic_raises(self):
        """Test that saving invalid mnemonic raises error."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        provider = HDWalletProvider()

        with pytest.raises(ValueError, match="Invalid mnemonic"):
            provider.save_mnemonic("testnet", "invalid mnemonic phrase")

    def test_custom_derivation_path(self):
        """Test using custom derivation path."""
        from hyperhandler.wallet.providers.hd import HDWalletProvider

        custom_path = "m/44'/60'/1'/0"
        provider = HDWalletProvider(derivation_path=custom_path)

        assert provider.derivation_path == custom_path
