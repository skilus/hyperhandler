package models

import "github.com/shopspring/decimal"

// VaultInfo describes a Hyperliquid vault. Mirrors vault.py:VaultInfo.
type VaultInfo struct {
	Address      string
	Name         string
	Leader       string
	TVL          decimal.Decimal
	APR          decimal.Decimal
	ProfitShare  decimal.Decimal
	LockupPeriod int // seconds
	IsPublic     bool
	Followers    int
	MaxCapacity  *decimal.Decimal
}

// LockupHours returns the lockup period in hours.
func (v VaultInfo) LockupHours() float64 { return float64(v.LockupPeriod) / 3600 }

// VaultPosition is a user's stake in a vault. Mirrors vault.py:VaultPosition.
type VaultPosition struct {
	Vault        string
	VaultName    string
	Shares       decimal.Decimal
	Deposited    decimal.Decimal
	CurrentValue decimal.Decimal
}

// Pnl returns the absolute profit/loss.
func (v VaultPosition) Pnl() decimal.Decimal { return v.CurrentValue.Sub(v.Deposited) }

// PnlPercent returns the profit/loss as a percentage of the deposit (0 if the
// deposit is zero).
func (v VaultPosition) PnlPercent() decimal.Decimal {
	if v.Deposited.IsZero() {
		return decimal.Zero
	}
	return v.Pnl().Div(v.Deposited).Mul(decimal.NewFromInt(100))
}

// VaultDetails bundles vault info with the user's portfolio view. Mirrors
// vault.py:VaultDetails.
type VaultDetails struct {
	Info          VaultInfo
	AccountValue  decimal.Decimal
	Positions     []map[string]any
	FollowerState map[string]any // nil if not following
}
