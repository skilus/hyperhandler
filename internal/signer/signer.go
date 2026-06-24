// Package signer implements the Hyperliquid L1 action signing scheme.
//
// FROZEN crypto core (SPEC-007 Phase 1): the byte layout here determines
// whether Hyperliquid accepts an order. It mirrors the Python reference
// (src/hyperhandler/signer.py) and is verified byte-for-byte against the
// golden vectors generated from the official HL SDK.
//
// Scheme:
//  1. msgpack(action)                       — fixed key order is critical
//  2. || nonce (8 bytes, big-endian)
//  3. || vault flag (0x00, or 0x01 + 20-byte address)
//  4. || expires flag (0x00) + value (8 bytes BE), if set
//  5. keccak256                             — the "action hash"
//  6. phantom agent {source, connectionId}  — source "a" mainnet / "b" testnet
//  7. EIP-712 sign (domain Exchange/1/chainId 1337/0x0)
package signer

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/vmihailenco/msgpack/v5"
)

// Signature holds the EIP-712 {r, s, v} components. r and s are rendered as
// minimal 0x-hex (matching eth_utils.to_hex / big.Int.Text(16)); v ∈ {27, 28}.
type Signature struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

// Payload is the signed request body sent to /exchange.
type Payload struct {
	Action       any       `json:"action"`
	Nonce        int64     `json:"nonce"`
	Signature    Signature `json:"signature"`
	VaultAddress *string   `json:"vaultAddress"`
	ExpiresAfter *int64    `json:"expiresAfter"`
}

// Signer signs actions for the Hyperliquid exchange API.
type Signer struct {
	key       *ecdsaKey
	isMainnet bool
}

// New builds a Signer from a 0x-prefixed hex private key.
func New(privateKeyHex string, isMainnet bool) (*Signer, error) {
	k, err := newKey(privateKeyHex)
	if err != nil {
		return nil, err
	}
	return &Signer{key: k, isMainnet: isMainnet}, nil
}

// Address returns the signer's checksummed Ethereum address.
func (s *Signer) Address() string {
	return crypto.PubkeyToAddress(s.key.priv.PublicKey).Hex()
}

// SignAction signs an action and returns the full /exchange payload.
//
// vaultAddress and expiresAfter are optional (nil to omit). The action must
// msgpack-encode with a deterministic, fixed key order — use the typed wires
// from internal/order or an OrderedMap, never a Go map.
func (s *Signer) SignAction(action any, nonce int64, vaultAddress *string, expiresAfter *int64) (Payload, error) {
	hash, err := ActionHash(action, vaultAddress, nonce, expiresAfter)
	if err != nil {
		return Payload{}, err
	}
	sig, err := s.signL1(hash)
	if err != nil {
		return Payload{}, err
	}
	return Payload{
		Action:       action,
		Nonce:        nonce,
		Signature:    sig,
		VaultAddress: vaultAddress,
		ExpiresAfter: expiresAfter,
	}, nil
}

// MarshalAction msgpack-encodes an action with the fixed key order required by
// the action hash. Use only ordered values (typed wires or OrderedMap).
func MarshalAction(action any) ([]byte, error) {
	return msgpack.Marshal(action)
}

// ActionHash computes the keccak256 action hash (step 1–5 above).
func ActionHash(action any, vaultAddress *string, nonce int64, expiresAfter *int64) ([]byte, error) {
	packed, err := MarshalAction(action)
	if err != nil {
		return nil, fmt.Errorf("msgpack action: %w", err)
	}

	data := make([]byte, 0, len(packed)+30)
	data = append(data, packed...)
	data = binary.BigEndian.AppendUint64(data, uint64(nonce))

	if vaultAddress == nil {
		data = append(data, 0x00)
	} else {
		addr, err := addressToBytes(*vaultAddress)
		if err != nil {
			return nil, err
		}
		data = append(data, 0x01)
		data = append(data, addr...)
	}

	if expiresAfter != nil {
		data = append(data, 0x00)
		data = binary.BigEndian.AppendUint64(data, uint64(*expiresAfter))
	}

	return crypto.Keccak256(data), nil
}

// signL1 builds the phantom agent and signs it as EIP-712 typed data.
func (s *Signer) signL1(actionHash []byte) (Signature, error) {
	source := "a"
	if !s.isMainnet {
		source = "b"
	}

	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Agent": []apitypes.Type{
				{Name: "source", Type: "string"},
				{Name: "connectionId", Type: "bytes32"},
			},
		},
		PrimaryType: "Agent",
		Domain: apitypes.TypedDataDomain{
			Name:              "Exchange",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(1337),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"source":       source,
			"connectionId": actionHash,
		},
	}

	digest, err := eip712Digest(typedData)
	if err != nil {
		return Signature{}, err
	}

	sig, err := crypto.Sign(digest, s.key.priv)
	if err != nil {
		return Signature{}, fmt.Errorf("sign: %w", err)
	}

	// go-ethereum returns [R(32) || S(32) || V(1)] with V ∈ {0,1};
	// eth_account uses V ∈ {27,28}.
	r := new(big.Int).SetBytes(sig[0:32])
	sVal := new(big.Int).SetBytes(sig[32:64])
	v := int(sig[64]) + 27

	return Signature{
		R: "0x" + r.Text(16),
		S: "0x" + sVal.Text(16),
		V: v,
	}, nil
}

// eip712Digest computes keccak256("\x19\x01" || domainSeparator || hashStruct(message)).
func eip712Digest(td apitypes.TypedData) ([]byte, error) {
	domainSeparator, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("hash domain: %w", err)
	}
	messageHash, err := td.HashStruct(td.PrimaryType, td.Message)
	if err != nil {
		return nil, fmt.Errorf("hash message: %w", err)
	}
	raw := make([]byte, 0, 2+32+32)
	raw = append(raw, 0x19, 0x01)
	raw = append(raw, domainSeparator...)
	raw = append(raw, messageHash...)
	return crypto.Keccak256(raw), nil
}
