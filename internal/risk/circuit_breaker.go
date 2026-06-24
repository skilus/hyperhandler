package risk

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
)

// CircuitBreaker tracks losses and enforces trading limits. Mirrors
// circuit_breaker.py:CircuitBreaker. The clock is injectable for deterministic
// tests (the Python version calls datetime.now(utc) directly).
type CircuitBreaker struct {
	Profile RiskProfile
	now     func() time.Time
}

// NewCircuitBreaker builds a CircuitBreaker for the profile using the real clock.
func NewCircuitBreaker(profile RiskProfile) *CircuitBreaker {
	return &CircuitBreaker{Profile: profile, now: time.Now}
}

// WithClock overrides the clock (for tests).
func (cb *CircuitBreaker) WithClock(now func() time.Time) *CircuitBreaker {
	cb.now = now
	return cb
}

// Check evaluates the circuit-breaker status from trade history. Mirrors
// circuit_breaker.py:check.
func (cb *CircuitBreaker) Check(
	tradeHistory []models.TradeResult,
	accountValue decimal.Decimal,
) models.CircuitBreakerStatus {
	// Count consecutive losses (most recent first).
	consecutiveLosses := 0
	for i := len(tradeHistory) - 1; i >= 0; i-- {
		if tradeHistory[i].IsLoss() {
			consecutiveLosses++
		} else {
			break
		}
	}

	// Calculate daily P&L (trades closed since UTC day start).
	todayStart := cb.utcDayStart()
	dailyPnl := decimal.Zero
	for _, t := range tradeHistory {
		if !t.ClosedAt.Before(todayStart) {
			dailyPnl = dailyPnl.Add(t.Pnl)
		}
	}
	dailyLossPct := decimal.Zero
	if accountValue.GreaterThan(decimal.Zero) {
		// abs(min(0, dailyPnl)) / accountValue
		negPart := decimal.Min(decimal.Zero, dailyPnl)
		dailyLossPct = negPart.Abs().Div(accountValue)
	}

	// Check daily loss limit (HARD stop).
	if dailyLossPct.GreaterThanOrEqual(cb.Profile.DailyLossLimit) {
		reason := fmt.Sprintf("Daily loss %s >= limit %s",
			pctOne(dailyLossPct), pctOne(cb.Profile.DailyLossLimit))
		return models.CircuitBreakerStatus{
			Active:            true,
			Level:             "HARD",
			Trigger:           models.TriggerDailyLoss,
			RiskMultiplier:    decimal.Zero,
			Reason:            &reason,
			ConsecutiveLosses: consecutiveLosses,
			DailyLossPct:      dailyLossPct,
		}
	}

	// Check hard stop (consecutive losses).
	if consecutiveLosses >= cb.Profile.HardStopLosses {
		reason := fmt.Sprintf("%d consecutive losses (hard limit: %d)",
			consecutiveLosses, cb.Profile.HardStopLosses)
		return models.CircuitBreakerStatus{
			Active:            true,
			Level:             "HARD",
			Trigger:           models.TriggerConsecutive,
			RiskMultiplier:    decimal.Zero,
			Reason:            &reason,
			ConsecutiveLosses: consecutiveLosses,
			DailyLossPct:      dailyLossPct,
		}
	}

	// Check soft stop (reduced risk).
	if consecutiveLosses >= cb.Profile.SoftStopLosses {
		reason := fmt.Sprintf("%d consecutive losses (soft limit: %d)",
			consecutiveLosses, cb.Profile.SoftStopLosses)
		return models.CircuitBreakerStatus{
			Active:            true,
			Level:             "SOFT",
			Trigger:           models.TriggerConsecutive,
			RiskMultiplier:    decimal.NewFromFloat(0.5),
			Reason:            &reason,
			ConsecutiveLosses: consecutiveLosses,
			DailyLossPct:      dailyLossPct,
		}
	}

	// No circuit breaker active.
	return models.CircuitBreakerStatus{
		Active:            false,
		Level:             "NONE",
		Trigger:           models.TriggerNone,
		RiskMultiplier:    decimal.NewFromInt(1),
		ConsecutiveLosses: consecutiveLosses,
		DailyLossPct:      dailyLossPct,
	}
}

// GetReject returns a RiskReject when the breaker is at HARD level, else nil.
// SOFT only reduces risk via the multiplier. Mirrors
// circuit_breaker.py:get_reject.
func (cb *CircuitBreaker) GetReject(status models.CircuitBreakerStatus) *models.RiskReject {
	if !status.Active {
		return nil
	}
	if status.Level == "HARD" {
		switch status.Trigger {
		case models.TriggerDailyLoss:
			return &models.RiskReject{
				Reason:          models.RejectDailyLossLimit,
				Details:         derefOr(status.Reason, "Daily loss limit reached"),
				SuggestedAction: "wait",
			}
		case models.TriggerConsecutive:
			return &models.RiskReject{
				Reason:          models.RejectCircuitBreakerHard,
				Details:         derefOr(status.Reason, "Too many consecutive losses"),
				SuggestedAction: "manual_reset",
			}
		}
	}
	return nil
}

// utcDayStart returns the start of the current UTC day. Mirrors
// circuit_breaker.py:_get_utc_day_start.
func (cb *CircuitBreaker) utcDayStart() time.Time {
	now := cb.now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// pctOne formats a ratio as a percentage with one decimal (Python's "{:.1%}").
func pctOne(d decimal.Decimal) string {
	return d.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%"
}

func derefOr(s *string, fallback string) string {
	if s != nil && *s != "" {
		return *s
	}
	return fallback
}
