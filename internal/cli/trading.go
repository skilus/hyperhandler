package cli

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/skilus/hyperhandler/internal/client"
	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/models"
	"github.com/skilus/hyperhandler/internal/service"
	"github.com/skilus/hyperhandler/internal/storage"
)

func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Execute a trading signal",
		RunE:  runExec,
	}
	cmd.Flags().StringP("signal", "s", "", "Path to signal JSON file")
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().StringP("vault", "v", "", "Vault address for vault trading")
	cmd.Flags().Bool("dry-run", false, "Validate only, don't execute")
	cmd.Flags().StringP("risk-level", "r", "", "Risk level (low/medium/high). Enables managed risk mode.")
	return cmd
}

func runExec(cmd *cobra.Command, _ []string) error {
	network, err := resolveNetwork(cmd, true)
	if err != nil {
		return err
	}
	signalPath, _ := cmd.Flags().GetString("signal")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	vault := optionalFlag(cmd, "vault")
	riskLevelStr, _ := cmd.Flags().GetString("risk-level")

	// Read signal from file or stdin.
	var signal *models.TradingSignal
	if signalPath != "" {
		if _, statErr := os.Stat(signalPath); statErr != nil {
			out("%s", red("Signal file not found: "+signalPath))
			return errExit
		}
		signal, err = service.ParseSignalFile(signalPath)
	} else {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			out("%s", red("No signal file provided and stdin is empty"))
			out("Usage: hyperhandler exec --signal signal.json")
			out("   or: echo '{...}' | hyperhandler exec")
			return errExit
		}
		signal, err = service.ParseSignalReader(os.Stdin)
	}
	if err != nil {
		out("%s", red("Invalid signal: "+err.Error()))
		return errExit
	}

	// Resolve risk level (managed mode when set).
	var riskLevel *models.RiskLevel
	if riskLevelStr != "" {
		lvl, ok := parseRiskLevel(riskLevelStr)
		if !ok {
			out("%s", red("Invalid risk level: "+riskLevelStr+". Use low/medium/high."))
			return errExit
		}
		riskLevel = &lvl
	}

	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	_, sgn, err := walletAndSigner(network)
	if err != nil {
		return err
	}
	store, err := storage.New("")
	if err != nil {
		return err
	}
	defer store.Close()

	exec := &service.Executor{
		Config:   cfg,
		Signer:   sgn,
		Storage:  store,
		Reporter: func(s string) { out("%s", s) },
	}

	res, err := exec.Exec(context.Background(), service.ExecRequest{
		Signal:    signal,
		Network:   network,
		Vault:     vault,
		DryRun:    dryRun,
		RiskLevel: riskLevel,
	})
	if err != nil {
		var ve *service.ValidationError
		if errors.As(err, &ve) {
			out("%s", red("Signal validation failed:"))
			for _, e := range ve.Errors {
				out("  - %s", e)
			}
			return errExit
		}
		out("%s", red("Execution failed: "+err.Error()))
		return errExit
	}

	for _, w := range res.Warnings {
		out("%s", yellow("Warning: "+w))
	}

	switch res.Outcome {
	case service.OutcomeRejected:
		out("\n%s", red("Signal rejected by risk manager"))
		out("  Reason: %s", res.Reject.Reason)
		out("  Details: %s", res.Reject.Details)
		out("  Suggested action: %s", res.Reject.SuggestedAction)
		return errExit

	case service.OutcomeDryRun:
		if res.Managed {
			printRiskDiff(res.Signal, res.Order)
		}
		out("%s", green("Signal validated successfully (dry run)"))
		printTradeOrderSummary(res.Order)
		return nil

	case service.OutcomeExecuted:
		if res.Managed {
			printRiskDiff(res.Signal, res.Order)
		}
		successCount := 0
		for _, r := range res.Results {
			if r.Success {
				successCount++
			}
		}
		out("\n%s", green("Executed "+itoa(successCount)+"/"+itoa(len(res.Results))+" orders successfully"))
		for i, r := range res.Results {
			label := orderLabel(i, res.Signal.StopLoss != nil)
			if r.Success {
				oid := "?"
				if r.OrderID != nil {
					oid = i64toa(*r.OrderID)
				}
				out("  %s: Order ID %s", label, oid)
			} else {
				out("%s", red("  "+label+": Failed - "+r.Error))
			}
		}
		return nil
	}
	return nil
}

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a trading signal without executing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			signalPath, _ := cmd.Flags().GetString("signal")
			if signalPath == "" {
				out("%s", red("--signal is required"))
				return errExit
			}
			if _, statErr := os.Stat(signalPath); statErr != nil {
				out("%s", red("Signal file not found: "+signalPath))
				return errExit
			}
			signal, err := service.ParseSignalFile(signalPath)
			if err != nil {
				out("%s", red("Invalid signal: "+err.Error()))
				return errExit
			}
			validator := models.NewSignalValidator(nil)
			result := validator.Validate(signal, nil)
			if result.Valid {
				out("%s", green("Signal is valid"))
				printSignalSummary(signal)
			} else {
				out("%s", red("Signal validation failed:"))
				for _, e := range result.Errors {
					out("  - %s", e)
				}
				return errExit
			}
			for _, w := range result.Warnings {
				out("%s", yellow("Warning: "+w))
			}
			return nil
		},
	}
	cmd.Flags().StringP("signal", "s", "", "Path to signal JSON file")
	return cmd
}

func newPositionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "positions",
		Short: "Show open positions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			vault := optionalFlag(cmd, "vault")
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			address := sgn.Address()
			if vault != nil {
				address = *vault
			}
			info := client.NewInfoClient(netCfg)
			positions, err := info.GetPositions(context.Background(), address)
			if err != nil {
				return err
			}
			if len(positions) == 0 {
				out("%s", dim("No open positions"))
				return nil
			}
			tbl := NewTable("Positions (" + network + ")")
			tbl.Col("Pair").Col("Side").ColRight("Size").ColRight("Entry").
				ColRight("Value").ColRight("PnL").ColRight("Leverage")
			for _, p := range positions {
				tbl.Row(
					p.Coin,
					sideLong(p.IsLong()),
					p.AbsSize().String(),
					fixed(p.EntryPrice, 2),
					"$"+fixed(p.PositionValue, 2),
					pnlColored(p.UnrealizedPnl, 2),
					itoa(p.Leverage)+"x",
				)
			}
			tbl.Render()
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().StringP("vault", "v", "", "Vault address for vault trading")
	return cmd
}

func newOrdersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orders",
		Short: "Show open orders",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			vault := optionalFlag(cmd, "vault")
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			address := sgn.Address()
			if vault != nil {
				address = *vault
			}
			info := client.NewInfoClient(netCfg)
			orders, err := info.GetOpenOrders(context.Background(), address)
			if err != nil {
				return err
			}
			if len(orders) == 0 {
				out("%s", dim("No open orders"))
				return nil
			}
			tbl := NewTable("Open Orders (" + network + ")")
			tbl.Col("ID").Col("Pair").Col("Side").ColRight("Price").ColRight("Size")
			for _, o := range orders {
				side := red("SELL")
				if o.IsBuy() {
					side = green("BUY")
				}
				tbl.Row(i64toa(o.OrderID), o.Coin, side, fixed(o.Price, 2), o.Size.String())
			}
			tbl.Render()
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().StringP("vault", "v", "", "Vault address for vault trading")
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show account status",
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
			ctx := context.Background()
			info := client.NewInfoClient(netCfg)
			margin, err := info.GetMarginSummary(ctx, sgn.Address())
			if err != nil {
				return err
			}
			positions, err := info.GetPositions(ctx, sgn.Address())
			if err != nil {
				return err
			}
			orders, err := info.GetOpenOrders(ctx, sgn.Address())
			if err != nil {
				return err
			}
			out("\n%s", bold("Account Status ("+network+")"))
			out("Address: %s", cyan(sgn.Address()))
			out("")
			out("%s", bold("Margin Summary:"))
			out("  Account Value: $%s", fixed(margin.AccountValue, 2))
			out("  Margin Used: $%s", fixed(margin.TotalMarginUsed, 2))
			out("  Position Value: $%s", fixed(margin.TotalNtlPos, 2))
			out("")
			out("%s %d", bold("Positions:"), len(positions))
			out("%s %d", bold("Open Orders:"), len(orders))
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newCancelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel orders",
		RunE: func(cmd *cobra.Command, _ []string) error {
			orderID, _ := cmd.Flags().GetInt64("order-id")
			pair := optionalFlag(cmd, "pair")
			all, _ := cmd.Flags().GetBool("all")
			if orderID == 0 && pair == nil && !all {
				out("%s", red("Specify --order-id, --pair, or --all"))
				return errExit
			}
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			vault := optionalFlag(cmd, "vault")
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			req := service.CancelRequest{Network: network, Vault: vault, Pair: pair, All: all}
			if orderID != 0 {
				req.OrderID = &orderID
			}
			count, err := service.CancelOrders(context.Background(), netCfg, sgn, req)
			if err != nil {
				return err
			}
			if count > 0 {
				out("%s", green(itoa(count)+" order(s) cancelled"))
			} else {
				out("%s", yellow("No orders cancelled"))
			}
			return nil
		},
	}
	cmd.Flags().Int64P("order-id", "o", 0, "Order ID to cancel")
	cmd.Flags().StringP("pair", "p", "", "Cancel all orders for this pair")
	cmd.Flags().Bool("all", false, "Cancel all open orders")
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().StringP("vault", "v", "", "Vault address for vault trading")
	return cmd
}

func newFaucetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "faucet",
		Short: "Request testnet funds (testnet only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			if network != "testnet" {
				out("%s", red("Faucet is only available on testnet"))
				return errExit
			}
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			info := client.NewInfoClient(netCfg)
			result, err := info.Faucet(context.Background(), sgn.Address())
			if err != nil {
				out("%s", red("Faucet request failed: "+err.Error()))
				return nil
			}
			if result["status"] == "ok" {
				out("%s", green("Faucet request successful!"))
			} else {
				out("%s", yellow("Faucet response: "+mapToString(result)))
			}
			return nil
		},
	}
	addNetworkFlag(cmd, "testnet")
	return cmd
}
