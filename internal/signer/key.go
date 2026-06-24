package signer

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// ecdsaKey wraps a secp256k1 private key parsed from hex.
type ecdsaKey struct {
	priv *ecdsa.PrivateKey
}

// newKey parses a private key from hex (with or without 0x prefix).
func newKey(privateKeyHex string) (*ecdsaKey, error) {
	h := strings.TrimPrefix(privateKeyHex, "0x")
	priv, err := crypto.HexToECDSA(h)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return &ecdsaKey{priv: priv}, nil
}

// addressToBytes converts a hex Ethereum address to its 20 raw bytes.
func addressToBytes(address string) ([]byte, error) {
	h := strings.TrimPrefix(address, "0x")
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", address, err)
	}
	if len(b) != 20 {
		return nil, fmt.Errorf("address must be 20 bytes, got %d", len(b))
	}
	return b, nil
}
