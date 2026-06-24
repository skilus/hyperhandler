package cli

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/service"
	"github.com/skilus/hyperhandler/internal/storage"
)

func newRiskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "risk",
		Short: "Risk management commands",
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newRiskCheckCmd(), newRiskStatusCmd(), newRiskResetCmd())
	return cmd
}

func newRiskCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check signal against risk rules without executing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			signalPath, _ := cmd.Flags().GetString("signal")
			if signalPath == "" {
				out("%s", red("--signal is required"))
				return errExit
			}
			signal, err := service.ParseSignalFile(signalPath)
			if err != nil {
				out("%s", red("Invalid signal: "+err.Error()))
				return errExit
			}
			levelStr, _ := cmd.Flags().GetString("risk-level")
			level, ok := parseRiskLevel(levelStr)
			if !ok {
				out("%s", red("Invalid risk level: "+levelStr+". Use low/medium/high."))
				return errExit
			}
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			store, err := storage.New("")
			if err != nil {
				return err
			}
			defer store.Close()

			order, reject, err := service.RiskCheck(context.Background(), netCfg, sgn, store, signal, network, level)
			if err != nil {
				return err
			}
			if reject != nil {
				out("\n%s", red("Signal rejected"))
				out("  Reason: %s", reject.Reason)
				out("  Details: %s", reject.Details)
				out("  Suggested action: %s", reject.SuggestedAction)
				return errExit
			}
			out("\n%s", green("Signal approved"))
			printRiskDiff(signal, order)
			printTradeOrderSummary(order)
			if len(order.CalculationDetails) > 0 {
				out("\n%s", bold("Calculation Details:"))
				for k, v := range order.CalculationDetails {
					out("  %s: %v", k, v)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringP("signal", "s", "", "Path to signal JSON file")
	addNetworkFlag(cmd, "testnet")
	cmd.Flags().StringP("risk-level", "r", "medium", "Risk level (low/medium/high)")
	return cmd
}

func newRiskStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current risk status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			levelStr, _ := cmd.Flags().GetString("risk-level")
			level, ok := parseRiskLevel(levelStr)
			if !ok {
				out("%s", red("Invalid risk level: "+levelStr+". Use low/medium/high."))
				return errExit
			}
			netCfg, _ := config.Network(network)
			_, sgn, err := walletAndSigner(network)
			if err != nil {
				return err
			}
			store, err := storage.New("")
			if err != nil {
				return err
			}
			defer store.Close()

			data, err := service.RiskStatus(context.Background(), netCfg, sgn, store, network, level)
			if err != nil {
				return err
			}
			renderRiskStatus(data, network)
			return nil
		},
	}
	addNetworkFlag(cmd, "testnet")
	cmd.Flags().StringP("risk-level", "r", "medium", "Risk profile for the circuit breaker (low/medium/high)")
	return cmd
}

func renderRiskStatus(d *service.RiskStatusData, network string) {
	out("\n%s", bold("Risk Status ("+network+")"))
	out("Address: %s", cyan(d.Address))
	out("")
	out("%s", bold("Account:"))
	out("  Account Value: %s", green("$"+fixed(d.AccountValue, 2)))
	out("  Available Balance: $%s", fixed(d.Available, 2))
	out("")

	if len(d.Positions) > 0 {
		out("%s", bold("Open Positions ("+itoa(len(d.Positions))+"):"))
		tbl := NewTable("")
		tbl.Col("Coin").Col("Side").ColRight("Size").ColRight("Entry").
			ColRight("PnL").ColRight("Risk $")
		for _, p := range d.Positions {
			riskCell := "-"
			if p.RiskAmount != nil && !p.RiskAmount.IsZero() {
				riskCell = "$" + fixed(*p.RiskAmount, 2)
			}
			tbl.Row(
				p.Coin,
				sideLong(p.IsLong()),
				p.AbsSize().String(),
				fixed(p.EntryPrice, 2),
				pnlColored(p.UnrealizedPnl, 2),
				riskCell,
			)
		}
		tbl.Render()
		riskPct := decimal.Zero
		if d.AccountValue.Sign() > 0 {
			riskPct = d.TotalRisk.Div(d.AccountValue)
		}
		out("  Total Risk: $%s (%s)", fixed(d.TotalRisk, 2), pct(riskPct, 1))
	} else {
		out("%s", dim("No open positions"))
	}
	out("")

	out("%s", bold("Circuit Breaker:"))
	cb := d.CBStatus
	if cb.Active {
		statusText := yellow(cb.Level)
		if cb.Level == "HARD" {
			statusText = red(cb.Level)
		}
		out("  Status: %s", statusText)
		out("  Trigger: %s", cb.Trigger)
		if cb.Reason != nil {
			out("  Reason: %s", *cb.Reason)
		}
		out("  Risk Multiplier: %s", cb.RiskMultiplier)
	} else {
		out("  Status: %s", green("INACTIVE"))
	}
	out("  Consecutive Losses: %d", cb.ConsecutiveLosses)
	out("  Daily Loss: %s", pct(cb.DailyLossPct, 2))
	out("")

	if len(d.TradeHistory) > 0 {
		n := 5
		if len(d.TradeHistory) < n {
			n = len(d.TradeHistory)
		}
		out("%s", bold("Recent Trades (last "+itoa(n)+"):"))
		tbl := NewTable("")
		tbl.Col("Coin").Col("Side").ColRight("PnL").Col("Closed")
		for _, t := range d.TradeHistory[:n] {
			tbl.Row(
				t.Coin,
				sideLong(t.Side == "long"),
				pnlColored(t.Pnl, 2),
				t.ClosedAt.Format("2006-01-02 15:04"),
			)
		}
		tbl.Render()
	} else {
		out("%s", dim("No recent trades"))
	}
}

func newRiskResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset circuit breaker (manual override)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			skipConfirm, _ := cmd.Flags().GetBool("yes")
			store, err := storage.New("")
			if err != nil {
				return err
			}
			defer store.Close()

			history, err := store.GetRecentTradeResults(network, 50)
			if err != nil {
				return err
			}
			losses := service.RecentConsecutiveLosses(history)
			if losses == 0 {
				out("%s", yellow("Circuit breaker is not triggered (no consecutive losses)"))
				return nil
			}
			out("%s", bold("Current consecutive losses: "+itoa(losses)))
			if !skipConfirm {
				if !confirm("Reset circuit breaker by adding a virtual win?") {
					out("%s", yellow("Cancelled"))
					return nil
				}
			}
			if err := service.ResetCircuitBreaker(store, network); err != nil {
				return err
			}
			out("%s", green("Circuit breaker reset successfully"))
			out("%s", dim("A virtual trade with $0.01 PnL was added to reset consecutive losses"))
			return nil
		},
	}
	addNetworkFlag(cmd, "testnet")
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	return cmd
}
