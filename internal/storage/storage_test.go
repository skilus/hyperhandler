package storage_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/storage"
)

// newTempStore opens a fresh database in a temp dir. Ports the temp_db fixture.
func newTempStore(t *testing.T) *storage.Storage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sampleSignal ports the sample_signal fixture.
func sampleSignal(t *testing.T) *models.TradingSignal {
	t.Helper()
	sig, err := models.NewTradingSignal(models.SignalParams{
		Pair:       "BTC",
		Side:       models.Long,
		OrderType:  models.Limit,
		EntryPrice: models.Ptr(decimal.RequireFromString("67500")),
		Size:       decimal.RequireFromString("0.1"),
		Leverage:   5,
		StopLoss:   models.Ptr(decimal.RequireFromString("66000")),
	})
	require.NoError(t, err)
	return sig
}

// sampleResult ports the sample_result fixture.
func sampleResult() models.OrderResult {
	return models.OrderResult{
		Success:    true,
		OrderID:    models.Ptr(int64(123456)),
		FilledSize: decimal.RequireFromString("0.1"),
		AvgPrice:   models.Ptr(decimal.RequireFromString("67500")),
		Status:     models.StatusFilled,
	}
}

func TestSaveSignal(t *testing.T) {
	s := newTempStore(t)
	id, err := s.SaveSignal(sampleSignal(t), "testnet", true, false)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestGetSignal(t *testing.T) {
	s := newTempStore(t)
	id, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	rec, err := s.GetSignal(id)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "BTC", rec.Pair)
	assert.Equal(t, "long", rec.Side)
}

func TestGetSignalNotFound(t *testing.T) {
	s := newTempStore(t)
	rec, err := s.GetSignal(999)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestSaveOrder(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	orderID, err := s.SaveOrder(
		&signalID, "testnet", "BTC", "long", "limit",
		decimal.RequireFromString("0.1"),
		models.Ptr(decimal.RequireFromString("67500")),
		sampleResult(), nil,
	)
	require.NoError(t, err)
	assert.Greater(t, orderID, int64(0))
}

func TestGetOrdersBySignal(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	_, err = s.SaveOrder(
		&signalID, "testnet", "BTC", "long", "entry",
		decimal.RequireFromString("0.1"),
		models.Ptr(decimal.RequireFromString("67500")),
		sampleResult(), nil,
	)
	require.NoError(t, err)

	orders, err := s.GetOrdersBySignal(signalID)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "BTC", orders[0].Pair)
}

func TestUpdateSignalExecuted(t *testing.T) {
	s := newTempStore(t)
	id, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	require.NoError(t, s.UpdateSignalExecuted(id, true))

	rec, err := s.GetSignal(id)
	require.NoError(t, err)
	assert.True(t, rec.Executed)
}

func TestGetRecentSignals(t *testing.T) {
	s := newTempStore(t)
	for i := 0; i < 5; i++ {
		_, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
		require.NoError(t, err)
	}
	signals, err := s.GetRecentSignals(3, nil, nil)
	require.NoError(t, err)
	assert.Len(t, signals, 3)
}

func TestGetRecentSignalsWithFilter(t *testing.T) {
	s := newTempStore(t)
	_, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)
	_, err = s.SaveSignal(sampleSignal(t), "mainnet", false, false)
	require.NoError(t, err)

	signals, err := s.GetRecentSignals(50, models.Ptr("testnet"), nil)
	require.NoError(t, err)
	require.NotEmpty(t, signals)
	for _, sig := range signals {
		assert.Equal(t, "testnet", sig.Network)
	}
}

func TestGetRecentOrders(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err := s.SaveOrder(
			&signalID, "testnet", "BTC", "long", "entry",
			decimal.RequireFromString("0.1"),
			models.Ptr(decimal.RequireFromString("67500")),
			sampleResult(), nil,
		)
		require.NoError(t, err)
	}

	orders, err := s.GetRecentOrders(3, nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, orders, 3)
}

func TestGetRecentOrdersWithFilters(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)
	_, err = s.SaveOrder(&signalID, "testnet", "BTC", "long", "entry",
		decimal.RequireFromString("0.1"), models.Ptr(decimal.RequireFromString("67500")),
		sampleResult(), nil)
	require.NoError(t, err)
	_, err = s.SaveOrder(&signalID, "mainnet", "ETH", "short", "entry",
		decimal.RequireFromString("1"), nil, sampleResult(), nil)
	require.NoError(t, err)

	orders, err := s.GetRecentOrders(50, models.Ptr("testnet"), models.Ptr("BTC"), models.Ptr("filled"))
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "BTC", orders[0].Pair)
	assert.Equal(t, "testnet", orders[0].Network)
}

func TestGetStats(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", true, true)
	require.NoError(t, err)
	_, err = s.SaveOrder(
		&signalID, "testnet", "BTC", "long", "entry",
		decimal.RequireFromString("0.1"),
		models.Ptr(decimal.RequireFromString("67500")),
		sampleResult(), nil,
	)
	require.NoError(t, err)

	stats, err := s.GetStats(nil)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Signals.Total)
	assert.Equal(t, 1, stats.Signals.Executed)
	assert.Equal(t, 1, stats.Orders.Total)
	assert.Equal(t, 1, stats.Orders.Filled)
}

