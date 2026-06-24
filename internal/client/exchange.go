package client

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/order"
	"github.com/skilus/hyperhandler/internal/signer"
)

// ExchangeClient drives the Hyperliquid Exchange API (trading). Mirrors
// client/exchange.py:ExchangeClient.
type ExchangeClient struct {
	*BaseClient
	signer  *signer.Signer
	builder *order.Builder

	// nonce returns the action nonce (timestamp in ms). Overridable for tests.
	nonce func() int64
}

// NewExchangeClient builds an ExchangeClient with the given signer and slippage.
func NewExchangeClient(network config.NetworkConfig, s *signer.Signer, slippage decimal.Decimal, opts ...Option) *ExchangeClient {
	return &ExchangeClient{
		BaseClient: NewBaseClient(network, opts...),
		signer:     s,
		builder:    order.NewBuilder(slippage),
		nonce:      func() int64 { return time.Now().UnixMilli() },
	}
}

// Address returns the signer's address.
func (c *ExchangeClient) Address() string { return c.signer.Address() }

// signAction signs an action with a fresh nonce and the optional vault address.
func (c *ExchangeClient) signAction(action any, vaultAddress *string) (signer.Payload, error) {
	return c.signer.SignAction(action, c.nonce(), vaultAddress, nil)
}

// PlaceOrder places a single order. price is the wire price (caller applies
// slippage for market orders). Mirrors exchange.py:place_order.
func (c *ExchangeClient) PlaceOrder(
	ctx context.Context,
	assetIndex int,
	isBuy bool,
	size, price decimal.Decimal,
	orderType models.OrderType,
	reduceOnly bool,
	vaultAddress *string,
) models.OrderResult {
	action := c.builder.BuildSingleOrderPayload(assetIndex, isBuy, size, price, orderType, reduceOnly)
	return c.executeOrder(ctx, action, vaultAddress)
}

// PlaceOrderFromSignal builds and submits the order group for a signal (entry +
// optional SL/TP). On a successful grouped submit it returns one result per leg,
// with the SL/TP legs as OPEN placeholders. Mirrors exchange.py:place_order_from_signal.
func (c *ExchangeClient) PlaceOrderFromSignal(
	ctx context.Context,
	sig *models.TradingSignal,
	assetIndex int,
	currentPrice *decimal.Decimal,
	vaultAddress *string,
	szDecimals int,
) ([]models.OrderResult, error) {
	action, err := c.builder.BuildOrderPayload(sig, assetIndex, currentPrice, szDecimals)
	if err != nil {
		return nil, err
	}

	result := c.executeOrder(ctx, action, vaultAddress)

	numOrders := 1
	if orders, ok := action.Get("orders"); ok {
		if list, ok := orders.([]any); ok {
			numOrders = len(list)
		}
	}

	if result.Success && numOrders > 1 {
		results := []models.OrderResult{result}
		for i := 1; i < numOrders; i++ {
			results = append(results, models.OrderResult{Success: true, Status: models.StatusOpen})
		}
		return results, nil
	}
	return []models.OrderResult{result}, nil
}

// orderResponse is the /exchange success envelope for order/cancel actions.
type orderResponse struct {
	Status   string `json:"status"`
	Response struct {
		Data struct {
			Statuses []orderStatusEntry `json:"statuses"`
		} `json:"data"`
	} `json:"response"`
}

// orderStatusEntry is one entry of the statuses array; exactly one of the
// pointers is set per entry.
type orderStatusEntry struct {
	Filled  *filledStatus  `json:"filled"`
	Resting *restingStatus `json:"resting"`
	Error   *string        `json:"error"`
}

type filledStatus struct {
	Oid     int64            `json:"oid"`
	TotalSz decimal.Decimal  `json:"totalSz"`
	AvgPx   *decimal.Decimal `json:"avgPx"`
}

type restingStatus struct {
	Oid int64 `json:"oid"`
}

