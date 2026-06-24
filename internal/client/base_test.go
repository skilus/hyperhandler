package client_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/signer"
)

// slippage is the standard 0.5% market slippage used across client tests.
var slippage = decimal.RequireFromString("0.005")

// anvilKey is a well-known throwaway test key (anvil account #0). Never a real secret.
const anvilKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// netFor returns a NetworkConfig pointing at a test server.
func netFor(url string) config.NetworkConfig {
	return config.NetworkConfig{Name: "test", APIURL: url}
}

// fastRetry keeps backoff negligible so retry tests stay quick.
func fastRetry() client.Option { return client.WithRetryDelay(time.Millisecond) }

func testSigner(t *testing.T) *signer.Signer {
	t.Helper()
	s, err := signer.New(anvilKey, false)
	require.NoError(t, err)
	return s
}

func TestRetryThenSuccessOn5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"BTC":"50000"}`))
	}))
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL), fastRetry())
	mids, err := c.GetAllMids(context.Background())
	require.NoError(t, err)
	assert.True(t, mids["BTC"].Equal(decimal.RequireFromString("50000")))
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls), "should retry twice then succeed")
}

func TestMaxRetriesExceeded(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL), fastRetry(), client.WithMaxRetries(2))
	_, err := c.GetAllMids(context.Background())
	require.Error(t, err)

	apiErr, ok := err.(*client.APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	// 1 initial + 2 retries.
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestRateLimitMapsToTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := client.NewInfoClient(netFor(srv.URL), fastRetry(), client.WithMaxRetries(0))
	_, err := c.GetAllMids(context.Background())
	require.Error(t, err)
	_, ok := err.(*client.RateLimitError)
	assert.True(t, ok, "429 should map to *RateLimitError, got %T", err)
}

func TestErrorEnvelopeMapping(t *testing.T) {
	cases := []struct {
		name    string
		respMsg string
		assertT func(t *testing.T, err error)
	}{
		{"signature", "Invalid signature for order", func(t *testing.T, err error) {
			_, ok := err.(*client.SignatureError)
			assert.True(t, ok, "got %T", err)
		}},
		{"margin", "Insufficient margin to place order", func(t *testing.T, err error) {
			_, ok := err.(*client.InsufficientMarginError)
			assert.True(t, ok, "got %T", err)
		}},
		{"notfound", "Asset not found", func(t *testing.T, err error) {
			_, ok := err.(*client.AssetNotFoundError)
			assert.True(t, ok, "got %T", err)
		}},
		{"generic", "Something else broke", func(t *testing.T, err error) {
			_, ok := err.(*client.APIError)
			assert.True(t, ok, "got %T", err)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{"status":"err","response":"` + tc.respMsg + `"}`))
			}))
			defer srv.Close()

			c := client.NewInfoClient(netFor(srv.URL))
			_, err := c.GetAllMids(context.Background())
			require.Error(t, err)
			tc.assertT(t, err)
			// The base *APIError is reachable via the chain too.
			var base *client.APIError
			assert.ErrorAs(t, err, &base)
			assert.Equal(t, tc.respMsg, base.Message)
		})
	}
}

// TestExchangeRetryReplaysSameBody verifies SPEC-007 B.6: a retried /exchange POST
// resends the identical signed body (same nonce), never re-signs.
func TestExchangeRetryReplaysSameBody(t *testing.T) {
	var bodies []string
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if atomic.AddInt32(&calls, 1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"status":"ok","response":{"data":{"statuses":[{"resting":{"oid":1}}]}}}`))
	}))
	defer srv.Close()

	ex := client.NewExchangeClient(netFor(srv.URL), testSigner(t), slippage, fastRetry())
	res := ex.PlaceOrder(context.Background(), 0, true, decimal.RequireFromString("0.1"), decimal.RequireFromString("50000"), models.Limit, false, nil)
	require.True(t, res.Success)

	require.Len(t, bodies, 2)
	assert.Equal(t, bodies[0], bodies[1], "retry must replay the same signed body (same nonce)")
}

func TestContextCancelStopsRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	c := client.NewInfoClient(netFor(srv.URL), client.WithRetryDelay(time.Second))
	_, err := c.GetAllMids(ctx)
	require.Error(t, err)
}
