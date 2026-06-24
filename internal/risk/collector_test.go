package risk_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

// fakeStore is an in-memory TradeResultStore for collector tests.
type fakeStore struct {
	saved   []models.TradeResult
	nextID  int64
	network string
}

func (s *fakeStore) SaveTradeResult(result models.TradeResult, network string) (int64, error) {
	s.nextID++
	s.network = network
	s.saved = append(s.saved, result)
	return s.nextID, nil
}

func (s *fakeStore) GetRecentTradeResults(network string, limit int) ([]models.TradeResult, error) {
	return s.saved, nil
}

// fakeFills returns canned fills as raw JSON.
type fakeFills struct{ fills []json.RawMessage }

func (f *fakeFills) GetUserFills(ctx context.Context, address string, limit int) ([]json.RawMessage, error) {
	return f.fills, nil
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestCollectorRecordClose(t *testing.T) {
	store := &fakeStore{}
	c := risk.NewCollector(store, "testnet").
		WithClock(func() time.Time { return fixedNow })

	pos := models.Position{
		Coin:       "BTC",
		Size:       decimal.RequireFromString("0.1"), // long
		EntryPrice: decimal.NewFromInt(50000),
		OpenedAt:   models.Ptr(fixedNow.Add(-2 * time.Hour)),
	}
	res, err := c.RecordClose(pos, decimal.NewFromInt(51000), decimal.NewFromInt(5), decimal.Zero, models.Ptr(int64(7)))
	require.NoError(t, err)

	// gross = (51000-50000)*0.1 = 100; net = 100 - 5 - 0 = 95.
	assert.True(t, res.Pnl.Equal(decimal.NewFromInt(95)), "pnl=%s", res.Pnl)
	assert.Equal(t, "long", res.Side)
	require.NotNil(t, res.ID)
	assert.Equal(t, int64(1), *res.ID)
	require.NotNil(t, res.SignalID)
	assert.Equal(t, int64(7), *res.SignalID)
	assert.Len(t, store.saved, 1)
	assert.Equal(t, "testnet", store.network)
}

func TestCollectorRecordCloseShort(t *testing.T) {
	store := &fakeStore{}
	c := risk.NewCollector(store, "mainnet").WithClock(func() time.Time { return fixedNow })

	pos := models.Position{
		Coin:       "ETH",
		Size:       decimal.RequireFromString("-2"), // short
		EntryPrice: decimal.NewFromInt(3000),
	}
	// gross = (3000-2900)*2 = 200; net = 200 - 4 - 1 = 195.
	res, err := c.RecordClose(pos, decimal.NewFromInt(2900), decimal.NewFromInt(4), decimal.NewFromInt(1), nil)
	require.NoError(t, err)
	assert.Equal(t, "short", res.Side)
	assert.True(t, res.Pnl.Equal(decimal.NewFromInt(195)), "pnl=%s", res.Pnl)
	assert.True(t, res.Size.Equal(decimal.NewFromInt(2)))
}

func TestCollectorCollectFromFills(t *testing.T) {
	store := &fakeStore{}
	c := risk.NewCollector(store, "testnet").WithClock(func() time.Time { return fixedNow })

	closeMs := fixedNow.Add(-time.Hour).UnixMilli()
	openMs := fixedNow.Add(-3 * time.Hour).UnixMilli()
	fills := &fakeFills{fills: []json.RawMessage{
		// Closing fill (has closedPnl).
		rawJSON(t, map[string]any{
			"coin": "BTC", "px": "51000", "sz": "0.1", "side": "B",
			"time": closeMs, "oid": 111, "closedPnl": "100", "fee": "5",
			"startPosition": map[string]any{"entryPx": "50000", "time": openMs},
		}),
		// Non-closing fill (no closedPnl) -> skipped.
		rawJSON(t, map[string]any{
			"coin": "ETH", "px": "3000", "sz": "1", "side": "A",
			"time": closeMs, "oid": 222, "fee": "2",
		}),
	}}

	results, err := c.CollectFromFills(context.Background(), fills, "0xabc", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	r := results[0]
	assert.Equal(t, "BTC", r.Coin)
	assert.Equal(t, "long", r.Side)
	assert.True(t, r.EntryPrice.Equal(decimal.NewFromInt(50000)))
	assert.True(t, r.Pnl.Equal(decimal.NewFromInt(100)))
	require.NotNil(t, r.ID)

	// Re-collecting dedups the already-recorded fill.
	again, err := c.CollectFromFills(context.Background(), fills, "0xabc", nil)
	require.NoError(t, err)
	assert.Len(t, again, 0)
}

func TestCollectorCollectFromFillsSinceFilter(t *testing.T) {
	store := &fakeStore{}
	c := risk.NewCollector(store, "testnet").WithClock(func() time.Time { return fixedNow })

	oldMs := fixedNow.Add(-48 * time.Hour).UnixMilli()
	fills := &fakeFills{fills: []json.RawMessage{
		rawJSON(t, map[string]any{
			"coin": "BTC", "px": "51000", "sz": "0.1", "side": "B",
			"time": oldMs, "oid": 1, "closedPnl": "100", "fee": "5",
		}),
	}}

	since := fixedNow.Add(-24 * time.Hour)
	results, err := c.CollectFromFills(context.Background(), fills, "0xabc", &since)
	require.NoError(t, err)
	assert.Len(t, results, 0) // filtered out (older than since)
}

func TestCollectorGetRecentResults(t *testing.T) {
	store := &fakeStore{saved: []models.TradeResult{{Coin: "BTC"}, {Coin: "ETH"}}}
	c := risk.NewCollector(store, "testnet")
	got, err := c.GetRecentResults(50)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}
