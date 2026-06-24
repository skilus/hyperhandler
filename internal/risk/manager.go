package risk

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
)

// AssetMeta is the per-asset metadata the risk manager needs. The service layer
// populates it from the Info API (client.AssetMeta + asset index). Mirrors the
// dict keys read by manager.py (szDecimals, maxLeverage, onlyIsolated,
// _asset_id) with their Python defaults applied by the caller.
type AssetMeta struct {
	SzDecimals   int
	MaxLeverage  int  // HL max leverage for the coin (Python default 50)
	OnlyIsolated bool // forces isolated margin
	AssetID      int
}

// Manager is the stateless risk evaluator. All data comes through arguments —
// the async data-fetching wrapper lives in the service layer (SPEC-007 Phase 6,
// A.1). Mirrors manager.py:RiskManager (pure core). The clock is injectable for
// deterministic decision-log timestamps.
type Manager struct {
	RiskLevel      models.RiskLevel
	RiskMode       models.RiskMode
	Profile        RiskProfile
	HLConfig       HLConfig
	Calculator     *Calculator
	CircuitBreaker *CircuitBreaker

	now             func() time.Time
	lastDecisionLog *models.RiskDecisionLog
}

// NewManager builds a Manager for the level/mode. A nil hlConfig uses defaults.
func NewManager(level models.RiskLevel, mode models.RiskMode, hlConfig *HLConfig) *Manager {
	cfg := DefaultHLConfig()
	if hlConfig != nil {
		cfg = *hlConfig
	}
	profile := GetProfile(level)
	return &Manager{
		RiskLevel:      level,
		RiskMode:       mode,
		Profile:        profile,
		HLConfig:       cfg,
		Calculator:     NewCalculator(profile, cfg),
		CircuitBreaker: NewCircuitBreaker(profile),
		now:            time.Now,
	}
}

// WithClock overrides the clock for the manager and its circuit breaker.
func (m *Manager) WithClock(now func() time.Time) *Manager {
	m.now = now
	m.CircuitBreaker.WithClock(now)
	return m
}

// DecisionLog returns the last evaluation's decision log (nil if none). Mirrors
// manager.py:get_decision_log.
func (m *Manager) DecisionLog() *models.RiskDecisionLog { return m.lastDecisionLog }

// EvaluateInput groups the market/account data for a pure evaluation. Mirrors
// the arguments of manager.py:evaluate_signal_with_data.
type EvaluateInput struct {
	Signal           models.TradingSignal
	AccountValue     decimal.Decimal
	AvailableBalance decimal.Decimal
	OpenPositions    []models.Position
	AssetMeta        AssetMeta
	Candles          []Candle
	FundingRate      decimal.Decimal
	MarkPrice        decimal.Decimal
	TradeHistory     []models.TradeResult
}

// EvaluateSignalWithData runs the full risk evaluation. Returns exactly one of
// (*TradeOrder, *RiskReject); the other is nil. Behavior depends on RiskMode:
// MANUAL validates the signal as-is, MANAGED derives size/SL/leverage from the
// risk budget. Mirrors manager.py:evaluate_signal_with_data.
func (m *Manager) EvaluateSignalWithData(in EvaluateInput) (*models.TradeOrder, *models.RiskReject) {
	signal := in.Signal

	// 1. Circuit breaker check (both modes).
	cbStatus := m.CircuitBreaker.Check(in.TradeHistory, in.AccountValue)
	if cbReject := m.CircuitBreaker.GetReject(cbStatus); cbReject != nil {
		m.logDecision(signal, nil, cbReject, in, cbStatus, nil)
		return nil, cbReject
	}

	// 2. Validate signal vs market (entry-price deviation).
	entryPrice := in.MarkPrice
	if signal.EntryPrice != nil {
		entryPrice = *signal.EntryPrice
		deviation := entryPrice.Sub(in.MarkPrice).Abs().Div(in.MarkPrice)
		if deviation.GreaterThan(m.HLConfig.MaxEntryDeviation) {
			reject := &models.RiskReject{
				Reason:          models.RejectStaleSignal,
				Details:         fmt.Sprintf("Entry deviation %s > max %s", pctOne(deviation), pctOne(m.HLConfig.MaxEntryDeviation)),
				SuggestedAction: "wait",
			}
			m.logDecision(signal, nil, reject, in, cbStatus, nil)
			return nil, reject
		}
	}

	// 3. Check duplicate position (same coin, same side).
	for _, pos := range in.OpenPositions {
		if pos.Coin != signal.Pair {
			continue
		}
		sameSide := (pos.IsLong() && signal.Side == models.Long) ||
			(pos.IsShort() && signal.Side == models.Short)
		if sameSide {
			reject := &models.RiskReject{
				Reason:          models.RejectDuplicatePosition,
				Details:         fmt.Sprintf("Already have %s position on %s", signal.Side, signal.Pair),
				SuggestedAction: "wait",
			}
			m.logDecision(signal, nil, reject, in, cbStatus, nil)
			return nil, reject
		}
	}

	// 4. Mode-specific processing.
	if m.RiskMode == models.ModeManaged {
		return m.evaluateManaged(in, entryPrice, cbStatus)
	}
	return m.evaluateManual(in, entryPrice, cbStatus)
}

