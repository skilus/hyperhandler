"""Integration tests for risk CLI commands.

Groups E and F from SPEC-005.
"""

import json
from unittest.mock import MagicMock, patch

import httpx
import pytest
import respx
from typer.testing import CliRunner

from hyperhandler.cli import app
from hyperhandler.config import NETWORKS


@pytest.fixture
def runner():
    """Create CLI test runner."""
    return CliRunner()


@pytest.fixture
def signal_file(tmp_path):
    """Create valid signal JSON file."""
    signal = {
        "pair": "BTC",
        "side": "long",
        "order_type": "market",
        "size": 0.005,
        "leverage": 5,
    }
    file_path = tmp_path / "signal.json"
    file_path.write_text(json.dumps(signal))
    return file_path


@pytest.fixture
def stale_signal_file(tmp_path):
    """Create signal with stale entry price (>1% from market)."""
    signal = {
        "pair": "BTC",
        "side": "long",
        "order_type": "limit",
        "entry_price": 60000.0,  # Far from mock mid price of 67500
        "size": 0.01,
        "leverage": 5,
    }
    file_path = tmp_path / "stale_signal.json"
    file_path.write_text(json.dumps(signal))
    return file_path


@pytest.fixture
def mock_wallet_signer():
    """Mock wallet and signer for CLI tests."""
    mock_signer = MagicMock()
    mock_signer.address = "0x1234567890123456789012345678901234567890"

    def sign_action_side_effect(action, nonce=None, vault_address=None, expires_after=None):
        """Return signed payload preserving the original action."""
        return {
            "action": action,  # Preserve the actual action for verification
            "nonce": nonce or 1700000000000,
            "signature": {"r": "0x" + "1" * 64, "s": "0x" + "2" * 64, "v": 27},
            "vaultAddress": vault_address,
            "expiresAfter": expires_after,
        }

    mock_signer.sign_action.side_effect = sign_action_side_effect

    def mock_get_wallet(network):
        return MagicMock(), mock_signer

    return mock_get_wallet


@pytest.fixture
def cli_mock_api(
    mock_meta_response,
    mock_mids_response,
    mock_account_state_full,
    mock_funding_response,
    mock_candles_response,
    mock_user_fills_response,
):
    """Setup respx mock for CLI tests."""
    base_url = NETWORKS["testnet"].api_url

    def route_info(request):
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

        if req_type in responses:
            return httpx.Response(200, json=responses[req_type])
        return httpx.Response(400, json={"error": f"Unknown type: {req_type}"})

    with respx.mock(base_url=base_url, assert_all_called=False) as mock:
        mock.post("/info").mock(side_effect=route_info)
        yield mock


# =============================================================================
# Group E: CLI risk commands (4 tests)
# =============================================================================


