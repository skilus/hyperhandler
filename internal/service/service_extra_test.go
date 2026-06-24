package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
	"github.com/skilus/hyperhandler/internal/signer"
	"github.com/skilus/hyperhandler/internal/storage"
)

// anvilKey is a well-known throwaway test key (anvil account #0). Never a secret.
const anvilKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func mustSignal(t *testing.T, body string) *models.TradingSignal {
	t.Helper()
	sig, err := ParseSignal([]byte(body))
	if err != nil {
		t.Fatalf("ParseSignal(%s): %v", body, err)
	}
	return sig
}

// --- ParseSignal file/reader -------------------------------------------------

func TestParseSignalFileAndReader(t *testing.T) {
	body := `{"pair":"btc","side":"long","order_type":"market","size":"0.5","leverage":3}`
	path := filepath.Join(t.TempDir(), "signal.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	sig, err := ParseSignalFile(path)
	if err != nil {
		t.Fatalf("ParseSignalFile: %v", err)
	}
	if sig.Pair != "BTC" {
		t.Errorf("pair = %q, want BTC", sig.Pair)
	}

	sig2, err := ParseSignalReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseSignalReader: %v", err)
	}
	if !sig2.Size.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("size = %s, want 0.5", sig2.Size)
	}
}

func TestParseSignalFileMissing(t *testing.T) {
	if _, err := ParseSignalFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- ValidatorFromConfig -----------------------------------------------------

func TestValidatorFromConfigOverrides(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Set("security", map[string]any{
		"max_position_size_usd": 1000,
		"max_leverage":          5,
		"require_stop_loss":     true,
	})

	v := ValidatorFromConfig(cfg)

	// Leverage 10 exceeds the override of 5 → invalid.
	overLev := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.001","leverage":10,"stop_loss":"49000"}`)
	if res := v.Validate(overLev, nil); res.Valid {
		t.Errorf("expected invalid for leverage over max, got %+v", res)
	}

	// Missing stop loss → invalid because require_stop_loss=true.
	noStop := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.001","leverage":3}`)
	if res := v.Validate(noStop, nil); res.Valid {
		t.Errorf("expected invalid for missing stop loss, got %+v", res)
	}
}

func TestValidatorFromConfigDefaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	// No security section → Python defaults apply.
	v := ValidatorFromConfig(cfg)
	sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.001","leverage":3}`)
	if res := v.Validate(sig, nil); !res.Valid {
		t.Errorf("expected valid under defaults, got %+v", res)
	}
}

// toInt is exercised through ValidatorFromConfig with different numeric types.
func TestValidatorFromConfigMaxLeverageTypes(t *testing.T) {
	for name, val := range map[string]any{
		"int":     int(2),
		"int64":   int64(2),
		"float64": float64(2),
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := config.Load("")
			cfg.Set("security", map[string]any{"max_leverage": val})
			v := ValidatorFromConfig(cfg)
			sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.001","leverage":5,"stop_loss":"49000"}`)
			if res := v.Validate(sig, nil); res.Valid {
				t.Errorf("leverage 5 should exceed max 2 (%s)", name)
			}
		})
	}
}

// --- WalletAndSigner ---------------------------------------------------------

func TestWalletAndSignerFromEnv(t *testing.T) {
	t.Setenv("HL_PRIVATE_KEY", anvilKey)
	_, s, err := WalletAndSigner("testnet", false)
	if err != nil {
		t.Fatalf("WalletAndSigner: %v", err)
	}
	if s == nil {
		t.Fatal("expected a signer when key present in env")
	}
	if !strings.HasPrefix(s.Address(), "0x") {
		t.Errorf("address = %q, want 0x-prefixed", s.Address())
	}
}

func TestWalletAndSignerNoKey(t *testing.T) {
	t.Setenv("HL_PRIVATE_KEY", "")
	t.Setenv("HL_TESTNET_PRIVATE_KEY", "")
	_, s, err := WalletAndSigner("testnet", false)
	if err != nil {
		t.Fatalf("WalletAndSigner returned error instead of nil signer: %v", err)
	}
	if s != nil {
		t.Error("expected nil signer when no key configured")
	}
}

// --- EvaluateSignal error propagation ---------------------------------------

// erroringMarket wraps a working fake but returns an error from one named method.
type erroringMarket struct {
	fakeMarketData
	failOn string
	err    error
}

