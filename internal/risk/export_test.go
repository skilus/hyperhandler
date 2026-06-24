package risk

import "github.com/shopspring/decimal"

// RoundDownForTest exposes the unexported roundDown helper to the external test
// package so the golden vectors can verify ROUND_DOWN behaviour directly.
func RoundDownForTest(value decimal.Decimal, decimals int) decimal.Decimal {
	return roundDown(value, decimals)
}
