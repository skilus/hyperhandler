package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// TradingSettings mirrors config.py:TradingSettings.
type TradingSettings struct {
	DefaultSlippage float64 `yaml:"default_slippage"`
	MaxRetries      int     `yaml:"max_retries"`
	RetryDelay      float64 `yaml:"retry_delay"`
}

// DefaultTradingSettings returns the Python field defaults.
func DefaultTradingSettings() TradingSettings {
	return TradingSettings{DefaultSlippage: 0.01, MaxRetries: 3, RetryDelay: 1.0}
}

// Settings is the resolved application configuration. It is built from the YAML
// data and then overlaid with HL_-prefixed environment variables (with "__" as
// the nested delimiter), mirroring pydantic-settings in config.py:Settings.
type Settings struct {
	Network string          `yaml:"network"`
	Trading TradingSettings `yaml:"trading"`
}

// Config loads and exposes configuration from a YAML file plus environment
// overrides. Mirrors config.py:Config. Construct it with Load (no global
// singleton — DI, SPEC-007 D7).
type Config struct {
	path     string
	data     map[string]any
	settings Settings
}

// DefaultConfigPath returns ~/.hyperhandler/config.yaml. It falls back to a
// relative path when the home directory cannot be resolved.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".hyperhandler", "config.yaml")
	}
	return filepath.Join(home, ".hyperhandler", "config.yaml")
}

// Load reads configuration from path (DefaultConfigPath when empty). A missing
// file is not an error: defaults plus env overrides are used. Mirrors
// config.py:Config.__init__ + _load.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	c := &Config{path: path, data: map[string]any{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(raw, &c.data); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
		if c.data == nil {
			c.data = map[string]any{}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	c.settings = buildSettings(c.data)
	return c, nil
}

// buildSettings derives Settings from the YAML data and HL_ env overrides.
func buildSettings(data map[string]any) Settings {
	s := Settings{Network: "mainnet", Trading: DefaultTradingSettings()}

	if v, ok := data["network"].(string); ok {
		s.Network = v
	}
	if t, ok := data["trading"].(map[string]any); ok {
		if v, ok := toFloat(t["default_slippage"]); ok {
			s.Trading.DefaultSlippage = v
		}
		if v, ok := toInt(t["max_retries"]); ok {
			s.Trading.MaxRetries = v
		}
		if v, ok := toFloat(t["retry_delay"]); ok {
			s.Trading.RetryDelay = v
		}
	}

	// Environment overrides (HL_ prefix; "__" nests). Mirrors pydantic-settings.
	if v := os.Getenv("HL_NETWORK"); v != "" {
		s.Network = v
	}
	if v, ok := envFloat("HL_TRADING__DEFAULT_SLIPPAGE"); ok {
		s.Trading.DefaultSlippage = v
	}
	if v, ok := envInt("HL_TRADING__MAX_RETRIES"); ok {
		s.Trading.MaxRetries = v
	}
	if v, ok := envFloat("HL_TRADING__RETRY_DELAY"); ok {
		s.Trading.RetryDelay = v
	}
	return s
}

// Settings returns the resolved settings.
func (c *Config) Settings() Settings { return c.settings }

// Path returns the config file path.
func (c *Config) Path() string { return c.path }

// Network returns the active network name. HL_NETWORK overrides the YAML value;
// the default is "mainnet". Mirrors config.py:Config.network.
func (c *Config) Network() string {
	if v := os.Getenv("HL_NETWORK"); v != "" {
		return v
	}
	if v, ok := c.data["network"].(string); ok {
		return v
	}
	return "mainnet"
}

// NetworkConfig returns the endpoint config for the named network, or the active
// network when name is empty. Mirrors config.py:get_network_config.
func (c *Config) NetworkConfig(name string) (NetworkConfig, error) {
	if name == "" {
		name = c.Network()
	}
	return Network(name)
}

// Get returns a top-level config value, or def when absent. Mirrors
// config.py:Config.get.
func (c *Config) Get(key string, def any) any {
	if v, ok := c.data[key]; ok {
		return v
	}
	return def
}

// Set assigns a top-level config value and invalidates cached settings. Mirrors
// config.py:Config.set.
func (c *Config) Set(key string, value any) {
	c.data[key] = value
	c.settings = buildSettings(c.data)
}

// Save writes the current data back to the YAML file, creating the parent
// directory if needed. Mirrors config.py:Config.save.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(c.data)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, raw, 0o600)
}

// Section returns a nested map under key (e.g. "security"), or an empty map when
// absent or not a map. Mirrors config.get("security", {}) access in cli.py.
func (c *Config) Section(key string) map[string]any {
	if m, ok := c.data[key].(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func envFloat(key string) (float64, bool) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil
}

func envInt(key string) (int, bool) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}
