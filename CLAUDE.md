# CLAUDE.md

## Project Overview

hyperhandler — CLI-сервис для автоматизации торговли на Hyperliquid DEX.

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
src/hyperhandler/
├── cli.py              # CLI commands
├── config.py           # Configuration
├── signer.py           # EIP-712 signing
├── storage.py          # SQLite storage
├── models/             # Pydantic models
│   ├── signal.py       # TradingSignal
│   ├── order.py        # OrderResult, Position
│   ├── vault.py        # VaultInfo
│   ├── validator.py    # SignalValidator
│   └── risk.py         # RiskLevel, TradeOrder, TradeResult
├── client/             # API clients
│   ├── base.py         # BaseClient with retry
│   ├── info.py         # Info API
│   ├── exchange.py     # Exchange API
│   ├── vault.py        # Vault API
│   └── order_builder.py
├── risk/               # Risk Management
│   ├── manager.py      # RiskManager
│   ├── calculator.py   # ATR, position sizing
│   ├── circuit_breaker.py
│   ├── collector.py    # TradeResultCollector
│   └── config.py       # RiskProfile
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
hyperhandler --help
hyperhandler config check
hyperhandler validate --signal signal.json
hyperhandler exec --signal signal.json --network testnet
```

## Configuration

- Config file: `~/.hyperhandler/config.yaml`
- Private key: `HL_PRIVATE_KEY` env var or system keyring

## Documentation

- `README.md` — пользовательская документация, установка, использование
- `ARCHITECTURE.md` — техническая архитектура, компоненты, потоки данных

## Development Rules

### Project-Specific OpenSpec

Specs are stored in `specs/` directory (not `docs/specs/`).

### Documentation Updates

After code changes, update:
1. **README.md** — CLI commands, signal format, configuration
2. **ARCHITECTURE.md** — project structure, data flows, components
3. **Claude Skill** (`.claude/skills/hyperhandler/SKILL.md`) — commands, examples

### Testing

- Run `pytest tests/ -v` before committing
- Unit tests in `tests/unit/`
- Integration tests in `tests/integration/`
