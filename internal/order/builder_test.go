package order

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/golden"
)

// TestOrderFloatPathGolden is the SPEC-007 Phase 1 gate for the frozen float
// path: slippage price and price/size formatting must match the Python
// reference byte-for-byte.
func TestOrderFloatPathGolden(t *testing.T) {
	g, err := golden.LoadOrder()
	require.NoError(t, err)

	t.Run("slippage", func(t *testing.T) {
		for _, c := range g.Slippage {
			b := NewBuilder(decimal.RequireFromString(c.Slippage))
			price := decimal.RequireFromString(c.Price)

			got := b.slippagePrice(price, c.IsBuy, c.SzDecimals, c.IsSpot)

			// Compare by value (the intermediate Decimal representation may
			// carry a trailing zero in Python, e.g. "67838.0").
			want := decimal.RequireFromString(c.Result)
			assert.Truef(t, got.Equal(want),
				"slippage %s buy=%v sz=%d spot=%v: want %s got %s",
				c.Price, c.IsBuy, c.SzDecimals, c.IsSpot, c.Result, got.String())

			// The wire string must match exactly.
			assert.Equalf(t, c.Formatted, formatPrice(got),
				"formatted slippage %s buy=%v sz=%d", c.Price, c.IsBuy, c.SzDecimals)
		}
	})

	t.Run("formatting", func(t *testing.T) {
		for _, c := range g.Formatting {
			v := decimal.RequireFromString(c.Value)
			assert.Equalf(t, c.FormattedPrice, formatPrice(v),
				"formatPrice(%s)", c.Value)
			assert.Equalf(t, c.FormattedSize, formatSize(v),
				"formatSize(%s)", c.Value)
		}
	})
}
