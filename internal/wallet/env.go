package wallet

import (
	"os"
	"strings"
)

// EnvKeyProvider reads private keys from environment variables, preferring the
// network-specific HL_{NETWORK}_PRIVATE_KEY over the generic HL_PRIVATE_KEY.
// Mirrors wallet/providers/env.py:EnvKeyProvider.
type EnvKeyProvider struct{}

// Name returns the provider name.
func (EnvKeyProvider) Name() string { return "environment" }

// GetKey returns the key from the network-specific env var, falling back to the
// generic one.
func (EnvKeyProvider) GetKey(network string) (string, bool) {
	if v := os.Getenv("HL_" + strings.ToUpper(network) + "_PRIVATE_KEY"); v != "" {
		return v, true
	}
	if v := os.Getenv("HL_PRIVATE_KEY"); v != "" {
		return v, true
	}
	return "", false
}

// IsAvailable is always true for the environment provider.
func (EnvKeyProvider) IsAvailable() bool { return true }

// HasKey reports whether a key is set for the network.
func (p EnvKeyProvider) HasKey(network string) bool {
	_, ok := p.GetKey(network)
	return ok
}
