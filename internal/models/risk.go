package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// RiskLevel is the configured risk tolerance.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// RiskMode selects how the risk manager treats an incoming signal.
type RiskMode string

const (
	ModeManual  RiskMode = "manual"  // validate the signal as-is
	ModeManaged RiskMode = "managed" // size/SL derived from risk budget
)

// RejectReason enumerates why the risk manager rejects a signal.
type RejectReason string

const (
	RejectCircuitBreakerSoft RejectReason = "circuit_breaker_soft"
	RejectCircuitBreakerHard RejectReason = "circuit_breaker_hard"
	RejectDailyLossLimit     RejectReason = "daily_loss_limit"
	RejectRiskBudgetExceeded RejectReason = "risk_budget_exceeded"
	RejectCorrelationLimit   RejectReason = "correlation_limit"
	RejectMaxPositions       RejectReason = "max_positions_reached"
	RejectInsufficientMargin RejectReason = "insufficient_margin"
	RejectPositionTooSmall   RejectReason = "position_too_small"
	RejectDuplicatePosition  RejectReason = "duplicate_position"
	RejectInvalidCoin        RejectReason = "invalid_coin"
	RejectStaleSignal        RejectReason = "stale_signal"
	RejectLeverageExceeded   RejectReason = "leverage_exceeded"
	RejectLiquidationClose   RejectReason = "liquidation_too_close"
	RejectHighFundingCost    RejectReason = "high_funding_cost"
	RejectATRUnavailable     RejectReason = "atr_unavailable"
)

// CircuitBreakerTrigger names what tripped the circuit breaker.
type CircuitBreakerTrigger string

const (
	TriggerNone        CircuitBreakerTrigger = "none"
	TriggerDailyLoss   CircuitBreakerTrigger = "daily_loss"
	TriggerConsecutive CircuitBreakerTrigger = "consecutive"
)

// RiskReject is returned when a signal is rejected. Mirrors risk.py:RiskReject.
type RiskReject struct {
	Reason          RejectReason
	Details         string
	SuggestedAction string // wait | reduce_risk | close_positions | manual_reset
}

// StopLossResult holds a calculated stop loss. Mirrors risk.py:StopLossResult.
type StopLossResult struct {
	Price         decimal.Decimal
	Distance      decimal.Decimal
	DistancePct   decimal.Decimal
	ATRValue      decimal.Decimal
	ATRMultiplier decimal.Decimal
}

// PositionSizeResult holds a calculated position size. Mirrors
// risk.py:PositionSizeResult.
type PositionSizeResult struct {
	Size               decimal.Decimal
	Notional           decimal.Decimal
	MarginRequired     decimal.Decimal
	RiskAmount         decimal.Decimal
	RiskPct            decimal.Decimal
	CommissionEstimate decimal.Decimal
}

// LeverageResult holds the selected leverage. Mirrors risk.py:LeverageResult.
type LeverageResult struct {
	Leverage  int
	MaxSafe   int
	MaxCoin   int
	MaxConfig int
	Reason    string
}

// CumulativeRiskResult is the portfolio cumulative-risk calculation. Mirrors
// risk.py:CumulativeRiskResult.
type CumulativeRiskResult struct {
	RawRisk           decimal.Decimal
	AdjustedRisk      decimal.Decimal
	RiskPct           decimal.Decimal
	AvailableBudget   decimal.Decimal
	WithinLimit       bool
	CorrelationGroups map[string][]string
}

// FundingEstimate holds estimated funding costs. Mirrors risk.py:FundingEstimate.
type FundingEstimate struct {
	HourlyRate         decimal.Decimal
	HourlyCost         decimal.Decimal
	HourlyIncome       decimal.Decimal
	Projected24h       decimal.Decimal
	FundingEatsRiskPct decimal.Decimal
}