func (m *erroringMarket) GetAccountState(ctx context.Context, addr string) (*client.AccountState, error) {
	if m.failOn == "account" {
		return nil, m.err
	}
	return m.fakeMarketData.GetAccountState(ctx, addr)
}
func (m *erroringMarket) GetPositions(ctx context.Context, addr string) ([]models.Position, error) {
	if m.failOn == "positions" {
		return nil, m.err
	}
	return m.fakeMarketData.GetPositions(ctx, addr)
}
func (m *erroringMarket) GetAssetInfo(ctx context.Context, sym string) (client.AssetMeta, error) {
	if m.failOn == "assetinfo" {
		return client.AssetMeta{}, m.err
	}
	return m.fakeMarketData.GetAssetInfo(ctx, sym)
}
func (m *erroringMarket) GetMidPrice(ctx context.Context, sym string) (decimal.Decimal, error) {
	if m.failOn == "mid" {
		return decimal.Zero, m.err
	}
	return m.fakeMarketData.GetMidPrice(ctx, sym)
}
func (m *erroringMarket) GetFundingRate(ctx context.Context, sym string) (decimal.Decimal, error) {
	if m.failOn == "funding" {
		return decimal.Zero, m.err
	}
	return m.fakeMarketData.GetFundingRate(ctx, sym)
}
func (m *erroringMarket) GetAssetIndex(ctx context.Context, sym string) (int, error) {
	if m.failOn == "index" {
		return 0, m.err
	}
	return m.fakeMarketData.GetAssetIndex(ctx, sym)
}
func (m *erroringMarket) GetCandles(ctx context.Context, sym, iv string, lb int) ([]client.Candle, error) {
	if m.failOn == "candles" {
		return nil, m.err
	}
	return m.fakeMarketData.GetCandles(ctx, sym, iv, lb)
}

func TestEvaluateSignalDataErrors(t *testing.T) {
	wantErr := os.ErrClosed
	// "candles" only fetched in managed mode or when no stop loss; use managed.
	for _, failOn := range []string{"account", "positions", "assetinfo", "mid", "funding", "index", "candles"} {
		t.Run(failOn, func(t *testing.T) {
			sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.01","leverage":5,"horizon":"swing"}`)
			mgr := risk.NewManager(models.RiskMedium, models.ModeManaged, nil)
			m := &erroringMarket{failOn: failOn, err: wantErr}
			_, _, err := EvaluateSignal(context.Background(), mgr, m, sig, "0xabc", nil, nil, "testnet")
			if err == nil {
				t.Fatalf("expected error when %s fails", failOn)
			}
		})
	}
}

// dupPositionMarket returns an open same-side position to trigger a reject.
type dupPositionMarket struct{ fakeMarketData }

func (m *dupPositionMarket) GetPositions(ctx context.Context, addr string) ([]models.Position, error) {
	return []models.Position{{
		Coin:     "BTC",
		Size:     decimal.RequireFromString("0.1"), // positive = long
		Leverage: 5,
	}}, nil
}

func TestEvaluateSignalReject(t *testing.T) {
	sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.01","leverage":5,"stop_loss":"49000"}`)
	mgr := risk.NewManager(models.RiskMedium, models.ModeManual, nil)
	order, reject, err := EvaluateSignal(context.Background(), mgr, &dupPositionMarket{}, sig, "0xabc", nil, nil, "testnet")
	if err != nil {
		t.Fatalf("EvaluateSignal: %v", err)
	}
	if order != nil {
		t.Errorf("expected no order, got %+v", order)
	}
	if reject == nil {
		t.Fatal("expected a reject for duplicate position")
	}
	if reject.Reason != models.RejectDuplicatePosition {
		t.Errorf("reason = %q, want duplicate_position", reject.Reason)
	}
}

// --- ResetCircuitBreaker + RecentConsecutiveLosses (real storage) -----------

func newTestStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestResetCircuitBreakerAppendsWin(t *testing.T) {
	s := newTestStorage(t)

	if err := ResetCircuitBreaker(s, "testnet"); err != nil {
		t.Fatalf("ResetCircuitBreaker: %v", err)
	}

	// The reset persists a virtual winning trade ($0.01) so it becomes the most
	// recent entry, matching cli.py:risk_reset.
	hist, err := s.GetRecentTradeResults("testnet", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history len = %d, want 1", len(hist))
	}
	newest := hist[0] // DESC order → most recent first
	if newest.Coin != "RESET" {
		t.Errorf("coin = %q, want RESET", newest.Coin)
	}
	if newest.Pnl.Sign() <= 0 {
		t.Errorf("pnl = %s, want positive (a virtual win)", newest.Pnl)
	}
}

