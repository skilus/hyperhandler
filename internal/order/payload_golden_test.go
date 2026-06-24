package order_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/golden"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/signer"
)

// TestBuildOrderPayloadGolden proves the Go signal→payload assembly is
// byte-identical to the Python OrderBuilder: same msgpack bytes (so same key
// order, slippage and trigger prices) and same keccak256 action hash, with the
// action hash taken from the official Hyperliquid SDK oracle.
func TestBuildOrderPayloadGolden(t *testing.T) {
	g, err := golden.LoadOrder()
	require.NoError(t, err)
	require.NotEmpty(t, g.Payloads, "no payload vectors")

	for _, v := range g.Payloads {
		t.Run(v.Label, func(t *testing.T) {
			sig := signalFromRecipe(t, v.Signal)
			b := builder() // 0.5% slippage, matches the generator

			payload, err := b.BuildOrderPayload(sig, v.AssetIndex, optDec(v.CurrentPrice), v.SzDecimals)
			require.NoError(t, err)

			packed, err := signer.MarshalAction(payload)
			require.NoError(t, err)
			assert.Equal(t, v.MsgpackHex, hex.EncodeToString(packed), "msgpack bytes mismatch")

			hash, err := signer.ActionHash(payload, nil, v.Nonce, nil)
			require.NoError(t, err)
			assert.Equal(t, strings.ToLower(v.ActionHash), "0x"+hex.EncodeToString(hash), "action hash mismatch")
		})
	}
}

func signalFromRecipe(t *testing.T, r golden.PayloadSignal) *models.TradingSignal {
	t.Helper()
	sig, err := models.NewTradingSignal(models.SignalParams{
		Pair:       r.Pair,
		Side:       models.OrderSide(r.Side),
		OrderType:  models.OrderType(r.OrderType),
		Size:       dec(r.Size),
		Leverage:   r.Leverage,
		EntryPrice: optDec(r.EntryPrice),
		StopLoss:   optDec(r.StopLoss),
		TakeProfit: optDec(r.TakeProfit),
	})
	require.NoError(t, err)
	return sig
}

func optDec(s *string) *decimal.Decimal {
	if s == nil {
		return nil
	}
	return decP(*s)
}
