"""Pytest fixtures for hyperhandler tests."""

import os
from decimal import Decimal
from unittest.mock import patch

import pytest

from hyperhandler.models import OrderSide, OrderType, TradingSignal
from hyperhandler.wallet import WalletManager


# Test private key (DO NOT use in production!)
TEST_PRIVATE_KEY = "0x" + "a" * 64


@pytest.fixture
def clean_env():
    """Remove any HL_ environment variables for clean testing."""
    env_vars = [k for k in os.environ if k.startswith("HL_")]
    old_values = {k: os.environ.pop(k) for k in env_vars}
    yield
    os.environ.update(old_values)


@pytest.fixture
def mock_keyring():
    """Mock the keyring module."""
    storage = {}

    def get_password(service, username):
        return storage.get((service, username))

    def set_password(service, username, password):
        storage[(service, username)] = password

    def delete_password(service, username):
        key = (service, username)
        if key in storage:
            del storage[key]
        else:
            from keyring.errors import PasswordDeleteError
            raise PasswordDeleteError()

    with patch("keyring.get_password", side_effect=get_password), \
         patch("keyring.set_password", side_effect=set_password), \
         patch("keyring.delete_password", side_effect=delete_password):
        yield storage


@pytest.fixture
def wallet_manager(clean_env, mock_keyring):
    """Create a WalletManager with mocked keyring and clean environment."""
    return WalletManager(allow_prompt=False)


@pytest.fixture
def test_key():
    """Return a valid test private key."""
    return TEST_PRIVATE_KEY


@pytest.fixture
def valid_long_signal():
    """Create a valid long limit signal."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.LONG,
        order_type=OrderType.LIMIT,
        entry_price=Decimal("67500"),
        size=Decimal("0.1"),
        leverage=5,
        stop_loss=Decimal("66000"),
        take_profit=Decimal("70000"),
    )


@pytest.fixture
def valid_short_signal():
    """Create a valid short market signal."""
    return TradingSignal(
        pair="ETH",
        side=OrderSide.SHORT,
        order_type=OrderType.MARKET,
        size=Decimal("1.0"),
        leverage=10,
    )


@pytest.fixture
def mock_meta_response():
    """Standard meta API response."""
    return {
        "universe": [
            {"name": "BTC", "szDecimals": 5, "maxLeverage": 50},
            {"name": "ETH", "szDecimals": 4, "maxLeverage": 50},
        ]
    }


@pytest.fixture
def mock_account_state():
    """Standard clearinghouseState API response."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "500.0",
        },
        "assetPositions": [],
    }
