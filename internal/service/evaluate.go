package service

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/risk"
)

// candleLookback matches the Python get_candles default (info.py:get_candles).
const candleLookback = 100

// defaultCoinMaxLeverage matches asset_meta.get("maxLeverage", 50) in
// manager.py — used when the universe entry omits the field.
const defaultCoinMaxLeverage = 50

// MarketDataClient is the read-only market/account surface the risk evaluation
// needs. *client.InfoClient satisfies it; tests provide fakes.
type MarketDataClient interface {
	GetAccountState(ctx context.Context, address string) (*client.AccountState, error)
	GetPositions(ctx context.Context, address string) ([]models.Position, error)
	GetAssetInfo(ctx context.Context, symbol string) (client.AssetMeta, error)
	GetAssetIndex(ctx context.Context, symbol string) (int, error)
	GetMidPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetFundingRate(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetCandles(ctx context.Context, symbol, interval string, lookback int) ([]client.Candle, error)
}

// DecisionStore persists a risk decision log. *storage.Storage satisfies it.
type DecisionStore interface {
	SaveRiskDecision(decision models.RiskDecisionLog, network string) (int64, error)
}

// EvaluateSignal is the async data-fetching wrapper around the pure risk core
// (SPEC-007 Phase 6, A.1). It fetches account/market data via info, runs
// mgr.EvaluateSignalWithData, and persists the decision log when store is
// non-nil. Returns exactly one of (*TradeOrder, *RiskReject); the third return
// is a transport/data error. Mirrors manager.py:evaluate_signal.
func EvaluateSignal(
	ctx context.Context,
	mgr *risk.Manager,
	info MarketDataClient,
	signal *models.TradingSignal,
	address string,
	tradeHistory []models.TradeResult,
	store DecisionStore,
	network string,
) (*models.TradeOrder, *models.RiskReject, error) {
	state, err := info.GetAccountState(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	positions, err := info.GetPositions(ctx, address)
	if err != nil {
		return nil, nil, err
	}
	meta, err := info.GetAssetInfo(ctx, signal.Pair)
	if err != nil {
		return nil, nil, err
	}
	markPrice, err := info.GetMidPrice(ctx, signal.Pair)
	if err != nil {
		return nil, nil, err
	}
	fundingRate, err := info.GetFundingRate(ctx, signal.Pair)
	if err != nil {
		return nil, nil, err
	}
	assetID, err := info.GetAssetIndex(ctx, signal.Pair)
	if err != nil {
		return nil, nil, err
	}

	// Candles are only needed in MANAGED mode or when the signal lacks a stop.
	var candles []risk.Candle
	if mgr.RiskMode == models.ModeManaged || signal.StopLoss == nil {
		interval := risk.ATRSettings[signal.Horizon].Interval
		raw, err := info.GetCandles(ctx, signal.Pair, interval, candleLookback)
		if err != nil {
			return nil, nil, err
		}
		candles = toRiskCandles(raw)
	}

	maxLev := meta.MaxLeverage
	if maxLev <= 0 {
		maxLev = defaultCoinMaxLeverage
	}

	order, reject := mgr.EvaluateSignalWithData(risk.EvaluateInput{
		Signal:           *signal,
		AccountValue:     state.MarginSummary.AccountValue,
		AvailableBalance: state.Withdrawable,
		OpenPositions:    positions,
		AssetMeta: risk.AssetMeta{
			SzDecimals:   meta.SzDecimals,
			MaxLeverage:  maxLev,
			OnlyIsolated: meta.OnlyIsolated,
			AssetID:      assetID,
		},
		Candles:      candles,
		FundingRate:  fundingRate,
		MarkPrice:    markPrice,
		TradeHistory: tradeHistory,
	})

	if store != nil {
		if log := mgr.DecisionLog(); log != nil {
			if _, err := store.SaveRiskDecision(*log, network); err != nil {
				return nil, nil, err
			}
		}
	}

	return order, reject, nil
}

func toRiskCandles(raw []client.Candle) []risk.Candle {
	out := make([]risk.Candle, len(raw))
	for i, c := range raw {
		out[i] = risk.Candle{High: c.H, Low: c.L, Close: c.C}
	}
	return out
}
