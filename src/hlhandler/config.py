"""Configuration management for hlhandler."""

import os
from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings


class NetworkConfig(BaseModel):
    """Network-specific configuration."""

    name: str
    api_url: str
    ws_url: str


# Default network configurations
NETWORKS = {
    "mainnet": NetworkConfig(
        name="mainnet",
        api_url="https://api.hyperliquid.xyz",
        ws_url="wss://api.hyperliquid.xyz/ws",
    ),
    "testnet": NetworkConfig(
        name="testnet",
        api_url="https://api.hyperliquid-testnet.xyz",
        ws_url="wss://api.hyperliquid-testnet.xyz/ws",
    ),
}


class TradingSettings(BaseModel):
    """Trading-specific settings."""

    default_slippage: float = Field(default=0.01, ge=0, le=1)
    max_retries: int = Field(default=3, ge=0)
    retry_delay: float = Field(default=1.0, ge=0)


class Settings(BaseSettings):
    """Application settings with environment variable support."""

    model_config = {"env_prefix": "HL_", "env_nested_delimiter": "__"}

    network: str = Field(default="mainnet")
    trading: TradingSettings = Field(default_factory=TradingSettings)


class Config:
    """Main configuration class with YAML and environment support."""

    DEFAULT_CONFIG_DIR = Path.home() / ".hlhandler"
    DEFAULT_CONFIG_PATH = DEFAULT_CONFIG_DIR / "config.yaml"

    def __init__(self, config_path: Path | None = None):
        self.config_path = config_path or self.DEFAULT_CONFIG_PATH
        self._data: dict[str, Any] = {}
        self._settings: Settings | None = None
        self._load()

    def _load(self) -> None:
        """Load configuration from YAML file."""
        if self.config_path.exists():
            with open(self.config_path) as f:
                self._data = yaml.safe_load(f) or {}
        self._settings = Settings(**self._data)

    def save(self) -> None:
        """Save current configuration to YAML file."""
        self.config_path.parent.mkdir(parents=True, exist_ok=True)
        with open(self.config_path, "w") as f:
            yaml.dump(self._data, f, default_flow_style=False)

    @property
    def settings(self) -> Settings:
        """Get application settings."""
        if self._settings is None:
            self._settings = Settings(**self._data)
        return self._settings

    @property
    def network(self) -> str:
        """Get current network name."""
        return os.environ.get("HL_NETWORK", self._data.get("network", "mainnet"))

    def get_network_config(self, network: str | None = None) -> NetworkConfig:
        """Get network configuration."""
        net = network or self.network
        if net not in NETWORKS:
            raise ValueError(f"Unknown network: {net}. Available: {list(NETWORKS.keys())}")
        return NETWORKS[net]

    def set(self, key: str, value: Any) -> None:
        """Set a configuration value."""
        self._data[key] = value
        self._settings = None  # Reset cached settings

    def get(self, key: str, default: Any = None) -> Any:
        """Get a configuration value."""
        return self._data.get(key, default)


# Global config instance
_config: Config | None = None


def get_config(config_path: Path | None = None) -> Config:
    """Get or create the global config instance."""
    global _config
    if _config is None or config_path is not None:
        _config = Config(config_path)
    return _config
