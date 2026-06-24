package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/signer"
)

// errSignerRequired is returned by vault write operations when no signer is set.
var errSignerRequired = errors.New("signer required for vault operations")

// VaultClient drives the Hyperliquid Vault API. The signer is optional: it is
// required only for authenticated operations (deposit/withdraw/create). Mirrors
// client/vault.py:VaultClient.
type VaultClient struct {
	*BaseClient
	signer *signer.Signer
}

// NewVaultClient builds a VaultClient. signer may be nil for read-only use.
func NewVaultClient(network config.NetworkConfig, s *signer.Signer, opts ...Option) *VaultClient {
	return &VaultClient{BaseClient: NewBaseClient(network, opts...), signer: s}
}

// vaultWire is the raw vault summary/details object.
type vaultWire struct {
	Vault        string           `json:"vault"`
	Name         string           `json:"name"`
	Leader       string           `json:"leader"`
	TVL          decimal.Decimal  `json:"tvl"`
	APR          decimal.Decimal  `json:"apr"`
	ProfitShare  decimal.Decimal  `json:"profitShare"`
	LockupPeriod int              `json:"lockupPeriod"`
	IsPublic     *bool            `json:"isPublic"`
	Followers    int              `json:"followers"`
	MaxCapacity  *decimal.Decimal `json:"maxCapacity"`
}

func (w vaultWire) toInfo() models.VaultInfo {
	isPublic := true
	if w.IsPublic != nil {
		isPublic = *w.IsPublic
	}
	return models.VaultInfo{
		Address:      w.Vault,
		Name:         w.Name,
		Leader:       w.Leader,
		TVL:          w.TVL,
		APR:          w.APR,
		ProfitShare:  w.ProfitShare,
		LockupPeriod: w.LockupPeriod,
		IsPublic:     isPublic,
		Followers:    w.Followers,
		MaxCapacity:  w.MaxCapacity,
	}
}

// ListVaults lists public vaults, optionally filtered by minimum TVL/APR (nil to
// skip a filter). Mirrors vault.py:list_vaults.
func (c *VaultClient) ListVaults(ctx context.Context, minTVL, minAPR *decimal.Decimal) ([]models.VaultInfo, error) {
	raw, err := c.post(ctx, "info", map[string]any{"type": "vaultSummaries"}, true)
	if err != nil {
		return nil, err
	}
	var wire []vaultWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode vault summaries: %v", err), 0, nil)
	}
	vaults := make([]models.VaultInfo, 0, len(wire))
	for _, w := range wire {
		v := w.toInfo()
		if minTVL != nil && v.TVL.Cmp(*minTVL) < 0 {
			continue
		}
		if minAPR != nil && v.APR.Cmp(*minAPR) < 0 {
			continue
		}
		vaults = append(vaults, v)
	}
	return vaults, nil
}