// --- Integration: fake HL server driving netCfg-parameterized functions ------

// fakeHL is a minimal Hyperliquid API stand-in. It routes /info by request type
// and answers /exchange with a generic ok envelope. Tests tweak the exported
// fields before driving the service functions.
type fakeHL struct {
	openOrders []map[string]any
	positions  []map[string]any
	cancels    int
}

func (h *fakeHL) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/exchange" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if action, ok := body["action"].(map[string]any); ok {
				if action["type"] == "cancel" {
					h.cancels++
				}
			}
			w.Write([]byte(`{"status":"ok","response":{"type":"default"}}`))
			return
		}
		// /info routed by "type".
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		switch req["type"] {
		case "meta":
			json.NewEncoder(w).Encode(map[string]any{
				"universe": []map[string]any{
					{"name": "BTC", "szDecimals": 3, "maxLeverage": 50},
					{"name": "ETH", "szDecimals": 4, "maxLeverage": 50},
				},
			})
		case "allMids":
			json.NewEncoder(w).Encode(map[string]any{"BTC": "50000"})
		case "metaAndAssetCtxs":
			w.Write([]byte(`[{"universe":[{"name":"BTC","szDecimals":3,"maxLeverage":50}]},[{"funding":"0.00001"}]]`))
		case "candleSnapshot":
			w.Write([]byte(`[]`))
		case "openOrders":
			json.NewEncoder(w).Encode(h.openOrders)
		case "clearinghouseState":
			json.NewEncoder(w).Encode(map[string]any{
				"marginSummary":  map[string]any{"accountValue": "10000"},
				"withdrawable":   "9000",
				"assetPositions": h.positions,
			})
		default:
			w.Write([]byte(`{}`))
		}
	}
}

func (h *fakeHL) start(t *testing.T) config.NetworkConfig {
	t.Helper()
	srv := httptest.NewServer(h.handler())
	t.Cleanup(srv.Close)
	return config.NetworkConfig{Name: "test", APIURL: srv.URL}
}

