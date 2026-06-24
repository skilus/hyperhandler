package order

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/signer"
)

// BuildOrderPayload converts a trading signal into a Hyperliquid "order" action.
// The result is a signer.OrderedMap so the msgpack key order matches the Python
// reference exactly (order_builder.build_order_payload). currentPrice (nil to
// omit) is required for market orders.
func (b *Builder) BuildOrderPayload(
	sig *models.TradingSignal,
	assetIndex int,
	currentPrice *decimal.Decimal,
	szDecimals int,
) (*signer.OrderedMap, error) {
	entry, err := b.buildEntryOrder(sig, assetIndex, currentPrice, szDecimals)
	if err != nil {
		return nil, err
	}

	orders := []any{entry}

	if sl := b.buildSLOrder(sig, assetIndex); sl != nil {
		orders = append(orders, sl)
	}
	if tp := b.buildTPOrder(sig, assetIndex); tp != nil {
		orders = append(orders, tp)
	}

	grouping := "na"
	if len(orders) > 1 {
		grouping = "normalTpsl"
	}

	return signer.NewOrderedMap(
		"type", "order",
		"orders", orders,
		"grouping", grouping,
	), nil
}

// buildEntryOrder builds the entry leg. Market orders apply slippage and use
// Ioc; limit orders use the signal's entry price and Gtc.
func (b *Builder) buildEntryOrder(
	sig *models.TradingSignal,
	assetIndex int,
	currentPrice *decimal.Decimal,
	szDecimals int,
) (*signer.OrderedMap, error) {
	isBuy := sig.IsBuy()

	var price decimal.Decimal
	var tif string
	if sig.IsMarket() {
		if currentPrice == nil {
			return nil, fmt.Errorf("Current price required for market orders")
		}
		price = b.slippagePrice(*currentPrice, isBuy, szDecimals, false)
		tif = "Ioc"
	} else {
		if sig.EntryPrice == nil {
			return nil, fmt.Errorf("Entry price required for limit orders")
		}
		price = *sig.EntryPrice
		tif = "Gtc"
	}

	return signer.NewOrderedMap(
		"a", assetIndex,
		"b", isBuy,
		"p", formatPrice(price),
		"s", formatSize(sig.Size),
		"r", false,
		"t", signer.NewOrderedMap("limit", signer.NewOrderedMap("tif", tif)),
	), nil
}

// buildSLOrder builds the reduce-only stop-loss trigger order, or nil when the
// signal has no stop loss.
func (b *Builder) buildSLOrder(sig *models.TradingSignal, assetIndex int) *signer.OrderedMap {
	if sig.StopLoss == nil {
		return nil
	}
	return b.buildTriggerOrder(sig, assetIndex, *sig.StopLoss, "sl")
}

// buildTPOrder builds the reduce-only take-profit trigger order, or nil when the
// signal has no take profit.
func (b *Builder) buildTPOrder(sig *models.TradingSignal, assetIndex int) *signer.OrderedMap {
	if sig.TakeProfit == nil {
		return nil
	}
	return b.buildTriggerOrder(sig, assetIndex, *sig.TakeProfit, "tp")
}

// buildTriggerOrder builds a reduce-only trigger order closing the position; the
// execution price is nudged past the trigger by the slippage. The key order is
// significant for the action hash.
func (b *Builder) buildTriggerOrder(
	sig *models.TradingSignal,
	assetIndex int,
	triggerPx decimal.Decimal,
	tpsl string,
) *signer.OrderedMap {
	isBuy := !sig.IsBuy() // closing order is the opposite side

	one := decimal.NewFromInt(1)
	var execPrice decimal.Decimal
	if isBuy {
		execPrice = triggerPx.Mul(one.Add(b.slippage))
	} else {
		execPrice = triggerPx.Mul(one.Sub(b.slippage))
	}

	return signer.NewOrderedMap(
		"a", assetIndex,
		"b", isBuy,
		"p", formatPrice(execPrice),
		"s", formatSize(sig.Size),
		"r", true,
		"t", signer.NewOrderedMap("trigger", signer.NewOrderedMap(
			"isMarket", true,
			"triggerPx", formatPrice(triggerPx),
			"tpsl", tpsl,
		)),
	)
}

// BuildCancelPayload builds a "cancel" action for a single order. Mirrors
// order_builder.build_cancel_payload.
func (b *Builder) BuildCancelPayload(assetIndex int, orderID int64) *signer.OrderedMap {
	return signer.NewOrderedMap(
		"type", "cancel",
		"cancels", []any{signer.NewOrderedMap("a", assetIndex, "o", orderID)},
	)
}

// BuildLeveragePayload builds an "updateLeverage" action. Mirrors
// order_builder.build_leverage_payload.
func (b *Builder) BuildLeveragePayload(assetIndex, leverage int, isCross bool) *signer.OrderedMap {
	return signer.NewOrderedMap(
		"type", "updateLeverage",
		"asset", assetIndex,
		"isCross", isCross,
		"leverage", leverage,
	)
}
