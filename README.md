# hlhandler

Hyperliquid trading handler CLI.

## Installation

```bash
pip install -e ".[dev]"
```

## Configuration

### Private Keys

Private keys can be configured in three ways (checked in order):

1. **Environment variables** (highest priority):
   ```bash
   export HL_MAINNET_PRIVATE_KEY="0x..."
   # or
   export HL_PRIVATE_KEY="0x..."  # fallback for any network
   ```

2. **System keyring**:
   ```bash
   hlhandler config set-key --network mainnet
   ```

3. **Interactive prompt** (if running in a terminal)

### Configuration file

Create `~/.hlhandler/config.yaml`:

```yaml
network: mainnet

trading:
  default_slippage: 0.01
  max_retries: 3
  retry_delay: 1.0
```

## CLI Commands

```bash
# Show help
hlhandler --help

# Configuration commands
hlhandler config set-key --network mainnet    # Save key to keyring
hlhandler config remove-key --network mainnet # Remove key from keyring
hlhandler config show-address                 # Show wallet addresses
hlhandler config check                        # Check provider status
```

## Development

```bash
# Install with dev dependencies
pip install -e ".[dev]"

# Run tests
pytest tests/ -v
```
