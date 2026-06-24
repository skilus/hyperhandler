package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
)

// AssetMeta is one entry of the perp universe. Mirrors the asset dicts in the
// "meta" response.
type AssetMeta struct {
	Name         string `json:"name"`
	SzDecimals   int    `json:"szDecimals"`
	MaxLeverage  int    `json:"maxLeverage"`
	OnlyIsolated bool   `json:"onlyIsolated"`
}

// Meta is the market metadata response.
type Meta struct {
	Universe []AssetMeta `json:"universe"`
}

// MarginSummary is the account margin summary. Money fields are strings on the
// wire. Mirrors the "marginSummary" object.
type MarginSummary struct {
	AccountValue    decimal.Decimal `json:"accountValue"`
	TotalNtlPos     decimal.Decimal `json:"totalNtlPos"`
	TotalRawUsd     decimal.Decimal `json:"totalRawUsd"`
	TotalMarginUsed decimal.Decimal `json:"totalMarginUsed"`
}

// AccountState is the clearinghouse state for an address.
type AccountState struct {
	MarginSummary  MarginSummary   `json:"marginSummary"`
	Withdrawable   decimal.Decimal `json:"withdrawable"`
	AssetPositions []assetPosition `json:"assetPositions"`
}

type assetPosition struct {
	Position positionData `json:"position"`
}

type positionData struct {
	Coin          string           `json:"coin"`
	Szi           decimal.Decimal  `json:"szi"`
	EntryPx       decimal.Decimal  `json:"entryPx"`
	PositionValue decimal.Decimal  `json:"positionValue"`
	UnrealizedPnl decimal.Decimal  `json:"unrealizedPnl"`
	LiquidationPx *decimal.Decimal `json:"liquidationPx"`
	Leverage      struct {
		Type  string `json:"type"`
		Value int    `json:"value"`
	} `json:"leverage"`
}

// InfoClient queries the Hyperliquid Info API (public data). Mirrors
// client/info.py:InfoClient. Not safe for concurrent use (caches are unguarded,
// matching the single-threaded Python design).
type InfoClient struct {
	*BaseClient
	meta       *Meta
	assetIndex map[string]int
	now        func() time.Time
}

// NewInfoClient builds an InfoClient for the network.
func NewInfoClient(network config.NetworkConfig, opts ...Option) *InfoClient {
	return &InfoClient{
		BaseClient: NewBaseClient(network, opts...),
		assetIndex: map[string]int{},
		now:        time.Now,
	}
}

// GetMeta fetches market metadata and refreshes the asset-index cache.
func (c *InfoClient) GetMeta(ctx context.Context) (*Meta, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "meta"}, true)
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode meta: %v", err), 0, nil)
	}
	c.meta = &meta
	for i, asset := range meta.Universe {
		c.assetIndex[asset.Name] = i
	}
	return &meta, nil
}

// GetAssetIndex returns the asset index for a symbol, fetching metadata on the
// first miss. Returns *AssetNotFoundError if the symbol is unknown.
func (c *InfoClient) GetAssetIndex(ctx context.Context, symbol string) (int, error) {
	if idx, ok := c.assetIndex[symbol]; ok {
		return idx, nil
	}
	if c.meta == nil {
		if _, err := c.GetMeta(ctx); err != nil {
			return 0, err
		}
	}
	if idx, ok := c.assetIndex[symbol]; ok {
		return idx, nil
	}
	return 0, &AssetNotFoundError{newAPIError("Asset not found: "+symbol, 0, nil)}
}

// GetAssetInfo returns the metadata entry for a symbol, fetching metadata if
// needed. Returns *AssetNotFoundError if the symbol is unknown.
func (c *InfoClient) GetAssetInfo(ctx context.Context, symbol string) (AssetMeta, error) {
	if c.meta == nil {
		if _, err := c.GetMeta(ctx); err != nil {
			return AssetMeta{}, err
		}
	}
	for _, asset := range c.meta.Universe {
		if asset.Name == symbol {
			return asset, nil
		}
	}
	return AssetMeta{}, &AssetNotFoundError{newAPIError("Asset not found: "+symbol, 0, nil)}
}

// GetAllMids returns current mid prices for all assets.
func (c *InfoClient) GetAllMids(ctx context.Context) (map[string]decimal.Decimal, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "allMids"}, true)
	if err != nil {
		return nil, err
	}
	var mids map[string]decimal.Decimal
	if err := json.Unmarshal(raw, &mids); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode allMids: %v", err), 0, nil)
	}
	return mids, nil
}

// GetMidPrice returns the current mid price for a symbol. Returns
// *AssetNotFoundError if no price is available.
func (c *InfoClient) GetMidPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	mids, err := c.GetAllMids(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	px, ok := mids[symbol]
	if !ok {
		return decimal.Zero, &AssetNotFoundError{newAPIError("Price not found for: "+symbol, 0, nil)}
	}
	return px, nil
}

// GetAccountState returns the clearinghouse state (margin + positions).
func (c *InfoClient) GetAccountState(ctx context.Context, address string) (*AccountState, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "clearinghouseState", "user": address}, true)
	if err != nil {
		return nil, err
	}
	var state AccountState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode account state: %v", err), 0, nil)
	}
	return &state, nil
}

