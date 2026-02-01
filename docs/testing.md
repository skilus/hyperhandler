# Testing Guide

## Unit Tests

```bash
# Run all tests
pytest tests/ -v

# Run specific test file
pytest tests/unit/test_order_builder.py -v

# Run with coverage
pytest tests/ --cov=hyperhandler --cov-report=term-missing
```

## E2E Testing on Testnet

### Prerequisites

1. Install hyperhandler in dev mode:
   ```bash
   pip install -e ".[dev]"
   ```

2. Get testnet funds:
   - Deposit $5+ USDC to mainnet to activate account
   - Claim mock USDC from https://app.hyperliquid-testnet.xyz/drip

### Quick E2E Test

```bash
# 1. Generate wallet
hyperhandler wallet generate --words 12 --save --network testnet

# 2. Export key (use index 0)
hyperhandler wallet use --index 0 --network testnet
export HL_TESTNET_PRIVATE_KEY="0x..."

# 3. Check balance
hyperhandler status --network testnet

# 4. Create and validate signal
cat > /tmp/signal.json << 'EOF'
{
  "pair": "BTC",
  "side": "long",
  "order_type": "market",
  "size": 0.001,
  "leverage": 5
}
EOF

hyperhandler validate --signal /tmp/signal.json

# 5. Execute order
hyperhandler exec --signal /tmp/signal.json --network testnet

# 6. Check position
hyperhandler positions --network testnet

# 7. Close position (opposite direction)
cat > /tmp/close.json << 'EOF'
{
  "pair": "BTC",
  "side": "short",
  "order_type": "market",
  "size": 0.001,
  "leverage": 5
}
EOF

hyperhandler exec --signal /tmp/close.json --network testnet
```

### Full Test Checklist

#### Wallet Management
- [ ] `wallet generate` creates valid seed phrase
- [ ] `wallet generate --save` stores in keyring
- [ ] `wallet list` shows derived addresses
- [ ] `wallet use` exports private key
- [ ] `wallet import` imports existing seed
- [ ] `wallet delete` removes from keyring

#### Configuration
- [ ] `config check` shows provider status
- [ ] `config set-key` saves key to keyring
- [ ] `config show-address` displays addresses
- [ ] `config remove-key` removes key

#### Trading
- [ ] `status` shows balance and margin
- [ ] `validate` accepts valid signals
- [ ] `validate` rejects invalid signals (wrong SL/TP levels)
- [ ] `exec --dry-run` validates without executing
- [ ] `exec` limit order with SL/TP
- [ ] `exec` market order
- [ ] `positions` shows open positions
- [ ] `orders` shows open orders
- [ ] `cancel --order-id` cancels specific order
- [ ] `cancel --pair` cancels by pair
- [ ] `cancel --all` cancels all orders

#### Vaults
- [ ] `vaults list` shows public vaults
- [ ] `vaults my-positions` shows user positions

## Signal Examples

### Limit Order with SL/TP
```json
{
  "pair": "ETH",
  "side": "long",
  "order_type": "limit",
  "entry_price": 2000.0,
  "size": 0.01,
  "leverage": 3,
  "stop_loss": 1900.0,
  "take_profit": 2200.0
}
```

### Market Order
```json
{
  "pair": "BTC",
  "side": "short",
  "order_type": "market",
  "size": 0.001,
  "leverage": 10
}
```

## Known Issues & Solutions

### "User or API Wallet does not exist"
**Cause:** Account not activated on Hyperliquid.
**Solution:** Deposit $5+ USDC to mainnet first, then claim testnet funds.

### "Order has invalid price"
**Cause:** Price not rounded to asset's tick size.
**Solution:** Fixed in v0.2.1. Prices are now rounded based on `szDecimals`.

### "Price too far from oracle"
**Cause:** Testnet oracle prices can diverge from market.
**Solution:** Use different asset (BTC usually works) or wait for oracle sync.

### Faucet rate limit
**Cause:** Faucet has cooldown between requests.
**Solution:** Use testnet drip page instead: https://app.hyperliquid-testnet.xyz/drip

## Testnet Limitations

- Oracle prices may differ significantly from mainnet
- Some assets have low liquidity
- Faucet has rate limits
- Vault features may have limited data
