// Package decimalx centralizes the shopspring/decimal configuration so the Go
// port matches Python's Decimal arithmetic on the frozen crypto path.
//
// Python's default Decimal context carries 28 significant digits; shopspring's
// default DivisionPrecision is only 16, which diverges on the tails of repeated
// divisions (SPEC-007 risk #2). Importing this package (directly or for its
// side effect) raises the precision to 28.
//
// Any package performing decimal division that feeds the signature or risk math
// MUST import decimalx so the global context is set before first use.
package decimalx

import "github.com/shopspring/decimal"

// Precision matches Python's default decimal context (28 significant digits).
const Precision = 28

func init() {
	decimal.DivisionPrecision = Precision
}
