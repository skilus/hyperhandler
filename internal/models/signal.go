package models

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Ptr returns a pointer to v. It keeps optional struct fields (entry price,
// stop loss, confidence, ...) ergonomic at call sites.
func Ptr[T any](v T) *T { return &v }

// OrderSide is the trade direction.
type OrderSide string

const (
	Long  OrderSide = "long"
	Short OrderSide = "short"
)

// Valid reports whether s is a known side.
func (s OrderSide) Valid() bool { return s == Long || s == Short }

// OrderType is the order's execution type.
type OrderType string

const (
	Market OrderType = "market"
	Limit  OrderType = "limit"
)

// Valid reports whether t is a known order type.
func (t OrderType) Valid() bool { return t == Market || t == Limit }

// SignalHorizon is the expected hold duration, used to pick the ATR timeframe.
type SignalHorizon string

const (
	HorizonScalp    SignalHorizon = "scalp"    // <4h, 15m candles
	HorizonIntraday SignalHorizon = "intraday" // 4h–24h, 1h candles
	HorizonSwing    SignalHorizon = "swing"    // 1d–7d, 4h candles
	HorizonPosition SignalHorizon = "position" // >7d, 1d candles
)

// Valid reports whether h is a known horizon.
func (h SignalHorizon) Valid() bool {
	switch h {
	case HorizonScalp, HorizonIntraday, HorizonSwing, HorizonPosition:
		return true
	}
	return false
}

// DefaultLeverage matches the Python field default (5).
const DefaultLeverage = 5

// TradingSignal is a validated trade instruction. Construct it with
// NewTradingSignal so the normalization and cross-field checks run; the zero
// value is not valid. Mirrors models/signal.py:TradingSignal.
type TradingSignal struct {
	Pair       string           `json:"pair" yaml:"pair"`
	Side       OrderSide        `json:"side" yaml:"side"`
	OrderType  OrderType        `json:"order_type" yaml:"order_type"`
	Size       decimal.Decimal  `json:"size" yaml:"size"`
	Leverage   int              `json:"leverage" yaml:"leverage"`
	EntryPrice *decimal.Decimal `json:"entry_price,omitempty" yaml:"entry_price,omitempty"`
	StopLoss   *decimal.Decimal `json:"stop_loss,omitempty" yaml:"stop_loss,omitempty"`
	TakeProfit *decimal.Decimal `json:"take_profit,omitempty" yaml:"take_profit,omitempty"`

	// Risk management fields.
	Confidence *float64      `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Source     *string       `json:"source,omitempty" yaml:"source,omitempty"`
	Horizon    SignalHorizon `json:"horizon,omitempty" yaml:"horizon,omitempty"`
}

// SignalParams holds the inputs to NewTradingSignal. It mirrors the keyword
// arguments of the Python constructor: Leverage 0 defaults to DefaultLeverage
// and an empty Horizon defaults to Intraday.
type SignalParams struct {
	Pair       string
	Side       OrderSide
	OrderType  OrderType
	Size       decimal.Decimal
	Leverage   int
	EntryPrice *decimal.Decimal
	StopLoss   *decimal.Decimal
	TakeProfit *decimal.Decimal
	Confidence *float64
	Source     *string
	Horizon    SignalHorizon
}

// NewTradingSignal validates and normalizes p into a TradingSignal. It performs
// the same checks as the Pydantic model: side/order_type are known, size > 0,
// leverage ∈ [1,50], confidence ∈ [0,1], pair normalization, and the
// entry/stop-loss/take-profit cross-field rules (validate_prices).
func NewTradingSignal(p SignalParams) (*TradingSignal, error) {
	if !p.Side.Valid() {
		return nil, fmt.Errorf("invalid side: %q", p.Side)
	}
	if !p.OrderType.Valid() {
		return nil, fmt.Errorf("invalid order_type: %q", p.OrderType)
	}

	leverage := p.Leverage
	if leverage == 0 {
		leverage = DefaultLeverage
	}
	if leverage < 1 || leverage > 50 {
		return nil, fmt.Errorf("leverage %d out of range [1, 50]", leverage)
	}

	if p.Size.Sign() <= 0 {
		return nil, fmt.Errorf("size must be greater than 0, got %s", p.Size.String())
	}

	if p.Confidence != nil {
		c := *p.Confidence
		if c < 0.0 || c > 1.0 {
			return nil, fmt.Errorf("confidence %v out of range [0.0, 1.0]", c)
		}
	}

	horizon := p.Horizon
	if horizon == "" {
		horizon = HorizonIntraday
	} else if !horizon.Valid() {
		return nil, fmt.Errorf("invalid horizon: %q", horizon)
	}

	s := &TradingSignal{
		Pair:       normalizePair(p.Pair),
		Side:       p.Side,
		OrderType:  p.OrderType,
		Size:       p.Size,
		Leverage:   leverage,
		EntryPrice: p.EntryPrice,
		StopLoss:   p.StopLoss,
		TakeProfit: p.TakeProfit,
		Confidence: p.Confidence,
		Source:     p.Source,
		Horizon:    horizon,
	}

	if err := s.validatePrices(); err != nil {
		return nil, err
	}
	return s, nil
}

// normalizePair uppercases the symbol and strips the quote suffixes. The
// replacement order is significant and matches signal.py:normalize_pair.
func normalizePair(v string) string {
	v = strings.ToUpper(v)
	v = strings.ReplaceAll(v, "-USD", "")
	v = strings.ReplaceAll(v, "-PERP", "")
	v = strings.ReplaceAll(v, "/USD", "")
	return v
}

// validatePrices enforces the entry/stop-loss/take-profit rules from
// signal.py:validate_prices.
func (s *TradingSignal) validatePrices() error {
	if s.OrderType == Limit && s.EntryPrice == nil {
		return fmt.Errorf("entry_price is required for limit orders")
	}

	// Without a reference price (market order, no entry) SL/TP can't be checked.
	if s.EntryPrice == nil {
		return nil
	}
	ref := *s.EntryPrice

	if s.StopLoss != nil {
		switch s.Side {
		case Long:
			if s.StopLoss.Cmp(ref) >= 0 {
				return fmt.Errorf("stop_loss must be below entry_price for long positions")
			}
		case Short:
			if s.StopLoss.Cmp(ref) <= 0 {
				return fmt.Errorf("stop_loss must be above entry_price for short positions")
			}
		}
	}

	if s.TakeProfit != nil {
		switch s.Side {
		case Long:
			if s.TakeProfit.Cmp(ref) <= 0 {
				return fmt.Errorf("take_profit must be above entry_price for long positions")
			}
		case Short:
			if s.TakeProfit.Cmp(ref) >= 0 {
				return fmt.Errorf("take_profit must be below entry_price for short positions")
			}
		}
	}

	return nil
}

// IsBuy reports whether the signal opens a long (buy) position.
func (s *TradingSignal) IsBuy() bool { return s.Side == Long }

// IsMarket reports whether the signal is a market order.
func (s *TradingSignal) IsMarket() bool { return s.OrderType == Market }
