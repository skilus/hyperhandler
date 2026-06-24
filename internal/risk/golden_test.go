package risk_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/decimalx"
	"github.com/skilus/hyperhandler/internal/golden"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

// Ensure DivisionPrecision matches Python before the golden math runs.
var _ = decimalx.Precision

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	require.NoError(t, err, "parse decimal %q", s)
	return d
}

// eqDec compares two decimals by value (so "4.000…" == "4" and "0E-8" == "0").
func eqDec(t *testing.T, want string, got decimal.Decimal, msg string) {
	t.Helper()
	assert.Truef(t, dec(t, want).Equal(got), "%s: want %s, got %s", msg, want, got.String())
}

func mediumCalc() *risk.Calculator {
	return risk.NewCalculator(risk.GetProfile(models.RiskMedium), risk.DefaultHLConfig())
}

func horizonFor(s string) models.SignalHorizon { return models.SignalHorizon(s) }

// TestRiskGolden verifies the Decimal-heavy risk math byte-for-byte (by value)
// against reference outputs generated from the Python RiskCalculator.
func TestRiskGolden(t *testing.T) {
	g, err := golden.LoadRisk()
	require.NoError(t, err)
	calc := mediumCalc()

	t.Run("ATR", func(t *testing.T) {
		for _, v := range g.ATR {
			candles := make([]risk.Candle, len(v.Candles))
			for i, c := range v.Candles {
				candles[i] = risk.Candle{High: dec(t, c.H), Low: dec(t, c.L), Close: dec(t, c.C)}
			}
			got, err := calc.CalculateATR(candles, v.Period)
			require.NoError(t, err, v.Label)
			eqDec(t, v.Result, got, "atr "+v.Label)
		}
	})

	t.Run("StopLoss", func(t *testing.T) {
		for _, v := range g.StopLoss {
			r := calc.CalculateStopLoss(dec(t, v.Entry), v.Side, dec(t, v.ATR), horizonFor(v.Horizon))
			eqDec(t, v.Price, r.Price, "sl.price "+v.Horizon)
			eqDec(t, v.Distance, r.Distance, "sl.distance "+v.Horizon)
			eqDec(t, v.DistancePct, r.DistancePct, "sl.distance_pct "+v.Horizon)
			eqDec(t, v.ATRValue, r.ATRValue, "sl.atr_value "+v.Horizon)
			eqDec(t, v.ATRMultiplier, r.ATRMultiplier, "sl.atr_multiplier "+v.Horizon)
		}
	})

	t.Run("Liquidation", func(t *testing.T) {
		for _, v := range g.Liquidation {
			got := calc.EstimateLiquidationPrice(dec(t, v.Entry), v.Leverage, v.Side)
			eqDec(t, v.Result, got, "liq")
		}
	})

	t.Run("ValidateStop", func(t *testing.T) {
		for _, v := range g.ValidateStop {
			got := calc.ValidateStopVsLiquidation(dec(t, v.Stop), dec(t, v.Liq), dec(t, v.Entry), v.Side)
			assert.Equalf(t, v.Valid, got, "validate_stop %s/%s/%s/%s", v.Stop, v.Liq, v.Entry, v.Side)
		}
	})

	t.Run("SelectLeverage", func(t *testing.T) {
		for _, v := range g.SelectLeverage {
			r := calc.SelectLeverage(dec(t, v.StopDistancePct), v.MaxCoin)
			assert.Equal(t, v.Leverage, r.Leverage, "lev.leverage")
			assert.Equal(t, v.MaxSafe, r.MaxSafe, "lev.max_safe")
			assert.Equal(t, v.MaxCoinOut, r.MaxCoin, "lev.max_coin")
			assert.Equal(t, v.MaxConfig, r.MaxConfig, "lev.max_config")
			assert.Equal(t, v.Reason, r.Reason, "lev.reason")
		}
	})

	t.Run("SelectLeverageForStop", func(t *testing.T) {
		for _, v := range g.SelectLeverageForStop {
			r := calc.SelectLeverageForStop(dec(t, v.Stop), dec(t, v.Entry), v.Side, v.MaxCoin)
			assert.Equal(t, v.Leverage, r.Leverage, "levfs.leverage")
			assert.Equal(t, v.MaxSafe, r.MaxSafe, "levfs.max_safe")
			assert.Equal(t, v.Reason, r.Reason, "levfs.reason")
		}
	})

	t.Run("PositionSize", func(t *testing.T) {
		for _, v := range g.PositionSize {
			in := risk.PositionSizeInput{
				AccountValue:     dec(t, v.Input.AccountValue),
				AvailableBalance: dec(t, v.Input.AvailableBalance),
				EntryPrice:       dec(t, v.Input.EntryPrice),
				StopPrice:        dec(t, v.Input.StopPrice),
				Leverage:         v.Input.Leverage,
				SzDecimals:       v.Input.SzDecimals,
				Confidence:       v.Input.Confidence,
				RiskMultiplier:   decimal.NewFromInt(1),
			}
			if v.Input.RiskMultiplier != nil {
				in.RiskMultiplier = dec(t, *v.Input.RiskMultiplier)
			}
			if v.Input.MaxRiskAmount != nil {
				m := dec(t, *v.Input.MaxRiskAmount)
				in.MaxRiskAmount = &m
			}
			res, rej := calc.CalculatePositionSize(in)
			if v.IsReject {
				require.NotNilf(t, rej, "%s: expected reject", v.Label)
				assert.Equal(t, models.RejectReason(v.RejectReason), rej.Reason, v.Label)
				continue
			}
			require.NotNilf(t, res, "%s: expected result, got reject", v.Label)
			eqDec(t, v.Result.Size, res.Size, v.Label+".size")
			eqDec(t, v.Result.Notional, res.Notional, v.Label+".notional")
			eqDec(t, v.Result.MarginRequired, res.MarginRequired, v.Label+".margin")
			eqDec(t, v.Result.RiskAmount, res.RiskAmount, v.Label+".risk_amount")
			eqDec(t, v.Result.RiskPct, res.RiskPct, v.Label+".risk_pct")
			eqDec(t, v.Result.CommissionEstimate, res.CommissionEstimate, v.Label+".commission")
		}
	})

	t.Run("CumulativeRisk", func(t *testing.T) {
		for _, v := range g.CumulativeRisk {
			positions := make([]models.Position, len(v.Positions))
			for i, p := range v.Positions {
				pos := models.Position{Coin: p.Coin}
				if p.RiskAmount != nil {
					ra := dec(t, *p.RiskAmount)
					pos.RiskAmount = &ra
				}
				positions[i] = pos
			}
			r := calc.CalculateCumulativeRisk(positions, dec(t, v.NewRiskAmount), v.NewCoin, dec(t, v.AccountValue))
			eqDec(t, v.RawRisk, r.RawRisk, v.Label+".raw_risk")
			eqDec(t, v.AdjustedRisk, r.AdjustedRisk, v.Label+".adjusted_risk")
			eqDec(t, v.RiskPct, r.RiskPct, v.Label+".risk_pct")
			eqDec(t, v.AvailableBudget, r.AvailableBudget, v.Label+".available_budget")
			assert.Equal(t, v.WithinLimit, r.WithinLimit, v.Label+".within_limit")
			assert.Equal(t, v.CorrelationGroups, r.CorrelationGroups, v.Label+".correlation_groups")
		}
	})

	t.Run("Funding", func(t *testing.T) {
		for _, v := range g.Funding {
			r := calc.EstimateFundingCost(dec(t, v.Size), dec(t, v.Entry), v.Side, dec(t, v.FundingRate), dec(t, v.RiskAmount), v.HoldHours)
			eqDec(t, v.HourlyRate, r.HourlyRate, "funding.hourly_rate")
			eqDec(t, v.HourlyCost, r.HourlyCost, "funding.hourly_cost")
			eqDec(t, v.HourlyIncome, r.HourlyIncome, "funding.hourly_income")
			eqDec(t, v.Projected24h, r.Projected24h, "funding.projected_24h")
			eqDec(t, v.FundingEatsRiskPct, r.FundingEatsRiskPct, "funding.eats_risk_pct")
		}
	})

	t.Run("RoundDown", func(t *testing.T) {
		for _, v := range g.RoundDown {
			got := risk.RoundDownForTest(dec(t, v.Value), v.Decimals)
			eqDec(t, v.Result, got, "round_down")
		}
	})
}
