package wallet

import "github.com/zalando/go-keyring"

// KeyProvider is a source of private keys for a network. Mirrors
// wallet/providers/base.py:KeyProvider. GetKey returns (key, true) when the
// provider supplies a key, or ("", false) when it has none.
type KeyProvider interface {
	Name() string
	GetKey(network string) (string, bool)
	IsAvailable() bool
}

// keyringBackend abstracts the OS keyring so providers stay testable. The
// production backend wraps github.com/zalando/go-keyring.
type keyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// osKeyring is the default keyringBackend, backed by the system keychain.
type osKeyring struct{}

func (osKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (osKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }
