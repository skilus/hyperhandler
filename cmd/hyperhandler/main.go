// Command hyperhandler is the CLI entrypoint for the Hyperliquid trading service.
//
// This is the Go port of the Python hyperhandler (SPEC-007). The cobra command
// tree lives in internal/cli; this file only delegates to it.
package main

import (
	"os"

	"github.com/skilus/hyperhandler/internal/cli"
)

// version is the target Go binary version (SPEC-007). Overridden at build time
// via -ldflags "-X main.version=...".
var version = "0.4.0-dev"

func main() {
	if err := cli.Execute(version); err != nil {
		os.Exit(1)
	}
}
