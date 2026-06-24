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

// fixedNow is the clock used by circuit-breaker tests; trades are positioned
// relative to it so "today" vs "yesterday" is deterministic.
var fixedNow = time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

func mediumCB() *risk.CircuitBreaker {
	return risk.NewCircuitBreaker(risk.GetProfile(models.RiskMedium)).
		WithClock(func() time.Time { return fixedNow })
}

func trade(pnl string, closedAt time.Time) models.TradeResult {
	return models.TradeResult{
		Coin:       "BTC",
		Side:       "long",
		EntryPrice: decimal.NewFromInt(50000),
		ExitPrice:  decimal.NewFromInt(49500),
		Size:       decimal.RequireFromString("0.02"),
		Pnl:        decimal.RequireFromString(pnl),
		Fees:       decimal.NewFromInt(1),
		OpenedAt:   closedAt.Add(-time.Hour),
		ClosedAt:   closedAt,
	}
}

func todayLoss(pnl string) models.TradeResult { return trade(pnl, fixedNow.Add(-30*time.Minute)) }
func todayWin(pnl string) models.TradeResult  { return trade(pnl, fixedNow.Add(-time.Hour)) }

func TestCircuitBreakerCheck(t *testing.T) {
	cb := mediumCB()
	acct := decimal.NewFromInt(10000)

	t.Run("no trades no trigger", func(t *testing.T) {
		s := cb.Check(nil, acct)
		assert.False(t, s.Active)
		assert.Equal(t, "NONE", s.Level)
		assert.Equal(t, models.TriggerNone, s.Trigger)
		assert.True(t, s.RiskMultiplier.Equal(decimal.NewFromInt(1)))
		assert.Equal(t, 0, s.ConsecutiveLosses)
	})

	t.Run("winning trades no trigger", func(t *testing.T) {
		trades := []models.TradeResult{todayWin("100"), todayWin("100"), todayWin("100")}
		s := cb.Check(trades, acct)
		assert.False(t, s.Active)
		assert.Equal(t, 0, s.ConsecutiveLosses)
	})

	t.Run("soft stop at 3 consecutive losses", func(t *testing.T) {
		trades := []models.TradeResult{todayLoss("-10"), todayLoss("-10"), todayLoss("-10")}
		s := cb.Check(trades, acct)
		assert.True(t, s.Active)
		assert.Equal(t, "SOFT", s.Level)
		assert.Equal(t, models.TriggerConsecutive, s.Trigger)
		assert.True(t, s.RiskMultiplier.Equal(decimal.RequireFromString("0.5")))
		assert.Equal(t, 3, s.ConsecutiveLosses)
	})

	t.Run("hard stop at 5 consecutive losses", func(t *testing.T) {
		trades := make([]models.TradeResult, 5)
		for i := range trades {
			trades[i] = todayLoss("-10")
		}
		s := cb.Check(trades, acct)
		assert.Equal(t, "HARD", s.Level)
		assert.Equal(t, models.TriggerConsecutive, s.Trigger)
		assert.True(t, s.RiskMultiplier.IsZero())
		assert.Equal(t, 5, s.ConsecutiveLosses)
	})

	t.Run("winning trade resets consecutive", func(t *testing.T) {
		trades := []models.TradeResult{
			todayLoss("-10"), todayLoss("-10"), todayWin("100"), todayLoss("-10"), todayLoss("-10"),
		}
		s := cb.Check(trades, acct)
		assert.False(t, s.Active)
		assert.Equal(t, 2, s.ConsecutiveLosses)
	})

	t.Run("daily loss limit triggers hard", func(t *testing.T) {
		trades := []models.TradeResult{trade("-350", fixedNow.Add(-30*time.Minute))}
		s := cb.Check(trades, acct)
		assert.Equal(t, "HARD", s.Level)
		assert.Equal(t, models.TriggerDailyLoss, s.Trigger)
		assert.True(t, s.RiskMultiplier.IsZero())
		assert.True(t, s.DailyLossPct.GreaterThanOrEqual(decimal.RequireFromString("0.03")))
	})

	t.Run("daily loss ignores yesterday", func(t *testing.T) {
		yesterday := fixedNow.Add(-24 * time.Hour)
		trades := []models.TradeResult{trade("-1000", yesterday)}
		s := cb.Check(trades, acct)
		assert.NotEqual(t, models.TriggerDailyLoss, s.Trigger)
		assert.Equal(t, 1, s.ConsecutiveLosses)
	})

	t.Run("daily loss takes priority over consecutive", func(t *testing.T) {
		trades := []models.TradeResult{trade("-500", fixedNow.Add(-30*time.Minute))}
		s := cb.Check(trades, acct)
		assert.Equal(t, "HARD", s.Level)
		assert.Equal(t, models.TriggerDailyLoss, s.Trigger)
	})

	t.Run("consecutive counted from most recent", func(t *testing.T) {
		trades := []models.TradeResult{todayWin("100"), todayLoss("-10"), todayLoss("-10"), todayLoss("-10")}
		s := cb.Check(trades, acct)
		assert.Equal(t, 3, s.ConsecutiveLosses)
		assert.Equal(t, "SOFT", s.Level)
	})
}

