package risk_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

func TestRiskProfileValues(t *testing.T) {
	low := risk.GetProfile(models.RiskLow)
	med := risk.GetProfile(models.RiskMedium)
	high := risk.GetProfile(models.RiskHigh)

	// risk_per_trade increases with risk level.
	assert.True(t, low.RiskPerTrade.LessThan(med.RiskPerTrade))
	assert.True(t, med.RiskPerTrade.LessThan(high.RiskPerTrade))

	// max positions / leverage widen with risk level.
	assert.Equal(t, 3, low.MaxOpenPositions)
	assert.Equal(t, 5, med.MaxOpenPositions)
	assert.Equal(t, 8, high.MaxOpenPositions)
	assert.Equal(t, 5, low.MaxLeverage)
	assert.Equal(t, 10, med.MaxLeverage)
	assert.Equal(t, 20, high.MaxLeverage)

	// circuit-breaker thresholds.
	assert.Equal(t, 2, low.SoftStopLosses)
	assert.Equal(t, 4, low.HardStopLosses)
	assert.Equal(t, 3, med.SoftStopLosses)
	assert.Equal(t, 5, med.HardStopLosses)
	assert.Equal(t, 6, high.HardStopLosses)
}

func TestGetProfileUnknownFallsBackToMedium(t *testing.T) {
	got := risk.GetProfile(models.RiskLevel("bogus"))
	assert.Equal(t, risk.GetProfile(models.RiskMedium), got)
}

func TestDefaultHLConfig(t *testing.T) {
	cfg := risk.DefaultHLConfig()
	assert.True(t, cfg.TakerFee.Equal(decimal.RequireFromString("0.00045")))
	assert.True(t, cfg.MinOrderValue.Equal(decimal.RequireFromString("10.0")))
	assert.Equal(t, "cross", cfg.DefaultMarginMode)
	assert.Equal(t, 300, cfg.MaxSignalAgeSecond)
}

func TestATRSettings(t *testing.T) {
	assert.Equal(t, "15m", risk.ATRSettings[models.HorizonScalp].Interval)
	assert.Equal(t, "1d", risk.ATRSettings[models.HorizonPosition].Interval)
	assert.True(t, risk.ATRSettings[models.HorizonScalp].Multiplier.
		LessThan(risk.ATRSettings[models.HorizonPosition].Multiplier))
}
