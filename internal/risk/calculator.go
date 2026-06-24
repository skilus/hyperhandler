package risk

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
)

// Candle is the minimal OHLC input the ATR calculation needs. The manager
// converts client candles into this shape so the calculator stays decoupled
// from the HTTP layer (and trivially testable). Mirrors the {h,l,c} dict fields
// used by calculator.py:calculate_atr.
type Candle struct {
	High  decimal.Decimal
	Low   decimal.Decimal
	Close decimal.Decimal
}

// Calculator holds the pure risk-calculation functions. Mirrors
// calculator.py:RiskCalculator.
type Calculator struct {
	Profile  RiskProfile
	HLConfig HLConfig
}

// NewCalculator builds a Calculator for the given profile and HL config.
func NewCalculator(profile RiskProfile, hlConfig HLConfig) *Calculator {
	return &Calculator{Profile: profile, HLConfig: hlConfig}
}

// CalculateATR computes ATR using the EMA method (pure decimal, no numpy).
// Returns an error if there are fewer than 2 candles. Mirrors
// calculator.py:calculate_atr.
func (c *Calculator) CalculateATR(candles []Candle, period int) (decimal.Decimal, error) {
	if len(candles) < 2 {
		return decimal.Zero, fmt.Errorf("Need at least 2 candles for ATR")
	}

	trueRanges := make([]decimal.Decimal, 0, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		high := candles[i].High
		low := candles[i].Low
		prevClose := candles[i-1].Close

		tr := decimal.Max(
			high.Sub(low),
			high.Sub(prevClose).Abs(),
			low.Sub(prevClose).Abs(),
		)
		trueRanges = append(trueRanges, tr)
	}

	if len(trueRanges) < period {
		// Not enough data for full EMA, use simple average.
		sum := decimal.Zero
		for _, tr := range trueRanges {
			sum = sum.Add(tr)
		}
		return round28(sum.Div(decimal.NewFromInt(int64(len(trueRanges))))), nil
	}

	// EMA calculation: alpha = 2 / (period + 1). Python rounds every Decimal
	// operation to 28 significant figures (ROUND_HALF_EVEN); shopspring only
	// rounds on division, so we round each step explicitly to stay byte-equal.
	alpha := round28(decimal.NewFromInt(2).Div(decimal.NewFromInt(int64(period)).Add(decimal.NewFromInt(1))))
	oneMinusAlpha := round28(decimal.NewFromInt(1).Sub(alpha))
	ema := trueRanges[0]
	for _, tr := range trueRanges[1:] {
		term1 := round28(alpha.Mul(tr))
		term2 := round28(oneMinusAlpha.Mul(ema))
		ema = round28(term1.Add(term2))
	}
	return ema, nil
}

// round28 rounds to 28 significant figures with banker's rounding, emulating
// Python's default decimal context (prec=28, ROUND_HALF_EVEN). shopspring keeps
// full precision through Mul/Add/Sub, so sequential operations (notably the ATR
// EMA) must round at each step to match Python exactly. SPEC-007 risk #2.
func round28(d decimal.Decimal) decimal.Decimal {
	if d.IsZero() {
		return d
	}
	// adjExp is the exponent of the most-significant digit.
	adjExp := d.NumDigits() - 1 + int(d.Exponent())
	places := 27 - adjExp // decimal places that keep 28 significant figures
	return d.RoundBank(int32(places))
}

// CalculateStopLoss computes an ATR-based stop-loss price. Mirrors
// calculator.py:calculate_stop_loss.
func (c *Calculator) CalculateStopLoss(
	entryPrice decimal.Decimal,
	side string,
	atr decimal.Decimal,
	horizon models.SignalHorizon,
) models.StopLossResult {
	multiplier := ATRSettings[horizon].Multiplier

	stopDistance := atr.Mul(multiplier)
	buffer := stopDistance.Mul(c.HLConfig.SlippageBuffer)
	stopWithBuffer := stopDistance.Add(buffer)

	var stopPrice decimal.Decimal
	if side == "long" {
		stopPrice = entryPrice.Sub(stopWithBuffer)
	} else {
		stopPrice = entryPrice.Add(stopWithBuffer)
	}

	return models.StopLossResult{
		Price:         stopPrice,
		Distance:      stopWithBuffer,
		DistancePct:   stopWithBuffer.Div(entryPrice),
		ATRValue:      atr,
		ATRMultiplier: multiplier,
	}
}

