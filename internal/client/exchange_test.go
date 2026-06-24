package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/models"
)

func fixedExchange(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/exchange", r.URL.Path)
		w.Write([]byte(body))
	}))
}

func TestPlaceOrderFilled(t *testing.T) {
	srv := fixedExchange(t, `{"status":"ok","response":{"data":{"statuses":[{"filled":{"oid":42,"totalSz":"0.1","avgPx":"50010"}}]}}}`)
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage)
	res := ex.PlaceOrder(context.Background(), 0, true, decimal.RequireFromString("0.1"), decimal.RequireFromString("50000"), models.Limit, false, nil)

	require.True(t, res.Success)
	assert.True(t, res.IsFilled())
	require.NotNil(t, res.OrderID)
	assert.Equal(t, int64(42), *res.OrderID)
	assert.True(t, res.FilledSize.Equal(decimal.RequireFromString("0.1")))
	require.NotNil(t, res.AvgPrice)
	assert.True(t, res.AvgPrice.Equal(decimal.RequireFromString("50010")))
}

func TestPlaceOrderResting(t *testing.T) {
	srv := fixedExchange(t, `{"status":"ok","response":{"data":{"statuses":[{"resting":{"oid":7}}]}}}`)
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage)
	res := ex.PlaceOrder(context.Background(), 0, false, decimal.RequireFromString("1"), decimal.RequireFromString("100"), models.Limit, false, nil)

	require.True(t, res.Success)
	assert.Equal(t, models.StatusOpen, res.Status)
	require.NotNil(t, res.OrderID)
	assert.Equal(t, int64(7), *res.OrderID)
}

func TestPlaceOrderRejected(t *testing.T) {
	srv := fixedExchange(t, `{"status":"ok","response":{"data":{"statuses":[{"error":"Order price too aggressive"}]}}}`)
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage)
	res := ex.PlaceOrder(context.Background(), 0, true, decimal.RequireFromString("1"), decimal.RequireFromString("100"), models.Limit, false, nil)

	assert.False(t, res.Success)
	assert.Equal(t, models.StatusRejected, res.Status)
	assert.Equal(t, "Order price too aggressive", res.Error)
}

func TestPlaceOrderFromSignalGroupedPlaceholders(t *testing.T) {
	srv := fixedExchange(t, `{"status":"ok","response":{"data":{"statuses":[{"resting":{"oid":1}}]}}}`)
	defer srv.Close()

	sig, err := models.NewTradingSignal(models.SignalParams{
		Pair:       "BTC",
		Side:       models.Long,
		OrderType:  models.Limit,
		Size:       decimal.RequireFromString("0.1"),
		Leverage:   5,
		EntryPrice: models.Ptr(decimal.RequireFromString("50000")),
		StopLoss:   models.Ptr(decimal.RequireFromString("48000")),
		TakeProfit: models.Ptr(decimal.RequireFromString("55000")),
	})
	require.NoError(t, err)

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage)
	results, err := ex.PlaceOrderFromSignal(context.Background(), sig, 0, nil, nil, 3)
	require.NoError(t, err)

	// entry + SL + TP → 3 results, the latter two OPEN placeholders.
	require.Len(t, results, 3)
	assert.True(t, results[0].Success)
	assert.Equal(t, models.StatusOpen, results[1].Status)
	assert.Equal(t, models.StatusOpen, results[2].Status)
}

func TestCancelOrder(t *testing.T) {
	srv := fixedExchange(t, `{"status":"ok"}`)
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage)
	assert.True(t, ex.CancelOrder(context.Background(), 0, 42, nil))
}

func TestCancelOrderServerErrorReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage, fastRetry(), client.WithMaxRetries(0))
	assert.False(t, ex.CancelOrder(context.Background(), 0, 42, nil))
}
