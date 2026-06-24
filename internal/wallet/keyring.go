package wallet

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// DefaultKeyringService is the OS keyring service name for stored private keys.
// SPEC-007 uses a fresh service name (clean cutover): keys are re-entered rather
// than read from the Python install's keychain entries.
const DefaultKeyringService = "hyperhandler"

// KeyringProvider stores and retrieves private keys in the OS keyring. Mirrors
// wallet/providers/keyring_provider.py:KeyringProvider.
type KeyringProvider struct {
	Service string
	backend keyringBackend
}

// NewKeyringProvider returns a provider backed by the system keyring.
func NewKeyringProvider() *KeyringProvider {
	return &KeyringProvider{Service: DefaultKeyringService, backend: osKeyring{}}
}

// Name returns the provider name.
func (p *KeyringProvider) Name() string { return "keyring" }

// username is the keyring account name for a network.
func (p *KeyringProvider) username(network string) string { return "private_key_" + network }

// GetKey returns the stored key for a network.
func (p *KeyringProvider) GetKey(network string) (string, bool) {
	v, err := p.backend.Get(p.Service, p.username(network))
	if err != nil {
		return "", false
	}
	return v, true
}

// SetKey stores a key for a network.
func (p *KeyringProvider) SetKey(network, key string) error {
	return p.backend.Set(p.Service, p.username(network), key)
}

// DeleteKey removes a key, returning true if it existed and was deleted.
func (p *KeyringProvider) DeleteKey(network string) bool {
	return p.backend.Delete(p.Service, p.username(network)) == nil
}

// HasKey reports whether a key is stored for a network.
func (p *KeyringProvider) HasKey(network string) bool {
	_, ok := p.GetKey(network)
	return ok
}

// IsAvailable probes the keyring with a sentinel lookup. A "not found" result
// means the backend works (the key is simply absent); any other error means the
// keyring itself is unavailable.
func (p *KeyringProvider) IsAvailable() bool {
	_, err := p.backend.Get(p.Service, "__test__")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}
