package wallet

import (
	"errors"
	"strings"

	bip39 "github.com/tyler-smith/go-bip39"
	"github.com/zalando/go-keyring"
)

// MnemonicService is the OS keyring service name for stored BIP-39 mnemonics.
const MnemonicService = "hyperhandler-mnemonic"

// HDKeyResult is a key derived from an HD mnemonic. Mirrors the dataclass in
// wallet/providers/hd.py.
type HDKeyResult struct {
	Key      string // 0x-prefixed hex private key
	Provider string
	Address  string
}

// AddressEntry is an (index, address) pair from a derivation walk.
type AddressEntry struct {
	Index   int
	Address string
}

// HDWalletProvider derives keys from a BIP-39 mnemonic stored in the OS keyring,
// using the BIP-44 path DerivationPath/{index}. Mirrors
// wallet/providers/hd.py:HDWalletProvider. Derivation itself is the frozen
// DeriveHDKey from Phase 1.
type HDWalletProvider struct {
	DerivationPath string
	backend        keyringBackend
}

// NewHDWalletProvider returns a provider using the default Ethereum path.
func NewHDWalletProvider() *HDWalletProvider {
	return &HDWalletProvider{DerivationPath: DefaultHDPath, backend: osKeyring{}}
}

// NewHDWalletProviderWithPath returns a provider using a custom base path.
func NewHDWalletProviderWithPath(path string) *HDWalletProvider {
	return &HDWalletProvider{DerivationPath: path, backend: osKeyring{}}
}

// Name returns the provider name.
func (p *HDWalletProvider) Name() string { return "hdwallet" }

// IsAvailable reports whether the keyring backend is reachable.
func (p *HDWalletProvider) IsAvailable() bool {
	_, err := p.backend.Get(MnemonicService, "__test__")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

// getMnemonic reads the stored mnemonic for a network.
func (p *HDWalletProvider) getMnemonic(network string) (string, bool) {
	v, err := p.backend.Get(MnemonicService, network)
	if err != nil {
		return "", false
	}
	return v, true
}

// HasKey reports whether a mnemonic is stored for a network.
func (p *HDWalletProvider) HasKey(network string) bool {
	_, ok := p.getMnemonic(network)
	return ok
}

// GetKey derives the account-0 key for a network, satisfying KeyProvider.
func (p *HDWalletProvider) GetKey(network string) (string, bool) {
	r, ok := p.GetKeyAt(network, 0)
	if !ok {
		return "", false
	}
	return r.Key, true
}

// GetKeyAt derives the key at the given account index. Mirrors
// hd.py:get_key(account_index=...).
func (p *HDWalletProvider) GetKeyAt(network string, index int) (*HDKeyResult, bool) {
	mnemonic, ok := p.getMnemonic(network)
	if !ok {
		return nil, false
	}
	dk, err := DeriveHDKey(mnemonic, p.DerivationPath, index)
	if err != nil {
		return nil, false
	}
	return &HDKeyResult{Key: dk.PrivateKey, Provider: p.Name(), Address: dk.Address}, true
}

// SaveMnemonic validates and stores a mnemonic for a network. Mirrors
// hd.py:save_mnemonic.
func (p *HDWalletProvider) SaveMnemonic(network, mnemonic string) error {
	if !ValidateMnemonic(mnemonic) {
		return errors.New("invalid mnemonic phrase")
	}
	return p.backend.Set(MnemonicService, network, mnemonic)
}

// DeleteMnemonic removes a stored mnemonic, returning true if it existed.
func (p *HDWalletProvider) DeleteMnemonic(network string) bool {
	return p.backend.Delete(MnemonicService, network) == nil
}

// ListAddresses derives count addresses starting at startIndex. Returns nil when
// no mnemonic is stored. Mirrors hd.py:list_addresses.
func (p *HDWalletProvider) ListAddresses(network string, count, startIndex int) ([]AddressEntry, error) {
	mnemonic, ok := p.getMnemonic(network)
	if !ok {
		return nil, nil
	}
	out := make([]AddressEntry, 0, count)
	for i := startIndex; i < startIndex+count; i++ {
		dk, err := DeriveHDKey(mnemonic, p.DerivationPath, i)
		if err != nil {
			return nil, err
		}
		out = append(out, AddressEntry{Index: i, Address: dk.Address})
	}
	return out, nil
}

// GenerateMnemonic creates a new BIP-39 mnemonic of 12 or 24 words. Mirrors
// hd.py:generate_mnemonic.
func GenerateMnemonic(numWords int) (string, error) {
	var bits int
	switch numWords {
	case 12:
		bits = 128
	case 24:
		bits = 256
	default:
		return "", errors.New("num_words must be 12 or 24")
	}
	entropy, err := bip39.NewEntropy(bits)
	if err != nil {
		return "", err
	}
	return bip39.NewMnemonic(entropy)
}

// ValidateMnemonic reports whether mnemonic is a valid 12- or 24-word BIP-39
// phrase. Mirrors hd.py:validate_mnemonic.
func ValidateMnemonic(mnemonic string) bool {
	words := strings.Fields(strings.TrimSpace(mnemonic))
	if len(words) != 12 && len(words) != 24 {
		return false
	}
	return bip39.IsMnemonicValid(strings.Join(words, " "))
}
