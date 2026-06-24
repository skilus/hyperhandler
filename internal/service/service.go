// Package service holds the orchestration extracted from the Python CLI: signal
// parsing, the async risk-evaluation data wrapper, trade execution, order
// cancellation and risk status/reset. The cobra layer (internal/cli) stays thin
// and only renders the structured results returned here. SPEC-007 Phase 6 (A.1).
package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/signer"
	"github.com/skilus/hyperhandler/internal/wallet"
)

// signalJSON mirrors the JSON shape of a TradingSignal. Decimal fields accept
// either JSON numbers or strings (shopspring/decimal handles both).
type signalJSON struct {
	Pair       string               `json:"pair"`
	Side       models.OrderSide     `json:"side"`
	OrderType  models.OrderType     `json:"order_type"`
	Size       decimal.Decimal      `json:"size"`
	Leverage   int                  `json:"leverage"`
	EntryPrice *decimal.Decimal     `json:"entry_price"`
	StopLoss   *decimal.Decimal     `json:"stop_loss"`
	TakeProfit *decimal.Decimal     `json:"take_profit"`
	Confidence *float64             `json:"confidence"`
	Source     *string              `json:"source"`
	Horizon    models.SignalHorizon `json:"horizon"`
}

// ParseSignal decodes raw JSON into a validated TradingSignal, running the same
// normalization and cross-field checks as the Python Pydantic model. Mirrors
// TradingSignal(**signal_data) in cli.py.
func ParseSignal(data []byte) (*models.TradingSignal, error) {
	var j signalJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	sig, err := models.NewTradingSignal(models.SignalParams{
		Pair:       j.Pair,
		Side:       j.Side,
		OrderType:  j.OrderType,
		Size:       j.Size,
		Leverage:   j.Leverage,
		EntryPrice: j.EntryPrice,
		StopLoss:   j.StopLoss,
		TakeProfit: j.TakeProfit,
		Confidence: j.Confidence,
		Source:     j.Source,
		Horizon:    j.Horizon,
	})
	if err != nil {
		return nil, err
	}
	return sig, nil
}

// ParseSignalReader reads all of r and parses it as a signal.
func ParseSignalReader(r io.Reader) (*models.TradingSignal, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return ParseSignal(data)
}

// ParseSignalFile reads and parses a signal from a JSON file.
func ParseSignalFile(path string) (*models.TradingSignal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSignal(data)
}

// ValidatorFromConfig builds a SignalValidator using the "security" section of
// the config, falling back to the Python defaults. Mirrors the validator setup
// in cli.py:exec.
func ValidatorFromConfig(cfg *config.Config) *models.SignalValidator {
	vc := models.DefaultValidationConfig()
	sec := cfg.Section("security")
	if v, ok := sec["max_position_size_usd"]; ok {
		vc.MaxPositionSizeUSD = decimal.RequireFromString(fmt.Sprintf("%v", v))
	}
	if v, ok := sec["max_leverage"]; ok {
		if n, ok2 := toInt(v); ok2 {
			vc.MaxLeverage = n
		}
	}
	if v, ok := sec["require_stop_loss"].(bool); ok {
		vc.RequireStopLoss = v
	}
	return models.NewSignalValidator(&vc)
}

// WalletAndSigner resolves the private key for network and builds a signer. When
// allowPrompt is true the interactive prompt provider participates. Returns a
// nil signer (no error) when no key is configured, so callers can print the
// Python "no private key" guidance. Mirrors cli.py:get_wallet_and_signer.
func WalletAndSigner(network string, allowPrompt bool) (*wallet.WalletManager, *signer.Signer, error) {
	mgr := wallet.NewWalletManager(allowPrompt)
	key, err := mgr.GetPrivateKey(network)
	if err != nil {
		return mgr, nil, err
	}
	if key == nil {
		return mgr, nil, nil
	}
	s, err := signer.New(key.Key, network == "mainnet")
	if err != nil {
		return mgr, nil, err
	}
	return mgr, s, nil
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
