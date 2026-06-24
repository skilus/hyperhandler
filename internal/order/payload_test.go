package order_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/order"
	"github.com/skilus/hyperhandler/internal/signer"
)

func dec(s string) decimal.Decimal   { return decimal.RequireFromString(s) }
func decP(s string) *decimal.Decimal { return models.Ptr(dec(s)) }
func builder() *order.Builder        { return order.NewBuilder(dec("0.005")) }

// get fetches a key from an OrderedMap, failing the test if absent.
func get(t *testing.T, m *signer.OrderedMap, key string) any {
	t.Helper()
	v, ok := m.Get(key)
	require.Truef(t, ok, "key %q missing", key)
	return v
}

// om asserts the value is an *OrderedMap and returns it.
func om(t *testing.T, v any) *signer.OrderedMap {
	t.Helper()
	m, ok := v.(*signer.OrderedMap)
	require.True(t, ok, "expected OrderedMap, got %T", v)
	return m
}

// orders returns the orders slice from a payload.
func orders(t *testing.T, payload *signer.OrderedMap) []any {
	t.Helper()
	v := get(t, payload, "orders")
	xs, ok := v.([]any)
	require.True(t, ok, "orders not a slice")
	return xs
}

func mustSignal(t *testing.T, p models.SignalParams) *models.TradingSignal {
	t.Helper()
	s, err := models.NewTradingSignal(p)
	require.NoError(t, err)
	return s
}