// executeOrder signs and posts an order action and parses the result. Any error
// (signing, transport, mapped HL error) is folded into a rejected OrderResult,
// matching the Python try/except that never propagates. Mirrors exchange.py:_execute_order.
func (c *ExchangeClient) executeOrder(ctx context.Context, action any, vaultAddress *string) models.OrderResult {
	payload, err := c.signAction(action, vaultAddress)
	if err != nil {
		return models.OrderResult{Success: false, Error: err.Error(), Status: models.StatusRejected}
	}

	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return models.OrderResult{Success: false, Error: err.Error(), Status: models.StatusRejected}
	}

	var resp orderResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return models.OrderResult{Success: false, Error: err.Error(), Status: models.StatusRejected}
	}

	if resp.Status != "ok" {
		return models.OrderResult{Success: false, Error: "Unknown error", Status: models.StatusRejected}
	}

	statuses := resp.Response.Data.Statuses
	if len(statuses) > 0 {
		st := statuses[0]
		switch {
		case st.Filled != nil:
			oid := st.Filled.Oid
			return models.OrderResult{
				Success:    true,
				OrderID:    &oid,
				FilledSize: st.Filled.TotalSz,
				AvgPrice:   st.Filled.AvgPx,
				Status:     models.StatusFilled,
			}
		case st.Resting != nil:
			oid := st.Resting.Oid
			return models.OrderResult{Success: true, OrderID: &oid, Status: models.StatusOpen}
		case st.Error != nil:
			return models.OrderResult{Success: false, Error: *st.Error, Status: models.StatusRejected}
		}
	}

	return models.OrderResult{Success: true, Status: models.StatusPending}
}

// statusOK reports whether the raw /exchange response has status "ok".
func statusOK(raw []byte) bool {
	var env struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(raw, &env) == nil && env.Status == "ok"
}

// CancelOrder cancels a single order. Returns false on any error, matching the
// Python except-swallow. Mirrors exchange.py:cancel_order.
func (c *ExchangeClient) CancelOrder(ctx context.Context, assetIndex int, orderID int64, vaultAddress *string) bool {
	action := c.builder.BuildCancelPayload(assetIndex, orderID)
	payload, err := c.signAction(action, vaultAddress)
	if err != nil {
		return false
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return false
	}
	return statusOK(raw)
}

// CancelAllOrders cancels all open orders (empty cancels = cancel all) and
// returns the number cancelled, 0 on any error. Mirrors exchange.py:cancel_all_orders.
func (c *ExchangeClient) CancelAllOrders(ctx context.Context, vaultAddress *string) int {
	action := signer.NewOrderedMap(
		"type", "cancelByCloid",
		"cancels", []any{},
	)
	payload, err := c.signAction(action, vaultAddress)
	if err != nil {
		return 0
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return 0
	}
	var resp struct {
		Status   string `json:"status"`
		Response struct {
			Data struct {
				Cancelled int `json:"cancelled"`
			} `json:"data"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.Status != "ok" {
		return 0
	}
	return resp.Response.Data.Cancelled
}

// SetLeverage sets the leverage for an asset. Mirrors exchange.py:set_leverage.
func (c *ExchangeClient) SetLeverage(ctx context.Context, assetIndex, leverage int, isCross bool, vaultAddress *string) bool {
	action := c.builder.BuildLeveragePayload(assetIndex, leverage, isCross)
	payload, err := c.signAction(action, vaultAddress)
	if err != nil {
		return false
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return false
	}
	return statusOK(raw)
}

// ClosePosition closes a position by placing an opposite reduce-only market
// order. Mirrors exchange.py:close_position.
func (c *ExchangeClient) ClosePosition(ctx context.Context, assetIndex int, size decimal.Decimal, isLong bool, price decimal.Decimal, vaultAddress *string) models.OrderResult {
	return c.PlaceOrder(ctx, assetIndex, !isLong, size, price, models.Market, true, vaultAddress)
}
