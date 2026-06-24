package models_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
)

func defaultValidator() *models.SignalValidator {
	return models.NewSignalValidator(nil)
}

func strictValidator() *models.SignalValidator {
	return models.NewSignalValidator(&models.ValidationConfig{
		MaxPositionSizeUSD: dec("5000"),
		MaxLeverage:        10,
		RequireStopLoss:    true,
		AllowedPairs:       []string{"BTC", "ETH"},
		MinOrderSize:       dec("0.01"),
	})
}

func validSignal(t *testing.T) *models.TradingSignal {
	t.Helper()
	s, err := models.NewTradingSignal(models.SignalParams{
		Pair: "BTC", Side: models.Long, OrderType: models.Limit,
		EntryPrice: decP("67500"), Size: dec("0.1"), Leverage: 5, StopLoss: decP("66000"),
	})
	require.NoError(t, err)
	return s
}

func anyContains(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

func TestSignalValidator(t *testing.T) {
	t.Run("U-VAL-01 valid signal passes", func(t *testing.T) {
		res := defaultValidator().Validate(validSignal(t), decP("67500"))
		assert.True(t, res.Valid)
	})

	t.Run("U-VAL-02 size exceeds limit", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("1.0"), StopLoss: decP("66000"),
		})
		require.NoError(t, err)
		res := strictValidator().Validate(s, nil)
		assert.False(t, res.Valid)
		assert.True(t, anyContains(res.Errors, "exceeds maximum"))
	})

	t.Run("U-VAL-03 leverage within limit", func(t *testing.T) {
		res := strictValidator().Validate(validSignal(t), nil)
		assert.False(t, anyContains(res.Errors, "Leverage"))
	})

	t.Run("U-VAL-04 leverage exceeds limit", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("100"), Size: dec("0.01"), Leverage: 15, StopLoss: decP("90"),
		})
		require.NoError(t, err)
		res := strictValidator().Validate(s, nil)
		assert.False(t, res.Valid)
		assert.True(t, anyContains(res.Errors, "Leverage"))
	})

	t.Run("U-VAL-05 require SL without SL fails", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.01"),
		})
		require.NoError(t, err)
		res := strictValidator().Validate(s, decP("100"))
		assert.False(t, res.Valid)
		assert.True(t, anyContains(res.Errors, "Stop-loss is required"))
	})

	t.Run("U-VAL-06 SL optional when not required", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.01"),
		})
		require.NoError(t, err)
		res := defaultValidator().Validate(s, decP("100"))
		assert.True(t, res.Valid)
		assert.True(t, anyContains(res.Warnings, "No stop-loss"))
	})

	t.Run("U-VAL-07 pair in whitelist passes", func(t *testing.T) {
		res := strictValidator().Validate(validSignal(t), nil)
		assert.False(t, anyContains(res.Errors, "not in allowed list"))
	})

	t.Run("U-VAL-08 pair not in whitelist fails", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "SOL", Side: models.Long, OrderType: models.Market, Size: dec("0.01"), StopLoss: decP("90"),
		})
		require.NoError(t, err)
		res := strictValidator().Validate(s, decP("100"))
		assert.False(t, res.Valid)
		assert.True(t, anyContains(res.Errors, "not in allowed list"))
	})

	t.Run("U-VAL-09 empty whitelist allows all", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "OBSCURE", Side: models.Long, OrderType: models.Market, Size: dec("0.01"),
		})
		require.NoError(t, err)
		res := defaultValidator().Validate(s, decP("100"))
		assert.False(t, anyContains(res.Errors, "not in allowed list"))
	})

	t.Run("U-VAL-10 size below minimum fails", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.001"), StopLoss: decP("90"),
		})
		require.NoError(t, err)
		res := strictValidator().Validate(s, decP("100"))
		assert.False(t, res.Valid)
		assert.True(t, anyContains(res.Errors, "below minimum"))
	})

	t.Run("high leverage warning", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.01"), Leverage: 15,
		})
		require.NoError(t, err)
		res := defaultValidator().Validate(s, decP("100"))
		assert.True(t, anyContains(res.Warnings, "High leverage"))
	})

	t.Run("validate_or_raise success", func(t *testing.T) {
		assert.NoError(t, defaultValidator().ValidateOrRaise(validSignal(t), nil))
	})

	t.Run("validate_or_raise failure", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "SOL", Side: models.Long, OrderType: models.Market, Size: dec("0.001"),
		})
		require.NoError(t, err)
		err = strictValidator().ValidateOrRaise(s, decP("100"))
		require.ErrorContains(t, err, "validation failed")
	})
}
