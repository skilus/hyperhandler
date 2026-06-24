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

// RiskCandle is one {h,l,c} candle in a risk ATR vector.
type RiskCandle struct {
	H string `json:"h"`
	L string `json:"l"`
	C string `json:"c"`
}

// ATRVector locks one calculate_atr result.
type ATRVector struct {
	Label   string       `json:"label"`
	Candles []RiskCandle `json:"candles"`
	Period  int          `json:"period"`
	Result  string       `json:"result"`
}

// StopLossVector locks one calculate_stop_loss result.
type StopLossVector struct {
	Entry         string `json:"entry"`
	Side          string `json:"side"`
	ATR           string `json:"atr"`
	Horizon       string `json:"horizon"`
	Price         string `json:"price"`
	Distance      string `json:"distance"`
	DistancePct   string `json:"distance_pct"`
	ATRValue      string `json:"atr_value"`
	ATRMultiplier string `json:"atr_multiplier"`
}

// LiquidationVector locks one estimate_liquidation_price result.
type LiquidationVector struct {
	Entry    string `json:"entry"`
	Leverage int    `json:"leverage"`
	Side     string `json:"side"`
	Result   string `json:"result"`
}

// ValidateStopVector locks one validate_stop_vs_liquidation result.
type ValidateStopVector struct {
	Stop  string `json:"stop"`
	Liq   string `json:"liq"`
	Entry string `json:"entry"`
	Side  string `json:"side"`
	Valid bool   `json:"valid"`
}

// LeverageVector locks one select_leverage / select_leverage_for_stop result.
type LeverageVector struct {
	StopDistancePct string `json:"stop_distance_pct"`
	Stop            string `json:"stop"`
	Entry           string `json:"entry"`
	Side            string `json:"side"`
	MaxCoin         int    `json:"max_coin"`
	Leverage        int    `json:"leverage"`
	MaxSafe         int    `json:"max_safe"`
	MaxCoinOut      int    `json:"max_coin_out"`
	MaxConfig       int    `json:"max_config"`
	Reason          string `json:"reason"`
}

// PositionSizeInputV is the recorded input for a position-size vector.
type PositionSizeInputV struct {
	AccountValue     string   `json:"account_value"`
	AvailableBalance string   `json:"available_balance"`
	EntryPrice       string   `json:"entry_price"`
	StopPrice        string   `json:"stop_price"`
	Leverage         int      `json:"leverage"`
	SzDecimals       int      `json:"sz_decimals"`
	Confidence       *float64 `json:"confidence"`
	RiskMultiplier   *string  `json:"risk_multiplier"`
	MaxRiskAmount    *string  `json:"max_risk_amount"`
}

// PositionSizeResultV is the recorded result for a position-size vector.
type PositionSizeResultV struct {
	Size               string `json:"size"`
	Notional           string `json:"notional"`
	MarginRequired     string `json:"margin_required"`
	RiskAmount         string `json:"risk_amount"`
	RiskPct            string `json:"risk_pct"`
	CommissionEstimate string `json:"commission_estimate"`
}

// PositionSizeVector locks one calculate_position_size result or reject.
type PositionSizeVector struct {
	Label        string               `json:"label"`
	Input        PositionSizeInputV   `json:"input"`
	IsReject     bool                 `json:"is_reject"`
	RejectReason string               `json:"reject_reason"`
	Result       *PositionSizeResultV `json:"result"`
}

// CumRiskPositionV is one position in a cumulative-risk vector.
type CumRiskPositionV struct {
	Coin       string  `json:"coin"`
	RiskAmount *string `json:"risk_amount"`
}

// CumulativeRiskVector locks one calculate_cumulative_risk result.
type CumulativeRiskVector struct {
	Label             string              `json:"label"`
	Positions         []CumRiskPositionV  `json:"positions"`
	NewRiskAmount     string              `json:"new_risk_amount"`
	NewCoin           string              `json:"new_coin"`
	AccountValue      string              `json:"account_value"`
	RawRisk           string              `json:"raw_risk"`
	AdjustedRisk      string              `json:"adjusted_risk"`
	RiskPct           string              `json:"risk_pct"`
	AvailableBudget   string              `json:"available_budget"`
	WithinLimit       bool                `json:"within_limit"`
	CorrelationGroups map[string][]string `json:"correlation_groups"`
}

// FundingVector locks one estimate_funding_cost result.
type FundingVector struct {
	Size               string `json:"size"`
	Entry              string `json:"entry"`
	Side               string `json:"side"`
	FundingRate        string `json:"funding_rate"`
	RiskAmount         string `json:"risk_amount"`
	HoldHours          int    `json:"hold_hours"`
	HourlyRate         string `json:"hourly_rate"`
	HourlyCost         string `json:"hourly_cost"`
	HourlyIncome       string `json:"hourly_income"`
	Projected24h       string `json:"projected_24h"`
	FundingEatsRiskPct string `json:"funding_eats_risk_pct"`
}

// RoundDownVector locks one _round_down result.
type RoundDownVector struct {
	Value    string `json:"value"`
	Decimals int    `json:"decimals"`
	Result   string `json:"result"`
}

// RiskGolden is the parsed risk.json file.
type RiskGolden struct {
	Comment               string                 `json:"_comment"`
	Profile               string                 `json:"profile"`
	ATR                   []ATRVector            `json:"atr"`
	StopLoss              []StopLossVector       `json:"stop_loss"`
	Liquidation           []LiquidationVector    `json:"liquidation"`
	ValidateStop          []ValidateStopVector   `json:"validate_stop"`
	SelectLeverage        []LeverageVector       `json:"select_leverage"`
	SelectLeverageForStop []LeverageVector       `json:"select_leverage_for_stop"`
	PositionSize          []PositionSizeVector   `json:"position_size"`
	CumulativeRisk        []CumulativeRiskVector `json:"cumulative_risk"`
	Funding               []FundingVector        `json:"funding"`
	RoundDown             []RoundDownVector      `json:"round_down"`
}

// LoadRisk reads and parses testdata/golden/risk.json.
func LoadRisk() (*RiskGolden, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(), "risk.json"))
	if err != nil {
		return nil, err
	}
	var g RiskGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
