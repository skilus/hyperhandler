// Command hyperhandler is the CLI entrypoint for the Hyperliquid trading service.
//
// This is the Go port of the Python hyperhandler (SPEC-007). The cobra command
// tree is wired up in internal/cli; this file only delegates to it.
package main

import (
	"fmt"
	"os"
)

// version is the target Go binary version (SPEC-007). Overridden at build time
// via -ldflags "-X main.version=...".
var version = "0.4.0-dev"

func main() {
	// The cobra root command lands in internal/cli during Phase 6.
	// For now the binary reports its version so the scaffold is runnable.
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("hyperhandler %s\n", version)
		return
	}
	fmt.Fprintln(os.Stderr, "hyperhandler: CLI not yet wired (SPEC-007 in progress); try --version")
	os.Exit(1)
}
