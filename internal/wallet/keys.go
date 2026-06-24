package wallet

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// privKeyRe matches a bare 32-byte hex private key (lowercased, no 0x prefix).
var privKeyRe = regexp.MustCompile("^[0-9a-f]{64}$")

// errInvalidKey mirrors the Python ValueError message.
var errInvalidKey = errors.New("private key must be 64 hex characters (32 bytes)")

// NormalizePrivateKey lowercases, trims, strips an optional 0x prefix and
// validates a private key, returning it in canonical 0x-prefixed lowercase form.
// Mirrors utils.py:normalize_private_key.
func NormalizePrivateKey(key string) (string, error) {
	clean := strings.TrimSpace(strings.ToLower(key))
	clean = strings.TrimPrefix(clean, "0x")
	if !privKeyRe.MatchString(clean) {
		return "", errInvalidKey
	}
	return "0x" + clean, nil
}

// ValidatePrivateKey reports whether key is a well-formed private key. Mirrors
// utils.py:validate_private_key.
func ValidatePrivateKey(key string) bool {
	_, err := NormalizePrivateKey(key)
	return err == nil
}

// MaskKey masks a private key for display as "0x1234...abcd", showing four
// characters at each end. Short inputs are fully starred. Mirrors
// utils.py:mask_key (visible_chars=4).
func MaskKey(key string) string {
	const visible = 4
	if len(key) <= visible*2+3 {
		return strings.Repeat("*", len(key))
	}
	return key[:visible+2] + "..." + key[len(key)-visible:]
}

// DeriveAddress returns the EIP-55 checksummed Ethereum address for a private
// key, matching eth-account's Account.from_key(...).address.
func DeriveAddress(key string) (string, error) {
	norm, err := NormalizePrivateKey(key)
	if err != nil {
		return "", err
	}
	priv, err := crypto.HexToECDSA(strings.TrimPrefix(norm, "0x"))
	if err != nil {
		return "", fmt.Errorf("derive address: %w", err)
	}
	return crypto.PubkeyToAddress(priv.PublicKey).Hex(), nil
}
