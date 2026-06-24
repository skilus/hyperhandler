// Package golden loads the reference vectors generated from the official
// Hyperliquid Python SDK (SPEC-007, decision D5). The frozen crypto core
// (signer, order, wallet/hd) is verified byte-for-byte against these vectors.
//
// Vectors live in testdata/golden/ at the repo root and are regenerated with
// `make golden` (tools/goldengen/generate.py).
package golden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// SignerVector is one EIP-712 signing reference case.
type SignerVector struct {
	Label        string          `json:"label"`
	PrivateKey   string          `json:"private_key"`
	Address      string          `json:"address"`
	IsMainnet    bool            `json:"is_mainnet"`
	Action       json.RawMessage `json:"action"`
	Nonce        int64           `json:"nonce"`
	VaultAddress *string         `json:"vault_address"`
	ExpiresAfter *int64          `json:"expires_after"`
	MsgpackHex   string          `json:"msgpack_hex"`
	ActionHash   string          `json:"action_hash"`
	Signature    Signature       `json:"signature"`
}

// Signature holds the {r, s, v} EIP-712 components.
type Signature struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

// SignerGolden is the parsed signer.json file.
type SignerGolden struct {
	Comment string         `json:"_comment"`
	Vectors []SignerVector `json:"vectors"`
}

// HDAccount is one BIP-44 derivation reference.
type HDAccount struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
}

// HDGolden is the parsed hd.json file.
type HDGolden struct {
	Comment    string      `json:"_comment"`
	Mnemonic   string      `json:"mnemonic"`
	BasePath   string      `json:"base_path"`
	Passphrase string      `json:"passphrase"`
	Accounts   []HDAccount `json:"accounts"`
}

// Dir returns the absolute path to testdata/golden, resolved relative to this
// source file so it works regardless of the test's working directory.
func Dir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(repoRoot, "testdata", "golden")
}

// LoadSigner reads and parses testdata/golden/signer.json.
func LoadSigner() (*SignerGolden, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(), "signer.json"))
	if err != nil {
		return nil, err
	}
	var g SignerGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// LoadHD reads and parses testdata/golden/hd.json.
func LoadHD() (*HDGolden, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(), "hd.json"))
	if err != nil {
		return nil, err
	}
	var g HDGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
