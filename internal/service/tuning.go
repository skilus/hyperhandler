package service

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
)

// ClientOptions derives base-client tuning from the trading config section:
// trading.max_retries → retry budget, trading.retry_delay (seconds) → backoff
// base. An empty section yields the BaseClient defaults (buildSettings fills
// MaxRetries=3, RetryDelay=1.0). Pass the result to the client constructors so
// the same tuning applies everywhere a client is built.
func ClientOptions(t config.TradingSettings) []client.Option {
	return []client.Option{
		client.WithMaxRetries(t.MaxRetries),
		client.WithRetryDelay(time.Duration(t.RetryDelay * float64(time.Second))),
	}
}

// Slippage returns the market-order slippage from trading.default_slippage.
func Slippage(t config.TradingSettings) decimal.Decimal {
	return decimal.NewFromFloat(t.DefaultSlippage)
}