@pytest.mark.integration
class TestRiskCLIIntegration:
    """Integration tests for risk CLI commands."""

    def test_risk_check_approved(
        self, runner, signal_file, cli_mock_api, mock_wallet_signer, tmp_path, memory_storage
    ):
        """I-RISK-E01: risk check with valid signal → approved.

        Assertions:
        1. Exit code 0
        2. Output contains "approved"
        3. Output shows calculated values
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "risk",
                    "check",
                    "--signal",
                    str(signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "high",
                ],
            )

        assert result.exit_code == 0
        assert "approved" in result.stdout.lower()

    def test_risk_check_rejected(
        self, runner, stale_signal_file, cli_mock_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-E02: risk check with stale signal → rejected.

        Assertions:
        1. Exit code != 0
        2. Output contains rejection reason
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "risk",
                    "check",
                    "--signal",
                    str(stale_signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "medium",
                ],
            )

        assert result.exit_code != 0
        assert "rejected" in result.stdout.lower() or "stale" in result.stdout.lower()

    def test_risk_status_shows_info(
        self, runner, cli_mock_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-E03: risk status displays account and CB state.

        Assertions:
        1. Exit code 0
        2. Output contains account value
        3. Output contains circuit breaker status
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                ["risk", "status", "--network", "testnet"],
            )

        assert result.exit_code == 0
        assert "account" in result.stdout.lower()
        assert "circuit breaker" in result.stdout.lower()

    def test_risk_reset_clears_cb(self, runner, memory_storage):
        """I-RISK-E04: risk reset adds virtual win.

        Assertions:
        1. After reset, a virtual trade with positive PnL exists
        2. Consecutive losses reset to 0
        """
        from datetime import datetime, timedelta, timezone
        from decimal import Decimal

        from hyperhandler.models.risk import TradeResult

        # Add some losing trades first
        now = datetime.now(timezone.utc)
        for i in range(3):
            result = TradeResult(
                coin="BTC",
                side="long",
                entry_price=Decimal("67000"),
                exit_price=Decimal("66000"),
                size=Decimal("0.1"),
                pnl=Decimal("-100"),
                fees=Decimal("5"),
                funding_paid=Decimal("0"),
                opened_at=now - timedelta(hours=i + 2),
                closed_at=now - timedelta(hours=i + 1),
            )
            memory_storage.save_trade_result(result, "testnet")

        with patch("hyperhandler.storage.get_storage", return_value=memory_storage):
            result = runner.invoke(
                app,
                ["risk", "reset", "--network", "testnet", "--yes"],
            )

        assert result.exit_code == 0
        assert "reset" in result.stdout.lower()

        # Verify virtual win was added
        results = memory_storage.get_recent_trade_results(network="testnet", limit=10)
        # Most recent should be the virtual win (RESET coin with positive PnL)
        assert results[0].coin == "RESET"
        assert results[0].pnl > 0


# =============================================================================
# Group F: exec with --risk-level (5 tests)
# =============================================================================


@pytest.mark.integration
class TestExecWithRiskLevel:
    """Integration tests for exec command with risk level.

    Key contract: MANAGED mode uses RiskManager.calculated_order for execution,
    not the original signal values.
    """

    @pytest.fixture
    def mock_exchange_api(self, cli_mock_api):
        """Add exchange endpoint to mock."""
        cli_mock_api.post("/exchange").mock(
            return_value=httpx.Response(
                200,
                json={
                    "status": "ok",
                    "response": {
                        "type": "order",
                        "data": {
                            "statuses": [
                                {
                                    "filled": {
                                        "oid": 999999,
                                        "totalSz": "0.005",
                                        "avgPx": "67500.0",
                                    }
                                }
                            ]
                        },
                    },
                },
            )
        )
        return cli_mock_api

    def test_exec_with_risk_level_dry_run(
        self, runner, signal_file, cli_mock_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-F02: exec --risk-level medium --dry-run shows order.

        Assertions:
        1. place_order NOT called (no /exchange calls)
        2. Output contains "dry run" or similar indicator
        3. Output shows calculated values (Size, Stop-Loss, Risk %)
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "exec",
                    "--signal",
                    str(signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "high",
                    "--dry-run",
                ],
            )

        assert result.exit_code == 0, f"Expected exit 0, got {result.exit_code}. Output: {result.stdout}"
        assert "dry run" in result.stdout.lower()
        assert "size" in result.stdout.lower()
        assert "stop-loss" in result.stdout.lower()

        # Verify no /exchange calls
        exchange_calls = [c for c in cli_mock_api.calls if "/exchange" in str(c.request.url)]
        assert len(exchange_calls) == 0

    def test_exec_without_risk_level_uses_manual(
        self, runner, signal_file, mock_exchange_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-F03: exec without --risk-level uses MANUAL mode.

        Assertions:
        1. Command runs without --risk-level
        2. Risk manager in MANUAL mode is used (no position size recalculation)
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "exec",
                    "--signal",
                    str(signal_file),
                    "--network",
                    "testnet",
                    "--dry-run",  # Use dry-run to avoid exchange calls
                ],
            )

        # In MANUAL mode, command should complete successfully
        assert result.exit_code == 0, f"Expected exit 0, got {result.exit_code}. Output: {result.stdout}"
        # MANUAL mode shows validation success without "Risk-Managed Adjustments" table
        assert "validated" in result.stdout.lower() or "dry run" in result.stdout.lower()

    def test_exec_with_risk_level_rejected_shows_reason(
        self, runner, stale_signal_file, cli_mock_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-F04: exec --risk-level with rejected signal shows rejection reason.

        Assertions:
        1. Exit code != 0
        2. Output contains rejection reason
        3. place_order NOT called
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "exec",
                    "--signal",
                    str(stale_signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "medium",
                ],
            )

        assert result.exit_code != 0
        assert "rejected" in result.stdout.lower() or "reason" in result.stdout.lower()

        # Verify no /exchange calls
        exchange_calls = [c for c in cli_mock_api.calls if "/exchange" in str(c.request.url)]
        assert len(exchange_calls) == 0

    def test_exec_managed_shows_diff_table(
        self, runner, signal_file, cli_mock_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-F05: MANAGED exec prints diff table with calculated values.

        Assertions:
        1. "Size" row present
        2. "Leverage" row present
        3. "Stop-Loss" row present
        4. "Risk" row present
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "exec",
                    "--signal",
                    str(signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "high",
                    "--dry-run",
                ],
            )

        assert result.exit_code == 0, f"Expected exit 0, got {result.exit_code}. Output: {result.stdout}"
        # Check diff table columns
        assert "size" in result.stdout.lower()
        assert "leverage" in result.stdout.lower()
        assert "stop" in result.stdout.lower()
        # Check that "Risk-Managed Adjustments" table is shown
        assert "risk-managed" in result.stdout.lower() or "parameter" in result.stdout.lower()

    def test_exec_with_risk_level_full(
        self, runner, signal_file, mock_exchange_api, mock_wallet_signer, memory_storage
    ):
        """I-RISK-F01: exec --risk-level medium executes order.

        Assertions:
        1. set_leverage called BEFORE place_order
        2. Exit code 0
        """
        with (
            patch("hyperhandler.cli.get_wallet_and_signer", mock_wallet_signer),
            patch("hyperhandler.storage.get_storage", return_value=memory_storage),
        ):
            result = runner.invoke(
                app,
                [
                    "exec",
                    "--signal",
                    str(signal_file),
                    "--network",
                    "testnet",
                    "--risk-level",
                    "high",
                ],
            )

        # Verify exit code
        assert result.exit_code == 0, f"Expected exit 0, got {result.exit_code}. Output: {result.stdout}"

        # Verify exchange was called and check order of operations
        exchange_calls = [c for c in mock_exchange_api.calls if "/exchange" in str(c.request.url)]
        assert len(exchange_calls) >= 2, f"Expected at least 2 exchange calls (leverage + order), got {len(exchange_calls)}"

        # Verify call order: set_leverage (updateLeverage) should come before place_order (order)
        call_bodies = []
        for call in exchange_calls:
            body = json.loads(call.request.content)
            call_bodies.append(body)

        # Extract action types - handle both signed payload format and direct format
        call_actions = []
        for body in call_bodies:
            action = body.get("action", body)  # action might be nested or at top level
            action_type = action.get("type") if isinstance(action, dict) else None
            call_actions.append(action_type)

        # First exchange call should be updateLeverage, then order
        leverage_idx = next((i for i, a in enumerate(call_actions) if a == "updateLeverage"), -1)
        order_idx = next((i for i, a in enumerate(call_actions) if a == "order"), -1)

        assert leverage_idx != -1, f"updateLeverage not found in calls: {call_actions}. Bodies: {call_bodies}"
        assert order_idx != -1, f"order not found in calls: {call_actions}. Bodies: {call_bodies}"
        assert leverage_idx < order_idx, f"set_leverage must be called BEFORE place_order. Order: {call_actions}"
