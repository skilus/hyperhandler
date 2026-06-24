package cli

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
)

func newVaultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vaults",
		Short: "Vault operations",
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(
		newVaultsListCmd(),
		newVaultsInfoCmd(),
		newVaultsDepositCmd(),
		newVaultsWithdrawCmd(),
		newVaultsMyPositionsCmd(),
	)
	return cmd
}

func newVaultsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List public vaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			var minTVL, minAPR *decimal.Decimal
			if cmd.Flags().Changed("min-tvl") {
				v, _ := cmd.Flags().GetFloat64("min-tvl")
				d := decimal.NewFromFloat(v)
				minTVL = &d
			}
			if cmd.Flags().Changed("min-apr") {
				v, _ := cmd.Flags().GetFloat64("min-apr")
				d := decimal.NewFromFloat(v)
				minAPR = &d
			}
			netCfg, _ := config.Network(network)
			vc := client.NewVaultClient(netCfg, nil)
			vaults, err := vc.ListVaults(context.Background(), minTVL, minAPR)
			if err != nil {
				return err
			}
			if len(vaults) == 0 {
				out("%s", dim("No vaults found"))
				return nil
			}
			if limit > 0 && len(vaults) > limit {
				vaults = vaults[:limit]
			}
			tbl := NewTable("Public Vaults (" + network + ")")
			tbl.Col("Address").Col("Name").ColRight("TVL").ColRight("APR").
				ColRight("Followers").ColRight("Profit Share")
			for _, v := range vaults {
				tbl.Row(
					shortAddr(v.Address),
					truncate(v.Name, 30),
					"$"+fixed(v.TVL, 0),
					fixed(v.APR, 1)+"%",
					itoa(v.Followers),
					fixed(v.ProfitShare, 0)+"%",
				)
			}
			tbl.Render()
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().Float64("min-tvl", 0, "Minimum TVL filter")
	cmd.Flags().Float64("min-apr", 0, "Minimum APR filter")
	cmd.Flags().IntP("limit", "l", 20, "Maximum number of vaults to show")
	return cmd
}

func newVaultsInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <vault-address>",
		Short: "Show vault details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			netCfg, _ := config.Network(network)
			vc := client.NewVaultClient(netCfg, nil)
			details, err := vc.GetVaultDetails(context.Background(), args[0], "")
			if err != nil {
				out("%s", red("Error: "+err.Error()))
				return errExit
			}
			info := details.Info
			out("\n%s", bold(info.Name))
			out("Address: %s", cyan(info.Address))
			out("Leader: %s", cyan(info.Leader))
			out("")
			out("TVL: %s", green("$"+fixed(info.TVL, 2)))
			out("APR: %s", yellow(fixed(info.APR, 1)+"%"))
			out("Profit Share: %s%%", info.ProfitShare)
			out("Lockup Period: %.1f hours", info.LockupHours())
			out("Followers: %d", info.Followers)
			yesNo := "No"
			if info.IsPublic {
				yesNo = "Yes"
			}
			out("Public: %s", yesNo)
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newVaultsDepositCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: "Deposit to a vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			vaultAddr, _ := cmd.Flags().GetString("vault")
			amount, _ := cmd.Flags().GetFloat64("amount")
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			vc := client.NewVaultClient(netCfg, sgn)
			ok, err := vc.DepositToVault(context.Background(), vaultAddr, decimal.NewFromFloat(amount))
			if err != nil {
				out("%s", red("Error: "+err.Error()))
				return errExit
			}
			if ok {
				out("%s", green("Deposited $"+fixed(decimal.NewFromFloat(amount), 2)+" to vault"))
			} else {
				out("%s", red("Deposit failed"))
			}
			return nil
		},
	}
	cmd.Flags().StringP("vault", "v", "", "Vault address")
	cmd.Flags().Float64P("amount", "a", 0, "Amount in USD")
	_ = cmd.MarkFlagRequired("vault")
	_ = cmd.MarkFlagRequired("amount")
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newVaultsWithdrawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: "Withdraw from a vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			vaultAddr, _ := cmd.Flags().GetString("vault")
			shares, _ := cmd.Flags().GetFloat64("shares")
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			vc := client.NewVaultClient(netCfg, sgn)
			ok, err := vc.WithdrawFromVault(context.Background(), vaultAddr, decimal.NewFromFloat(shares))
			if err != nil {
				var lockErr *client.LockupPeriodError
				if errors.As(err, &lockErr) {
					out("%s", red("Cannot withdraw during lockup period"))
					return errExit
				}
				out("%s", red("Error: "+err.Error()))
				return errExit
			}
			label := fixed(decimal.NewFromFloat(shares*100), 1) + "%"
			if ok {
				out("%s", green("Withdrew "+label+" from vault"))
			} else {
				out("%s", red("Withdrawal failed"))
			}
			return nil
		},
	}
	cmd.Flags().StringP("vault", "v", "", "Vault address")
	cmd.Flags().Float64P("shares", "s", 0, "Shares to withdraw (0-1)")
	_ = cmd.MarkFlagRequired("vault")
	_ = cmd.MarkFlagRequired("shares")
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newVaultsMyPositionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "my-positions",
		Short: "Show my positions in vaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			vc := client.NewVaultClient(netCfg, sgn)
			positions, err := vc.GetMyVaultPositions(context.Background(), sgn.Address())
			if err != nil {
				return err
			}
			if len(positions) == 0 {
				out("%s", dim("No vault positions"))
				return nil
			}
			tbl := NewTable("My Vault Positions (" + network + ")")
			tbl.Col("Vault").Col("Name").ColRight("Deposited").ColRight("Current").
				ColRight("PnL").ColRight("PnL %")
			for _, p := range positions {
				tbl.Row(
					shortAddr(p.Vault),
					truncate(p.VaultName, 20),
					"$"+fixed(p.Deposited, 2),
					"$"+fixed(p.CurrentValue, 2),
					pnlColored(p.Pnl(), 2),
					pnlColoredPct(p.PnlPercent()),
				)
			}
			tbl.Render()
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}
