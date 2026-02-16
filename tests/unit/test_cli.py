"""Tests for CLI commands.

Note: These tests focus on the validate command which doesn't require
external dependencies. Full CLI testing with mocked API calls would be
more complex and is better suited for integration tests.
"""

import json

import pytest
from typer.testing import CliRunner

from hyperhandler.cli import app


@pytest.fixture
def runner():
    """Create CLI test runner."""
    return CliRunner()


@pytest.fixture
def valid_signal_json(tmp_path):
    """Create valid signal JSON file."""
    signal = {
        "pair": "BTC",
        "side": "long",
        "order_type": "market",
        "size": 0.1,
        "leverage": 5,
    }
    file_path = tmp_path / "signal.json"
    file_path.write_text(json.dumps(signal))
    return file_path


@pytest.fixture
def limit_signal_json(tmp_path):
    """Create limit signal JSON file."""
    signal = {
        "pair": "ETH",
        "side": "short",
        "order_type": "limit",
        "entry_price": 3500.0,
        "size": 0.5,
        "leverage": 3,
        "stop_loss": 3600.0,
        "take_profit": 3300.0,
    }
    file_path = tmp_path / "limit_signal.json"
    file_path.write_text(json.dumps(signal))
    return file_path


class TestValidateCommand:
    """Tests for validate command.

    Validate command doesn't require external dependencies, making it
    ideal for unit testing without complex mocking.
    """

    def test_validate_valid_signal(self, runner, valid_signal_json):
        """CLI-VAL-01: Validate accepts valid signal."""
        result = runner.invoke(app, ["validate", "--signal", str(valid_signal_json)])

        assert result.exit_code == 0
        assert "valid" in result.stdout.lower() or "ok" in result.stdout.lower()

    def test_validate_with_limit_signal(self, runner, limit_signal_json):
        """CLI-VAL-02: Validate accepts limit signal with SL/TP."""
        result = runner.invoke(app, ["validate", "--signal", str(limit_signal_json)])

        assert result.exit_code == 0

    def test_validate_invalid_json(self, runner, tmp_path):
        """CLI-VAL-03: Validate rejects invalid JSON."""
        bad_file = tmp_path / "bad.json"
        bad_file.write_text("{invalid json")

        result = runner.invoke(app, ["validate", "--signal", str(bad_file)])

        assert result.exit_code != 0

    def test_validate_missing_required_field(self, runner, tmp_path):
        """CLI-VAL-04: Validate rejects signal missing required field."""
        signal = {"pair": "BTC", "side": "long"}  # Missing order_type, size
        file_path = tmp_path / "incomplete.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code != 0

    def test_validate_invalid_sl_tp_levels(self, runner, tmp_path):
        """CLI-VAL-05: Validate rejects invalid SL/TP levels."""
        signal = {
            "pair": "BTC",
            "side": "long",
            "order_type": "limit",
            "entry_price": 67500.0,
            "size": 0.1,
            "stop_loss": 69000.0,  # SL above entry for long (invalid)
            "take_profit": 70000.0,
        }
        file_path = tmp_path / "bad_sl.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code != 0
        assert "stop" in result.stdout.lower() or "loss" in result.stdout.lower()

    def test_validate_limit_without_entry_price(self, runner, tmp_path):
        """CLI-VAL-06: Validate rejects limit order without entry price."""
        signal = {
            "pair": "BTC",
            "side": "long",
            "order_type": "limit",
            "size": 0.1,
            # Missing entry_price for limit order
        }
        file_path = tmp_path / "no_price.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code != 0

    def test_validate_negative_size(self, runner, tmp_path):
        """CLI-VAL-07: Validate rejects negative size."""
        signal = {
            "pair": "BTC",
            "side": "long",
            "order_type": "market",
            "size": -0.1,  # Negative size
        }
        file_path = tmp_path / "negative_size.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code != 0

    def test_validate_zero_size(self, runner, tmp_path):
        """CLI-VAL-08: Validate rejects zero size."""
        signal = {
            "pair": "BTC",
            "side": "long",
            "order_type": "market",
            "size": 0,  # Zero size
        }
        file_path = tmp_path / "zero_size.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code != 0

    def test_validate_with_all_fields(self, runner, tmp_path):
        """CLI-VAL-09: Validate accepts signal with all optional fields."""
        signal = {
            "pair": "SOL",
            "side": "short",
            "order_type": "limit",
            "entry_price": 150.0,
            "size": 1.5,
            "leverage": 10,
            "stop_loss": 155.0,
            "take_profit": 140.0,
        }
        file_path = tmp_path / "full_signal.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(app, ["validate", "--signal", str(file_path)])

        assert result.exit_code == 0


class TestAppVersion:
    """Tests for version command."""

    def test_version_flag(self, runner):
        """CLI-VER-01: --version shows version."""
        result = runner.invoke(app, ["--version"])

        assert result.exit_code == 0
        # Version should be displayed
        assert len(result.stdout) > 0


class TestRiskCommands:
    """Tests for risk management CLI commands."""

    def test_risk_help(self, runner):
        """CLI-RISK-01: risk --help shows available commands."""
        result = runner.invoke(app, ["risk", "--help"])

        assert result.exit_code == 0
        assert "check" in result.stdout
        assert "status" in result.stdout
        assert "reset" in result.stdout

    def test_risk_check_missing_signal(self, runner):
        """CLI-RISK-02: risk check requires --signal."""
        result = runner.invoke(app, ["risk", "check"])

        # Should fail due to missing required option
        assert result.exit_code != 0

    def test_risk_check_invalid_risk_level(self, runner, tmp_path):
        """CLI-RISK-03: risk check rejects invalid risk level."""
        signal = {
            "pair": "BTC",
            "side": "long",
            "order_type": "market",
            "size": 0.1,
            "leverage": 5,
        }
        file_path = tmp_path / "signal.json"
        file_path.write_text(json.dumps(signal))

        result = runner.invoke(
            app,
            ["risk", "check", "--signal", str(file_path), "--risk-level", "invalid"],
        )

        assert result.exit_code != 0
        assert "invalid" in result.stdout.lower()

    def test_risk_check_signal_not_found(self, runner):
        """CLI-RISK-04: risk check fails for non-existent signal file."""
        result = runner.invoke(
            app,
            ["risk", "check", "--signal", "/nonexistent/signal.json"],
        )

        assert result.exit_code != 0
        assert "not found" in result.stdout.lower()

    def test_exec_with_risk_level_invalid(self, runner, valid_signal_json):
        """CLI-RISK-05: exec --risk-level rejects invalid level."""
        result = runner.invoke(
            app,
            [
                "exec",
                "--signal",
                str(valid_signal_json),
                "--risk-level",
                "ultra",
                "--dry-run",
            ],
        )

        assert result.exit_code != 0
        assert "invalid" in result.stdout.lower()


class TestExecWithRiskLevel:
    """Tests for exec command with --risk-level flag."""

    def test_exec_risk_level_option_exists(self, runner):
        """CLI-EXEC-RISK-01: exec has --risk-level option."""
        result = runner.invoke(app, ["exec", "--help"])

        assert result.exit_code == 0
        assert "--risk-level" in result.stdout or "-r" in result.stdout
