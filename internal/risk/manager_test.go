package risk_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

func sampleSignal() models.TradingSignal {
	return models.TradingSignal{
		Pair:       "BTC",
		Side:       models.Long,
		Size:       decimal.RequireFromString("0.1"),
		Leverage:   10,
		EntryPrice: models.Ptr(decimal.NewFromInt(50000)),
		StopLoss:   models.Ptr(decimal.NewFromInt(49000)),
		Confidence: models.Ptr(0.8),
		Horizon:    models.HorizonIntraday,
	}
}

func sampleAssetMeta() risk.AssetMeta {
	return risk.AssetMeta{SzDecimals: 5, MaxLeverage: 50, OnlyIsolated: false, AssetID: 0}
}

// flatCandles returns n identical candles (constant TR = high-low).
func flatCandles(n int, o, h, l, c string) []risk.Candle {
	out := make([]risk.Candle, n)
	for i := range out {
		out[i] = risk.Candle{
			High:  decimal.RequireFromString(h),
			Low:   decimal.RequireFromString(l),
			Close: decimal.RequireFromString(c),
		}
	}
	return out
}

func newManager(mode models.RiskMode) *risk.Manager {
	return risk.NewManager(models.RiskMedium, mode, nil).
		WithClock(func() time.Time { return fixedNow })
}

func baseInput(sig models.TradingSignal) risk.EvaluateInput {
	return risk.EvaluateInput{
		Signal:           sig,
		AccountValue:     decimal.NewFromInt(10000),
		AvailableBalance: decimal.NewFromInt(5000),
		OpenPositions:    nil,
		AssetMeta:        sampleAssetMeta(),
		Candles:          nil,
		FundingRate:      decimal.RequireFromString("0.0001"),
		MarkPrice:        decimal.NewFromInt(50000),
	}
}

func TestManagerManualMode(t *testing.T) {
	t.Run("approves valid signal", func(t *testing.T) {
		m := newManager(models.ModeManual)
		order, reject := m.EvaluateSignalWithData(baseInput(sampleSignal()))
		require.Nil(t, reject)
		require.NotNil(t, order)
		assert.Equal(t, "BTC", order.Coin)
		assert.True(t, order.Size.Equal(decimal.RequireFromString("0.1")))
		assert.Equal(t, 10, order.Leverage)
		assert.True(t, order.StopLoss.Equal(decimal.NewFromInt(49000)))
		assert.Equal(t, models.ModeManual, order.RiskMode)
		assert.Equal(t, "signal", order.SizeSource)
		assert.Equal(t, "signal", order.SLSource)
	})

	t.Run("rejects leverage exceeded", func(t *testing.T) {
		sig := sampleSignal()
		sig.Leverage = 15
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(baseInput(sig))
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectLeverageExceeded, reject.Reason)
	})

	t.Run("rejects max positions", func(t *testing.T) {
		in := baseInput(sampleSignal())
		for i := 0; i < 5; i++ {
			in.OpenPositions = append(in.OpenPositions, models.Position{
				Coin: "COIN" + string(rune('A'+i)), Size: decimal.NewFromInt(1),
				EntryPrice: decimal.NewFromInt(100),
			})
		}
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectMaxPositions, reject.Reason)
	})

	t.Run("rejects duplicate position", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.OpenPositions = []models.Position{{
			Coin: "BTC", Size: decimal.RequireFromString("0.5"), EntryPrice: decimal.NewFromInt(48000),
		}}
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectDuplicatePosition, reject.Reason)
	})

	t.Run("rejects insufficient margin", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.AvailableBalance = decimal.NewFromInt(100)
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectInsufficientMargin, reject.Reason)
	})

	t.Run("rejects stale signal", func(t *testing.T) {
		sig := sampleSignal()
		sig.EntryPrice = models.Ptr(decimal.NewFromInt(51000)) // 2% deviation
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(baseInput(sig))
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectStaleSignal, reject.Reason)
	})

	t.Run("respects only isolated", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.AssetMeta.OnlyIsolated = true
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
		require.Nil(t, reject)
		require.NotNil(t, order)
		assert.Equal(t, "isolated", order.MarginMode)
	})
}

