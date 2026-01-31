# CLAUDE.md

## Project Overview

hlhandler — CLI-сервис для автоматизации торговли на Hyperliquid DEX.

## Tech Stack

- Python 3.11+
- Typer (CLI)
- httpx (async HTTP)
- Pydantic (validation)
- Rich (output)
- SQLite (storage)
- eth-account (signing)

## Project Structure

```
src/hlhandler/
├── cli.py              # CLI commands
├── config.py           # Configuration
├── signer.py           # EIP-191 signing
├── storage.py          # SQLite storage
├── models/             # Pydantic models
│   ├── signal.py       # TradingSignal
│   ├── order.py        # OrderResult, Position
│   ├── vault.py        # VaultInfo
│   └── validator.py    # SignalValidator
├── client/             # API clients
│   ├── base.py         # BaseClient with retry
│   ├── info.py         # Info API
│   ├── exchange.py     # Exchange API
│   ├── vault.py        # Vault API
│   └── order_builder.py
└── wallet/             # Key management
    ├── manager.py
    └── providers/      # Env, Keyring, Prompt
```

## Commands

```bash
# Install
pip install -e ".[dev]"

# Run tests
pytest tests/ -v

# CLI
hlhandler --help
hlhandler config check
hlhandler validate --signal signal.json
hlhandler exec --signal signal.json --network testnet
```

## Configuration

- Config file: `~/.hlhandler/config.yaml`
- Private key: `HL_PRIVATE_KEY` env var or system keyring

## Git Conventions

- Do not add Co-Authored-By to commit messages
- Keep commits atomic and descriptive
