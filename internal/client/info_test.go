package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/client"
)

// jsonServer replies with a fixed body keyed by the request's "type" field.
func jsonServer(t *testing.T, byType map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(b, &req)
		body, ok := byType[req.Type]
		if !ok {
			t.Errorf("unexpected request type %q", req.Type)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
}

func TestGetMetaAndAssetIndex(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"meta": `{"universe":[{"name":"BTC","szDecimals":3,"maxLeverage":50},{"name":"ETH","szDecimals":4,"maxLeverage":25}]}`,
	})
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL))
	ctx := context.Background()

	idx, err := c.GetAssetIndex(ctx, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 1, idx)

	info, err := c.GetAssetInfo(ctx, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 3, info.SzDecimals)

	_, err = c.GetAssetIndex(ctx, "DOGE")
	var notFound *client.AssetNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestGetPositionsSkipsZero(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"clearinghouseState": `{
			"marginSummary":{"accountValue":"1000.5","totalMarginUsed":"100","totalNtlPos":"500"},
			"withdrawable":"900.5",
			"assetPositions":[
				{"position":{"coin":"BTC","szi":"0.5","entryPx":"50000","positionValue":"25000","unrealizedPnl":"100","leverage":{"type":"cross","value":10},"liquidationPx":"45000"}},
				{"position":{"coin":"ETH","szi":"0","entryPx":"3000","positionValue":"0","unrealizedPnl":"0","leverage":{"type":"isolated","value":5}}}
			]
		}`,
	})
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL))
	positions, err := c.GetPositions(context.Background(), "0xabc")
	require.NoError(t, err)
	require.Len(t, positions, 1, "zero-size position must be skipped")

	p := positions[0]
	assert.Equal(t, "BTC", p.Coin)
	assert.True(t, p.IsLong())
	assert.Equal(t, 10, p.Leverage)
	assert.Equal(t, "cross", p.LeverageType)
	require.NotNil(t, p.LiquidationPx)
	assert.True(t, p.LiquidationPx.Equal(decimal.RequireFromString("45000")))
}

func TestGetMarginSummary(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"clearinghouseState": `{"marginSummary":{"accountValue":"1000.5","totalMarginUsed":"100","totalNtlPos":"500","totalRawUsd":"1000.5"},"withdrawable":"900","assetPositions":[]}`,
	})
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL))
	m, err := c.GetMarginSummary(context.Background(), "0xabc")
	require.NoError(t, err)
	assert.True(t, m.AccountValue.Equal(decimal.RequireFromString("1000.5")))
	assert.True(t, m.TotalNtlPos.Equal(decimal.RequireFromString("500")))
}

func TestGetOpenOrders(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"openOrders": `[{"coin":"BTC","oid":123,"side":"B","limitPx":"49000","sz":"0.2","timestamp":1700000000000}]`,
	})
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL))
	orders, err := c.GetOpenOrders(context.Background(), "0xabc")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, int64(123), orders[0].OrderID)
	assert.True(t, orders[0].IsBuy())
	assert.True(t, orders[0].Price.Equal(decimal.RequireFromString("49000")))
}

func TestGetMidPriceNotFound(t *testing.T) {
	srv := jsonServer(t, map[string]string{"allMids": `{"BTC":"50000"}`})
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL))
	_, err := c.GetMidPrice(context.Background(), "ETH")
	var notFound *client.AssetNotFoundError
	assert.ErrorAs(t, err, &notFound)
}
