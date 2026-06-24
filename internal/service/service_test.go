package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

func TestParseSignalValid(t *testing.T) {
	data := []byte(`{"pair":"btc-usd","side":"long","order_type":"market","size":"0.5","leverage":10}`)
	sig, err := ParseSignal(data)
	if err != nil {
		t.Fatalf("ParseSignal: %v", err)
	}
	if sig.Pair != "BTC" {
		t.Errorf("pair = %q, want BTC (normalized)", sig.Pair)
	}
	if sig.Leverage != 10 {
		t.Errorf("leverage = %d, want 10", sig.Leverage)
	}
	if sig.Horizon != models.HorizonIntraday {
		t.Errorf("horizon = %q, want intraday default", sig.Horizon)
	}
}

func TestParseSignalNumericSize(t *testing.T) {
	sig, err := ParseSignal([]byte(`{"pair":"ETH","side":"short","order_type":"market","size":1.25}`))
	if err != nil {
		t.Fatalf("ParseSignal: %v", err)
	}
	if !sig.Size.Equal(decimal.RequireFromString("1.25")) {
		t.Errorf("size = %s, want 1.25", sig.Size)
	}
	if sig.Leverage != models.DefaultLeverage {
		t.Errorf("leverage = %d, want default %d", sig.Leverage, models.DefaultLeverage)
	}
}

func TestParseSignalInvalid(t *testing.T) {
	cases := map[string]string{
		"bad side":       `{"pair":"BTC","side":"up","order_type":"market","size":"1"}`,
		"zero size":      `{"pair":"BTC","side":"long","order_type":"market","size":"0"}`,
		"limit no entry": `{"pair":"BTC","side":"long","order_type":"limit","size":"1"}`,
		"malformed json": `{not json`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSignal([]byte(body)); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}
}

func TestRecentConsecutiveLosses(t *testing.T) {
	loss := models.TradeResult{Pnl: decimal.RequireFromString("-1")}
	win := models.TradeResult{Pnl: decimal.RequireFromString("1")}
	// Storage order is DESC (newest first); the trailing streak is counted from
	// the oldest (last) element until a non-loss.
	history := []models.TradeResult{loss, loss, win, loss, loss} // oldest=loss,loss
	if got := RecentConsecutiveLosses(history); got != 2 {
		t.Errorf("RecentConsecutiveLosses = %d, want 2", got)
	}
	if got := RecentConsecutiveLosses(nil); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}

// fakeMarketData implements MarketDataClient with canned values, enough to push
// a MANUAL-mode signal through the evaluator.
type fakeMarketData struct {
	calls []string
}

func (f *fakeMarketData) GetAccountState(ctx context.Context, address string) (*client.AccountState, error) {
	f.calls = append(f.calls, "account")
	return &client.AccountState{
		MarginSummary: client.MarginSummary{AccountValue: decimal.RequireFromString("10000")},
		Withdrawable:  decimal.RequireFromString("9000"),
	}, nil
}

func (f *fakeMarketData) GetPositions(ctx context.Context, address string) ([]models.Position, error) {
	return nil, nil
}

func (f *fakeMarketData) GetAssetInfo(ctx context.Context, symbol string) (client.AssetMeta, error) {
	return client.AssetMeta{Name: symbol, SzDecimals: 3, MaxLeverage: 50}, nil
}

func (f *fakeMarketData) GetAssetIndex(ctx context.Context, symbol string) (int, error) {
	return 0, nil
}

func (f *fakeMarketData) GetMidPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.RequireFromString("50000"), nil
}

func (f *fakeMarketData) GetFundingRate(ctx context.Context, symbol string) (decimal.Decimal, error) {
	return decimal.RequireFromString("0.00001"), nil
}

func (f *fakeMarketData) GetCandles(ctx context.Context, symbol, interval string, lookback int) ([]client.Candle, error) {
	f.calls = append(f.calls, "candles:"+interval)
	return nil, nil
}

type fakeDecisionStore struct{ saved int }

func (s *fakeDecisionStore) SaveRiskDecision(d models.RiskDecisionLog, network string) (int64, error) {
	s.saved++
	return 1, nil
}

func TestEvaluateSignalManualPersistsDecision(t *testing.T) {
	sig, err := ParseSignal([]byte(`{"pair":"BTC","side":"long","order_type":"market","size":"0.01","leverage":5,"stop_loss":"49000"}`))
	if err != nil {
		t.Fatal(err)
	}
	mgr := risk.NewManager(models.RiskMedium, models.ModeManual, nil)
	fake := &fakeMarketData{}
	store := &fakeDecisionStore{}

	order, reject, err := EvaluateSignal(context.Background(), mgr, fake, sig, "0xabc", nil, store, "testnet")
	if err != nil {
		t.Fatalf("EvaluateSignal: %v", err)
	}
	if reject != nil {
		t.Fatalf("unexpected reject: %+v", reject)
	}
	if order == nil {
		t.Fatal("expected an order")
	}
	if store.saved != 1 {
		t.Errorf("decision log saved %d times, want 1", store.saved)
	}
	// With a stop_loss present and MANUAL mode, candles must NOT be fetched.
	for _, c := range fake.calls {
		if len(c) >= 7 && c[:7] == "candles" {
			t.Errorf("unexpected candle fetch in manual mode with stop loss: %v", fake.calls)
		}
	}
}

func TestEvaluateSignalManagedFetchesCandles(t *testing.T) {
	sig, _ := ParseSignal([]byte(`{"pair":"BTC","side":"long","order_type":"market","size":"0.01","leverage":5,"horizon":"swing"}`))
	mgr := risk.NewManager(models.RiskMedium, models.ModeManaged, nil)
	fake := &fakeMarketData{}

	_, _, err := EvaluateSignal(context.Background(), mgr, fake, sig, "0xabc", nil, nil, "testnet")
	if err != nil {
		t.Fatalf("EvaluateSignal: %v", err)
	}
	found := false
	for _, c := range fake.calls {
		if c == "candles:4h" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected candles:4h fetch (swing horizon), got %v", fake.calls)
	}
}
