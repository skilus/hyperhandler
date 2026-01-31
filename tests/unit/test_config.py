"""Tests for configuration."""

import os
import tempfile
from pathlib import Path

import pytest
import yaml

from hyperhandler.config import Config, NETWORKS, Settings, get_config


@pytest.fixture
def temp_config_dir():
    """Create a temporary directory for config files."""
    with tempfile.TemporaryDirectory() as tmpdir:
        yield Path(tmpdir)


@pytest.fixture
def valid_config_file(temp_config_dir):
    """Create a valid config file."""
    config_path = temp_config_dir / "config.yaml"
    config_data = {
        "network": "testnet",
        "trading": {
            "default_slippage": 0.02,
            "max_retries": 5,
        },
    }
    with open(config_path, "w") as f:
        yaml.dump(config_data, f)
    return config_path


class TestConfig:
    """Tests for Config class."""

    def test_load_valid_config(self, valid_config_file):
        """U-CFG-01: Load valid config."""
        config = Config(valid_config_file)
        assert config.network == "testnet"

    def test_default_values(self, temp_config_dir):
        """U-CFG-02: Default values when config is minimal."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            yaml.dump({}, f)

        config = Config(config_path)
        assert config.settings.trading.default_slippage == 0.01
        assert config.settings.trading.max_retries == 3

    def test_env_override(self, temp_config_dir, monkeypatch):
        """U-CFG-03: Environment variable overrides config."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            yaml.dump({"network": "testnet"}, f)

        monkeypatch.setenv("HL_NETWORK", "mainnet")
        config = Config(config_path)

        assert config.network == "mainnet"

    def test_missing_file_uses_defaults(self, temp_config_dir):
        """U-CFG-04: Missing file uses defaults."""
        config_path = temp_config_dir / "nonexistent.yaml"
        config = Config(config_path)

        assert config.network == "mainnet"
        assert config.settings is not None

    def test_invalid_yaml_raises_error(self, temp_config_dir):
        """U-CFG-05: Invalid YAML raises error."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            f.write("invalid: yaml: content: [")

        with pytest.raises(Exception):
            Config(config_path)

    def test_testnet_urls(self):
        """U-CFG-06: Testnet has correct URLs."""
        config = NETWORKS["testnet"]
        assert "testnet" in config.api_url
        assert "testnet" in config.ws_url

    def test_mainnet_urls(self):
        """U-CFG-07: Mainnet has correct URLs."""
        config = NETWORKS["mainnet"]
        assert "testnet" not in config.api_url
        assert "testnet" not in config.ws_url

    def test_get_network_config(self, temp_config_dir):
        """get_network_config returns correct config."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            yaml.dump({}, f)

        config = Config(config_path)

        testnet = config.get_network_config("testnet")
        assert "testnet" in testnet.api_url

        mainnet = config.get_network_config("mainnet")
        assert "testnet" not in mainnet.api_url

    def test_unknown_network_raises(self, temp_config_dir):
        """Unknown network raises ValueError."""
        config_path = temp_config_dir / "config.yaml"
        with open(config_path, "w") as f:
            yaml.dump({}, f)

        config = Config(config_path)

        with pytest.raises(ValueError, match="Unknown network"):
            config.get_network_config("invalid")

    def test_save_config(self, temp_config_dir):
        """Config can be saved."""
        config_path = temp_config_dir / "config.yaml"
        config = Config(config_path)

        config.set("network", "testnet")
        config.save()

        # Reload and verify
        config2 = Config(config_path)
        assert config2.get("network") == "testnet"


class TestSettings:
    """Tests for Settings class."""

    def test_default_settings(self):
        """Default settings are correct."""
        settings = Settings()
        assert settings.network == "mainnet"
        assert settings.trading.default_slippage == 0.01
        assert settings.trading.max_retries == 3

    def test_custom_settings(self):
        """Custom settings are applied."""
        settings = Settings(
            network="testnet",
            trading={"default_slippage": 0.02, "max_retries": 5},
        )
        assert settings.network == "testnet"
        assert settings.trading.default_slippage == 0.02
        assert settings.trading.max_retries == 5


class TestNetworks:
    """Tests for network configurations."""

    def test_all_networks_have_required_fields(self):
        """All networks have required fields."""
        for name, config in NETWORKS.items():
            assert config.name == name
            assert config.api_url.startswith("https://")
            assert config.ws_url.startswith("wss://")
