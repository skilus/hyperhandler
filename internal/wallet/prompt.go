package wallet

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// PromptKeyProvider asks the user for a private key interactively, caching it for
// the session. Mirrors wallet/providers/prompt.py:PromptKeyProvider. The reader
// and TTY check are injectable for tests.
type PromptKeyProvider struct {
	cache        map[string]string
	readPassword func(prompt string) (string, error)
	isTTY        func() bool
}

// NewPromptKeyProvider returns a provider that reads from the terminal.
func NewPromptKeyProvider() *PromptKeyProvider {
	return &PromptKeyProvider{
		cache:        map[string]string{},
		readPassword: defaultReadPassword,
		isTTY:        func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
	}
}

// Name returns the provider name.
func (p *PromptKeyProvider) Name() string { return "prompt" }

// GetKey returns a cached key or prompts for one, caching the result.
func (p *PromptKeyProvider) GetKey(network string) (string, bool) {
	if v, ok := p.cache[network]; ok {
		return v, true
	}
	if !p.IsAvailable() {
		return "", false
	}
	key, err := p.readPassword(fmt.Sprintf("Enter private key for %s: ", network))
	if err != nil || key == "" {
		return "", false
	}
	p.cache[network] = key
	return key, true
}

// IsAvailable reports whether interactive input is possible.
func (p *PromptKeyProvider) IsAvailable() bool { return p.isTTY() }

// HasCached reports whether a key is already cached (without prompting).
func (p *PromptKeyProvider) HasCached(network string) bool {
	_, ok := p.cache[network]
	return ok
}

// ClearCache drops all cached keys.
func (p *PromptKeyProvider) ClearCache() { p.cache = map[string]string{} }

// ClearNetwork drops the cached key for one network.
func (p *PromptKeyProvider) ClearNetwork(network string) { delete(p.cache, network) }

// defaultReadPassword reads a password from the terminal without echo.
func defaultReadPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