// GetVaultDetails fetches a vault's details, optionally with the follower state
// for userAddress (empty to skip). Returns *VaultNotFoundError if the vault is
// unknown. Mirrors vault.py:get_vault_details.
func (c *VaultClient) GetVaultDetails(ctx context.Context, vaultAddress, userAddress string) (*models.VaultDetails, error) {
	req := map[string]any{"type": "vaultDetails", "vault": vaultAddress}
	if userAddress != "" {
		req["user"] = userAddress
	}

	raw, err := c.post(ctx, "info", req, true)
	if err != nil {
		if e, ok := asAPIError(err); ok && strings.Contains(strings.ToLower(e.Message), "not found") {
			return nil, &VaultNotFoundError{newAPIError("Vault not found: "+vaultAddress, 0, nil)}
		}
		return nil, err
	}

	var data struct {
		vaultWire
		Portfolio struct {
			AccountValue decimal.Decimal  `json:"accountValue"`
			Positions    []map[string]any `json:"positions"`
		} `json:"portfolio"`
		FollowerState map[string]any `json:"followerState"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode vault details: %v", err), 0, nil)
	}

	return &models.VaultDetails{
		Info:          data.toInfo(),
		AccountValue:  data.Portfolio.AccountValue,
		Positions:     data.Portfolio.Positions,
		FollowerState: data.FollowerState,
	}, nil
}

// DepositToVault deposits USD into a vault. Requires a signer. Mirrors
// vault.py:deposit_to_vault.
func (c *VaultClient) DepositToVault(ctx context.Context, vaultAddress string, amountUSD decimal.Decimal) (bool, error) {
	if c.signer == nil {
		return false, errSignerRequired
	}
	action := signer.NewOrderedMap(
		"type", "vaultDeposit",
		"vault", vaultAddress,
		"usd", amountUSD.String(),
	)
	payload, err := c.signer.SignAction(action, nonceNow(), nil, nil)
	if err != nil {
		return false, nil
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return false, nil
	}
	return statusOK(raw), nil
}

// WithdrawFromVault withdraws shares from a vault. Requires a signer. Returns
// *LockupPeriodError if the vault is still in lockup. Mirrors vault.py:withdraw_from_vault.
func (c *VaultClient) WithdrawFromVault(ctx context.Context, vaultAddress string, shares decimal.Decimal) (bool, error) {
	if c.signer == nil {
		return false, errSignerRequired
	}
	action := signer.NewOrderedMap(
		"type", "vaultWithdraw",
		"vault", vaultAddress,
		"shares", shares.String(),
	)
	payload, err := c.signer.SignAction(action, nonceNow(), nil, nil)
	if err != nil {
		return false, nil
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		if e, ok := asAPIError(err); ok && strings.Contains(strings.ToLower(e.Message), "lockup") {
			return false, &LockupPeriodError{newAPIError(e.Message, 0, nil)}
		}
		return false, nil
	}
	return statusOK(raw), nil
}

// GetMyVaultPositions returns the user's positions across all vaults. If
// userAddress is empty it uses the signer's address (signer required then).
// Mirrors vault.py:get_my_vault_positions.
func (c *VaultClient) GetMyVaultPositions(ctx context.Context, userAddress string) ([]models.VaultPosition, error) {
	if userAddress == "" {
		if c.signer == nil {
			return nil, errors.New("user address or signer required")
		}
		userAddress = c.signer.Address()
	}

	raw, err := c.post(ctx, "info", map[string]any{"type": "userVaultEquities", "user": userAddress}, true)
	if err != nil {
		return nil, err
	}
	var wire []struct {
		Vault        string          `json:"vault"`
		VaultName    string          `json:"vaultName"`
		Shares       decimal.Decimal `json:"shares"`
		Deposited    decimal.Decimal `json:"deposited"`
		CurrentValue decimal.Decimal `json:"currentValue"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, newAPIError(fmt.Sprintf("decode vault equities: %v", err), 0, nil)
	}
	positions := make([]models.VaultPosition, 0, len(wire))
	for _, w := range wire {
		positions = append(positions, models.VaultPosition{
			Vault:        w.Vault,
			VaultName:    w.VaultName,
			Shares:       w.Shares,
			Deposited:    w.Deposited,
			CurrentValue: w.CurrentValue,
		})
	}
	return positions, nil
}

// CreateVault creates a new vault and returns its address. Requires a signer.
// Mirrors vault.py:create_vault.
func (c *VaultClient) CreateVault(
	ctx context.Context,
	name, description string,
	isPublic bool,
	maxCapacity *decimal.Decimal,
	lockupPeriod int,
	profitShare decimal.Decimal,
) (string, error) {
	if c.signer == nil {
		return "", errSignerRequired
	}
	action := signer.NewOrderedMap(
		"type", "createVault",
		"name", name,
		"description", description,
		"isPublic", isPublic,
		"lockupPeriod", lockupPeriod,
		"profitShare", profitShare.String(),
	)
	if maxCapacity != nil {
		action.Set("maxCapacity", maxCapacity.String())
	}

	payload, err := c.signer.SignAction(action, nonceNow(), nil, nil)
	if err != nil {
		return "", err
	}
	raw, err := c.post(ctx, "exchange", payload, true)
	if err != nil {
		return "", err
	}
	var resp struct {
		Status   string `json:"status"`
		Response struct {
			Vault string `json:"vault"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.Status != "ok" {
		return "", newAPIError("Failed to create vault", 0, nil)
	}
	return resp.Response.Vault, nil
}
