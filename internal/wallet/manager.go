package wallet

import (
	"errors"
	"fmt"
)

// KeyResult is a resolved private key plus the provider it came from. Mirrors
// wallet/manager.py:KeyResult.
type KeyResult struct {
	Key      string
	Provider string
}

// Address derives the Ethereum address for the key.
func (r KeyResult) Address() (string, error) { return DeriveAddress(r.Key) }

// ProviderStatus reports a provider's availability and whether it holds a key.
type ProviderStatus struct {
	Available bool
	HasKey    bool
}

// WalletManager resolves private keys through an ordered provider chain
// (env → keyring → prompt by default). Mirrors wallet/manager.py:WalletManager.
type WalletManager struct {
	providers []KeyProvider
	keyring   *KeyringProvider
}

// NewWalletManager builds the default provider chain. When allowPrompt is true
// the interactive prompt provider is appended last.
func NewWalletManager(allowPrompt bool) *WalletManager {
	kr := NewKeyringProvider()
	providers := []KeyProvider{EnvKeyProvider{}, kr}
	if allowPrompt {
		providers = append(providers, NewPromptKeyProvider())
	}
	return &WalletManager{providers: providers, keyring: kr}
}

// NewWalletManagerWithProviders builds a manager from a custom provider list.
// The first KeyringProvider in the list backs save/remove operations.
func NewWalletManagerWithProviders(providers []KeyProvider) *WalletManager {
	m := &WalletManager{providers: providers}
	for _, p := range providers {
		if kr, ok := p.(*KeyringProvider); ok {
			m.keyring = kr
			break
		}
	}
	return m
}

// Providers returns the provider chain.
func (m *WalletManager) Providers() []KeyProvider { return m.providers }

// GetPrivateKey returns the first available key from the provider chain,
// validated and normalized. Returns (nil, nil) when no provider has a key, and
// an error when a provider yields an invalid key. Mirrors
// manager.py:get_private_key.
func (m *WalletManager) GetPrivateKey(network string) (*KeyResult, error) {
	for _, provider := range m.providers {
		if !provider.IsAvailable() {
			continue
		}
		key, ok := provider.GetKey(network)
		if !ok {
			continue
		}
		if !ValidatePrivateKey(key) {
			return nil, fmt.Errorf(
				"invalid private key from %s: must be 64 hex characters (32 bytes)",
				provider.Name(),
			)
		}
		normalized, _ := NormalizePrivateKey(key)
		return &KeyResult{Key: normalized, Provider: provider.Name()}, nil
	}
	return nil, nil
}

// GetAddress returns the Ethereum address for a network, or "" when no key is
// available. Mirrors manager.py:get_address.
func (m *WalletManager) GetAddress(network string) (string, error) {
	result, err := m.GetPrivateKey(network)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Address()
}

// SaveToKeyring validates and stores a key in the OS keyring. Mirrors
// manager.py:save_to_keyring.
func (m *WalletManager) SaveToKeyring(network, key string) error {
	if !ValidatePrivateKey(key) {
		return errInvalidKeyValue
	}
	if m.keyring == nil {
		m.keyring = NewKeyringProvider()
	}
	if !m.keyring.IsAvailable() {
		return errors.New("system keyring is not available")
	}
	normalized, _ := NormalizePrivateKey(key)
	return m.keyring.SetKey(network, normalized)
}

// RemoveFromKeyring removes a key from the OS keyring, returning true if it
// existed. Mirrors manager.py:remove_from_keyring.
func (m *WalletManager) RemoveFromKeyring(network string) bool {
	if m.keyring == nil {
		m.keyring = NewKeyringProvider()
	}
	return m.keyring.DeleteKey(network)
}

// CheckProviders reports each provider's availability and whether it has a key
// for the network. The prompt provider is inspected without triggering a prompt.
// Mirrors manager.py:check_providers.
func (m *WalletManager) CheckProviders(network string) map[string]ProviderStatus {
	status := make(map[string]ProviderStatus, len(m.providers))
	for _, provider := range m.providers {
		available := provider.IsAvailable()
		hasKey := false
		if available {
			if pr, ok := provider.(*PromptKeyProvider); ok {
				hasKey = pr.HasCached(network)
			} else {
				_, hasKey = provider.GetKey(network)
			}
		}
		status[provider.Name()] = ProviderStatus{Available: available, HasKey: hasKey}
	}
	return status
}

// errInvalidKeyValue mirrors the save-path Python ValueError ("Invalid private
// key: ..."), distinct from the chain-resolution message.
var errInvalidKeyValue = errors.New("invalid private key: must be 64 hex characters (32 bytes)")
