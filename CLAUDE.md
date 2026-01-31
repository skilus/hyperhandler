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

## Documentation

- `README.md` — пользовательская документация, установка, использование
- `ARCHITECTURE.md` — техническая архитектура, компоненты, потоки данных

## Development Rules

### Planning Process (IMPORTANT)

**После этапа планирования:**

1. Сохранить план в формате OpenSpec как черновик в файл `specs/draft-<feature-name>.md`
2. План должен содержать:
   - Цель изменений
   - Список файлов для создания/изменения
   - Структуры данных и интерфейсы
   - Последовательность реализации
   - Критерии приёмки
3. **Не приступать к реализации до подтверждения пользователя**
4. После подтверждения — переименовать `draft-*.md` в `*.md`

### Documentation Updates (IMPORTANT)

**После любых изменений кода ОБЯЗАТЕЛЬНО обновлять документацию:**

1. **README.md** — при изменении:
   - CLI команд или их параметров
   - Формата сигнала
   - Конфигурации
   - Процесса установки

2. **ARCHITECTURE.md** — при изменении:
   - Структуры проекта (новые файлы/модули)
   - API клиентов
   - Моделей данных
   - Потоков данных
   - Схемы подписи

### Git Conventions

- Не добавлять Co-Authored-By в commit messages
- Атомарные коммиты с описательными сообщениями
- Обновлять документацию в том же коммите, что и код

### Testing

- Запускать `pytest tests/ -v` перед коммитом
- Добавлять тесты для нового функционала
- Unit тесты в `tests/unit/`
- Integration тесты в `tests/integration/`

### Code Style

- Type hints для всех функций
- Docstrings для публичных методов
- Pydantic для валидации данных
- async/await для HTTP операций
