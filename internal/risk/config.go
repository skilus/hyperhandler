// Package risk ports the risk module (manager, calculator, circuit breaker,
// collector, config) from the Python implementation. SPEC-007 Phase 4.
//
// All Decimal constants are parsed from string literals to match Python's
// Decimal(str) construction exactly. Division on the critical path relies on
// the global precision set by internal/decimalx (DivisionPrecision=28).
package risk

import (
	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/decimalx"
	"github.com/skilus/hyperhandler/internal/models"
)

// Ensure decimal precision matches Python before any package-level division.
var _ = decimalx.Precision

// mustDec parses a decimal from a string literal, panicking on malformed input.
// Used only for compile-time constant tables, so a panic indicates a bug.
func mustDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic("risk: invalid decimal constant " + s + ": " + err.Error())
	}
	return d
}

// RiskProfile holds the risk-management parameters for a risk level. Mirrors
// config.py:RiskProfile.
type RiskProfile struct {
	RiskPerTrade      decimal.Decimal // % of account per trade
	MaxCumulativeRisk decimal.Decimal // max total risk across positions
	DailyLossLimit    decimal.Decimal // max daily loss before hard stop
	MaxOpenPositions  int
	MaxLeverage       int
	CorrelationFactor decimal.Decimal // penalty for correlated positions
	SoftStopLosses    int             // consecutive losses for soft CB
	HardStopLosses    int             // consecutive losses for hard CB
	MaxFundingRiskPct decimal.Decimal // max funding cost as % of risk
}

// RiskProfiles maps each risk level to its profile. Mirrors
// config.py:RISK_PROFILES.
var RiskProfiles = map[models.RiskLevel]RiskProfile{
	models.RiskLow: {
		RiskPerTrade:      mustDec("0.01"), // 1%
		MaxCumulativeRisk: mustDec("0.04"), // 4%
		DailyLossLimit:    mustDec("0.02"), // 2%
		MaxOpenPositions:  3,
		MaxLeverage:       5,
		CorrelationFactor: mustDec("0.4"),
		SoftStopLosses:    2,
		HardStopLosses:    4,
		MaxFundingRiskPct: mustDec("0.3"), // 30%
	},
	models.RiskMedium: {
		RiskPerTrade:      mustDec("0.02"), // 2%
		MaxCumulativeRisk: mustDec("0.06"), // 6%
		DailyLossLimit:    mustDec("0.03"), // 3%
		MaxOpenPositions:  5,
		MaxLeverage:       10,
		CorrelationFactor: mustDec("0.3"),
		SoftStopLosses:    3,
		HardStopLosses:    5,
		MaxFundingRiskPct: mustDec("0.5"), // 50%
	},
	models.RiskHigh: {
		RiskPerTrade:      mustDec("0.03"), // 3%
		MaxCumulativeRisk: mustDec("0.10"), // 10%
		DailyLossLimit:    mustDec("0.05"), // 5%
		MaxOpenPositions:  8,
		MaxLeverage:       20,
		CorrelationFactor: mustDec("0.25"),
		SoftStopLosses:    3,
		HardStopLosses:    6,
		MaxFundingRiskPct: mustDec("0.7"), // 70%
	},
}

// GetProfile returns the profile for a risk level, falling back to MEDIUM for
// an unknown level (matches the RiskManager default). Mirrors RiskProfile.get.
func GetProfile(level models.RiskLevel) RiskProfile {
	if p, ok := RiskProfiles[level]; ok {
		return p
	}
	return RiskProfiles[models.RiskMedium]
}

// HLConfig holds Hyperliquid-specific configuration. Mirrors config.py:HLConfig.
type HLConfig struct {
	// Fees (Tier 0, no staking discount).
	TakerFee decimal.Decimal // 0.045%
	MakerFee decimal.Decimal // 0.015%

	// Order constraints.
	MinOrderValue  decimal.Decimal // $10 minimum
	MaxSlippage    decimal.Decimal // 1%
	SlippageBuffer decimal.Decimal // 0.5%

	// Margin.
	DefaultMarginMode string
	LiqSafetyBuffer   decimal.Decimal // 2% buffer from liquidation

	// Signal validation.
	MaxEntryDeviation  decimal.Decimal // 1% max price deviation
	MaxSignalAgeSecond int             // 5 minutes
}

// DefaultHLConfig returns HLConfig with the Python dataclass defaults.
func DefaultHLConfig() HLConfig {
	return HLConfig{
		TakerFee:           mustDec("0.00045"),
		MakerFee:           mustDec("0.00015"),
		MinOrderValue:      mustDec("10.0"),
		MaxSlippage:        mustDec("0.01"),
		SlippageBuffer:     mustDec("0.005"),
		DefaultMarginMode:  "cross",
		LiqSafetyBuffer:    mustDec("0.02"),
		MaxEntryDeviation:  mustDec("0.01"),
		MaxSignalAgeSecond: 300,
	}
}

// ATRSetting is the per-horizon ATR configuration. Mirrors an entry of
// config.py:ATR_SETTINGS.
type ATRSetting struct {
	Interval   string
	Period     int
	Multiplier decimal.Decimal
}

// ATRSettings maps each signal horizon to its ATR configuration. Mirrors
// config.py:ATR_SETTINGS.
var ATRSettings = map[models.SignalHorizon]ATRSetting{
	models.HorizonScalp:    {Interval: "15m", Period: 14, Multiplier: mustDec("1.2")},
	models.HorizonIntraday: {Interval: "1h", Period: 14, Multiplier: mustDec("1.5")},
	models.HorizonSwing:    {Interval: "4h", Period: 14, Multiplier: mustDec("2.0")},
	models.HorizonPosition: {Interval: "1d", Period: 14, Multiplier: mustDec("2.5")},
}

// CorrelationMap groups correlated coins for cumulative-risk calculation.
// Mirrors config.py:CORRELATION_MAP.
var CorrelationMap = map[string][]string{
	"btc-major": {"BTC", "ETH"},
	"l1-alt":    {"SOL", "AVAX", "SUI", "APT", "SEI"},
	"defi":      {"AAVE", "UNI", "MKR", "CRV", "DYDX"},
	"meme":      {"DOGE", "SHIB", "PEPE", "WIF", "BONK"},
	"ai":        {"FET", "RNDR", "TAO", "NEAR"},
}

// Confidence scaling bounds. Mirrors config.py.
var (
	MinConfidenceFactor = mustDec("0.3") // minimum 30% of normal risk
	MaxConfidenceFactor = mustDec("1.0")
)

// HLMaintenanceMargin is HL's approximate maintenance margin (0.5%). Mirrors
// config.py:HL_MAINTENANCE_MARGIN.
var HLMaintenanceMargin = mustDec("0.005")
