# CLAUDE.md

## Project Overview

hyperhandler — CLI-сервис для автоматизации торговли на Hyperliquid DEX.

## Tech Stack

Go-реализация (порт с Python, SPEC-007). Паритет поведения подтверждён golden-векторами.

- Go 1.25+
- cobra (CLI)
- net/http (синхронный клиент с retry)
- shopspring/decimal (точные вычисления, DivisionPrecision=28)
- modernc.org/sqlite (pure-Go SQLite, статический бинарь без cgo)
- go-ethereum/crypto + tyler-smith/go-bip39 (EIP-712 подпись, HD-кошелёк)

> Python-исходники остаются в `src/` как референс на время миграции; рабочая
> реализация — Go.

## Project Structure

```
cmd/hyperhandler/main.go   # entrypoint (version ldflag → cli.Execute)
internal/
├── cli/                # cobra-команды (тонкий слой) + table/ANSI-рендер
├── service/            # оркестрация: ParseSignal, Executor.Exec, Cancel, Risk*
├── models/             # signal, order, vault, validator, risk
├── risk/               # manager, calculator, circuit_breaker, collector, config
├── client/             # base, info, exchange, vault, order_builder
├── wallet/             # manager + провайдеры env/keyring/hd/prompt
├── signer/             # EIP-712 подпись (msgpack action hash)
├── storage/            # SQLite (modernc.org/sqlite)
├── config/             # YAML + HL_-env
├── decimalx/           # decimal-хелперы
└── golden/             # загрузчик golden-векторов
testdata/golden/        # эталонные векторы (оракул — официальный HL SDK)
```

## Commands

```bash
# Build (статический бинарь в ./bin/)
make build

# Run tests / coverage / lint
make test
make cover
make lint            # golangci-lint (.golangci.yml)

# Cross-compile релизных бинарей в ./dist/
make release

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

- Run `make test` (`go test ./... -count=1`) before committing
- One `*_test.go` per package; HTTP mocked via `net/http/httptest`
- Golden-векторы (`testdata/golden/`) — байт-в-байт сверка подписи/payload/HD
- `make lint` должен быть чистым (golangci-lint)
