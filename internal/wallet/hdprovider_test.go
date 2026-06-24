package wallet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownMnemonic is the public BIP-39 test vector (Hardhat/abandon… about).
const knownMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func fakeHD() (*HDWalletProvider, *fakeBackend) {
	b := newFakeBackend()
	return &HDWalletProvider{DerivationPath: DefaultHDPath, backend: b}, b
}

func TestGenerateMnemonicWordCount(t *testing.T) {
	for _, n := range []int{12, 24} {
		m, err := GenerateMnemonic(n)
		require.NoError(t, err)
		assert.Len(t, strings.Fields(m), n)
		assert.True(t, ValidateMnemonic(m))
	}
}

func TestGenerateMnemonicInvalidWords(t *testing.T) {
	_, err := GenerateMnemonic(15)
	assert.ErrorContains(t, err, "num_words must be 12 or 24")
}

func TestValidateMnemonic(t *testing.T) {
	assert.True(t, ValidateMnemonic(knownMnemonic))
	assert.False(t, ValidateMnemonic(strings.TrimSpace(strings.Repeat("word ", 11))))
	assert.False(t, ValidateMnemonic(strings.TrimSpace(strings.Repeat("notaword ", 12))))
}

func TestHDProviderName(t *testing.T) {
	p, _ := fakeHD()
	assert.Equal(t, "hdwallet", p.Name())
	assert.True(t, p.IsAvailable())
}

func TestHDSaveAndGet(t *testing.T) {
	p, _ := fakeHD()
	require.NoError(t, p.SaveMnemonic("testnet", knownMnemonic))

	result, ok := p.GetKeyAt("testnet", 0)
	require.True(t, ok)
	assert.Equal(t, "hdwallet", result.Provider)
	assert.True(t, strings.HasPrefix(result.Address, "0x"))
	assert.Len(t, result.Address, 42)

	// GetKey satisfies KeyProvider with account index 0.
	key, ok := p.GetKey("testnet")
	require.True(t, ok)
	assert.Equal(t, result.Key, key)
}

func TestHDGetKeyNoMnemonic(t *testing.T) {
	p, _ := fakeHD()
	_, ok := p.GetKeyAt("testnet", 0)
	assert.False(t, ok)
	assert.False(t, p.HasKey("testnet"))
}

func TestHDHasKey(t *testing.T) {
	p, _ := fakeHD()
	assert.False(t, p.HasKey("mainnet"))
	require.NoError(t, p.SaveMnemonic("testnet", knownMnemonic))
	assert.True(t, p.HasKey("testnet"))
}

func TestHDDeleteMnemonic(t *testing.T) {
	p, _ := fakeHD()
	require.NoError(t, p.SaveMnemonic("testnet", knownMnemonic))
	assert.True(t, p.DeleteMnemonic("testnet"))
	assert.False(t, p.HasKey("testnet"))
	assert.False(t, p.DeleteMnemonic("nonexistent"))
}

func TestHDListAddresses(t *testing.T) {
	p, _ := fakeHD()
	require.NoError(t, p.SaveMnemonic("testnet", knownMnemonic))

	addrs, err := p.ListAddresses("testnet", 3, 0)
	require.NoError(t, err)
	require.Len(t, addrs, 3)
	for i, a := range addrs {
		assert.Equal(t, i, a.Index)
		assert.True(t, strings.HasPrefix(a.Address, "0x"))
		assert.Len(t, a.Address, 42)
	}
	// Distinct indices → distinct addresses.
	assert.NotEqual(t, addrs[0].Address, addrs[1].Address)
	assert.NotEqual(t, addrs[1].Address, addrs[2].Address)
}

func TestHDListAddressesNoMnemonic(t *testing.T) {
	p, _ := fakeHD()
	addrs, err := p.ListAddresses("testnet", 5, 0)
	require.NoError(t, err)
	assert.Nil(t, addrs)
}

func TestHDSaveInvalidMnemonicErrors(t *testing.T) {
	p, _ := fakeHD()
	err := p.SaveMnemonic("testnet", "invalid mnemonic phrase")
	assert.ErrorContains(t, err, "invalid mnemonic")
}

func TestHDCustomDerivationPath(t *testing.T) {
	custom := "m/44'/60'/1'/0"
	p := NewHDWalletProviderWithPath(custom)
	assert.Equal(t, custom, p.DerivationPath)
}

func TestHDDerivationConsistency(t *testing.T) {
	p, _ := fakeHD()
	require.NoError(t, p.SaveMnemonic("testnet", knownMnemonic))

	r1, ok := p.GetKeyAt("testnet", 0)
	require.True(t, ok)
	r2, ok := p.GetKeyAt("testnet", 0)
	require.True(t, ok)
	assert.Equal(t, r1.Address, r2.Address)
}