// CircuitBreakerStatus is the circuit breaker state. Mirrors
// risk.py:CircuitBreakerStatus. Use NewCircuitBreakerStatus for the Pydantic
// defaults (risk_multiplier 1.0).
type CircuitBreakerStatus struct {
	Active            bool
	Level             string // NONE | SOFT | HARD
	Trigger           CircuitBreakerTrigger
	RiskMultiplier    decimal.Decimal // 1.0 normal, 0.5 reduced, 0.0 blocked
	Reason            *string
	ConsecutiveLosses int
	DailyLossPct      decimal.Decimal
}

// NewCircuitBreakerStatus returns a status with the Python field defaults.
func NewCircuitBreakerStatus() CircuitBreakerStatus {
	return CircuitBreakerStatus{
		Level:          "NONE",
		Trigger:        TriggerNone,
		RiskMultiplier: decimal.NewFromInt(1),
		DailyLossPct:   decimal.Zero,
	}
}

// TradeOrder is a ready-to-execute order with risk parameters. Mirrors
// risk.py:TradeOrder.
type TradeOrder struct {
	// Order params.
	Coin       string
	AssetID    int
	Side       string // "long" | "short"
	Size       decimal.Decimal
	EntryPrice decimal.Decimal
	Leverage   int
	MarginMode string // "cross" | "isolated"

	// Risk params.
	StopLoss             decimal.Decimal
	RiskAmount           decimal.Decimal
	RiskPct              decimal.Decimal
	CumulativeRiskAfter  decimal.Decimal
	EstimatedLiquidation decimal.Decimal

	// Cost estimates.
	EstimatedCommission decimal.Decimal
	EstimatedFunding24h decimal.Decimal
	MarginRequired      decimal.Decimal

	// Mode tracking.
	RiskMode   RiskMode
	SizeSource string // signal | calculated
	SLSource   string // signal | calculated | none

	// Audit trail.
	CalculationDetails map[string]any
}

// TradeResult is a closed trade, used for circuit-breaker tracking. Mirrors
// risk.py:TradeResult.
type TradeResult struct {
	ID *int64
	// FillID is the exchange fill identity ("oid_time") for reconciled fills,
	// or nil for manual closes. The storage layer enforces UNIQUE(fill_id) so
	// reconciliation is idempotent across restarts (SPEC-007 B.5).
	FillID      *string
	SignalID    *int64
	Coin        string
	Side        string
	EntryPrice  decimal.Decimal
	ExitPrice   decimal.Decimal
	Size        decimal.Decimal
	Pnl         decimal.Decimal
	Fees        decimal.Decimal
	FundingPaid decimal.Decimal
	OpenedAt    time.Time
	ClosedAt    time.Time
}

// IsLoss reports whether the trade closed at a loss (pnl < 0).
func (t TradeResult) IsLoss() bool { return t.Pnl.Sign() < 0 }

// RiskDecisionLog is the full audit log of a risk decision. Mirrors
// risk.py:RiskDecisionLog. Persistence lives with the storage layer (Phase 5).
type RiskDecisionLog struct {
	Timestamp    time.Time
	RiskMode     RiskMode
	SignalSource *string
	Coin         string
	Side         string
	Decision     string // "approved" | "rejected"
	RejectReason *RejectReason

	// Input vs output (for diff display).
	InputSize      *decimal.Decimal
	InputLeverage  *int
	InputStopLoss  *decimal.Decimal
	OutputSize     *decimal.Decimal
	OutputLeverage *int
	OutputStopLoss *decimal.Decimal

	// Market snapshot.
	MarkPrice   decimal.Decimal
	ATRValue    *decimal.Decimal
	FundingRate decimal.Decimal

	// Risk state.
	RiskPerTradePct         decimal.Decimal
	CumulativeRiskBeforePct decimal.Decimal
	CumulativeRiskAfterPct  *decimal.Decimal
	OpenPositionsCount      int
	ConsecutiveLosses       int
	DailyPnlPct             decimal.Decimal

	// Account state.
	AccountValue         decimal.Decimal
	AvailableBalance     decimal.Decimal
	EstimatedLiquidation *decimal.Decimal
}