func TestManagerManagedMode(t *testing.T) {
	candles := flatCandles(20, "50000", "50500", "49500", "50100")

	t.Run("calculates size", func(t *testing.T) {
		sig := sampleSignal()
		sig.StopLoss = nil // derive from ATR
		in := baseInput(sig)
		in.Candles = candles
		order, reject := newManager(models.ModeManaged).EvaluateSignalWithData(in)
		require.Nil(t, reject)
		require.NotNil(t, order)
		assert.Equal(t, models.ModeManaged, order.RiskMode)
		assert.Equal(t, "calculated", order.SizeSource)
		assert.Equal(t, "calculated", order.SLSource)
		assert.True(t, order.StopLoss.GreaterThan(decimal.Zero))
		assert.True(t, order.RiskPct.GreaterThan(decimal.Zero))
		assert.True(t, order.RiskPct.LessThanOrEqual(decimal.RequireFromString("0.025")))
	})

	t.Run("confidence scaling reduces size", func(t *testing.T) {
		m := newManager(models.ModeManaged)
		full := sampleSignal()
		full.Confidence = models.Ptr(1.0)
		inFull := baseInput(full)
		inFull.Candles = candles
		orderFull, rejF := m.EvaluateSignalWithData(inFull)
		require.Nil(t, rejF)

		half := sampleSignal()
		half.Confidence = models.Ptr(0.5)
		inHalf := baseInput(half)
		inHalf.Candles = candles
		orderHalf, rejH := m.EvaluateSignalWithData(inHalf)
		require.Nil(t, rejH)

		assert.True(t, orderHalf.Size.LessThan(orderFull.Size))
	})

	t.Run("rejects insufficient candles", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.Candles = flatCandles(1, "50000", "50500", "49500", "50100")
		order, reject := newManager(models.ModeManaged).EvaluateSignalWithData(in)
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectATRUnavailable, reject.Reason)
	})

	t.Run("adjusts leverage on high volatility", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.Candles = flatCandles(20, "50000", "55000", "45000", "50000")
		order, reject := newManager(models.ModeManaged).EvaluateSignalWithData(in)
		require.Nil(t, reject)
		require.NotNil(t, order)
		assert.Less(t, order.Leverage, 10)
	})
}

func TestManagerCircuitBreakerIntegration(t *testing.T) {
	mkLosses := func(n int) []models.TradeResult {
		out := make([]models.TradeResult, n)
		for i := 0; i < n; i++ {
			out[i] = trade("-10", fixedNow.Add(-time.Duration(n-i)*time.Hour))
		}
		return out
	}

	t.Run("hard stop rejects", func(t *testing.T) {
		in := baseInput(sampleSignal())
		in.TradeHistory = mkLosses(5)
		order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
		require.Nil(t, order)
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectCircuitBreakerHard, reject.Reason)
	})

	t.Run("soft stop reduces size", func(t *testing.T) {
		m := newManager(models.ModeManaged)
		candles := flatCandles(20, "50000", "50500", "49500", "50100")

		inNormal := baseInput(sampleSignal())
		inNormal.Candles = candles
		normal, rejN := m.EvaluateSignalWithData(inNormal)
		require.Nil(t, rejN)

		inSoft := baseInput(sampleSignal())
		inSoft.Candles = candles
		inSoft.TradeHistory = mkLosses(3)
		soft, rejS := m.EvaluateSignalWithData(inSoft)
		require.Nil(t, rejS)

		assert.True(t, soft.Size.LessThan(normal.Size))
	})
}

func TestManagerDecisionLog(t *testing.T) {
	t.Run("created on approve", func(t *testing.T) {
		m := newManager(models.ModeManual)
		order, _ := m.EvaluateSignalWithData(baseInput(sampleSignal()))
		require.NotNil(t, order)
		log := m.DecisionLog()
		require.NotNil(t, log)
		assert.Equal(t, "approved", log.Decision)
		assert.Equal(t, "BTC", log.Coin)
		assert.Nil(t, log.RejectReason)
		require.NotNil(t, log.OutputSize)
		assert.True(t, log.OutputSize.Equal(order.Size))
	})

	t.Run("created on reject", func(t *testing.T) {
		sig := sampleSignal()
		sig.Leverage = 25
		m := newManager(models.ModeManual)
		_, reject := m.EvaluateSignalWithData(baseInput(sig))
		require.NotNil(t, reject)
		log := m.DecisionLog()
		require.NotNil(t, log)
		assert.Equal(t, "rejected", log.Decision)
		require.NotNil(t, log.RejectReason)
		assert.Equal(t, models.RejectLeverageExceeded, *log.RejectReason)
		assert.Nil(t, log.OutputSize)
	})
}

func TestManagerCumulativeRiskBudget(t *testing.T) {
	sig := sampleSignal()
	sig.Size = decimal.RequireFromString("0.5")
	in := baseInput(sig)
	in.OpenPositions = []models.Position{{
		Coin: "ETH", Size: decimal.NewFromInt(5), EntryPrice: decimal.NewFromInt(3000),
		RiskAmount: models.Ptr(decimal.NewFromInt(500)),
	}}
	order, reject := newManager(models.ModeManual).EvaluateSignalWithData(in)
	require.Nil(t, order)
	require.NotNil(t, reject)
	assert.Equal(t, models.RejectRiskBudgetExceeded, reject.Reason)
}
