package wallet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// testKey is a valid (public) secp256k1 test private key, matching the golden
// vectors' 0xaaaa… key.
const testKey = "0x" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// fakeBackend is an in-memory keyringBackend for tests.
type fakeBackend struct{ store map[string]string }

func newFakeBackend() *fakeBackend { return &fakeBackend{store: map[string]string{}} }

func (f *fakeBackend) k(service, user string) string { return service + "\x00" + user }

func (f *fakeBackend) Get(service, user string) (string, error) {
	v, ok := f.store[f.k(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeBackend) Set(service, user, password string) error {
	f.store[f.k(service, user)] = password
	return nil
}

func (f *fakeBackend) Delete(service, user string) error {
	key := f.k(service, user)
	if _, ok := f.store[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.store, key)
	return nil
}

func fakeKeyring() (*KeyringProvider, *fakeBackend) {
	b := newFakeBackend()
	return &KeyringProvider{Service: DefaultKeyringService, backend: b}, b
}

// --- normalize / validate / mask ---

func TestNormalizePrivateKey(t *testing.T) {
	want := "0x" + strings.Repeat("a", 64)

	cases := map[string]string{
		"with 0x prefix":    "0x" + strings.Repeat("a", 64),
		"without 0x prefix": strings.Repeat("a", 64),
		"uppercase":         "0x" + strings.Repeat("A", 64),
		"with whitespace":   "  0x" + strings.Repeat("a", 64) + "  ",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizePrivateKey(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestNormalizePrivateKeyInvalid(t *testing.T) {
	for name, in := range map[string]string{
		"too short": "0x" + strings.Repeat("a", 63),
		"too long":  "0x" + strings.Repeat("a", 65),
		"not hex":   "0x" + strings.Repeat("g", 64),
		"empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizePrivateKey(in)
			assert.ErrorContains(t, err, "64 hex characters")
		})
	}
}

func TestValidatePrivateKey(t *testing.T) {
	assert.True(t, ValidatePrivateKey("0x"+strings.Repeat("a", 64)))
	assert.True(t, ValidatePrivateKey(strings.Repeat("a", 64)))
	assert.False(t, ValidatePrivateKey("invalid"))
	assert.False(t, ValidatePrivateKey(""))
}

func TestMaskKey(t *testing.T) {
	masked := MaskKey("0x" + strings.Repeat("a", 64))
	assert.True(t, strings.HasPrefix(masked, "0x"))
	assert.Contains(t, masked, "...")
	assert.Less(t, len(masked), 66)

	assert.Equal(t, "*****", MaskKey("short"))
}

func TestDeriveAddress(t *testing.T) {
	addr, err := DeriveAddress(testKey)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(addr, "0x"))
	assert.Len(t, addr, 42)
}

// --- EnvKeyProvider ---

func TestEnvNetworkSpecificKey(t *testing.T) {
	t.Setenv("HL_PRIVATE_KEY", "")
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "0x"+strings.Repeat("b", 64))

	key, ok := EnvKeyProvider{}.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, "0x"+strings.Repeat("b", 64), key)
}

func TestEnvGenericFallback(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "")
	t.Setenv("HL_PRIVATE_KEY", "0x"+strings.Repeat("c", 64))

	key, ok := EnvKeyProvider{}.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, "0x"+strings.Repeat("c", 64), key)
}

func TestEnvNetworkSpecificPriority(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "0x"+strings.Repeat("d", 64))
	t.Setenv("HL_PRIVATE_KEY", "0x"+strings.Repeat("e", 64))

	key, ok := EnvKeyProvider{}.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, "0x"+strings.Repeat("d", 64), key)
}

func TestEnvNoKey(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "")
	t.Setenv("HL_PRIVATE_KEY", "")

	_, ok := EnvKeyProvider{}.GetKey("mainnet")
	assert.False(t, ok)
	assert.True(t, EnvKeyProvider{}.IsAvailable())
}

// --- PromptKeyProvider (injected reader) ---

func TestPromptProviderReadsAndCaches(t *testing.T) {
	calls := 0
	p := &PromptKeyProvider{
		cache:        map[string]string{},
		readPassword: func(string) (string, error) { calls++; return testKey, nil },
		isTTY:        func() bool { return true },
	}

	key, ok := p.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, testKey, key)
	assert.True(t, p.HasCached("mainnet"))

	// Second call is served from cache (reader not invoked again).
	_, ok = p.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, 1, calls)

	p.ClearNetwork("mainnet")
	assert.False(t, p.HasCached("mainnet"))
}

