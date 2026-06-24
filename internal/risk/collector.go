package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
)

// TradeResultStore is the storage surface the collector needs. Implemented by
// the storage layer (SPEC-007 Phase 5). Mirrors the Storage methods used by
// collector.py.
type TradeResultStore interface {
	SaveTradeResult(result models.TradeResult, network string) (int64, error)
	GetRecentTradeResults(network string, limit int) ([]models.TradeResult, error)
}

// FillsProvider is the Info API surface needed to reconcile from fills.
// Satisfied by *client.InfoClient.
type FillsProvider interface {
	GetUserFills(ctx context.Context, address string, limit int) ([]json.RawMessage, error)
}

// Collector collects closed trade results for circuit-breaker tracking. Mirrors
// collector.py:TradeResultCollector. The in-memory fill dedup mirrors the Python
// behaviour; the UNIQUE-on-fill_id upsert is scheduled for Phase 5 (B.5). The
// clock is injectable for deterministic tests.
type Collector struct {
	Storage TradeResultStore
	Network string

	recordedFills map[string]struct{}
	now           func() time.Time
}

// NewCollector builds a Collector backed by storage for a network.
func NewCollector(storage TradeResultStore, network string) *Collector {
	return &Collector{
		Storage:       storage,
		Network:       network,
		recordedFills: map[string]struct{}{},
		now:           time.Now,
	}
}

// WithClock overrides the clock (for tests).
func (c *Collector) WithClock(now func() time.Time) *Collector {
	c.now = now
	return c
}

// startPosition mirrors the nested startPosition object in a fill.
type startPosition struct {
	EntryPx *string `json:"entryPx"`
	Time    *int64  `json:"time"`
}

// rawFill mirrors the fields the collector reads from an HL user fill.
type rawFill struct {
	Coin          string         `json:"coin"`
	Px            string         `json:"px"`
	Sz            string         `json:"sz"`
	Side          string         `json:"side"`
	Time          int64          `json:"time"`
	Oid           int64          `json:"oid"`
	ClosedPnl     *string        `json:"closedPnl"`
	Fee           string         `json:"fee"`
	StartPosition *startPosition `json:"startPosition"`
}

// CollectFromFills reconciles trade results from HL user fills, matching closing
// fills to PnL. Returns the newly recorded results. Mirrors
// collector.py:collect_from_fills. A nil sinceTimestamp processes all fills.
func (c *Collector) CollectFromFills(
	ctx context.Context,
	info FillsProvider,
	address string,
	sinceTimestamp *time.Time,
) ([]models.TradeResult, error) {
	rawFills, err := info.GetUserFills(ctx, address, 100)
	if err != nil {
		return nil, err
	}

	var results []models.TradeResult
	for _, raw := range rawFills {
		var fill rawFill
		if err := json.Unmarshal(raw, &fill); err != nil {
			return nil, fmt.Errorf("decode fill: %w", err)
		}

		// Skip non-closing fills (closedPnl absent or empty).
		if fill.ClosedPnl == nil || *fill.ClosedPnl == "" {
			continue
		}

		// Dedup by unique fill ID.
		fillID := fmt.Sprintf("%d_%d", fill.Oid, fill.Time)
		if _, seen := c.recordedFills[fillID]; seen {
			continue
		}

		fillTime := time.UnixMilli(fill.Time).UTC()
		if sinceTimestamp != nil && fillTime.Before(*sinceTimestamp) {
			continue
		}

		// Entry price from startPosition if available, else the fill price.
		entryPx := fill.Px
		openedMs := fill.Time
		if fill.StartPosition != nil {
			if fill.StartPosition.EntryPx != nil {
				entryPx = *fill.StartPosition.EntryPx
			}
			if fill.StartPosition.Time != nil {
				openedMs = *fill.StartPosition.Time
			}
		}

		side := "short"
		if fill.Side == "B" {
			side = "long"
		}

		result := models.TradeResult{
			Coin:        fill.Coin,
			Side:        side,
			EntryPrice:  mustParseDec(entryPx),
			ExitPrice:   mustParseDec(fill.Px),
			Size:        mustParseDec(fill.Sz),
			Pnl:         mustParseDec(*fill.ClosedPnl),
			Fees:        mustParseDec(fill.Fee),
			FundingPaid: decimal.Zero, // not available in fills
			OpenedAt:    time.UnixMilli(openedMs).UTC(),
			ClosedAt:    fillTime,
		}

		id, err := c.Storage.SaveTradeResult(result, c.Network)
		if err != nil {
			return nil, err
		}
		result.ID = &id
		c.recordedFills[fillID] = struct{}{}
		results = append(results, result)
	}

	return results, nil
}

// RecordClose records a position close initiated via the CLI. Mirrors
// collector.py:record_close.
func (c *Collector) RecordClose(
	position models.Position,
	exitPrice, fees, fundingPaid decimal.Decimal,
	signalID *int64,
) (models.TradeResult, error) {
	pnl := c.calculatePnl(position, exitPrice, fees, fundingPaid)

	side := "short"
	if position.IsLong() {
		side = "long"
	}
	openedAt := c.now().UTC()
	if position.OpenedAt != nil {
		openedAt = *position.OpenedAt
	}

	result := models.TradeResult{
		SignalID:    signalID,
		Coin:        position.Coin,
		Side:        side,
		EntryPrice:  position.EntryPrice,
		ExitPrice:   exitPrice,
		Size:        position.AbsSize(),
		Pnl:         pnl,
		Fees:        fees,
		FundingPaid: fundingPaid,
		OpenedAt:    openedAt,
		ClosedAt:    c.now().UTC(),
	}

	id, err := c.Storage.SaveTradeResult(result, c.Network)
	if err != nil {
		return models.TradeResult{}, err
	}
	result.ID = &id
	return result, nil
}

// GetRecentResults returns recent trade results from storage. Mirrors
// collector.py:get_recent_results.
func (c *Collector) GetRecentResults(limit int) ([]models.TradeResult, error) {
	return c.Storage.GetRecentTradeResults(c.Network, limit)
}

// calculatePnl computes realized net PnL. Mirrors collector.py:_calculate_pnl.
func (c *Collector) calculatePnl(
	position models.Position,
	exitPrice, fees, fundingPaid decimal.Decimal,
) decimal.Decimal {
	var grossPnl decimal.Decimal
	if position.IsLong() {
		grossPnl = exitPrice.Sub(position.EntryPrice).Mul(position.AbsSize())
	} else {
		grossPnl = position.EntryPrice.Sub(exitPrice).Mul(position.AbsSize())
	}
	return grossPnl.Sub(fees).Sub(fundingPaid)
}

// mustParseDec parses a decimal string, treating malformed/empty input as zero
// (mirrors Python's Decimal(str(x)) over API strings, which are well-formed).
func mustParseDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}