// evaluateManual validates the signal parameters against the risk limits.
// Mirrors manager.py:_evaluate_manual.
func (m *Manager) evaluateManual(
	in EvaluateInput,
	entryPrice decimal.Decimal,
	cbStatus models.CircuitBreakerStatus,
) (*models.TradeOrder, *models.RiskReject) {
	signal := in.Signal
	maxLeverageCoin := in.AssetMeta.MaxLeverage
	onlyIsolated := in.AssetMeta.OnlyIsolated

	// Leverage vs profile.
	if signal.Leverage > m.Profile.MaxLeverage {
		reject := &models.RiskReject{
			Reason:          models.RejectLeverageExceeded,
			Details:         fmt.Sprintf("Leverage %d > profile max %d", signal.Leverage, m.Profile.MaxLeverage),
			SuggestedAction: "reduce_risk",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Leverage vs coin max.
	if signal.Leverage > maxLeverageCoin {
		reject := &models.RiskReject{
			Reason:          models.RejectLeverageExceeded,
			Details:         fmt.Sprintf("Leverage %d > coin max %d", signal.Leverage, maxLeverageCoin),
			SuggestedAction: "reduce_risk",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Max positions.
	if len(in.OpenPositions) >= m.Profile.MaxOpenPositions {
		reject := &models.RiskReject{
			Reason:          models.RejectMaxPositions,
			Details:         fmt.Sprintf("Already have %d positions (max %d)", len(in.OpenPositions), m.Profile.MaxOpenPositions),
			SuggestedAction: "close_positions",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	estimatedLiq := m.Calculator.EstimateLiquidationPrice(entryPrice, signal.Leverage, string(signal.Side))

	// Risk from signal's SL.
	var riskAmount decimal.Decimal
	if signal.StopLoss != nil {
		stopDistance := entryPrice.Sub(*signal.StopLoss).Abs()
		riskAmount = signal.Size.Mul(stopDistance)

		if !m.Calculator.ValidateStopVsLiquidation(*signal.StopLoss, estimatedLiq, entryPrice, string(signal.Side)) {
			reject := &models.RiskReject{
				Reason:          models.RejectLiquidationClose,
				Details:         fmt.Sprintf("Stop-loss %s beyond estimated liquidation %s", signal.StopLoss.String(), estimatedLiq.StringFixed(2)),
				SuggestedAction: "reduce_risk",
			}
			m.logDecision(signal, nil, reject, in, cbStatus, nil)
			return nil, reject
		}
	} else {
		// No SL = max risk (full position value).
		riskAmount = signal.Size.Mul(entryPrice)
	}

	riskPct := decimal.NewFromInt(1)
	if in.AccountValue.GreaterThan(decimal.Zero) {
		riskPct = riskAmount.Div(in.AccountValue)
	}

	// Cumulative risk.
	cumRisk := m.Calculator.CalculateCumulativeRisk(in.OpenPositions, riskAmount, signal.Pair, in.AccountValue)
	if !cumRisk.WithinLimit {
		reject := &models.RiskReject{
			Reason:          models.RejectRiskBudgetExceeded,
			Details:         fmt.Sprintf("Cumulative risk %s > max %s", pctOne(cumRisk.RiskPct), pctOne(m.Profile.MaxCumulativeRisk)),
			SuggestedAction: "reduce_risk",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Margin.
	marginRequired := signal.Size.Mul(entryPrice).Div(decimal.NewFromInt(int64(signal.Leverage)))
	if marginRequired.GreaterThan(in.AvailableBalance) {
		reject := &models.RiskReject{
			Reason:          models.RejectInsufficientMargin,
			Details:         fmt.Sprintf("Required $%s > available $%s", marginRequired.StringFixed(2), in.AvailableBalance.StringFixed(2)),
			SuggestedAction: "reduce_risk",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Min order.
	notional := signal.Size.Mul(entryPrice)
	if notional.LessThan(m.HLConfig.MinOrderValue) {
		reject := &models.RiskReject{
			Reason:          models.RejectPositionTooSmall,
			Details:         fmt.Sprintf("Order value $%s < min $%s", notional.StringFixed(2), m.HLConfig.MinOrderValue.String()),
			SuggestedAction: "wait",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Funding cost check (only when SL is present, matching Python).
	if signal.StopLoss != nil {
		funding := m.Calculator.EstimateFundingCost(signal.Size, entryPrice, string(signal.Side), in.FundingRate, riskAmount, 24)
		if funding.FundingEatsRiskPct.GreaterThan(m.Profile.MaxFundingRiskPct) {
			reject := &models.RiskReject{
				Reason:          models.RejectHighFundingCost,
				Details:         fmt.Sprintf("Funding cost %s of risk > %s", pctZero(funding.FundingEatsRiskPct), pctZero(m.Profile.MaxFundingRiskPct)),
				SuggestedAction: "wait",
			}
			m.logDecision(signal, nil, reject, in, cbStatus, nil)
			return nil, reject
		}
	}

	// All checks passed.
	marginMode := "cross"
	if onlyIsolated {
		marginMode = "isolated"
	}
	stopLoss := decimal.Zero
	slSource := "none"
	if signal.StopLoss != nil {
		stopLoss = *signal.StopLoss
		slSource = "signal"
	}

	order := &models.TradeOrder{
		Coin:                 signal.Pair,
		AssetID:              in.AssetMeta.AssetID,
		Side:                 string(signal.Side),
		Size:                 signal.Size,
		EntryPrice:           entryPrice,
		Leverage:             signal.Leverage,
		MarginMode:           marginMode,
		StopLoss:             stopLoss,
		RiskAmount:           riskAmount,
		RiskPct:              riskPct,
		CumulativeRiskAfter:  cumRisk.RiskPct,
		EstimatedLiquidation: estimatedLiq,
		EstimatedCommission:  notional.Mul(m.HLConfig.TakerFee).Mul(decimal.NewFromInt(2)),
		EstimatedFunding24h:  decimal.Zero,
		MarginRequired:       marginRequired,
		RiskMode:             models.ModeManual,
		SizeSource:           "signal",
		SLSource:             slSource,
		CalculationDetails: map[string]any{
			"mode":          "manual",
			"cb_status":     cbStatus.Level,
			"only_isolated": onlyIsolated,
		},
	}

	m.logDecision(signal, order, nil, in, cbStatus, nil)
	return order, nil
}

// evaluateManaged derives the optimal position from the risk budget. Mirrors
// manager.py:_evaluate_managed.
func (m *Manager) evaluateManaged(
	in EvaluateInput,
	entryPrice decimal.Decimal,
	cbStatus models.CircuitBreakerStatus,
) (*models.TradeOrder, *models.RiskReject) {
	signal := in.Signal
	szDecimals := in.AssetMeta.SzDecimals
	maxLeverageCoin := in.AssetMeta.MaxLeverage
	onlyIsolated := in.AssetMeta.OnlyIsolated

	// Max positions.
	if len(in.OpenPositions) >= m.Profile.MaxOpenPositions {
		reject := &models.RiskReject{
			Reason:          models.RejectMaxPositions,
			Details:         fmt.Sprintf("Already have %d positions", len(in.OpenPositions)),
			SuggestedAction: "close_positions",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// ATR.
	atr, err := m.Calculator.CalculateATR(in.Candles, 14)
	if err != nil {
		reject := &models.RiskReject{
			Reason:          models.RejectATRUnavailable,
			Details:         err.Error(),
			SuggestedAction: "wait",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}
	if atr.LessThanOrEqual(decimal.Zero) {
		reject := &models.RiskReject{
			Reason:          models.RejectATRUnavailable,
			Details:         "ATR is zero (no volatility)",
			SuggestedAction: "wait",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, nil)
		return nil, reject
	}

	// Stop-loss.
	stopResult := m.Calculator.CalculateStopLoss(entryPrice, string(signal.Side), atr, signal.Horizon)

	// Leverage.
	leverageResult := m.Calculator.SelectLeverage(stopResult.DistancePct, maxLeverageCoin)

	// Liquidation + validate stop, reducing leverage if needed.
	estimatedLiq := m.Calculator.EstimateLiquidationPrice(entryPrice, leverageResult.Leverage, string(signal.Side))
	if !m.Calculator.ValidateStopVsLiquidation(stopResult.Price, estimatedLiq, entryPrice, string(signal.Side)) {
		leverageResult = m.Calculator.SelectLeverageForStop(stopResult.Price, entryPrice, string(signal.Side), maxLeverageCoin)
		estimatedLiq = m.Calculator.EstimateLiquidationPrice(entryPrice, leverageResult.Leverage, string(signal.Side))
	}

	// Cumulative risk budget BEFORE sizing.
	cumRiskPreview := m.Calculator.CalculateCumulativeRisk(in.OpenPositions, decimal.Zero, signal.Pair, in.AccountValue)
	maxNewRisk := cumRiskPreview.AvailableBudget
	if maxNewRisk.LessThanOrEqual(decimal.Zero) {
		reject := &models.RiskReject{
			Reason:          models.RejectRiskBudgetExceeded,
			Details:         "No risk budget available",
			SuggestedAction: "close_positions",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, &atr)
		return nil, reject
	}

	// Position size with budget constraint.
	sizeResult, sizeReject := m.Calculator.CalculatePositionSize(PositionSizeInput{
		AccountValue:     in.AccountValue,
		AvailableBalance: in.AvailableBalance,
		EntryPrice:       entryPrice,
		StopPrice:        stopResult.Price,
		Leverage:         leverageResult.Leverage,
		SzDecimals:       szDecimals,
		Confidence:       signal.Confidence,
		RiskMultiplier:   cbStatus.RiskMultiplier,
		MaxRiskAmount:    &maxNewRisk,
	})
	if sizeReject != nil {
		m.logDecision(signal, nil, sizeReject, in, cbStatus, &atr)
		return nil, sizeReject
	}

	// Final cumulative risk check with actual size.
	cumRisk := m.Calculator.CalculateCumulativeRisk(in.OpenPositions, sizeResult.RiskAmount, signal.Pair, in.AccountValue)
	if !cumRisk.WithinLimit {
		reject := &models.RiskReject{
			Reason:          models.RejectRiskBudgetExceeded,
			Details:         fmt.Sprintf("Cumulative risk %s > max", pctOne(cumRisk.RiskPct)),
			SuggestedAction: "reduce_risk",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, &atr)
		return nil, reject
	}

	// Funding cost check.
	funding := m.Calculator.EstimateFundingCost(sizeResult.Size, entryPrice, string(signal.Side), in.FundingRate, sizeResult.RiskAmount, 24)
	if funding.FundingEatsRiskPct.GreaterThan(m.Profile.MaxFundingRiskPct) {
		reject := &models.RiskReject{
			Reason:          models.RejectHighFundingCost,
			Details:         fmt.Sprintf("Funding %s > %s", pctZero(funding.FundingEatsRiskPct), pctZero(m.Profile.MaxFundingRiskPct)),
			SuggestedAction: "wait",
		}
		m.logDecision(signal, nil, reject, in, cbStatus, &atr)
		return nil, reject
	}

	marginMode := "cross"
	if onlyIsolated {
		marginMode = "isolated"
	}

	order := &models.TradeOrder{
		Coin:                 signal.Pair,
		AssetID:              in.AssetMeta.AssetID,
		Side:                 string(signal.Side),
		Size:                 sizeResult.Size,
		EntryPrice:           entryPrice,
		Leverage:             leverageResult.Leverage,
		MarginMode:           marginMode,
		StopLoss:             stopResult.Price,
		RiskAmount:           sizeResult.RiskAmount,
		RiskPct:              sizeResult.RiskPct,
		CumulativeRiskAfter:  cumRisk.RiskPct,
		EstimatedLiquidation: estimatedLiq,
		EstimatedCommission:  sizeResult.CommissionEstimate,
		EstimatedFunding24h:  funding.Projected24h,
		MarginRequired:       sizeResult.MarginRequired,
		RiskMode:             models.ModeManaged,
		SizeSource:           "calculated",
		SLSource:             "calculated",
		CalculationDetails: map[string]any{
			"mode":              "managed",
			"atr":               atr.String(),
			"atr_multiplier":    stopResult.ATRMultiplier.String(),
			"stop_distance_pct": stopResult.DistancePct.String(),
			"leverage_reason":   leverageResult.Reason,
			"confidence":        signal.Confidence,
			"cb_multiplier":     cbStatus.RiskMultiplier.String(),
			"only_isolated":     onlyIsolated,
		},
	}

	m.logDecision(signal, order, nil, in, cbStatus, &atr)
	return order, nil
}

// logDecision records the audit log for the evaluation. Mirrors
// manager.py:_log_decision. Persistence is wired in the storage layer (Phase 5).
func (m *Manager) logDecision(
	signal models.TradingSignal,
	order *models.TradeOrder,
	reject *models.RiskReject,
	in EvaluateInput,
	cbStatus models.CircuitBreakerStatus,
	atrValue *decimal.Decimal,
) {
	cumRiskBefore := decimal.Zero
	for _, p := range in.OpenPositions {
		cumRiskBefore = cumRiskBefore.Add(posRisk(p))
	}
	cumRiskBeforePct := decimal.Zero
	if in.AccountValue.GreaterThan(decimal.Zero) {
		cumRiskBeforePct = cumRiskBefore.Div(in.AccountValue)
	}

	decision := "rejected"
	if order != nil {
		decision = "approved"
	}

	log := &models.RiskDecisionLog{
		Timestamp:               m.now().UTC(),
		RiskMode:                m.RiskMode,
		SignalSource:            signal.Source,
		Coin:                    signal.Pair,
		Side:                    string(signal.Side),
		Decision:                decision,
		MarkPrice:               in.MarkPrice,
		ATRValue:                atrValue,
		FundingRate:             in.FundingRate,
		RiskPerTradePct:         m.Profile.RiskPerTrade,
		CumulativeRiskBeforePct: cumRiskBeforePct,
		OpenPositionsCount:      len(in.OpenPositions),
		ConsecutiveLosses:       cbStatus.ConsecutiveLosses,
		DailyPnlPct:             cbStatus.DailyLossPct,
		AccountValue:            in.AccountValue,
		AvailableBalance:        in.AvailableBalance,
		InputSize:               &signal.Size,
		InputLeverage:           &signal.Leverage,
		InputStopLoss:           signal.StopLoss,
	}
	if reject != nil {
		log.RejectReason = &reject.Reason
	}
	if order != nil {
		log.OutputSize = &order.Size
		log.OutputLeverage = &order.Leverage
		log.OutputStopLoss = &order.StopLoss
		log.CumulativeRiskAfterPct = &order.CumulativeRiskAfter
		log.EstimatedLiquidation = &order.EstimatedLiquidation
	}

	m.lastDecisionLog = log
}

// pctZero formats a ratio as a percentage with no decimals (Python's "{:.0%}").
func pctZero(d decimal.Decimal) string {
	return d.Mul(decimal.NewFromInt(100)).StringFixed(0) + "%"
}
