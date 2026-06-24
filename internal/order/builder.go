// Package order ports the Python order builder.
//
// The slippage price path is a FROZEN crypto-adjacent core (SPEC-007 Phase 1,
// risk #1/#3): it deliberately mixes Decimal and float64 arithmetic to match
// the Hyperliquid SDK byte-for-byte. In particular it reproduces:
//
//   - 5-significant-figure formatting via Python's "%.5g"
//   - Python's round(float, n) which is round-HALF-to-EVEN on the float value
//   - Decimal(str(float)) round-trip
//
// Do NOT "clean up" this float path (e.g. switch slippage to pure Decimal)
// before the golden vectors are green — it changes the wire price.
package order

import (
	"strconv"

	"github.com/shopspring/decimal"

	_ "github.com/skilus/hyperhandler/internal/decimalx" // set DivisionPrecision=28
)

// Default price decimals: 6 for perps, 8 for spot.
const (
	defaultPerpPriceDecimals = 6
	defaultSpotPriceDecimals = 8
)

// DefaultSlippage is the market-order slippage (0.5%).
var DefaultSlippage = decimal.RequireFromString("0.005")

// Builder converts trading signals into Hyperliquid order payloads.
type Builder struct {
	slippage decimal.Decimal
}

// NewBuilder returns a Builder with the given slippage (use DefaultSlippage for
// the standard 0.5%).
func NewBuilder(slippage decimal.Decimal) *Builder {
	return &Builder{slippage: slippage}
}

// slippagePrice applies slippage and rounds exactly as the Python reference
// (order_builder._slippage_price). FROZEN.
func (b *Builder) slippagePrice(price decimal.Decimal, isBuy bool, szDecimals int, isSpot bool) decimal.Decimal {
	one := decimal.NewFromInt(1)

	var px decimal.Decimal
	if isBuy {
		px = price.Mul(one.Add(b.slippage))
	} else {
		px = price.Mul(one.Sub(b.slippage))
	}

	// float(px): nearest float64, matching Python float(Decimal).
	pxFloat, _ := px.Float64()

	// f"{px_float:.5g}" -> float(...): 5 significant figures.
	sigFigStr := strconv.FormatFloat(pxFloat, 'g', 5, 64)
	pxRounded, _ := strconv.ParseFloat(sigFigStr, 64)

	maxDecimals := defaultPerpPriceDecimals
	if isSpot {
		maxDecimals = defaultSpotPriceDecimals
	}
	decimalPlaces := maxDecimals - szDecimals

	finalPrice := pyRound(pxRounded, decimalPlaces)

	// Decimal(str(final_price)): NewFromFloat takes the shortest round-trip
	// decimal of the float, which is exactly what Python's str(float) feeds to
	// Decimal.
	return decimal.NewFromFloat(finalPrice)
}

// formatPrice renders a price for the wire: round to 8 decimals (half-to-even),
// strip trailing zeros. Mirrors order_builder._format_price.
func formatPrice(price decimal.Decimal) string {
	return price.RoundBank(8).String()
}

// formatSize renders a size for the wire; identical rules to formatPrice.
func formatSize(size decimal.Decimal) string {
	return size.RoundBank(8).String()
}

// pyRound reproduces Python's round(x, ndigits) for floats: round-half-to-even
// on the float's exact value. Go's strconv.FormatFloat with 'f' rounds to
// nearest, ties to even, on the exact value — the same algorithm — and parsing
// back yields the nearest float64, matching CPython's double_round.
//
// ndigits must be >= 0 (all Hyperliquid perp/spot decimal places are).
func pyRound(x float64, ndigits int) float64 {
	if ndigits < 0 {
		// Not reachable for HL price decimals; fall back to decimal rounding.
		return decimal.NewFromFloat(x).RoundBank(int32(ndigits)).InexactFloat64()
	}
	s := strconv.FormatFloat(x, 'f', ndigits, 64)
	r, _ := strconv.ParseFloat(s, 64)
	return r
}
