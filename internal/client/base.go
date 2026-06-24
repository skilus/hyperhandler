package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/skilus/hyperhandler/internal/config"
)

// Default client tuning, matching the Python BaseClient defaults.
const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
	defaultRetryDelay = 1 * time.Second
)

// BaseClient is the shared HTTP client for the Hyperliquid API. It is synchronous
// (net/http, no goroutines) and retries on 429/5xx/timeout/network with
// exponential backoff (retryDelay * 2^n, no jitter). Mirrors client/base.py:BaseClient.
type BaseClient struct {
	network    config.NetworkConfig
	http       *http.Client
	maxRetries int
	retryDelay time.Duration

	// sleep waits d or returns ctx.Err() on cancellation. Overridable in tests
	// to keep retry paths fast and deterministic.
	sleep func(ctx context.Context, d time.Duration) error
}

// Option configures a BaseClient.
type Option func(*BaseClient)

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option { return func(c *BaseClient) { c.http.Timeout = d } }

// WithMaxRetries sets the maximum number of retry attempts.
func WithMaxRetries(n int) Option { return func(c *BaseClient) { c.maxRetries = n } }

// WithRetryDelay sets the base retry delay (the n-th retry waits delay*2^n).
func WithRetryDelay(d time.Duration) Option { return func(c *BaseClient) { c.retryDelay = d } }

// WithHTTPClient overrides the underlying *http.Client (for tests/transport tuning).
func WithHTTPClient(h *http.Client) Option { return func(c *BaseClient) { c.http = h } }

// NewBaseClient builds a BaseClient for the given network.
func NewBaseClient(network config.NetworkConfig, opts ...Option) *BaseClient {
	c := &BaseClient{
		network:    network,
		http:       &http.Client{Timeout: defaultTimeout},
		maxRetries: defaultMaxRetries,
		retryDelay: defaultRetryDelay,
		sleep:      sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// nonceNow returns the current Unix time in milliseconds, the default action nonce.
func nonceNow() int64 { return time.Now().UnixMilli() }

// sleepCtx waits for d or returns early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// post sends a POST to {api_url}/{endpoint} with the JSON body and retry logic.
//
// On a 2xx response it returns the raw body for the caller to decode into a typed
// DTO; the only inspection done here is the HL error envelope
// ({"status":"err","response":...}), which is mapped to a typed error. When retry
// is false (or attempts are exhausted) the first transport/5xx/429 failure is
// returned. The request body is encoded once and resent verbatim on every retry —
// for /exchange this means the same signed nonce is replayed (replay-protected),
// never re-signed (SPEC-007 B.6 retry idempotency).
func (c *BaseClient) post(ctx context.Context, endpoint string, body any, retry bool) (json.RawMessage, error) {
	url := c.network.APIURL + "/" + endpoint

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, newAPIError(fmt.Sprintf("encode request: %v", err), 0, nil)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, newAPIError(fmt.Sprintf("build request: %v", err), 0, nil)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			// Context cancellation/deadline is terminal — do not retry.
			if ctx.Err() != nil {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", err), 0, nil)
			}
			lastErr = err
			if !retry || attempt >= c.maxRetries {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", err), 0, nil)
			}
			if werr := c.waitRetry(ctx, attempt); werr != nil {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", werr), 0, nil)
			}
			continue
		}

		status := resp.StatusCode

		if status == http.StatusTooManyRequests {
			resp.Body.Close()
			if !retry || attempt >= c.maxRetries {
				return nil, &RateLimitError{newAPIError("Rate limit exceeded", status, nil)}
			}
			if werr := c.waitRetry(ctx, attempt); werr != nil {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", werr), 0, nil)
			}
			continue
		}

		if status >= 500 {
			resp.Body.Close()
			if !retry || attempt >= c.maxRetries {
				return nil, newAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
			}
			if werr := c.waitRetry(ctx, attempt); werr != nil {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", werr), 0, nil)
			}
			continue
		}

		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			if !retry || attempt >= c.maxRetries {
				return nil, newAPIError(fmt.Sprintf("read response: %v", err), 0, nil)
			}
			if werr := c.waitRetry(ctx, attempt); werr != nil {
				return nil, newAPIError(fmt.Sprintf("request failed: %v", werr), 0, nil)
			}
			continue
		}

		if apiErr := c.checkErrorEnvelope(raw); apiErr != nil {
			return nil, apiErr
		}
		return json.RawMessage(raw), nil
	}

	return nil, newAPIError(fmt.Sprintf("Max retries exceeded: %v", lastErr), 0, nil)
}

// checkErrorEnvelope maps the HL {"status":"err","response":...} envelope to a
// typed error. It returns nil when the body is not an error envelope (e.g. a bare
// array, or a {"status":"ok"} response).
func (c *BaseClient) checkErrorEnvelope(raw []byte) error {
	var env struct {
		Status   string          `json:"status"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil // not a JSON object; let the caller decode it
	}
	if env.Status != "err" {
		return nil
	}

	msg := "Unknown error"
	if len(env.Response) > 0 {
		var s string
		if json.Unmarshal(env.Response, &s) == nil {
			msg = s
		} else {
			msg = string(env.Response)
		}
	}
	return c.handleError(msg, raw)
}

// waitRetry sleeps with exponential backoff before the next attempt: the n-th
// retry waits retryDelay * 2^n (no jitter), matching base.py:_wait_retry.
func (c *BaseClient) waitRetry(ctx context.Context, attempt int) error {
	delay := c.retryDelay * (1 << attempt)
	return c.sleep(ctx, delay)
}

// handleError maps an HL error string to a typed error via substring matching, in
// the same order as base.py:_handle_error. The order matters: "signature" wins
// over "margin", which wins over "not found".
func (c *BaseClient) handleError(msg string, raw []byte) error {
	lower := strings.ToLower(msg)
	base := newAPIError(msg, 0, json.RawMessage(raw))

	switch {
	case strings.Contains(lower, "signature"), strings.Contains(lower, "invalid sig"):
		return &SignatureError{base}
	case strings.Contains(lower, "margin"), strings.Contains(lower, "insufficient"):
		return &InsufficientMarginError{base}
	case strings.Contains(lower, "not found"), strings.Contains(lower, "unknown"):
		return &AssetNotFoundError{base}
	default:
		return base
	}
}

// asAPIError extracts the underlying *APIError from err, if any.
func asAPIError(err error) (*APIError, bool) {
	var e *APIError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
