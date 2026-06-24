package wallet

import (
	"fmt"
	"strings"

	hdwallet "github.com/miguelmota/go-ethereum-hdwallet"
)

// DefaultHDPath is the base BIP-44 derivation path (Ethereum standard); the
// account index is appended as DefaultHDPath + "/" + index.
const DefaultHDPath = "m/44'/60'/0'/0"

// DerivedKey is a private key derived from an HD mnemonic.
type DerivedKey struct {
	Index      int
	Path       string
	Address    string
	PrivateKey string // 0x-prefixed hex
}

// DeriveHDKey derives the key at basePath/index from a BIP-39 mnemonic using an
// empty passphrase, matching the Python reference
// (Account.from_mnemonic(account_path=...)). FROZEN: the derivation must yield
// the same addresses as the golden vectors.
func DeriveHDKey(mnemonic, basePath string, index int) (DerivedKey, error) {
	if basePath == "" {
		basePath = DefaultHDPath
	}
	w, err := hdwallet.NewFromMnemonic(strings.TrimSpace(mnemonic))
	if err != nil {
		return DerivedKey{}, fmt.Errorf("invalid mnemonic: %w", err)
	}

	pathStr := fmt.Sprintf("%s/%d", basePath, index)
	path, err := hdwallet.ParseDerivationPath(pathStr)
	if err != nil {
		return DerivedKey{}, fmt.Errorf("parse path %q: %w", pathStr, err)
	}

	account, err := w.Derive(path, false)
	if err != nil {
		return DerivedKey{}, fmt.Errorf("derive %q: %w", pathStr, err)
	}

	privHex, err := w.PrivateKeyHex(account)
	if err != nil {
		return DerivedKey{}, fmt.Errorf("export private key: %w", err)
	}

	return DerivedKey{
		Index:      index,
		Path:       pathStr,
		Address:    account.Address.Hex(),
		PrivateKey: "0x" + privHex,
	}, nil
}
