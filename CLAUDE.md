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

3. **Claude Skill** (`.claude/skills/hyperhandler/SKILL.md`) — при изменении:
   - CLI команд или их параметров
   - Структуры проекта
   - Формата сигнала
   - API эндпоинтов
   - Примеров использования

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

### Versioning (Semantic Versioning 2.0.0)

**После реализации нового функционала обновлять версию в `pyproject.toml` и `src/hyperhandler/__init__.py`:**

Формат: `MAJOR.MINOR.PATCH`

| Изменение | Действие | Пример |
|-----------|----------|--------|
| Breaking changes (несовместимые изменения API) | MAJOR++ | 0.1.0 → 1.0.0 |
| Новый функционал (обратно совместимый) | MINOR++ | 0.1.0 → 0.2.0 |
| Исправления багов | PATCH++ | 0.1.0 → 0.1.1 |

**Pre-release версии:**
- Alpha: `0.1.0-alpha.1`
- Beta: `0.1.0-beta.1`
- RC: `0.1.0-rc.1`

**Правила:**
- При MAJOR=0 — API нестабильный, breaking changes допустимы в MINOR
- Сбрасывать PATCH при увеличении MINOR
- Сбрасывать MINOR и PATCH при увеличении MAJOR

Ссылка: https://semver.org/lang/ru/