func TestPromptProviderUnavailable(t *testing.T) {
	p := &PromptKeyProvider{
		cache:        map[string]string{},
		readPassword: func(string) (string, error) { return testKey, nil },
		isTTY:        func() bool { return false },
	}
	_, ok := p.GetKey("mainnet")
	assert.False(t, ok)
	assert.False(t, p.IsAvailable())
}

// --- KeyringProvider ---

func TestKeyringSetAndGet(t *testing.T) {
	p, _ := fakeKeyring()
	require.NoError(t, p.SetKey("mainnet", testKey))

	got, ok := p.GetKey("mainnet")
	require.True(t, ok)
	assert.Equal(t, testKey, got)
}

func TestKeyringGetNonexistent(t *testing.T) {
	p, _ := fakeKeyring()
	_, ok := p.GetKey("mainnet")
	assert.False(t, ok)
}

func TestKeyringDelete(t *testing.T) {
	p, _ := fakeKeyring()
	require.NoError(t, p.SetKey("mainnet", testKey))
	assert.True(t, p.DeleteKey("mainnet"))
	_, ok := p.GetKey("mainnet")
	assert.False(t, ok)
}

func TestKeyringDeleteNonexistent(t *testing.T) {
	p, _ := fakeKeyring()
	assert.False(t, p.DeleteKey("mainnet"))
}

func TestKeyringHasKeyAndAvailable(t *testing.T) {
	p, _ := fakeKeyring()
	assert.True(t, p.IsAvailable())
	assert.False(t, p.HasKey("mainnet"))
	require.NoError(t, p.SetKey("mainnet", testKey))
	assert.True(t, p.HasKey("mainnet"))
}

// --- WalletManager ---

// newTestManager builds a manager with env + fake keyring (no prompt).
func newTestManager() (*WalletManager, *fakeBackend) {
	kr, b := fakeKeyring()
	return NewWalletManagerWithProviders([]KeyProvider{EnvKeyProvider{}, kr}), b
}

func TestManagerGetKeyFromEnv(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", testKey)
	m, _ := newTestManager()

	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, testKey, result.Key)
	assert.Equal(t, "environment", result.Provider)
}

func TestManagerGetKeyFromKeyring(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "")
	t.Setenv("HL_PRIVATE_KEY", "")
	m, _ := newTestManager()
	require.NoError(t, m.SaveToKeyring("mainnet", testKey))

	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, testKey, result.Key)
	assert.Equal(t, "keyring", result.Provider)
}

func TestManagerEnvTakesPriority(t *testing.T) {
	envKey := "0x" + strings.Repeat("1", 64)
	keyringKey := "0x" + strings.Repeat("2", 64)
	t.Setenv("HL_MAINNET_PRIVATE_KEY", envKey)

	m, _ := newTestManager()
	require.NoError(t, m.SaveToKeyring("mainnet", keyringKey))

	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, envKey, result.Key)
	assert.Equal(t, "environment", result.Provider)
}

func TestManagerNoKey(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "")
	t.Setenv("HL_PRIVATE_KEY", "")
	m, _ := newTestManager()

	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestManagerInvalidKeyErrors(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "invalid")
	m, _ := newTestManager()

	_, err := m.GetPrivateKey("mainnet")
	assert.ErrorContains(t, err, "invalid private key")
}

func TestManagerGetAddress(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", testKey)
	m, _ := newTestManager()

	addr, err := m.GetAddress("mainnet")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(addr, "0x"))
	assert.Len(t, addr, 42)
}

func TestManagerSaveInvalidKeyErrors(t *testing.T) {
	m, _ := newTestManager()
	err := m.SaveToKeyring("mainnet", "invalid")
	assert.ErrorContains(t, err, "invalid private key")
}

func TestManagerRemoveFromKeyring(t *testing.T) {
	m, _ := newTestManager()
	require.NoError(t, m.SaveToKeyring("mainnet", testKey))

	assert.True(t, m.RemoveFromKeyring("mainnet"))
	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	// env may still hold a key in this process; ensure keyring no longer has it.
	if result != nil {
		assert.NotEqual(t, "keyring", result.Provider)
	}
}

func TestManagerCheckProviders(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", testKey)
	m, _ := newTestManager()

	status := m.CheckProviders("mainnet")

	require.Contains(t, status, "environment")
	assert.True(t, status["environment"].Available)
	assert.True(t, status["environment"].HasKey)

	require.Contains(t, status, "keyring")
	assert.True(t, status["keyring"].Available)
	assert.False(t, status["keyring"].HasKey)
}

func TestKeyResultAddress(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", testKey)
	m, _ := newTestManager()

	result, err := m.GetPrivateKey("mainnet")
	require.NoError(t, err)
	require.NotNil(t, result)

	addr, err := result.Address()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(addr, "0x"))
	assert.Len(t, addr, 42)
}