// EstimateLiquidationPrice estimates the liquidation price for a NEW position
// (simplified HL cross-margin formula). Mirrors
// calculator.py:estimate_liquidation_price.
func (c *Calculator) EstimateLiquidationPrice(
	entryPrice decimal.Decimal,
	leverage int,
	side string,
) decimal.Decimal {
	liqDistancePct := decimal.NewFromInt(1).Div(decimal.NewFromInt(int64(leverage))).Sub(HLMaintenanceMargin)
	if floor := decimal.NewFromFloat(0.01); liqDistancePct.LessThan(floor) {
		liqDistancePct = floor // floor at 1%
	}

	if side == "long" {
		return entryPrice.Mul(decimal.NewFromInt(1).Sub(liqDistancePct))
	}
	return entryPrice.Mul(decimal.NewFromInt(1).Add(liqDistancePct))
}

// ValidateStopVsLiquidation reports whether the stop-loss sits closer to entry
// than liquidation, with the configured safety buffer. Mirrors
// calculator.py:validate_stop_vs_liquidation.
func (c *Calculator) ValidateStopVsLiquidation(
	stopPrice, liquidationPrice, entryPrice decimal.Decimal,
	side string,
) bool {
	if side == "long" {
		// For long: stop must be ABOVE liquidation.
		if stopPrice.LessThanOrEqual(liquidationPrice) {
			return false
		}
	} else {
		// For short: stop must be BELOW liquidation.
		if stopPrice.GreaterThanOrEqual(liquidationPrice) {
			return false
		}
	}

	// Check safety buffer (stop should be at least liq_safety_buffer away).
	liqBuffer := stopPrice.Sub(liquidationPrice).Abs().Div(entryPrice)
	return liqBuffer.GreaterThanOrEqual(c.HLConfig.LiqSafetyBuffer)
}

// SelectLeverage selects leverage so liquidation stays beyond the stop-loss.
// Mirrors calculator.py:select_leverage.
func (c *Calculator) SelectLeverage(
	stopDistancePct decimal.Decimal,
	maxLeverageCoin int,
) models.LeverageResult {
	safetyFactor := decimal.NewFromFloat(1.5)

	var maxSafe int
	if stopDistancePct.LessThanOrEqual(decimal.Zero) {
		maxSafe = c.Profile.MaxLeverage
	} else {
		maxSafe = int(decimal.NewFromInt(1).Div(stopDistancePct.Mul(safetyFactor)).IntPart())
		if maxSafe < 1 {
			maxSafe = 1
		}
	}

	leverage := minInt(maxSafe, maxLeverageCoin, c.Profile.MaxLeverage)

	var reason []string
	if leverage == maxSafe {
		reason = append(reason, "safe_for_stop")
	}
	if leverage == maxLeverageCoin {
		reason = append(reason, "coin_max")
	}
	if leverage == c.Profile.MaxLeverage {
		reason = append(reason, "config_max")
	}
	reasonStr := "default"
	if len(reason) > 0 {
		reasonStr = joinPlus(reason)
	}

	return models.LeverageResult{
		Leverage:  leverage,
		MaxSafe:   maxSafe,
		MaxCoin:   maxLeverageCoin,
		MaxConfig: c.Profile.MaxLeverage,
		Reason:    reasonStr,
	}
}

// SelectLeverageForStop selects leverage that makes the given stop valid (used
// when an ATR-based stop requires lower leverage). Mirrors
// calculator.py:select_leverage_for_stop.
func (c *Calculator) SelectLeverageForStop(
	stopPrice, entryPrice decimal.Decimal,
	side string,
	maxLeverageCoin int,
) models.LeverageResult {
	stopDistancePct := entryPrice.Sub(stopPrice).Abs().Div(entryPrice)

	// Liquidation must be further than stop + safety buffer.
	requiredLiqDistance := stopDistancePct.Add(c.HLConfig.LiqSafetyBuffer)

	// leverage ≈ 1 / (liq_distance + maintenance).
	maxSafe := int(decimal.NewFromInt(1).Div(requiredLiqDistance.Add(HLMaintenanceMargin)).IntPart())
	if maxSafe < 1 {
		maxSafe = 1
	}

	leverage := minInt(maxSafe, maxLeverageCoin, c.Profile.MaxLeverage)

	return models.LeverageResult{
		Leverage:  leverage,
		MaxSafe:   maxSafe,
		MaxCoin:   maxLeverageCoin,
		MaxConfig: c.Profile.MaxLeverage,
		Reason:    "adjusted_for_stop",
	}
}

// PositionSizeInput groups the arguments to CalculatePositionSize. Confidence
// is a pointer to mirror the Python `float | None` default.
type PositionSizeInput struct {
	AccountValue     decimal.Decimal
	AvailableBalance decimal.Decimal
	EntryPrice       decimal.Decimal
	StopPrice        decimal.Decimal
	Leverage         int
	SzDecimals       int
	Confidence       *float64
	RiskMultiplier   decimal.Decimal  // circuit-breaker multiplier (default 1.0)
	MaxRiskAmount    *decimal.Decimal // budget constraint, nil = unconstrained
}

// CalculatePositionSize sizes a position from the risk budget. Returns either a
// *models.PositionSizeResult or a *models.RiskReject (exactly one is non-nil),
// mirroring the Python union return of calculator.py:calculate_position_size.
func (c *Calculator) CalculatePositionSize(in PositionSizeInput) (*models.PositionSizeResult, *models.RiskReject) {
	// Clamp confidence factor.
	var confidenceFactor decimal.Decimal
	if in.Confidence != nil {
		confidenceFactor = decimal.NewFromFloat(*in.Confidence)
		if confidenceFactor.GreaterThan(MaxConfidenceFactor) {
			confidenceFactor = MaxConfidenceFactor
		}
		if confidenceFactor.LessThan(MinConfidenceFactor) {
			confidenceFactor = MinConfidenceFactor
		}
	} else {
		confidenceFactor = MaxConfidenceFactor
	}

	riskMultiplier := in.RiskMultiplier
	riskPct := c.Profile.RiskPerTrade.Mul(confidenceFactor).Mul(riskMultiplier)
	riskAmount := in.AccountValue.Mul(riskPct)

	// Apply budget constraint.
	if in.MaxRiskAmount != nil && riskAmount.GreaterThan(*in.MaxRiskAmount) {
		riskAmount = *in.MaxRiskAmount
	}

	stopDistance := in.EntryPrice.Sub(in.StopPrice).Abs()
	if stopDistance.IsZero() {
		return nil, &models.RiskReject{
			Reason:          models.RejectATRUnavailable,
			Details:         "Stop distance is zero",
			SuggestedAction: "wait",
		}
	}

	rawSize := riskAmount.Div(stopDistance)
	notional := rawSize.Mul(in.EntryPrice)
	commission := notional.Mul(c.HLConfig.TakerFee).Mul(decimal.NewFromInt(2))

	// Adjust for commission.
	adjustedRisk := riskAmount.Sub(commission)
	if adjustedRisk.LessThanOrEqual(decimal.Zero) {
		return nil, &models.RiskReject{
			Reason:          models.RejectPositionTooSmall,
			Details:         "Risk amount doesn't cover commission",
			SuggestedAction: "wait",
		}
	}

	adjustedSize := adjustedRisk.Div(stopDistance)

	// Check margin constraint.
	leverageDec := decimal.NewFromInt(int64(in.Leverage))
	marginRequired := adjustedSize.Mul(in.EntryPrice).Div(leverageDec)
	if marginRequired.GreaterThan(in.AvailableBalance) {
		maxSize := in.AvailableBalance.Mul(leverageDec).Div(in.EntryPrice)
		if maxSize.LessThan(adjustedSize) {
			adjustedSize = maxSize
		}
		marginRequired = adjustedSize.Mul(in.EntryPrice).Div(leverageDec)
	}

	// Round down to szDecimals.
	adjustedSize = roundDown(adjustedSize, in.SzDecimals)

	finalNotional := adjustedSize.Mul(in.EntryPrice)
	finalRisk := adjustedSize.Mul(stopDistance)
	finalCommission := finalNotional.Mul(c.HLConfig.TakerFee).Mul(decimal.NewFromInt(2))

	// Check minimum order.
	if finalNotional.LessThan(c.HLConfig.MinOrderValue) {
		return nil, &models.RiskReject{
			Reason:          models.RejectPositionTooSmall,
			Details:         fmt.Sprintf("Order $%s < min $%s", finalNotional.StringFixed(2), c.HLConfig.MinOrderValue.String()),
			SuggestedAction: "wait",
		}
	}

	riskPctOut := decimal.Zero
	if in.AccountValue.GreaterThan(decimal.Zero) {
		riskPctOut = finalRisk.Div(in.AccountValue)
	}

	return &models.PositionSizeResult{
		Size:               adjustedSize,
		Notional:           finalNotional,
		MarginRequired:     marginRequired,
		RiskAmount:         finalRisk,
		RiskPct:            riskPctOut,
		CommissionEstimate: finalCommission,
	}, nil
}