func TestGetStatsWithNetworkFilter(t *testing.T) {
	s := newTempStore(t)
	_, err := s.SaveSignal(sampleSignal(t), "testnet", false, true)
	require.NoError(t, err)
	_, err = s.SaveSignal(sampleSignal(t), "mainnet", false, false)
	require.NoError(t, err)

	stats, err := s.GetStats(models.Ptr("testnet"))
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Signals.Total)
	assert.Equal(t, 1, stats.Signals.Executed)
}

func TestVaultAddressStored(t *testing.T) {
	s := newTempStore(t)
	signalID, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)
	vault := "0x1234567890123456789012345678901234567890"

	_, err = s.SaveOrder(
		&signalID, "testnet", "BTC", "long", "entry",
		decimal.RequireFromString("0.1"),
		models.Ptr(decimal.RequireFromString("67500")),
		sampleResult(), &vault,
	)
	require.NoError(t, err)

	orders, err := s.GetOrdersBySignal(signalID)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.NotNil(t, orders[0].VaultAddress)
	assert.Equal(t, vault, *orders[0].VaultAddress)
}

// --- Trade results + B.5 fill_id idempotency ---

func sampleTradeResult(fillID *string) models.TradeResult {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	return models.TradeResult{
		FillID:      fillID,
		Coin:        "BTC",
		Side:        "long",
		EntryPrice:  decimal.RequireFromString("50000"),
		ExitPrice:   decimal.RequireFromString("51000"),
		Size:        decimal.RequireFromString("0.1"),
		Pnl:         decimal.RequireFromString("100"),
		Fees:        decimal.RequireFromString("5"),
		FundingPaid: decimal.Zero,
		OpenedAt:    now.Add(-2 * time.Hour),
		ClosedAt:    now,
	}
}

func TestSaveAndGetTradeResult(t *testing.T) {
	s := newTempStore(t)
	id, err := s.SaveTradeResult(sampleTradeResult(nil), "testnet")
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	results, err := s.GetRecentTradeResults("testnet", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "BTC", results[0].Coin)
	assert.True(t, results[0].Pnl.Equal(decimal.RequireFromString("100")))
	assert.True(t, results[0].OpenedAt.Equal(sampleTradeResult(nil).OpenedAt))
}

func TestTradeResultFillIDIdempotent(t *testing.T) {
	s := newTempStore(t)
	fid := "12345_1700000000"

	id1, err := s.SaveTradeResult(sampleTradeResult(&fid), "testnet")
	require.NoError(t, err)

	// Re-saving the same fill_id (e.g. after a restart) must not duplicate.
	id2, err := s.SaveTradeResult(sampleTradeResult(&fid), "testnet")
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "same fill_id should return the existing row id")

	results, err := s.GetRecentTradeResults("testnet", 50)
	require.NoError(t, err)
	assert.Len(t, results, 1, "fill_id UNIQUE prevents duplicate rows")
}

func TestTradeResultNilFillIDAllowsDuplicates(t *testing.T) {
	s := newTempStore(t)
	// Manual closes carry no fill_id; NULLs are distinct under UNIQUE.
	_, err := s.SaveTradeResult(sampleTradeResult(nil), "testnet")
	require.NoError(t, err)
	_, err = s.SaveTradeResult(sampleTradeResult(nil), "testnet")
	require.NoError(t, err)

	results, err := s.GetRecentTradeResults("testnet", 50)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestGetRecentTradeResultsByCoin(t *testing.T) {
	s := newTempStore(t)
	btc := sampleTradeResult(nil)
	eth := sampleTradeResult(nil)
	eth.Coin = "ETH"
	_, err := s.SaveTradeResult(btc, "testnet")
	require.NoError(t, err)
	_, err = s.SaveTradeResult(eth, "testnet")
	require.NoError(t, err)

	results, err := s.GetRecentTradeResultsByCoin("testnet", 50, "ETH")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "ETH", results[0].Coin)
}

func TestSaveRiskDecision(t *testing.T) {
	s := newTempStore(t)
	decision := models.RiskDecisionLog{
		RiskMode:         models.ModeManaged,
		Coin:             "BTC",
		Side:             "long",
		Decision:         "approved",
		MarkPrice:        decimal.RequireFromString("50000"),
		FundingRate:      decimal.RequireFromString("0.0001"),
		DailyPnlPct:      decimal.Zero,
		AccountValue:     decimal.RequireFromString("10000"),
		AvailableBalance: decimal.RequireFromString("9000"),
		InputSize:        models.Ptr(decimal.RequireFromString("0.1")),
		OutputSize:       models.Ptr(decimal.RequireFromString("0.08")),
	}
	id, err := s.SaveRiskDecision(decision, "testnet")
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestSignalJSONRoundTrips(t *testing.T) {
	s := newTempStore(t)
	id, err := s.SaveSignal(sampleSignal(t), "testnet", false, false)
	require.NoError(t, err)

	rec, err := s.GetSignal(id)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Contains(t, rec.SignalJSON, "BTC")
	require.NotNil(t, rec.EntryPrice)
	assert.Equal(t, "67500", *rec.EntryPrice)
}
