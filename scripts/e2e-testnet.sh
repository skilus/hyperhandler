#!/usr/bin/env bash
# E2E smoke test against the Hyperliquid TESTNET (SPEC-007 Phase 7).
#
# Drives the Go binary end-to-end against the real testnet API. Read-only and
# free steps always run; order-placing steps are gated behind RUN_LIVE=1 so a
# plain run never moves funds.
#
# Prereqs:
#   - A funded testnet wallet key in HL_TESTNET_PRIVATE_KEY (the faucet step
#     can fund a fresh address; allow a minute before placing orders).
#
# Usage:
#   HL_TESTNET_PRIVATE_KEY=0x... ./scripts/e2e-testnet.sh          # safe (read-only + dry-run)
#   HL_TESTNET_PRIVATE_KEY=0x... RUN_LIVE=1 ./scripts/e2e-testnet.sh  # also places + cancels a real order
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -z "${HL_TESTNET_PRIVATE_KEY:-}" ]]; then
  echo "error: set HL_TESTNET_PRIVATE_KEY to a testnet key" >&2
  exit 1
fi

BIN=bin/hyperhandler
NET=testnet

step() { printf '\n\033[1m=== %s ===\033[0m\n' "$1"; }

step "build"
make build

step "config check + address"
"$BIN" config check
"$BIN" config show-address

step "faucet (free testnet funds)"
"$BIN" faucet --network "$NET" || echo "(faucet may rate-limit; continuing)"

step "account status / positions / open orders (read-only)"
"$BIN" status --network "$NET"
"$BIN" positions --network "$NET"
"$BIN" orders --network "$NET"

step "risk status + risk check (read-only)"
"$BIN" risk status --network "$NET" --risk-level medium

SIGNAL="$(mktemp -t e2e-signal.XXXXXX.json)"
trap 'rm -f "$SIGNAL"' EXIT
cat > "$SIGNAL" <<'JSON'
{
  "pair": "ETH",
  "side": "long",
  "order_type": "limit",
  "entry_price": 1000.0,
  "size": 0.01,
  "leverage": 3,
  "stop_loss": 950.0,
  "take_profit": 1200.0
}
JSON

"$BIN" risk check --signal "$SIGNAL" --risk-level medium --network "$NET"

step "validate + exec --dry-run (no order placed)"
"$BIN" validate --signal "$SIGNAL"
"$BIN" exec --signal "$SIGNAL" --network "$NET" --dry-run

step "vaults list (read-only)"
"$BIN" vaults list --network "$NET" || echo "(no vaults / API hiccup; continuing)"

if [[ "${RUN_LIVE:-}" == "1" ]]; then
  step "LIVE: place a far-from-market limit order, then cancel it"
  # entry_price is intentionally far below market so it rests (not filled),
  # letting us exercise place → orders → cancel without a real fill.
  "$BIN" exec --signal "$SIGNAL" --network "$NET"
  "$BIN" orders --network "$NET"
  "$BIN" cancel --pair ETH --network "$NET"
  "$BIN" orders --network "$NET"
else
  echo
  echo "skipping live order placement (set RUN_LIVE=1 to exercise exec/cancel)"
fi

step "done"
