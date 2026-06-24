package config

import "fmt"

// NetworkConfig holds the per-network endpoints. Mirrors config.py:NetworkConfig.
type NetworkConfig struct {
	Name   string
	APIURL string
	WSURL  string
}

// Networks maps a network name to its configuration. Mirrors config.py:NETWORKS.
var Networks = map[string]NetworkConfig{
	"mainnet": {
		Name:   "mainnet",
		APIURL: "https://api.hyperliquid.xyz",
		WSURL:  "wss://api.hyperliquid.xyz/ws",
	},
	"testnet": {
		Name:   "testnet",
		APIURL: "https://api.hyperliquid-testnet.xyz",
		WSURL:  "wss://api.hyperliquid-testnet.xyz/ws",
	},
}

// Network returns the configuration for name ("mainnet" or "testnet").
func Network(name string) (NetworkConfig, error) {
	cfg, ok := Networks[name]
	if !ok {
		return NetworkConfig{}, fmt.Errorf("unknown network: %q", name)
	}
	return cfg, nil
}