func testSigner(t *testing.T) *signer.Signer {
	t.Helper()
	s, err := signer.New(anvilKey, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func openOrder(coin string, oid int64) map[string]any {
	return map[string]any{"coin": coin, "oid": oid, "side": "B", "limitPx": "50000", "sz": "0.1", "timestamp": 1}
}

func TestCancelOrdersAll(t *testing.T) {
	h := &fakeHL{openOrders: []map[string]any{openOrder("BTC", 1), openOrder("ETH", 2)}}
	netCfg := h.start(t)

	n, err := CancelOrders(context.Background(), netCfg, testSigner(t), CancelRequest{Network: "test", All: true})
	if err != nil {
		t.Fatalf("CancelOrders: %v", err)
	}
	if n != 2 {
		t.Errorf("cancelled = %d, want 2", n)
	}
}

func TestCancelOrdersByPair(t *testing.T) {
	h := &fakeHL{openOrders: []map[string]any{openOrder("BTC", 1), openOrder("ETH", 2)}}
	netCfg := h.start(t)

	pair := "btc" // case-insensitive match
	n, err := CancelOrders(context.Background(), netCfg, testSigner(t), CancelRequest{Network: "test", Pair: &pair})
	if err != nil {
		t.Fatalf("CancelOrders: %v", err)
	}
	if n != 1 {
		t.Errorf("cancelled = %d, want 1 (only BTC)", n)
	}
}

func TestCancelOrdersByOrderID(t *testing.T) {
	h := &fakeHL{openOrders: []map[string]any{openOrder("BTC", 1), openOrder("ETH", 2)}}
	netCfg := h.start(t)

	oid := int64(2)
	n, err := CancelOrders(context.Background(), netCfg, testSigner(t), CancelRequest{Network: "test", OrderID: &oid})
	if err != nil {
		t.Fatalf("CancelOrders: %v", err)
	}
	if n != 1 {
		t.Errorf("cancelled = %d, want 1 (only oid 2)", n)
	}
}

func TestCancelOrdersNoMatch(t *testing.T) {
	h := &fakeHL{openOrders: []map[string]any{openOrder("BTC", 1)}}
	netCfg := h.start(t)

	pair := "SOL"
	n, err := CancelOrders(context.Background(), netCfg, testSigner(t), CancelRequest{Network: "test", Pair: &pair})
	if err != nil {
		t.Fatalf("CancelOrders: %v", err)
	}
	if n != 0 {
		t.Errorf("cancelled = %d, want 0", n)
	}
	if h.cancels != 0 {
		t.Errorf("server saw %d cancels, want 0", h.cancels)
	}
}

func TestRiskStatusGathersData(t *testing.T) {
	h := &fakeHL{}
	netCfg := h.start(t)
	s := newTestStorage(t)

	data, err := RiskStatus(context.Background(), netCfg, testSigner(t), s, "test", models.RiskHigh)
	if err != nil {
		t.Fatalf("RiskStatus: %v", err)
	}
	if !data.AccountValue.Equal(decimal.RequireFromString("10000")) {
		t.Errorf("account value = %s, want 10000", data.AccountValue)
	}
	if !data.Available.Equal(decimal.RequireFromString("9000")) {
		t.Errorf("available = %s, want 9000", data.Available)
	}
	// B.7: the profile must reflect the requested level, not a hardcoded MEDIUM.
	if data.Profile != models.RiskHigh {
		t.Errorf("profile = %q, want high (B.7)", data.Profile)
	}
}

func TestRiskCheckReject(t *testing.T) {
	// Open same-side BTC position triggers a duplicate-position reject.
	h := &fakeHL{positions: []map[string]any{{
		"position": map[string]any{
			"coin": "BTC", "szi": "0.1", "entryPx": "50000",
			"positionValue": "5000", "unrealizedPnl": "0",
			"leverage": map[string]any{"type": "cross", "value": 5},
		},
	}}}
	netCfg := h.start(t)
	s := newTestStorage(t)

	sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.01","leverage":5,"stop_loss":"49000"}`)
	order, reject, err := RiskCheck(context.Background(), netCfg, testSigner(t), s, sig, "test", models.RiskMedium)
	if err != nil {
		t.Fatalf("RiskCheck: %v", err)
	}
	if order != nil {
		t.Errorf("expected no order, got %+v", order)
	}
	if reject == nil || reject.Reason != models.RejectDuplicatePosition {
		t.Fatalf("expected duplicate_position reject, got %+v", reject)
	}
}

// --- trading config wiring (slippage + retry) --------------------------------

func TestSlippageFromConfig(t *testing.T) {
	got := Slippage(config.TradingSettings{DefaultSlippage: 0.02})
	if !got.Equal(decimal.RequireFromString("0.02")) {
		t.Errorf("Slippage = %s, want 0.02", got)
	}
}

// TestClientOptionsRetryWired proves trading.max_retries flows through
// ClientOptions into the constructed client: a 500-always server is hit exactly
// (retries+1) times.
func TestClientOptionsRetryWired(t *testing.T) {
	for _, tc := range []struct{ retries, wantHits int }{{0, 1}, {2, 3}} {
		t.Run(itoaT(tc.retries), func(t *testing.T) {
			var hits int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			// RetryDelay 0 keeps the test instant.
			opts := ClientOptions(config.TradingSettings{MaxRetries: tc.retries, RetryDelay: 0})
			info := client.NewInfoClient(config.NetworkConfig{Name: "test", APIURL: srv.URL}, opts...)
			if _, err := info.GetAllMids(context.Background()); err == nil {
				t.Fatal("expected error from 500 server")
			}
			if hits != tc.wantHits {
				t.Errorf("server hit %d times, want %d (retries=%d)", hits, tc.wantHits, tc.retries)
			}
		})
	}
}

func itoaT(n int) string {
	if n == 0 {
		return "0-retries"
	}
	return "2-retries"
}

// --- Executor.Exec validation failure (no network needed) --------------------

func TestExecValidationFailure(t *testing.T) {
	cfg, _ := config.Load("")
	cfg.Set("security", map[string]any{"max_leverage": 2})
	s := newTestStorage(t)

	exec := &Executor{Config: cfg, Signer: testSigner(t), Storage: s}
	sig := mustSignal(t, `{"pair":"BTC","side":"long","order_type":"market","size":"0.001","leverage":10,"stop_loss":"49000"}`)

	_, err := exec.Exec(context.Background(), ExecRequest{Signal: sig, Network: "testnet"})
	if err == nil {
		t.Fatal("expected validation error for leverage over max")
	}
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("error type = %T, want *ValidationError", err)
	}
}
