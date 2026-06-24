package service

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
	"github.com/skilus/hyperhandler/internal/signer"
	"github.com/skilus/hyperhandler/internal/storage"
)

// execSlippage mirrors the ExchangeClient default slippage in exchange.py
// (Decimal("0.005")); cli.py constructs the client without overriding it.
var execSlippage = decimal.RequireFromString("0.005")

// ExecOutcome is the discriminated result of an exec run (SPEC-007 A.3).
type ExecOutcome string

const (
	OutcomeRejected ExecOutcome = "rejected"
	OutcomeDryRun   ExecOutcome = "dry_run"
	OutcomeExecuted ExecOutcome = "executed"
)

// ExecRequest is the input to Executor.Exec.
type ExecRequest struct {
	Signal    *models.TradingSignal
	Network   string
	Vault     *string
	DryRun    bool
	RiskLevel *models.RiskLevel // nil = manual mode
}

// ExecResult is the structured outcome of an exec run. Exactly which fields are
// populated depends on Outcome; the cli layer renders accordingly.
type ExecResult struct {
	Outcome  ExecOutcome
	Managed  bool
	Warnings []string
	Reject   *models.RiskReject
	Order    *models.TradeOrder    // risk order (diff / dry-run summary)
	Signal   *models.TradingSignal // final signal (risk-adjusted in managed mode)
	Results  []models.OrderResult  // placed orders (executed outcome)
}

// ValidationError carries the validator's error list so the cli can print each
// line, matching the Python "Signal validation failed" output.
type ValidationError struct{ Errors []string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("signal validation failed: %v", e.Errors)
}

// Executor orchestrates signal execution. It owns the side-effecting
// dependencies (config, signer, storage) and an optional Reporter for progress
// lines. Mirrors the body of cli.py:exec, lifted out of the CLI (SPEC-007 A.1).
type Executor struct {
	Config   *config.Config
	Signer   *signer.Signer
	Storage  *storage.Storage
	Reporter func(string) // progress lines ("Setting leverage..."); nil discards
}

func (e *Executor) report(format string, args ...any) {
	if e.Reporter != nil {
		e.Reporter(fmt.Sprintf(format, args...))
	}
}

// Exec validates, risk-evaluates and (unless dry-run) executes a signal.
// Mirrors cli.py:exec lines 158-322.
func (e *Executor) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	signal := req.Signal

	// Validate against config limits.
	validator := ValidatorFromConfig(e.Config)
	vr := validator.Validate(signal, nil)
	if !vr.Valid {
		return nil, &ValidationError{Errors: vr.Errors}
	}

	// Risk mode: a supplied risk level enables managed mode.
	level := models.RiskMedium
	mode := models.ModeManual
	if req.RiskLevel != nil {
		level = *req.RiskLevel
		mode = models.ModeManaged
	}

	netCfg, err := e.Config.NetworkConfig(req.Network)
	if err != nil {
		return nil, err
	}
	mgr := risk.NewManager(level, mode, nil)

	// Save the signal before evaluating (matches Python ordering).
	signalID, err := e.Storage.SaveSignal(signal, req.Network, true, false)
	if err != nil {
		return nil, err
	}

	info := client.NewInfoClient(netCfg)
	tradeHistory, err := e.Storage.GetRecentTradeResults(req.Network, 50)
	if err != nil {
		return nil, err
	}

	order, reject, err := EvaluateSignal(ctx, mgr, info, signal, e.Signer.Address(), tradeHistory, e.Storage, req.Network)
	if err != nil {
		return nil, err
	}
	if reject != nil {
		return &ExecResult{Outcome: OutcomeRejected, Reject: reject, Warnings: vr.Warnings}, nil
	}

	// Snapshot the signal before any risk-driven mutation so the cli can render
	// the original-vs-calculated diff (matches Python, which prints the diff
	// before overwriting signal.size/stop_loss).
	orig := *signal
	result := &ExecResult{
		Managed:  mode == models.ModeManaged,
		Warnings: vr.Warnings,
		Order:    order,
		Signal:   &orig,
	}

	if req.DryRun {
		result.Outcome = OutcomeDryRun
		return result, nil
	}

	// Execute on the exchange.
	exch := client.NewExchangeClient(netCfg, e.Signer, execSlippage)

	assetIndex, err := info.GetAssetIndex(ctx, signal.Pair)
	if err != nil {
		return nil, err
	}
	assetInfo, err := info.GetAssetInfo(ctx, signal.Pair)
	if err != nil {
		return nil, err
	}
	szDecimals := assetInfo.SzDecimals

	var currentPrice *decimal.Decimal
	if signal.IsMarket() {
		mid, err := info.GetMidPrice(ctx, signal.Pair)
		if err != nil {
			return nil, err
		}
		currentPrice = &mid
	}

	// Risk-adjusted leverage in managed mode, signal's own otherwise.
	leverage := signal.Leverage
	if req.RiskLevel != nil {
		leverage = order.Leverage
	}
	e.report("Setting leverage to %dx...", leverage)
	exch.SetLeverage(ctx, assetIndex, leverage, true, req.Vault)

	// Apply risk-calculated values in managed mode.
	if req.RiskLevel != nil {
		signal.Size = order.Size
		if !order.StopLoss.IsZero() {
			sl := order.StopLoss
			signal.StopLoss = &sl
		}
	}

	e.report("Placing orders...")
	results, err := exch.PlaceOrderFromSignal(ctx, signal, assetIndex, currentPrice, req.Vault, szDecimals)
	if err != nil {
		return nil, err
	}

	if err := e.Storage.UpdateSignalExecuted(signalID, true); err != nil {
		return nil, err
	}

	// Persist each order result. Order-type labelling matches Python:
	// i==0 entry, i==1+stop sl, otherwise tp.
	for i, r := range results {
		var orderType string
		switch {
		case i == 0:
			orderType = "entry"
		case i == 1 && signal.StopLoss != nil:
			orderType = "sl"
		default:
			orderType = "tp"
		}
		if _, err := e.Storage.SaveOrder(
			&signalID, req.Network, signal.Pair, string(signal.Side),
			orderType, signal.Size, signal.EntryPrice, r, req.Vault,
		); err != nil {
			return nil, err
		}
	}

	result.Outcome = OutcomeExecuted
	result.Results = results
	return result, nil
}
