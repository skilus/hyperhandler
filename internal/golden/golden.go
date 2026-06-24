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

// SlippageVector is one frozen float-path slippage case.
type SlippageVector struct {
	Price      string `json:"price"`
	IsBuy      bool   `json:"is_buy"`
	SzDecimals int    `json:"sz_decimals"`
	IsSpot     bool   `json:"is_spot"`
	Slippage   string `json:"slippage"`
	Result     string `json:"result"`
	Formatted  string `json:"formatted"`
}

// FormatVector is one price/size formatting case.
type FormatVector struct {
	Value          string `json:"value"`
	FormattedPrice string `json:"formatted_price"`
	FormattedSize  string `json:"formatted_size"`
}

// PayloadSignal is the signal recipe for a payload vector; the Go test rebuilds
// it through models.NewTradingSignal.
type PayloadSignal struct {
	Pair       string  `json:"pair"`
	Side       string  `json:"side"`
	OrderType  string  `json:"order_type"`
	Size       string  `json:"size"`
	Leverage   int     `json:"leverage"`
	EntryPrice *string `json:"entry_price"`
	StopLoss   *string `json:"stop_loss"`
	TakeProfit *string `json:"take_profit"`
}

// PayloadVector locks a full build_order_payload result (msgpack + action hash)
// for a given signal recipe.
type PayloadVector struct {
	Label        string        `json:"label"`
	Signal       PayloadSignal `json:"signal"`
	AssetIndex   int           `json:"asset_index"`
	CurrentPrice *string       `json:"current_price"`
	SzDecimals   int           `json:"sz_decimals"`
	Nonce        int64         `json:"nonce"`
	MsgpackHex   string        `json:"msgpack_hex"`
	ActionHash   string        `json:"action_hash"`
}

// OrderGolden is the parsed order.json file.
type OrderGolden struct {
	Comment    string           `json:"_comment"`
	Slippage   []SlippageVector `json:"slippage"`
	Formatting []FormatVector   `json:"formatting"`
	Payloads   []PayloadVector  `json:"payloads"`
}

// LoadOrder reads and parses testdata/golden/order.json.
func LoadOrder() (*OrderGolden, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(), "order.json"))
	if err != nil {
		return nil, err
	}
	var g OrderGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
