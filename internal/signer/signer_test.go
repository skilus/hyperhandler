package signer_test

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/golden"
	"github.com/skilus/hyperhandler/internal/signer"
)

// reconstructAction rebuilds the exact ordered action for a golden vector.
// Key order is hand-mirrored from the Python reference; JSON can't be reused
// because unmarshaling loses key order and int/bool typing.
func reconstructAction(label string) any {
	om := signer.NewOrderedMap
	switch label {
	case "order_mainnet":
		return om(
			"type", "order",
			"orders", []any{
				om("a", 0, "b", true, "p", "67500", "s", "0.1", "r", false),
			},
			"grouping", "na",
		)
	case "simple_testnet":
		return om("type", "test", "data", "value")
	case "order_with_vault", "order_with_expires", "order_vault_and_expires":
		return om("type", "order", "orders", []any{})
	case "full_order_tpsl":
		return om(
			"type", "order",
			"orders", []any{
				om("a", 0, "b", true, "p", "67500", "s", "0.1", "r", false,
					"t", om("limit", om("tif", "Ioc"))),
				om("a", 0, "b", false, "p", "63787.5", "s", "0.1", "r", true,
					"t", om("trigger", om("isMarket", true, "triggerPx", "64000", "tpsl", "sl"))),
				om("a", 0, "b", false, "p", "71437.5", "s", "0.1", "r", true,
					"t", om("trigger", om("isMarket", true, "triggerPx", "71000", "tpsl", "tp"))),
			},
			"grouping", "normalTpsl",
		)
	default:
		return nil
	}
}

// TestSignerGoldenByteIdentity is the SPEC-007 Phase 1 gate for the signer:
// every golden vector must reproduce msgpack bytes, action hash, and the
// EIP-712 signature exactly.
func TestSignerGoldenByteIdentity(t *testing.T) {
	g, err := golden.LoadSigner()
	require.NoError(t, err)
	require.NotEmpty(t, g.Vectors)

	for _, v := range g.Vectors {
		t.Run(v.Label, func(t *testing.T) {
			action := reconstructAction(v.Label)
			require.NotNil(t, action, "no reconstruction for %q", v.Label)

			s, err := signer.New(v.PrivateKey, v.IsMainnet)
			require.NoError(t, err)

			// Address parity.
			assert.Equal(t, v.Address, s.Address())

			// Action hash (which transitively pins the msgpack bytes).
			hash, err := signer.ActionHash(action, v.VaultAddress, v.Nonce, v.ExpiresAfter)
			require.NoError(t, err)
			assert.Equal(t, strings.ToLower(v.ActionHash), "0x"+hex.EncodeToString(hash),
				"action hash mismatch")

			// Full signed payload.
			payload, err := s.SignAction(action, v.Nonce, v.VaultAddress, v.ExpiresAfter)
			require.NoError(t, err)

			// Signature must match the oracle by integer value (r/s rendered as
			// minimal hex) and exact v.
			assertHexEqualByValue(t, v.Signature.R, payload.Signature.R, "r")
			assertHexEqualByValue(t, v.Signature.S, payload.Signature.S, "s")
			assert.Equal(t, v.Signature.V, payload.Signature.V, "v")

			assert.Equal(t, v.Nonce, payload.Nonce)
			assert.Equal(t, v.VaultAddress, payload.VaultAddress)
			assert.Equal(t, v.ExpiresAfter, payload.ExpiresAfter)
		})
	}
}

// TestSignerMsgpackByteIdentity checks the raw msgpack bytes directly against
// the golden hex, independent of the hashing/signing path.
func TestSignerMsgpackByteIdentity(t *testing.T) {
	g, err := golden.LoadSigner()
	require.NoError(t, err)

	for _, v := range g.Vectors {
		t.Run(v.Label, func(t *testing.T) {
			action := reconstructAction(v.Label)
			require.NotNil(t, action)
			packed, err := signer.MarshalAction(action)
			require.NoError(t, err)
			assert.Equal(t, v.MsgpackHex, hex.EncodeToString(packed), "msgpack bytes mismatch")
		})
	}
}

func assertHexEqualByValue(t *testing.T, want, got, name string) {
	t.Helper()
	w, ok := new(big.Int).SetString(strings.TrimPrefix(want, "0x"), 16)
	require.True(t, ok, "%s: bad golden hex %q", name, want)
	g, ok := new(big.Int).SetString(strings.TrimPrefix(got, "0x"), 16)
	require.True(t, ok, "%s: bad produced hex %q", name, got)
	assert.Zero(t, w.Cmp(g), "%s mismatch: want %s got %s", name, want, got)
}