// CalculateCumulativeRisk computes portfolio cumulative risk with correlation
// adjustment and cascade buffer. Mirrors calculator.py:calculate_cumulative_risk.
func (c *Calculator) CalculateCumulativeRisk(
	openPositions []models.Position,
	newRiskAmount decimal.Decimal,
	newCoin string,
	accountValue decimal.Decimal,
) models.CumulativeRiskResult {
	// Build correlation groups from positions, preserving insertion order.
	groups := map[string][]string{}
	var groupOrder []string
	addGroup := func(name string) {
		if _, ok := groups[name]; !ok {
			groups[name] = []string{}
			groupOrder = append(groupOrder, name)
		}
	}
	for _, pos := range openPositions {
		group := c.correlationGroup(pos.Coin)
		addGroup(group)
		groups[group] = append(groups[group], pos.Coin)
	}

	// Add new coin if we're actually adding risk.
	if newRiskAmount.GreaterThan(decimal.Zero) {
		newGroup := c.correlationGroup(newCoin)
		addGroup(newGroup)
		if !containsStr(groups[newGroup], newCoin) {
			groups[newGroup] = append(groups[newGroup], newCoin)
		}
	}

	// Sum raw risk.
	totalRisk := decimal.Zero
	for _, pos := range openPositions {
		totalRisk = totalRisk.Add(posRisk(pos))
	}

	// Adjusted risk with correlation penalty + cascade buffer.
	adjustedRisk := decimal.Zero
	cascadeBuffer := decimal.Zero
	for _, name := range groupOrder {
		coins := groups[name]
		groupRisk := decimal.Zero
		for _, pos := range openPositions {
			if containsStr(coins, pos.Coin) {
				groupRisk = groupRisk.Add(posRisk(pos))
			}
		}

		n := len(coins)
		penalized := groupRisk
		if n > 1 {
			penalty := decimal.NewFromInt(1).Add(decimal.NewFromInt(int64(n - 1)).Mul(c.Profile.CorrelationFactor))
			penalized = groupRisk.Mul(penalty)
			cascadeBuffer = cascadeBuffer.Add(groupRisk.Mul(decimal.NewFromFloat(0.1)))
		}
		adjustedRisk = adjustedRisk.Add(penalized)
	}

	totalAdjusted := adjustedRisk.Add(cascadeBuffer).Add(newRiskAmount)
	maxRisk := accountValue.Mul(c.Profile.MaxCumulativeRisk)

	riskPct := decimal.Zero
	if accountValue.GreaterThan(decimal.Zero) {
		riskPct = totalAdjusted.Div(accountValue)
	}
	availableBudget := maxRisk.Sub(adjustedRisk).Sub(cascadeBuffer)
	if availableBudget.LessThan(decimal.Zero) {
		availableBudget = decimal.Zero
	}

	return models.CumulativeRiskResult{
		RawRisk:           totalRisk.Add(newRiskAmount),
		AdjustedRisk:      totalAdjusted,
		RiskPct:           riskPct,
		AvailableBudget:   availableBudget,
		WithinLimit:       totalAdjusted.LessThanOrEqual(maxRisk),
		CorrelationGroups: groups,
	}
}

