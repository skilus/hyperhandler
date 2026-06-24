package risk_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

// Behavioral edge cases not covered by the numeric golden vectors.

func TestCalculateATRInsufficientCandles(t *testing.T) {
	calc := mediumCalc()
	_, err := calc.CalculateATR([]risk.Candle{{High: decimal.NewFromInt(101), Low: decimal.NewFromInt(99), Close: decimal.NewFromInt(100)}}, 14)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 candles")
}

func TestRoundDownNegativeDecimalsPanics(t *testing.T) {
	assert.Panics(t, func() {
		risk.RoundDownForTest(decimal.RequireFromString("1.23"), -1)
	})
}

func TestPositionSizeZeroStopReject(t *testing.T) {
	calc := mediumCalc()
	_, reject := calc.CalculatePositionSize(risk.PositionSizeInput{
		AccountValue:     decimal.NewFromInt(10000),
		AvailableBalance: decimal.NewFromInt(5000),
		EntryPrice:       decimal.NewFromInt(100),
		StopPrice:        decimal.NewFromInt(100), // zero distance
		Leverage:         10,
		SzDecimals:       2,
		RiskMultiplier:   decimal.NewFromInt(1),
	})
	require.NotNil(t, reject)
	assert.Equal(t, models.RejectATRUnavailable, reject.Reason)
}

func TestCorrelationGroupsViaCumulative(t *testing.T) {
	calc := mediumCalc()
	// DOGE (meme) existing; AAVE (defi) added as new risk -> distinct groups.
	r := calc.CalculateCumulativeRisk(
		[]models.Position{{Coin: "DOGE", RiskAmount: models.Ptr(decimal.NewFromInt(50))}},
		decimal.NewFromInt(50), "AAVE", decimal.NewFromInt(10000),
	)
	assert.Equal(t, []string{"DOGE"}, r.CorrelationGroups["meme"])
	assert.Contains(t, r.CorrelationGroups["defi"], "AAVE")
}
