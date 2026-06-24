package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/skilus/hyperhandler/internal/config"
	"github.com/skilus/hyperhandler/internal/service"
	"github.com/skilus/hyperhandler/internal/signer"
	"github.com/skilus/hyperhandler/internal/wallet"
)

// errExit signals "a user-facing message was already printed; exit non-zero
// without printing anything further". Mirrors typer.Exit(1) after console.print.
var errExit = errors.New("exit")

// errVersionExit short-circuits after printing the version, exiting 0. Mirrors
// the is_eager --version callback raising typer.Exit() (no code).
var errVersionExit = errors.New("version")

// Execute builds the command tree and runs it. It returns nil on success and a
// non-nil error on failure; main translates that into the process exit code.
func Execute(version string) error {
	root := newRootCmd(version)
	err := root.Execute()
	if err == nil || errors.Is(err, errVersionExit) {
		return nil
	}
	if !errors.Is(err, errExit) {
		errln("%s", red(fmt.Sprintf("Error: %v", err)))
	}
	return err
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "hyperhandler",
		Short:         "Hyperliquid trading handler CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		// no_args_is_help: show help when invoked bare.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var showVersion bool
	root.PersistentFlags().BoolVarP(&showVersion, "version", "V", false, "Show version and exit")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if showVersion {
			out("hyperhandler version %s", version)
			return errVersionExit
		}
		return nil
	}

	root.AddCommand(
		newExecCmd(),
		newValidateCmd(),
		newPositionsCmd(),
		newOrdersCmd(),
		newStatusCmd(),
		newCancelCmd(),
		newFaucetCmd(),
		newConfigCmd(),
		newVaultsCmd(),
		newRiskCmd(),
		newWalletCmd(),
	)
	return root
}

// addNetworkFlag registers the --network/-n flag with the given default.
func addNetworkFlag(cmd *cobra.Command, def string) {
	cmd.Flags().StringP("network", "n", def, "Network (mainnet/testnet)")
}

// resolveNetwork applies the flag/env/default precedence and validates the
// result. When bindEnv is true, HL_NETWORK overrides the default (but not an
// explicit flag), matching the Python NetworkOption envvar binding. The four
// config commands pass bindEnv=false. Mirrors the network contract in the
// SPEC-007 audit (line 74).
func resolveNetwork(cmd *cobra.Command, bindEnv bool) (string, error) {
	val, _ := cmd.Flags().GetString("network")
	if !cmd.Flags().Changed("network") && bindEnv {
		if e := os.Getenv("HL_NETWORK"); e != "" {
			val = e
		}
	}
	if _, err := config.Network(val); err != nil {
		out("%s", red(fmt.Sprintf("Unknown network: %s", val)))
		out("Available networks: mainnet, testnet")
		return "", errExit
	}
	return val, nil
}

// walletAndSigner resolves the key for network and builds a signer, printing the
// Python guidance and returning errExit when no key is configured. Mirrors
// cli.py:get_wallet_and_signer.
func walletAndSigner(network string) (*wallet.WalletManager, *signer.Signer, error) {
	mgr, s, err := service.WalletAndSigner(network, true)
	if err != nil {
		return nil, nil, err
	}
	if s == nil {
		out("%s", red(fmt.Sprintf("No private key configured for %s", network)))
		out("Use 'hyperhandler config set-key' or set HL_PRIVATE_KEY environment variable.")
		return nil, nil, errExit
	}
	return mgr, s, nil
}
