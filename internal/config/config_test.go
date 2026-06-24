package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Network() != "mainnet" {
		t.Errorf("Network() = %q, want mainnet", c.Network())
	}
	if got := c.Settings().Trading.DefaultSlippage; got != 0.01 {
		t.Errorf("DefaultSlippage = %v, want 0.01", got)
	}
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "network: testnet\ntrading:\n  default_slippage: 0.02\n  max_retries: 5\nsecurity:\n  max_leverage: 10\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Network() != "testnet" {
		t.Errorf("Network() = %q, want testnet", c.Network())
	}
	if got := c.Settings().Trading.DefaultSlippage; got != 0.02 {
		t.Errorf("DefaultSlippage = %v, want 0.02", got)
	}
	if got := c.Settings().Trading.MaxRetries; got != 5 {
		t.Errorf("MaxRetries = %v, want 5", got)
	}
	if got := c.Section("security")["max_leverage"]; toIntOr(got) != 10 {
		t.Errorf("security.max_leverage = %v, want 10", got)
	}

	t.Setenv("HL_NETWORK", "mainnet")
	if c.Network() != "mainnet" {
		t.Errorf("HL_NETWORK override: Network() = %q, want mainnet", c.Network())
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Set("network", "testnet")
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Network() != "testnet" {
		t.Errorf("reloaded Network() = %q, want testnet", c2.Network())
	}
}

func TestNetworkConfigResolution(t *testing.T) {
	c, _ := Load(filepath.Join(t.TempDir(), "x.yaml"))
	nc, err := c.NetworkConfig("testnet")
	if err != nil {
		t.Fatal(err)
	}
	if nc.APIURL != "https://api.hyperliquid-testnet.xyz" {
		t.Errorf("testnet api url = %q", nc.APIURL)
	}
	if _, err := c.NetworkConfig("devnet"); err == nil {
		t.Error("expected error for unknown network")
	}
}

func toIntOr(v any) int {
	n, _ := toInt(v)
	return n
}
