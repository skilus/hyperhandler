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


# =============================================================================
# Risk Management Fixtures (SPEC-005)
# =============================================================================


@pytest.fixture
def mock_candles_response():
    """15 candles for ATR calculation."""
    base_price = 67000
    candles = []
    for i in range(15):
        candles.append({
            "t": 1700000000000 + i * 3600000,  # hourly
            "o": str(base_price + i * 10),
            "h": str(base_price + i * 10 + 500),
            "l": str(base_price + i * 10 - 300),
            "c": str(base_price + i * 10 + 100),
        })
    return candles


@pytest.fixture
def mock_funding_response(mock_meta_response):
    """metaAndAssetCtxs response (2-element list)."""
    ctxs = [
        {"funding": "0.0001", "openInterest": "1000", "oraclePx": "67500"},
        {"funding": "0.00005", "openInterest": "500", "oraclePx": "3500"},
        {"funding": "0.00008", "openInterest": "300", "oraclePx": "145"},
    ]
    return [mock_meta_response, ctxs]


@pytest.fixture
def mock_account_state_full():
    """Account state with all fields for risk (no positions)."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "0.0",
            "totalNtlPos": "0.0",
        },
        "withdrawable": "10000.0",
        "assetPositions": [],
    }


@pytest.fixture
def mock_account_state_with_position():
    """Account state with existing BTC position."""
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "2000.0",
            "totalNtlPos": "10000.0",
        },
        "withdrawable": "5000.0",
        "assetPositions": [
            {
                "position": {
                    "coin": "BTC",
                    "szi": "0.1",
                    "entryPx": "67000.0",
                    "positionValue": "6700.0",
                    "unrealizedPnl": "50.0",
                    "leverage": {"value": 5, "type": "cross"},
                    "markPx": "67050.0",
                    "liquidationPx": "54000.0",
                    "marginUsed": "1340.0",
                    "cumFunding": {"allTime": "-12.5", "sinceOpen": "-2.1"},
                }
            }
        ],
    }


@pytest.fixture
def mock_user_fills_response():
    """User fills with closing and opening fills."""
    return [
        {
            "coin": "BTC",
            "oid": 123,
            "side": "B",
            "px": "67500.0",
            "sz": "0.1",
            "time": 1700000000000,
            "closedPnl": "50.0",
            "fee": "3.0",
            "startPosition": {
                "entryPx": "67000.0",
                "time": 1699900000000,
            },
        },
        {
            "coin": "ETH",
            "oid": 124,
            "side": "A",
            "px": "3500.0",
            "sz": "1.0",
            "time": 1700001000000,
            # No closedPnl — opening fill
        },
    ]


@pytest.fixture
def memory_storage(tmp_path):
    """In-memory SQLite storage for testing."""
    from hyperhandler.storage import Storage
    db_path = tmp_path / "test_risk.db"
    return Storage(db_path=db_path)


@pytest.fixture
def info_request_router(
    mock_meta_response,
    mock_mids_response,
    mock_account_state_full,
    mock_funding_response,
    mock_candles_response,
    mock_user_fills_response,
):
    """Router for multiple /info requests.

    Tracks called types for assertion and fails on unknown types.
    """
    import json
    import httpx

    called_types: set[str] = set()

    def route(request):
        body = json.loads(request.content)
        req_type = body.get("type")

        responses = {
            "meta": mock_meta_response,
            "allMids": mock_mids_response,
            "clearinghouseState": mock_account_state_full,
            "metaAndAssetCtxs": mock_funding_response,
            "candleSnapshot": mock_candles_response,
            "userFills": mock_user_fills_response,
        }

        # Fail-fast: unknown request type is a test bug
        assert req_type in responses, f"Unknown /info request type: {req_type}"
        called_types.add(req_type)
        return httpx.Response(200, json=responses[req_type])

    route.called_types = called_types
    return route


@pytest.fixture
def test_address():
    """Test wallet address."""
    return "0x1234567890123456789012345678901234567890"
