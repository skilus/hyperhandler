package wallet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/golden"
)

// TestHDDerivationGolden is the SPEC-007 Phase 1 gate for HD derivation: every
// golden account (address + private key) must match for the public test
// mnemonic.
func TestHDDerivationGolden(t *testing.T) {
	g, err := golden.LoadHD()
	require.NoError(t, err)
	require.NotEmpty(t, g.Accounts)
	require.Empty(t, g.Passphrase, "golden uses empty BIP-39 passphrase")

	for _, acct := range g.Accounts {
		got, err := DeriveHDKey(g.Mnemonic, g.BasePath, acct.Index)
		require.NoError(t, err)

		assert.Equalf(t, acct.Path, got.Path, "index %d path", acct.Index)
		assert.Equalf(t, acct.Address, got.Address, "index %d address", acct.Index)
		assert.Equalf(t, strings.ToLower(acct.PrivateKey), strings.ToLower(got.PrivateKey),
			"index %d private key", acct.Index)
	}
}
