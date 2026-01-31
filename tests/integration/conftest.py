"""Fixtures for integration tests."""

import pytest
import respx

from hyperhandler.config import NETWORKS


@pytest.fixture
def mock_api():
    """Mock the Hyperliquid API."""
    with respx.mock(base_url=NETWORKS["testnet"].api_url) as respx_mock:
        yield respx_mock


@pytest.fixture
def testnet_config():
    """Get testnet network config."""
    return NETWORKS["testnet"]


@pytest.fixture
def mainnet_config():
    """Get mainnet network config."""
    return NETWORKS["mainnet"]


@pytest.fixture
def mock_meta_response():
    """Standard meta API response."""
    return {
        "universe": [
            {"name": "BTC", "szDecimals": 5, "maxLeverage": 50, "onlyIsolated": False},
            {"name": "ETH", "szDecimals": 4, "maxLeverage": 50, "onlyIsolated": False},
            {"name": "SOL", "szDecimals": 2, "maxLeverage": 20, "onlyIsolated": False},
        ]
    }


@pytest.fixture
def mock_mids_response():
    """Standard allMids API response."""
    return {"BTC": "67500.5", "ETH": "3450.25", "SOL": "145.50"}


@pytest.fixture
def mock_account_state():
    """Standard clearinghouseState API response."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "500.0",
            "totalNtlPos": "2500.0",
            "totalRawUsd": "10000.0",
        },
        "assetPositions": [
            {
                "position": {
                    "coin": "BTC",
                    "szi": "0.1",
                    "entryPx": "67500.0",
                    "positionValue": "6750.0",
                    "unrealizedPnl": "125.0",
                    "leverage": {"type": "cross", "value": 5},
                }
            }
        ],
    }


@pytest.fixture
def mock_open_orders():
    """Standard openOrders API response."""
    return [
        {
            "coin": "BTC",
            "oid": 123456,
            "side": "B",
            "limitPx": "67000.0",
            "sz": "0.1",
            "timestamp": 1699999999999,
        },
        {
            "coin": "ETH",
            "oid": 123457,
            "side": "S",
            "limitPx": "3500.0",
            "sz": "1.0",
            "timestamp": 1699999999998,
        },
    ]


@pytest.fixture
def mock_order_success():
    """Successful order response."""
    return {
        "status": "ok",
        "response": {
            "type": "order",
            "data": {
                "statuses": [
                    {
                        "filled": {
                            "oid": 999999,
                            "totalSz": "0.1",
                            "avgPx": "67500.0",
                        }
                    }
                ]
            },
        },
    }


@pytest.fixture
def mock_order_resting():
    """Order placed but resting response."""
    return {
        "status": "ok",
        "response": {
            "type": "order",
            "data": {"statuses": [{"resting": {"oid": 888888}}]},
        },
    }


@pytest.fixture
def mock_order_error():
    """Order error response."""
    return {
        "status": "err",
        "response": "Insufficient margin",
    }
