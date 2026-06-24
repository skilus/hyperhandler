package client_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skilus/hyperhandler/internal/client"
)

func TestListVaultsWithFilters(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"vaultSummaries": `[
			{"vault":"0xaaa","name":"Alpha","tvl":"1000000","apr":"0.5","isPublic":true},
			{"vault":"0xbbb","name":"Beta","tvl":"5000","apr":"0.1","isPublic":true}
		]`,
	})
	defer srv.Close()

	c := client.NewVaultClient(netFor(srv.URL), nil)
	minTVL := decimal.RequireFromString("10000")
	vaults, err := c.ListVaults(context.Background(), &minTVL, nil)
	require.NoError(t, err)
	require.Len(t, vaults, 1, "Beta is below the TVL floor")
	assert.Equal(t, "Alpha", vaults[0].Name)
}

func TestGetVaultDetailsNotFound(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"vaultDetails": `{"status":"err","response":"Vault not found"}`,
	})
	defer srv.Close()

	c := client.NewVaultClient(netFor(srv.URL), nil)
	_, err := c.GetVaultDetails(context.Background(), "0xdead", "")
	var nf *client.VaultNotFoundError
	assert.ErrorAs(t, err, &nf)
}

func TestGetVaultDetailsOK(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"vaultDetails": `{"vault":"0xaaa","name":"Alpha","tvl":"1000000","apr":"0.5","portfolio":{"accountValue":"1000000","positions":[]}}`,
	})
	defer srv.Close()

	c := client.NewVaultClient(netFor(srv.URL), nil)
	d, err := c.GetVaultDetails(context.Background(), "0xaaa", "")
	require.NoError(t, err)
	assert.Equal(t, "Alpha", d.Info.Name)
	assert.True(t, d.AccountValue.Equal(decimal.RequireFromString("1000000")))
}

func TestVaultWriteRequiresSigner(t *testing.T) {
	c := client.NewVaultClient(netFor("http://unused"), nil)

	_, err := c.DepositToVault(context.Background(), "0xaaa", decimal.RequireFromString("100"))
	require.Error(t, err)

	_, err = c.WithdrawFromVault(context.Background(), "0xaaa", decimal.RequireFromString("1"))
	require.Error(t, err)

	_, err = c.CreateVault(context.Background(), "V", "", true, nil, 86400, decimal.RequireFromString("10"))
	require.Error(t, err)
}

func TestGetMyVaultPositionsUsesSignerAddress(t *testing.T) {
	srv := jsonServer(t, map[string]string{
		"userVaultEquities": `[{"vault":"0xaaa","vaultName":"Alpha","shares":"10","deposited":"100","currentValue":"150"}]`,
	})
	defer srv.Close()

	c := client.NewVaultClient(netFor(srv.URL), testSigner(t))
	positions, err := c.GetMyVaultPositions(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "Alpha", positions[0].VaultName)
	assert.True(t, positions[0].Pnl().Equal(decimal.RequireFromString("50")))
}