// EstimateFundingCost estimates funding costs/income over the hold period.
// Mirrors calculator.py:estimate_funding_cost.
func (c *Calculator) EstimateFundingCost(
	size, entryPrice decimal.Decimal,
	side string,
	fundingRate, riskAmount decimal.Decimal,
	holdHours int,
) models.FundingEstimate {
	notional := size.Mul(entryPrice)
	hourlyPayment := notional.Mul(fundingRate)

	var projectedCost decimal.Decimal
	if side == "long" {
		projectedCost = hourlyPayment.Mul(decimal.NewFromInt(int64(holdHours)))
	} else {
		projectedCost = hourlyPayment.Neg().Mul(decimal.NewFromInt(int64(holdHours)))
	}

	fundingEats := decimal.Zero
	if riskAmount.GreaterThan(decimal.Zero) {
		fundingEats = decimal.Max(decimal.Zero, projectedCost).Div(riskAmount)
	}

	hourlyCost := decimal.Zero
	hourlyIncome := decimal.Zero
	if projectedCost.GreaterThan(decimal.Zero) {
		hourlyCost = hourlyPayment.Abs()
	} else if projectedCost.LessThan(decimal.Zero) {
		hourlyIncome = hourlyPayment.Abs()
	}

	return models.FundingEstimate{
		HourlyRate:         fundingRate,
		HourlyCost:         hourlyCost,
		HourlyIncome:       hourlyIncome,
		Projected24h:       projectedCost,
		FundingEatsRiskPct: fundingEats,
	}
}

// GetAssetIDFromMeta extracts the asset ID from metadata. Mirrors
// calculator.py:get_asset_id_from_meta; the index is threaded via the meta map
// under "_asset_id".
func (c *Calculator) GetAssetIDFromMeta(meta map[string]any) int {
	if v, ok := meta["_asset_id"]; ok {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}

// correlationGroup returns the correlation group for a coin. Mirrors
// calculator.py:_get_correlation_group.
func (c *Calculator) correlationGroup(coin string) string {
	for group, coins := range CorrelationMap {
		if containsStr(coins, coin) {
			return group
		}
	}
	return "independent-" + coin
}

// roundDown rounds down (toward zero) to the given number of decimals. Mirrors
// calculator.py:_round_down (ROUND_DOWN). Panics on negative decimals to mirror
// the Python assertion.
func roundDown(value decimal.Decimal, decimals int) decimal.Decimal {
	if decimals < 0 {
		panic(fmt.Sprintf("decimals must be >= 0, got %d", decimals))
	}
	return value.Truncate(int32(decimals))
}

// posRisk returns a position's risk amount, treating nil as zero (mirrors the
// Python `pos.risk_amount or Decimal("0")`).
func posRisk(p models.Position) decimal.Decimal {
	if p.RiskAmount == nil {
		return decimal.Zero
	}
	return *p.RiskAmount
}

func containsStr(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func minInt(xs ...int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func joinPlus(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += "+" + p
	}
	return out
}
