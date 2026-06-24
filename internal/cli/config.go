package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/wallet"
)

// allNetworks lists the configured networks in a stable order for the
// None-means-all config commands.
var allNetworks = []string{"mainnet", "testnet"}

// readSecret reads a line from stdin without echo (getpass equivalent).
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(b)), err
}

// confirm prompts a yes/no question, returning true only on an explicit "y"/
// "yes". Mirrors typer.confirm with default No.
func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration and wallet management",
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(
		newConfigSetKeyCmd(),
		newConfigRemoveKeyCmd(),
		newConfigShowAddressCmd(),
		newConfigCheckCmd(),
	)
	return cmd
}

func newConfigSetKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-key",
		Short: "Save a private key to the system keyring",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, false)
			if err != nil {
				return err
			}
			key, err := readSecret(fmt.Sprintf("Enter private key for %s: ", network))
			if err != nil {
				return err
			}
			if key == "" {
				out("%s", yellow("No key provided, operation cancelled."))
				return nil
			}
			mgr := wallet.NewWalletManager(false)
			if err := mgr.SaveToKeyring(network, key); err != nil {
				out("%s", red("Invalid key: "+err.Error()))
				return errExit
			}
			address, _ := mgr.GetAddress(network)
			out("%s", green("Key saved to keyring for "+network))
			out("Address: %s", address)
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newConfigRemoveKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-key",
		Short: "Remove a private key from the system keyring",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, false)
			if err != nil {
				return err
			}
			mgr := wallet.NewWalletManager(false)
			if mgr.RemoveFromKeyring(network) {
				out("%s", green("Key removed from keyring for "+network))
			} else {
				out("%s", yellow("No key found in keyring for "+network))
			}
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newConfigShowAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-address",
		Short: "Show wallet addresses for configured keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
			networks, err := networkListFromFlag(cmd)
			if err != nil {
				return err
			}
			mgr := wallet.NewWalletManager(false)
			tbl := NewTable("Wallet Addresses")
			tbl.Col("Network").Col("Address").Col("Source")
			foundAny := false
			for _, net := range networks {
				result, err := mgr.GetPrivateKey(net)
				if err == nil && result != nil {
					addr, _ := result.Address()
					tbl.Row(net, addr, result.Provider)
					foundAny = true
				} else {
					tbl.Row(net, dim("not configured"), "-")
				}
			}
			tbl.Render()
			if !foundAny {
				out("\n%s", yellow("No keys configured. Use 'hyperhandler config set-key' or set HL_PRIVATE_KEY environment variable."))
			}
			return nil
		},
	}
	cmd.Flags().StringP("network", "n", "", "Network name (mainnet/testnet)")
	return cmd
}

func newConfigCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check configuration and provider status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			networks, err := networkListFromFlag(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			mgr := wallet.NewWalletManager(false)

			out("%s %s", bold("Current network:"), cfg.Network())
			out("%s %s", bold("Config file:"), cfg.Path())
			out("")

			netTbl := NewTable("Networks")
			netTbl.Col("Network").Col("API URL")
			for _, name := range allNetworks {
				netTbl.Row(name, config.Networks[name].APIURL)
			}
			netTbl.Render()
			out("")

			for _, net := range networks {
				out("%s", bold("Provider status for "+net+":"))
				status := mgr.CheckProviders(net)
				provTbl := NewTable("")
				provTbl.Col("Provider").Col("Available").Col("Has Key")
				for _, p := range mgr.Providers() {
					info := status[p.Name()]
					available := red("no")
					if info.Available {
						available = green("yes")
					}
					hasKey := dim("no")
					if info.HasKey {
						hasKey = green("yes")
					}
					provTbl.Row(p.Name(), available, hasKey)
				}
				provTbl.Render()

				if result, err := mgr.GetPrivateKey(net); err == nil && result != nil {
					addr, _ := result.Address()
					out("  Active key from: %s", yellow(result.Provider))
					out("  Address: %s", green(addr))
				}
				out("")
			}
			return nil
		},
	}
	cmd.Flags().StringP("network", "n", "", "Network name (mainnet/testnet)")
	return cmd
}

// networkListFromFlag returns the single validated network when --network is
// set, or all networks when it is empty (the None-means-all config commands).
func networkListFromFlag(cmd *cobra.Command) ([]string, error) {
	val, _ := cmd.Flags().GetString("network")
	if val == "" {
		return allNetworks, nil
	}
	if _, err := config.Network(val); err != nil {
		out("%s", red("Unknown network: "+val))
		return nil, errExit
	}
	return []string{val}, nil
}
