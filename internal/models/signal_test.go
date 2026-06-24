package models_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
)

// dec is a terse decimal literal for tests.
func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// decP is a terse *decimal literal for optional fields.
func decP(s string) *decimal.Decimal { return models.Ptr(dec(s)) }

func TestTradingSignal(t *testing.T) {
	t.Run("U-SIG-01 valid long limit", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, "BTC", s.Pair)
		assert.Equal(t, models.Long, s.Side)
		assert.True(t, s.IsBuy())
	})

	t.Run("U-SIG-02 valid short market", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "ETH", Side: models.Short, OrderType: models.Market, Size: dec("1.0"),
		})
		require.NoError(t, err)
		assert.Equal(t, "ETH", s.Pair)
		assert.Equal(t, models.Short, s.Side)
		assert.False(t, s.IsBuy())
		assert.True(t, s.IsMarket())
	})

	t.Run("U-SIG-03 normalize -USD suffix", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC-USD", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, "BTC", s.Pair)
	})

	t.Run("U-SIG-04 normalize to uppercase", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "btc", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, "BTC", s.Pair)
	})

	t.Run("normalize -PERP suffix", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "ETH-PERP", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, "ETH", s.Pair)
	})

	t.Run("U-SIG-05 limit without entry fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit, Size: dec("0.1"),
		})
		require.ErrorContains(t, err, "entry_price is required")
	})

	t.Run("U-SIG-06 negative size fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("-0.1"),
		})
		require.Error(t, err)
	})

	t.Run("U-SIG-07 zero size fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0"),
		})
		require.Error(t, err)
	})

	t.Run("U-SIG-08 invalid side fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.OrderSide("buy"), OrderType: models.Market, Size: dec("0.1"),
		})
		require.Error(t, err)
	})

	t.Run("U-SIG-09 default leverage is 5", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, 5, s.Leverage)
	})

	t.Run("U-SIG-10 SL above entry for long fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("100"), StopLoss: decP("110"), Size: dec("0.1"),
		})
		require.ErrorContains(t, err, "stop_loss must be below")
	})

	t.Run("U-SIG-11 SL below entry for short fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Short, OrderType: models.Limit,
			EntryPrice: decP("100"), StopLoss: decP("90"), Size: dec("0.1"),
		})
		require.ErrorContains(t, err, "stop_loss must be above")
	})

	t.Run("U-SIG-12 TP below entry for long fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("100"), TakeProfit: decP("90"), Size: dec("0.1"),
		})
		require.ErrorContains(t, err, "take_profit must be above")
	})

	t.Run("U-SIG-13 TP above entry for short fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Short, OrderType: models.Limit,
			EntryPrice: decP("100"), TakeProfit: decP("110"), Size: dec("0.1"),
		})
		require.ErrorContains(t, err, "take_profit must be below")
	})

	t.Run("U-SIG-14 leverage exceeds max fails", func(t *testing.T) {
		_, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.1"), Leverage: 100,
		})
		require.Error(t, err)
	})

	t.Run("U-SIG-15 full signal with all fields", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), Leverage: 10,
			StopLoss: decP("66000"), TakeProfit: decP("70000"),
		})
		require.NoError(t, err)
		assert.Equal(t, "BTC", s.Pair)
		assert.Equal(t, 10, s.Leverage)
		assert.True(t, s.StopLoss.Equal(dec("66000")))
		assert.True(t, s.TakeProfit.Equal(dec("70000")))
	})

	t.Run("default horizon is intraday", func(t *testing.T) {
		s, err := models.NewTradingSignal(models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		require.NoError(t, err)
		assert.Equal(t, models.HorizonIntraday, s.Horizon)
	})
}
