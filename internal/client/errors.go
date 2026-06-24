package client

// APIError is the base error for all Hyperliquid API failures. StatusCode is 0
// when the failure was not an HTTP status (timeout, decode, mapped "err"
// response). Mirrors client/base.py:APIError.
type APIError struct {
	Message    string
	StatusCode int
	Response   any
}

func (e *APIError) Error() string { return e.Message }

// newAPIError builds an *APIError. status 0 means "no HTTP status".
func newAPIError(message string, status int, response any) *APIError {
	return &APIError{Message: message, StatusCode: status, Response: response}
}

// The following typed errors map the substring-matched HL error strings to Go
// types so callers can branch with errors.As. Each embeds *APIError and unwraps
// to it, so errors.As(err, &apiErr) also succeeds for the base type.

// RateLimitError signals an HTTP 429. Mirrors base.py:RateLimitError.
type RateLimitError struct{ *APIError }

// SignatureError signals an invalid-signature response. Mirrors base.py:SignatureError.
type SignatureError struct{ *APIError }

// InsufficientMarginError signals an out-of-margin response. Mirrors base.py:InsufficientMarginError.
type InsufficientMarginError struct{ *APIError }

// AssetNotFoundError signals an unknown asset/pair. Mirrors base.py:AssetNotFoundError.
type AssetNotFoundError struct{ *APIError }

// VaultNotFoundError signals an unknown vault. Mirrors vault.py:VaultNotFoundError.
type VaultNotFoundError struct{ *APIError }

// LockupPeriodError signals a withdrawal blocked by the lockup period. Mirrors
// vault.py:LockupPeriodError.
type LockupPeriodError struct{ *APIError }

func (e *RateLimitError) Unwrap() error          { return e.APIError }
func (e *SignatureError) Unwrap() error          { return e.APIError }
func (e *InsufficientMarginError) Unwrap() error { return e.APIError }
func (e *AssetNotFoundError) Unwrap() error      { return e.APIError }
func (e *VaultNotFoundError) Unwrap() error      { return e.APIError }
func (e *LockupPeriodError) Unwrap() error       { return e.APIError }