func TestCircuitBreakerGetReject(t *testing.T) {
	cb := mediumCB()
	acct := decimal.NewFromInt(10000)

	t.Run("not active no reject", func(t *testing.T) {
		assert.Nil(t, cb.GetReject(cb.Check(nil, acct)))
	})

	t.Run("soft no reject", func(t *testing.T) {
		trades := []models.TradeResult{todayLoss("-10"), todayLoss("-10"), todayLoss("-10")}
		s := cb.Check(trades, acct)
		require.Equal(t, "SOFT", s.Level)
		assert.Nil(t, cb.GetReject(s))
	})

	t.Run("hard consecutive reject", func(t *testing.T) {
		trades := make([]models.TradeResult, 5)
		for i := range trades {
			trades[i] = todayLoss("-10")
		}
		reject := cb.GetReject(cb.Check(trades, acct))
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectCircuitBreakerHard, reject.Reason)
		assert.Equal(t, "manual_reset", reject.SuggestedAction)
	})

	t.Run("hard daily loss reject", func(t *testing.T) {
		trades := []models.TradeResult{trade("-500", fixedNow.Add(-30*time.Minute))}
		reject := cb.GetReject(cb.Check(trades, acct))
		require.NotNil(t, reject)
		assert.Equal(t, models.RejectDailyLossLimit, reject.Reason)
		assert.Equal(t, "wait", reject.SuggestedAction)
	})
}

func TestCircuitBreakerProfiles(t *testing.T) {
	acct := decimal.NewFromInt(10000)

	t.Run("low profile stricter", func(t *testing.T) {
		cb := risk.NewCircuitBreaker(risk.GetProfile(models.RiskLow)).
			WithClock(func() time.Time { return fixedNow })
		two := []models.TradeResult{todayLoss("-10"), todayLoss("-10")}
		four := make([]models.TradeResult, 4)
		for i := range four {
			four[i] = todayLoss("-10")
		}
		assert.Equal(t, "SOFT", cb.Check(two, acct).Level)
		assert.Equal(t, "HARD", cb.Check(four, acct).Level)
	})

	t.Run("high profile looser", func(t *testing.T) {
		cb := risk.NewCircuitBreaker(risk.GetProfile(models.RiskHigh)).
			WithClock(func() time.Time { return fixedNow })
		mk := func(n int) []models.TradeResult {
			out := make([]models.TradeResult, n)
			for i := range out {
				out[i] = todayLoss("-50") // 0.5% each
			}
			return out
		}
		assert.Equal(t, "SOFT", cb.Check(mk(3), acct).Level)
		assert.Equal(t, "SOFT", cb.Check(mk(5), acct).Level)
		assert.Equal(t, "HARD", cb.Check(mk(6), acct).Level)
	})
}

func TestTradeResultIsLoss(t *testing.T) {
	assert.True(t, models.TradeResult{Pnl: decimal.NewFromInt(-100)}.IsLoss())
	assert.False(t, models.TradeResult{Pnl: decimal.NewFromInt(100)}.IsLoss())
	assert.False(t, models.TradeResult{Pnl: decimal.Zero}.IsLoss())
}
