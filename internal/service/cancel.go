package service

import (
	"context"
	"strings"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/signer"
)

// CancelRequest selects which open orders to cancel. At least one of OrderID,
// Pair or All must be set (the cli enforces this before calling).
type CancelRequest struct {
	Network string
	Vault   *string
	OrderID *int64
	Pair    *string
	All     bool
}

// CancelOrders cancels matching open orders for the account (vault when set,
// else the signer). Returns the number cancelled. Mirrors cli.py:cancel
// do_cancel (the filtering logic, lifted out of the CLI per A.1).
func CancelOrders(ctx context.Context, netCfg config.NetworkConfig, s *signer.Signer, req CancelRequest) (int, error) {
	address := s.Address()
	if req.Vault != nil {
		address = *req.Vault
	}

	info := client.NewInfoClient(netCfg)
	exch := client.NewExchangeClient(netCfg, s, execSlippage)

	orders, err := info.GetOpenOrders(ctx, address)
	if err != nil {
		return 0, err
	}

	cancelled := 0
	for _, order := range orders {
		shouldCancel := false
		switch {
		case req.All:
			shouldCancel = true
		case req.OrderID != nil && order.OrderID == *req.OrderID:
			shouldCancel = true
		case req.Pair != nil && strings.EqualFold(order.Coin, *req.Pair):
			shouldCancel = true
		}
		if !shouldCancel {
			continue
		}

		assetIndex, err := info.GetAssetIndex(ctx, order.Coin)
		if err != nil {
			return cancelled, err
		}
		if exch.CancelOrder(ctx, assetIndex, order.OrderID, req.Vault) {
			cancelled++
		}
	}
	return cancelled, nil
}
