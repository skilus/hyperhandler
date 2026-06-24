package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// OrderStatus is the lifecycle state of an order.
type OrderStatus string

const (
	StatusPending         OrderStatus = "pending"
	StatusOpen            OrderStatus = "open"
	StatusFilled          OrderStatus = "filled"
	StatusPartiallyFilled OrderStatus = "partially_filled"
	StatusCancelled       OrderStatus = "cancelled"
	StatusRejected        OrderStatus = "rejected"
)

// OrderResult is the outcome of an order placement. Mirrors order.py:OrderResult.
type OrderResult struct {
	Success    bool
	OrderID    *int64
	FilledSize decimal.Decimal
	AvgPrice   *decimal.Decimal
	Error      string
	Status     OrderStatus
}

// IsFilled reports whether the order is fully filled.
func (r OrderResult) IsFilled() bool { return r.Status == StatusFilled }

// IsPartial reports whether the order is partially filled.
func (r OrderResult) IsPartial() bool { return r.Status == StatusPartiallyFilled }

// OpenOrder represents a resting order on the book. Mirrors order.py:OpenOrder.
type OpenOrder struct {
	Coin       string
	OrderID    int64
	Side       string // "B" buy, "S" sell
	Price      decimal.Decimal
	Size       decimal.Decimal
	Timestamp  int64
	OrderType  string
	ReduceOnly bool
}

// IsBuy reports whether this is a buy order.
func (o OpenOrder) IsBuy() bool { return o.Side == "B" }

// Position represents an open position. Size is positive for long and negative
// for short. Mirrors order.py:Position.
type Position struct {
	Coin          string
	Size          decimal.Decimal
	EntryPrice    decimal.Decimal
	PositionValue decimal.Decimal
	UnrealizedPnl decimal.Decimal
	Leverage      int
	LeverageType  string // "cross" or "isolated"
	LiquidationPx *decimal.Decimal

	// Risk tracking fields.
	MarkPrice      *decimal.Decimal
	FundingAccrued decimal.Decimal
	StopLossPrice  *decimal.Decimal
	OpenedAt       *time.Time

	// Calculated risk fields, populated by the risk manager.
	RiskAmount       *decimal.Decimal
	RiskPct          *decimal.Decimal
	CorrelationGroup *string
}

// IsLong reports whether the position is long (size > 0).
func (p Position) IsLong() bool { return p.Size.Sign() > 0 }

// IsShort reports whether the position is short (size < 0).
func (p Position) IsShort() bool { return p.Size.Sign() < 0 }

// AbsSize returns the absolute position size.
func (p Position) AbsSize() decimal.Decimal { return p.Size.Abs() }
