package models

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	_ "github.com/skilus/hyperhandler/internal/decimalx" // set DivisionPrecision=28
)

// ValidationConfig bounds what signals the validator accepts. The zero value is
// not the default — use NewValidationConfig (or DefaultValidationConfig) for the
// Python defaults. Mirrors validator.py:ValidationConfig.
type ValidationConfig struct {
	MaxPositionSizeUSD decimal.Decimal
	MaxLeverage        int
	MinOrderSize       decimal.Decimal
	RequireStopLoss    bool
	AllowedPairs       []string // empty = all allowed
}

// DefaultValidationConfig returns the Python default limits.
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxPositionSizeUSD: decimal.RequireFromString("10000"),
		MaxLeverage:        20,
		MinOrderSize:       decimal.RequireFromString("0.0001"),
		RequireStopLoss:    false,
	}
}

// ValidationResult is the outcome of validation. Mirrors
// validator.py:ValidationResult.
type ValidationResult struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

// SignalValidator checks signals against configured limits. Mirrors
// validator.py:SignalValidator.
type SignalValidator struct {
	Config ValidationConfig
}

// NewSignalValidator returns a validator with the given config. Pass a nil
// pointer for the default config.
func NewSignalValidator(cfg *ValidationConfig) *SignalValidator {
	if cfg == nil {
		c := DefaultValidationConfig()
		return &SignalValidator{Config: c}
	}
	return &SignalValidator{Config: *cfg}
}

// Validate checks the signal and returns the collected errors and warnings.
// currentPrice (nil to omit) overrides the entry price for USD-size checks. The
// rule order mirrors validator.py:validate.
func (v *SignalValidator) Validate(signal *TradingSignal, currentPrice *decimal.Decimal) ValidationResult {
	var errs, warns []string

	// Allowed pairs.
	if len(v.Config.AllowedPairs) > 0 && !contains(v.Config.AllowedPairs, signal.Pair) {
		errs = append(errs, fmt.Sprintf("Pair %s not in allowed list: %s",
			signal.Pair, formatPairList(v.Config.AllowedPairs)))
	}

	// Leverage.
	if signal.Leverage > v.Config.MaxLeverage {
		errs = append(errs, fmt.Sprintf("Leverage %d exceeds maximum %d",
			signal.Leverage, v.Config.MaxLeverage))
	}

	// Minimum order size.
	if signal.Size.Cmp(v.Config.MinOrderSize) < 0 {
		errs = append(errs, fmt.Sprintf("Size %s below minimum %s",
			signal.Size.String(), v.Config.MinOrderSize.String()))
	}

	// Position size in USD, if a price is available.
	price := currentPrice
	if price == nil {
		price = signal.EntryPrice
	}
	if price != nil {
		positionUSD := signal.Size.Mul(*price)
		if positionUSD.Cmp(v.Config.MaxPositionSizeUSD) > 0 {
			errs = append(errs, fmt.Sprintf("Position size $%s exceeds maximum $%s",
				positionUSD.StringFixed(2), v.Config.MaxPositionSizeUSD.String()))
		}
	}

	// Stop-loss requirement.
	if v.Config.RequireStopLoss && signal.StopLoss == nil {
		errs = append(errs, "Stop-loss is required but not provided")
	}

	// Warnings for risky setups.
	if signal.Leverage > 10 {
		warns = append(warns, fmt.Sprintf("High leverage (%dx) - increased risk", signal.Leverage))
	}
	if signal.StopLoss == nil {
		warns = append(warns, "No stop-loss set - position has unlimited downside risk")
	}

	return ValidationResult{
		Valid:    len(errs) == 0,
		Errors:   errs,
		Warnings: warns,
	}
}

// ValidateOrRaise returns an error if validation fails, mirroring
// validator.py:validate_or_raise.
func (v *SignalValidator) ValidateOrRaise(signal *TradingSignal, currentPrice *decimal.Decimal) error {
	result := v.Validate(signal, currentPrice)
	if !result.Valid {
		return fmt.Errorf("Signal validation failed: %s", strings.Join(result.Errors, "; "))
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// formatPairList renders the allowed pairs like Python's list repr, e.g.
// ['BTC', 'ETH'].
func formatPairList(pairs []string) string {
	quoted := make([]string, len(pairs))
	for i, p := range pairs {
		quoted[i] = "'" + p + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