// GetOpenOrders returns the open orders for an address.
func (c *InfoClient) GetOpenOrders(ctx context.Context, address string) ([]models.OpenOrder, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "openOrders", "user": address}, true)
	if err != nil {
		return nil, err
	}
	var wire []struct {
		Coin      string          `json:"coin"`
		Oid       int64           `json:"oid"`
		Side      string          `json:"side"`
		LimitPx   decimal.Decimal `json:"limitPx"`
		Sz        decimal.Decimal `json:"sz"`
		Timestamp int64           `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode open orders: %v", err), 0, nil)
	}
	orders := make([]models.OpenOrder, 0, len(wire))
	for _, o := range wire {
		orders = append(orders, models.OpenOrder{
			Coin:      o.Coin,
			OrderID:   o.Oid,
			Side:      o.Side,
			Price:     o.LimitPx,
			Size:      o.Sz,
			Timestamp: o.Timestamp,
		})
	}
	return orders, nil
}

// GetPositions returns the non-zero open positions for an address.
func (c *InfoClient) GetPositions(ctx context.Context, address string) ([]models.Position, error) {
	state, err := c.GetAccountState(ctx, address)
	if err != nil {
		return nil, err
	}
	positions := make([]models.Position, 0, len(state.AssetPositions))
	for _, ap := range state.AssetPositions {
		pos := ap.Position
		if pos.Coin == "" || pos.Szi.IsZero() {
			continue
		}
		leverage := pos.Leverage.Value
		if leverage == 0 {
			leverage = 1
		}
		leverageType := pos.Leverage.Type
		if leverageType == "" {
			leverageType = "cross"
		}
		positions = append(positions, models.Position{
			Coin:          pos.Coin,
			Size:          pos.Szi,
			EntryPrice:    pos.EntryPx,
			PositionValue: pos.PositionValue,
			UnrealizedPnl: pos.UnrealizedPnl,
			Leverage:      leverage,
			LeverageType:  leverageType,
			LiquidationPx: pos.LiquidationPx,
		})
	}
	return positions, nil
}

// GetMarginSummary returns the margin summary for an address.
func (c *InfoClient) GetMarginSummary(ctx context.Context, address string) (MarginSummary, error) {
	state, err := c.GetAccountState(ctx, address)
	if err != nil {
		return MarginSummary{}, err
	}
	return state.MarginSummary, nil
}

// GetUserFills returns up to limit recent fills for an address.
func (c *InfoClient) GetUserFills(ctx context.Context, address string, limit int) ([]json.RawMessage, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "userFills", "user": address}, true)
	if err != nil {
		return nil, err
	}
	var fills []json.RawMessage
	if err := json.Unmarshal(raw, &fills); err != nil {
		return nil, nil // non-list response → empty, matching Python's isinstance guard
	}
	if limit >= 0 && len(fills) > limit {
		fills = fills[:limit]
	}
	return fills, nil
}

// Candle is one OHLCV candle. Mirrors the candleSnapshot entries.
type Candle struct {
	T int64           `json:"t"` // open time, ms
	O decimal.Decimal `json:"o"`
	H decimal.Decimal `json:"h"`
	L decimal.Decimal `json:"l"`
	C decimal.Decimal `json:"c"`
	V decimal.Decimal `json:"v"`
}

// GetCandles fetches candles for ATR. It requests 20% extra to absorb gaps from
// maintenance, then trims to lookback. Mirrors info.py:get_candles.
func (c *InfoClient) GetCandles(ctx context.Context, symbol, interval string, lookback int) ([]Candle, error) {
	requestLookback := int(float64(lookback) * 1.2)
	endTime := c.now().UnixMilli()
	startTime := endTime - int64(requestLookback)*intervalToMs(interval)

	req := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      symbol,
			"interval":  interval,
			"startTime": startTime,
			"endTime":   endTime,
		},
	}
	raw, err := c.post(ctx, "info", req, true)
	if err != nil {
		return nil, err
	}
	var candles []Candle
	if err := json.Unmarshal(raw, &candles); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode candles: %v", err), 0, nil)
	}
	if len(candles) > lookback {
		candles = candles[len(candles)-lookback:]
	}
	return candles, nil
}

// GetFundingRate returns the current hourly funding rate for a symbol.
func (c *InfoClient) GetFundingRate(ctx context.Context, symbol string) (decimal.Decimal, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "metaAndAssetCtxs"}, true)
	if err != nil {
		return decimal.Zero, err
	}
	// Response is [meta, [ctx, ...]].
	var tuple []json.RawMessage
	if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) < 2 {
		return decimal.Zero, newAPIError("decode metaAndAssetCtxs", 0, nil)
	}
	var meta Meta
	if err := json.Unmarshal(tuple[0], &meta); err != nil {
		return decimal.Zero, newAPIError(fmt.Sprintf("decode meta: %v", err), 0, nil)
	}
	var ctxs []struct {
		Funding decimal.Decimal `json:"funding"`
	}
	if err := json.Unmarshal(tuple[1], &ctxs); err != nil {
		return decimal.Zero, newAPIError(fmt.Sprintf("decode asset ctxs: %v", err), 0, nil)
	}
	for i, asset := range meta.Universe {
		if asset.Name == symbol {
			if i < len(ctxs) {
				return ctxs[i].Funding, nil
			}
			break
		}
	}
	return decimal.Zero, &AssetNotFoundError{newAPIError("Asset context not found: "+symbol, 0, nil)}
}

// Faucet requests testnet funds for the address (testnet only). Returns the
// raw response envelope. Mirrors the inline POST in cli.py:faucet
// (info type "faucet").
func (c *InfoClient) Faucet(ctx context.Context, address string) (map[string]any, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "faucet", "user": address}, true)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode faucet: %v", err), 0, nil)
	}
	return out, nil
}

// intervalToMs converts a candle interval to milliseconds, defaulting to 1h.
// Mirrors info.py:_interval_to_ms.
func intervalToMs(interval string) int64 {
	switch interval {
	case "1m":
		return 60_000
	case "15m":
		return 900_000
	case "1h":
		return 3_600_000
	case "4h":
		return 14_400_000
	case "1d":
		return 86_400_000
	default:
		return 3_600_000
	}
}
