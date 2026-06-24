package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skilus/hyperhandler/internal/wallet"
)

func newWalletCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: "HD wallet management (seed phrases)",
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(
		newWalletGenerateCmd(),
		newWalletImportCmd(),
		newWalletListCmd(),
		newWalletUseCmd(),
		newWalletDeleteCmd(),
	)
	return cmd
}

// printDerivedAddresses shows the first three addresses derived from a mnemonic,
// matching the confirmation output in wallet generate/import.
func printDerivedAddresses(mnemonic string) error {
	out("%s", bold("Derived Addresses:"))
	for i := 0; i < 3; i++ {
		dk, err := wallet.DeriveHDKey(mnemonic, wallet.DefaultHDPath, i)
		if err != nil {
			return err
		}
		out("  [%d] %s", i, dk.Address)
	}
	return nil
}

func newWalletGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new HD wallet seed phrase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			words, _ := cmd.Flags().GetInt("words")
			save, _ := cmd.Flags().GetBool("save")
			if words != 12 && words != 24 {
				out("%s", red("Words must be 12 or 24"))
				return errExit
			}
			mnemonic, err := wallet.GenerateMnemonic(words)
			if err != nil {
				return err
			}
			out("\n%s", bold(yellow("WARNING: Store this seed phrase securely!")))
			out("%s\n", yellow("Anyone with this phrase can access your funds."))
			out("%s", bold(fmt.Sprintf("Seed Phrase (%d words):", words)))
			out("%s\n", cyan(mnemonic))
			if err := printDerivedAddresses(mnemonic); err != nil {
				return err
			}
			if save {
				provider := wallet.NewHDWalletProvider()
				if err := provider.SaveMnemonic(network, mnemonic); err != nil {
					return err
				}
				out("\n%s", green("Saved to keyring for "+network))
			} else {
				out("\n%s", dim("Use --save to store in keyring for "+network))
			}
			return nil
		},
	}
	cmd.Flags().IntP("words", "w", 12, "Number of words (12 or 24)")
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().BoolP("save", "s", false, "Save to keyring")
	return cmd
}

func newWalletImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing seed phrase",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			out("%s", bold("Enter your seed phrase (12 or 24 words):"))
			mnemonic, err := readSecret("Seed phrase: ")
			if err != nil {
				return err
			}
			if !wallet.ValidateMnemonic(mnemonic) {
				out("%s", red("Invalid seed phrase"))
				return errExit
			}
			out("\n%s", bold("Addresses from this seed:"))
			if err := printDerivedAddresses(mnemonic); err != nil {
				return err
			}
			if confirm("\nSave this seed phrase?") {
				provider := wallet.NewHDWalletProvider()
				if err := provider.SaveMnemonic(network, mnemonic); err != nil {
					return err
				}
				out("%s", green("Saved to keyring for "+network))
			} else {
				out("%s", yellow("Not saved"))
			}
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newWalletListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List derived addresses from HD wallet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			count, _ := cmd.Flags().GetInt("count")
			start, _ := cmd.Flags().GetInt("start")
			provider := wallet.NewHDWalletProvider()
			if !provider.HasKey(network) {
				out("%s", red("No HD wallet configured for "+network))
				out("Use 'hyperhandler wallet generate --save' or 'hyperhandler wallet import'")
				return errExit
			}
			addresses, err := provider.ListAddresses(network, count, start)
			if err != nil {
				return err
			}
			out("\n%s", bold("HD Wallet Addresses ("+network+")"))
			out("Path: m/44'/60'/0'/0/{index}\n")
			tbl := NewTable("")
			tbl.Col("Index").Col("Address")
			for _, a := range addresses {
				tbl.Row(itoa(a.Index), a.Address)
			}
			tbl.Render()
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	cmd.Flags().IntP("count", "c", 5, "Number of addresses to show")
	cmd.Flags().IntP("start", "s", 0, "Starting index")
	return cmd
}

func newWalletUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use",
		Short: "Use a derived key from HD wallet (exports to env-compatible format)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			index, _ := cmd.Flags().GetInt("index")
			provider := wallet.NewHDWalletProvider()
			result, ok := provider.GetKeyAt(network, index)
			if !ok {
				out("%s", red("No HD wallet configured for "+network))
				return errExit
			}
			out("\n%s", bold(fmt.Sprintf("Account %d", index)))
			out("Address: %s", cyan(result.Address))
			out("\nTo use this account, set environment variable:")
			envVar := "HL_" + strings.ToUpper(network) + "_PRIVATE_KEY"
			out("%s", dim(fmt.Sprintf("export %s=\"%s\"", envVar, result.Key)))
			return nil
		},
	}
	cmd.Flags().IntP("index", "i", 0, "Account index to use")
	addNetworkFlag(cmd, "mainnet")
	return cmd
}

func newWalletDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete HD wallet seed phrase from keyring",
		RunE: func(cmd *cobra.Command, _ []string) error {
			network, err := resolveNetwork(cmd, true)
			if err != nil {
				return err
			}
			provider := wallet.NewHDWalletProvider()
			if !provider.HasKey(network) {
				out("%s", yellow("No HD wallet found for "+network))
				return nil
			}
			if confirm(fmt.Sprintf("Delete HD wallet for %s? This cannot be undone!", network)) {
				provider.DeleteMnemonic(network)
				out("%s", green("Deleted HD wallet for "+network))
			} else {
				out("%s", yellow("Cancelled"))
			}
			return nil
		},
	}
	addNetworkFlag(cmd, "mainnet")
	return cmd
}
