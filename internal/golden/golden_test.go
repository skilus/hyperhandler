package golden

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSignerGoldenLoads is the SPEC-007 Phase 0 gate: the generated vectors must
// load and contain the test_signer.py U-SGN-01 example with a well-formed
// signature and a valid 32-byte action hash. Full byte-for-byte verification of
// a native Go signer lands in Phase 1.
func TestSignerGoldenLoads(t *testing.T) {
	g, err := LoadSigner()
	require.NoError(t, err)
	require.NotEmpty(t, g.Vectors, "signer golden must contain vectors")

	byLabel := make(map[string]SignerVector, len(g.Vectors))
	for _, v := range g.Vectors {
		byLabel[v.Label] = v
	}

	gate, ok := byLabel["order_mainnet"]
	require.True(t, ok, "U-SGN-01 gate vector 'order_mainnet' must be present")

	// The gate vector mirrors tests/unit/test_signer.py exactly.
	assert.Equal(t, "0x"+strings.Repeat("a", 64), gate.PrivateKey)
	assert.Equal(t, int64(1699999999999), gate.Nonce)
	assert.True(t, gate.IsMainnet)
	assert.Nil(t, gate.VaultAddress)
	assert.Nil(t, gate.ExpiresAfter)

	for _, v := range g.Vectors {
		assertHashAndSig(t, v)
	}
}

func assertHashAndSig(t *testing.T, v SignerVector) {
	t.Helper()

	// action hash: 0x + 32 bytes.
	hashBytes, err := hex.DecodeString(strings.TrimPrefix(v.ActionHash, "0x"))
	require.NoError(t, err, "%s: action_hash must be hex", v.Label)
	assert.Len(t, hashBytes, 32, "%s: action_hash must be 32 bytes", v.Label)

	// msgpack bytes decode as hex.
	_, err = hex.DecodeString(v.MsgpackHex)
	require.NoError(t, err, "%s: msgpack_hex must be hex", v.Label)

	// signature r/s are 256-bit integers rendered via eth_utils.to_hex, which
	// emits MINIMAL hex (no zero-padding) — so length may be < 64 and odd. HL
	// recovers the signer from the integer values, so we validate value range
	// (0 < x < 2^256), not byte length. Phase 1 normalizes for string parity.
	for name, comp := range map[string]string{"r": v.Signature.R, "s": v.Signature.S} {
		h := strings.TrimPrefix(comp, "0x")
		if len(h)%2 == 1 {
			h = "0" + h // left-pad to even nibble count for decoding
		}
		b, err := hex.DecodeString(h)
		require.NoError(t, err, "%s: sig.%s must be hex", v.Label, name)
		assert.NotEmpty(t, b, "%s: sig.%s must be non-zero", v.Label, name)
		assert.LessOrEqual(t, len(b), 32, "%s: sig.%s must fit in 32 bytes", v.Label, name)
	}
	assert.Contains(t, []int{27, 28}, v.Signature.V, "%s: sig.v must be 27 or 28", v.Label)
}

// TestHDGoldenLoads validates the HD derivation vectors: the public Hardhat
// mnemonic and its well-known account #0 address.
func TestHDGoldenLoads(t *testing.T) {
	g, err := LoadHD()
	require.NoError(t, err)
	require.Len(t, g.Accounts, 5)

	assert.Equal(t, "m/44'/60'/0'/0", g.BasePath)
	assert.Equal(t, "", g.Passphrase)

	// Hardhat account #0 — a public, well-known derivation.
	assert.Equal(t, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", g.Accounts[0].Address)
	assert.Equal(t, "m/44'/60'/0'/0/0", g.Accounts[0].Path)

	for i, a := range g.Accounts {
		assert.Equal(t, i, a.Index)
		assert.True(t, strings.HasPrefix(a.Address, "0x"))
		assert.Len(t, a.Address, 42, "account %d address length", i)
		assert.True(t, strings.HasPrefix(a.PrivateKey, "0x"))
	}
}
