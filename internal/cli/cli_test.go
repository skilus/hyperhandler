package cli

import (
	"os"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"
)

func init() { useColor = false } // deterministic output in tests

// leafCommands walks the tree and returns the fully-qualified names of runnable
// leaf commands (excluding cobra's built-in help/completion).
func leafCommands(cmd *cobra.Command, prefix string) []string {
	var leaves []string
	name := prefix
	if cmd.Name() != "hyperhandler" {
		if prefix != "" {
			name = prefix + " " + cmd.Name()
		} else {
			name = cmd.Name()
		}
	}
	children := cmd.Commands()
	hasRealChild := false
	for _, c := range children {
		switch c.Name() {
		case "help", "completion":
			continue
		}
		hasRealChild = true
		leaves = append(leaves, leafCommands(c, name)...)
	}
	if !hasRealChild && cmd.Name() != "hyperhandler" {
		leaves = append(leaves, name)
	}
	return leaves
}

func TestCommandTreeHas24Commands(t *testing.T) {
	root := newRootCmd("test")
	leaves := leafCommands(root, "")
	if len(leaves) != 24 {
		t.Errorf("got %d leaf commands, want 24:\n%v", len(leaves), leaves)
	}
}

// findCmd navigates a space-separated command path from root.
func findCmd(root *cobra.Command, path ...string) *cobra.Command {
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func networkDefault(t *testing.T, root *cobra.Command, path ...string) string {
	t.Helper()
	c := findCmd(root, path...)
	if c == nil {
		t.Fatalf("command not found: %v", path)
	}
	f := c.Flags().Lookup("network")
	if f == nil {
		t.Fatalf("command %v has no --network flag", path)
	}
	return f.DefValue
}

func TestNetworkDefaultsContract(t *testing.T) {
	root := newRootCmd("test")
	// Default network is testnet for faucet + risk commands, mainnet otherwise.
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"faucet"}, "testnet"},
		{[]string{"risk", "check"}, "testnet"},
		{[]string{"risk", "status"}, "testnet"},
		{[]string{"risk", "reset"}, "testnet"},
		{[]string{"exec"}, "mainnet"},
		{[]string{"positions"}, "mainnet"},
		{[]string{"orders"}, "mainnet"},
		{[]string{"status"}, "mainnet"},
		{[]string{"vaults", "list"}, "mainnet"},
		{[]string{"config", "set-key"}, "mainnet"},
	}
	for _, tc := range cases {
		if got := networkDefault(t, root, tc.path...); got != tc.want {
			t.Errorf("%v network default = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestConfigCommandsDoNotBindEnv(t *testing.T) {
	// The four config commands resolve network without HL_NETWORK binding.
	t.Setenv("HL_NETWORK", "testnet")
	root := newRootCmd("test")
	var setKey *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "config" {
			for _, sc := range c.Commands() {
				if sc.Name() == "set-key" {
					setKey = sc
				}
			}
		}
	}
	if setKey == nil {
		t.Fatal("config set-key not found")
	}
	got, err := resolveNetwork(setKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "mainnet" {
		t.Errorf("config set-key with HL_NETWORK=testnet but bindEnv=false: got %q, want mainnet", got)
	}
}

func TestResolveNetworkEnvBinding(t *testing.T) {
	t.Setenv("HL_NETWORK", "testnet")
	root := newRootCmd("test")
	var positions *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "positions" {
			positions = c
		}
	}
	got, err := resolveNetwork(positions, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "testnet" {
		t.Errorf("positions with HL_NETWORK=testnet: got %q, want testnet", got)
	}
}

func TestParseRiskLevel(t *testing.T) {
	for _, in := range []string{"low", "MEDIUM", "High"} {
		if _, ok := parseRiskLevel(in); !ok {
			t.Errorf("parseRiskLevel(%q) failed", in)
		}
	}
	if _, ok := parseRiskLevel("extreme"); ok {
		t.Error("parseRiskLevel(extreme) should fail")
	}
}

func TestRenderHelpers(t *testing.T) {
	if got := signedFixed(decimal.RequireFromString("1.5"), 2); got != "+1.50" {
		t.Errorf("signedFixed = %q, want +1.50", got)
	}
	if got := signedFixed(decimal.RequireFromString("-1.5"), 2); got != "-1.50" {
		t.Errorf("signedFixed = %q, want -1.50", got)
	}
	if got := pct(decimal.RequireFromString("0.025"), 2); got != "2.50%" {
		t.Errorf("pct = %q, want 2.50%%", got)
	}
	if got := shortAddr("0x1234567890abcdef1234"); got != "0x1234...1234" {
		t.Errorf("shortAddr = %q", got)
	}
	if got := truncate("abcdefgh", 3); got != "abc" {
		t.Errorf("truncate = %q, want abc", got)
	}
	if got := orderLabel(1, true); got != "Stop-Loss" {
		t.Errorf("orderLabel(1,true) = %q, want Stop-Loss", got)
	}
	if got := orderLabel(1, false); got != "Take-Profit" {
		t.Errorf("orderLabel(1,false) = %q, want Take-Profit", got)
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