func TestBuildOrderPayload(t *testing.T) {
	t.Run("U-BLD-01 limit long order", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), Leverage: 5,
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)

		os := orders(t, payload)
		require.Len(t, os, 1)
		o := om(t, os[0])
		assert.Equal(t, true, get(t, o, "b"))
		assert.Equal(t, false, get(t, o, "r"))
		tlimit := om(t, get(t, om(t, get(t, o, "t")), "limit"))
		assert.Equal(t, "Gtc", get(t, tlimit, "tif"))
	})

	t.Run("U-BLD-02 limit short order", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Short, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.Equal(t, false, get(t, o, "b"))
		tlimit := om(t, get(t, om(t, get(t, o, "t")), "limit"))
		assert.Equal(t, "Gtc", get(t, tlimit, "tif"))
	})

	t.Run("U-BLD-03 market long order", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "ETH", Side: models.Long, OrderType: models.Market, Size: dec("1.0"), Leverage: 10,
		})
		payload, err := builder().BuildOrderPayload(sig, 1, decP("3500"), 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.Equal(t, true, get(t, o, "b"))
		tlimit := om(t, get(t, om(t, get(t, o, "t")), "limit"))
		assert.Equal(t, "Ioc", get(t, tlimit, "tif"))
	})

	t.Run("U-BLD-04 market short order", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "ETH", Side: models.Short, OrderType: models.Market, Size: dec("1.0"),
		})
		payload, err := builder().BuildOrderPayload(sig, 1, decP("3500"), 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.Equal(t, false, get(t, o, "b"))
		tlimit := om(t, get(t, om(t, get(t, o, "t")), "limit"))
		assert.Equal(t, "Ioc", get(t, tlimit, "tif"))
	})

	t.Run("U-BLD-05 SL for long position", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), StopLoss: decP("66000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		os := orders(t, payload)
		require.Len(t, os, 2)
		sl := om(t, os[1])
		assert.Equal(t, false, get(t, sl, "b")) // sell to close long
		assert.Equal(t, true, get(t, sl, "r"))
		trig := om(t, get(t, om(t, get(t, sl, "t")), "trigger"))
		assert.Equal(t, "sl", get(t, trig, "tpsl"))
		assert.Equal(t, "66000", get(t, trig, "triggerPx"))
	})

	t.Run("U-BLD-06 SL for short position", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Short, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), StopLoss: decP("69000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		sl := om(t, orders(t, payload)[1])
		assert.Equal(t, true, get(t, sl, "b")) // buy to close short
		trig := om(t, get(t, om(t, get(t, sl, "t")), "trigger"))
		assert.Equal(t, "sl", get(t, trig, "tpsl"))
	})

	t.Run("U-BLD-07 TP for long position", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), TakeProfit: decP("70000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		tp := om(t, orders(t, payload)[1])
		assert.Equal(t, false, get(t, tp, "b")) // sell to close long
		trig := om(t, get(t, om(t, get(t, tp, "t")), "trigger"))
		assert.Equal(t, "tp", get(t, trig, "tpsl"))
		assert.Equal(t, "70000", get(t, trig, "triggerPx"))
	})

	t.Run("U-BLD-08 TP for short position", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Short, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), TakeProfit: decP("65000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		tp := om(t, orders(t, payload)[1])
		assert.Equal(t, true, get(t, tp, "b")) // buy to close short
		trig := om(t, get(t, om(t, get(t, tp, "t")), "trigger"))
		assert.Equal(t, "tp", get(t, trig, "tpsl"))
	})

	t.Run("U-BLD-09 entry+SL+TP -> normalTpsl, 3 orders", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), StopLoss: decP("66000"), TakeProfit: decP("70000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		assert.Equal(t, "normalTpsl", get(t, payload, "grouping"))
		assert.Len(t, orders(t, payload), 3)
	})

	t.Run("U-BLD-10 entry only -> na grouping", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		assert.Equal(t, "na", get(t, payload, "grouping"))
		assert.Len(t, orders(t, payload), 1)
	})

	t.Run("U-BLD-11 slippage price for long is higher", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "ETH", Side: models.Long, OrderType: models.Market, Size: dec("1.0"), Leverage: 10,
		})
		payload, err := builder().BuildOrderPayload(sig, 1, decP("100"), 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.True(t, dec(get(t, o, "p").(string)).Equal(dec("100.50000"))) // 100 * 1.005
	})

	t.Run("U-BLD-12 slippage price for short is lower", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "ETH", Side: models.Short, OrderType: models.Market, Size: dec("1.0"),
		})
		payload, err := builder().BuildOrderPayload(sig, 1, decP("100"), 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.True(t, dec(get(t, o, "p").(string)).Equal(dec("99.50000"))) // 100 * 0.995
	})

	t.Run("U-BLD-13 asset index mapping", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit, EntryPrice: decP("67500"), Size: dec("0.1"),
		})
		payload, err := builder().BuildOrderPayload(sig, 5, nil, 0)
		require.NoError(t, err)
		o := om(t, orders(t, payload)[0])
		assert.Equal(t, 5, get(t, o, "a"))
	})

	t.Run("U-BLD-14 reduce_only for SL/TP", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Limit,
			EntryPrice: decP("67500"), Size: dec("0.1"), StopLoss: decP("66000"), TakeProfit: decP("70000"),
		})
		payload, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.NoError(t, err)
		os := orders(t, payload)
		assert.Equal(t, false, get(t, om(t, os[0]), "r"))
		assert.Equal(t, true, get(t, om(t, os[1]), "r"))
		assert.Equal(t, true, get(t, om(t, os[2]), "r"))
	})

	t.Run("market order without price fails", func(t *testing.T) {
		sig := mustSignal(t, models.SignalParams{
			Pair: "ETH", Side: models.Long, OrderType: models.Market, Size: dec("1.0"),
		})
		_, err := builder().BuildOrderPayload(sig, 1, nil, 0)
		require.ErrorContains(t, err, "Current price required")
	})

	t.Run("U-BLD-18 limit without entry price fails at build", func(t *testing.T) {
		// A signal that bypasses constructor validation (market created, mutated to limit).
		sig := mustSignal(t, models.SignalParams{
			Pair: "BTC", Side: models.Long, OrderType: models.Market, Size: dec("0.1"),
		})
		sig.OrderType = models.Limit
		sig.EntryPrice = nil
		_, err := builder().BuildOrderPayload(sig, 0, nil, 0)
		require.ErrorContains(t, err, "Entry price required")
	})
}

func TestBuildCancelPayload(t *testing.T) {
	payload := builder().BuildCancelPayload(0, 123456)
	assert.Equal(t, "cancel", get(t, payload, "type"))
	cancels := get(t, payload, "cancels").([]any)
	require.Len(t, cancels, 1)
	c := om(t, cancels[0])
	assert.Equal(t, 0, get(t, c, "a"))
	assert.Equal(t, int64(123456), get(t, c, "o"))
}

func TestBuildLeveragePayload(t *testing.T) {
	payload := builder().BuildLeveragePayload(0, 10, true)
	assert.Equal(t, "updateLeverage", get(t, payload, "type"))
	assert.Equal(t, 0, get(t, payload, "asset"))
	assert.Equal(t, 10, get(t, payload, "leverage"))
	assert.Equal(t, true, get(t, payload, "isCross"))
}
