package service

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
	"github.com/skilus/hyperhandler/internal/signer"
	"github.com/skilus/hyperhandler/internal/storage"
)

// RiskCheck evaluates a signal in MANAGED mode without executing or persisting,
// returning the resulting order or reject. Mirrors cli.py:risk_check (it passes
// no storage, so the decision log is not persisted). Returns exactly one of
// (*TradeOrder, *RiskReject); the third value is a data error.
func RiskCheck(
	ctx context.Context,
	netCfg config.NetworkConfig,
	s *signer.Signer,
	store *storage.Storage,
	signal *models.TradingSignal,
	network string,
	level models.RiskLevel,
) (*models.TradeOrder, *models.RiskReject, error) {
	mgr := risk.NewManager(level, models.ModeManaged, nil)
	info := client.NewInfoClient(netCfg)
	tradeHistory, err := store.GetRecentTradeResults(network, 50)
	if err != nil {
		return nil, nil, err
	}
	return EvaluateSignal(ctx, mgr, info, signal, s.Address(), tradeHistory, nil, network)
}

// RiskStatusData is the rendered view for the `risk status` command.
type RiskStatusData struct {
	Address      string
	AccountValue decimal.Decimal
	Available    decimal.Decimal
	Positions    []models.Position
	TotalRisk    decimal.Decimal
	CBStatus     models.CircuitBreakerStatus
	TradeHistory []models.TradeResult
	Profile      models.RiskLevel // the profile used for the circuit breaker
}

// RiskStatus gathers account, position, circuit-breaker and trade-history data
// for display. The circuit breaker uses the profile for level (B.7: the real
// configured profile, not a hardcoded MEDIUM). Mirrors cli.py:risk_status.
func RiskStatus(
	ctx context.Context,
	netCfg config.NetworkConfig,
	s *signer.Signer,
	store *storage.Storage,
	network string,
	level models.RiskLevel,
) (*RiskStatusData, error) {
	info := client.NewInfoClient(netCfg)

	margin, err := info.GetMarginSummary(ctx, s.Address())
	if err != nil {
		return nil, err
	}
	positions, err := info.GetPositions(ctx, s.Address())
	if err != nil {
		return nil, err
	}

	accountValue := margin.AccountValue
	// withdrawable is exposed on the full account state, not the margin summary.
	state, err := info.GetAccountState(ctx, s.Address())
	if err != nil {
		return nil, err
	}
	available := state.Withdrawable

	totalRisk := decimal.Zero
	for _, p := range positions {
		if p.RiskAmount != nil {
			totalRisk = totalRisk.Add(*p.RiskAmount)
		}
	}

	tradeHistory, err := store.GetRecentTradeResults(network, 50)
	if err != nil {
		return nil, err
	}

	profile := risk.GetProfile(level)
	cb := risk.NewCircuitBreaker(profile)
	cbStatus := cb.Check(tradeHistory, accountValue)

	return &RiskStatusData{
		Address:      s.Address(),
		AccountValue: accountValue,
		Available:    available,
		Positions:    positions,
		TotalRisk:    totalRisk,
		CBStatus:     cbStatus,
		TradeHistory: tradeHistory,
		Profile:      level,
	}, nil
}

// RecentConsecutiveLosses counts the trailing loss streak in trade history,
// matching the iteration order used by the circuit breaker and cli.py:risk_reset
// (oldest-first over the DESC-ordered history, breaking at the first non-loss).
func RecentConsecutiveLosses(history []models.TradeResult) int {
	count := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Pnl.Sign() < 0 {
			count++
		} else {
			break
		}
	}
	return count
}

// ResetCircuitBreaker appends a virtual winning trade ($0.01 PnL) to reset the
// consecutive-loss counter. Mirrors the virtual-trade override in
// cli.py:risk_reset (lines 1402-1415).
func ResetCircuitBreaker(store *storage.Storage, network string) error {
	now := time.Now().UTC()
	virtual := models.TradeResult{
		Coin:        "RESET",
		Side:        "long",
		EntryPrice:  decimal.Zero,
		ExitPrice:   decimal.Zero,
		Size:        decimal.Zero,
		Pnl:         decimal.RequireFromString("0.01"),
		Fees:        decimal.Zero,
		FundingPaid: decimal.Zero,
		OpenedAt:    now,
		ClosedAt:    now,
	}
	_, err := store.SaveTradeResult(virtual, network)
	return err
}
