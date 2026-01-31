---
name: hlhandler
description: Work with hlhandler - CLI service for Hyperliquid DEX trading automation. Use when creating trading signals, validating orders, working with vaults, or modifying hlhandler code.
argument-hint: [command] [options]
---

# hlhandler Development Skill

Этот skill помогает работать с проектом hlhandler — CLI-сервисом для автоматизации торговли на Hyperliquid DEX.

## Документация проекта

- Обзор архитектуры: [ARCHITECTURE.md](../../../ARCHITECTURE.md)
- Руководство пользователя: [README.md](../../../README.md)
- Правила разработки: [CLAUDE.md](../../../CLAUDE.md)

## Структура проекта

```
src/hlhandler/
├── cli.py              # Typer CLI commands
├── config.py           # YAML + env configuration
├── signer.py           # EIP-712 request signing
├── storage.py          # SQLite history storage
├── models/             # Pydantic models
│   ├── signal.py       # TradingSignal
│   ├── order.py        # OrderResult, Position
│   ├── vault.py        # VaultInfo
│   └── validator.py    # SignalValidator
├── client/             # API clients
│   ├── base.py         # BaseClient with retry
│   ├── info.py         # Public data (no signature)
│   ├── exchange.py     # Trading operations
│   ├── vault.py        # Vault operations
│   └── order_builder.py
└── wallet/             # Key management
    ├── manager.py      # WalletManager
    └── providers/      # Env, Keyring, Prompt
```

## Команды

### Торговля

```bash
# Исполнить сигнал
hlhandler exec --signal signal.json [--network mainnet|testnet] [--vault 0x...]

# Валидация без исполнения
hlhandler validate --signal signal.json
```

### Мониторинг

```bash
hlhandler status      # Статус аккаунта
hlhandler positions   # Открытые позиции
hlhandler orders      # Открытые ордера
```

### Управление ордерами

```bash
hlhandler cancel --order-id 123456  # Отменить по ID
hlhandler cancel --pair BTC         # Отменить все для пары
hlhandler cancel --all              # Отменить все
```

### Vault операции

```bash
hlhandler vaults list --min-tvl 100000
hlhandler vaults info 0x...
hlhandler vaults deposit --vault 0x... --amount 1000
hlhandler vaults withdraw --vault 0x... --shares 0.5
hlhandler vaults my-positions
```

### Конфигурация

```bash
hlhandler config set-key --network mainnet
hlhandler config remove-key --network mainnet
hlhandler config show-address
hlhandler config check
```

## Формат торгового сигнала

```json
{
  "pair": "BTC",
  "side": "long",
  "order_type": "limit",
  "entry_price": 67500.0,
  "size": 0.1,
  "leverage": 5,
  "stop_loss": 66000.0,
  "take_profit": 70000.0
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|--------------|----------|
| `pair` | string | Да | Символ ассета (BTC, ETH, SOL) |
| `side` | enum | Да | `long` или `short` |
| `order_type` | enum | Да | `market` или `limit` |
| `entry_price` | decimal | Для limit | Цена входа |
| `size` | decimal | Да | Размер позиции |
| `leverage` | int | Нет | Плечо (по умолчанию 5) |
| `stop_loss` | decimal | Нет | Цена стоп-лосса |
| `take_profit` | decimal | Нет | Цена тейк-профита |

## Правила валидации сигнала

1. `entry_price` обязателен для `limit` ордеров
2. Stop-loss должен быть ниже entry для long, выше для short
3. Take-profit должен быть выше entry для long, ниже для short
4. Пара нормализуется к uppercase (BTC-USD → BTC)

## Разработка

```bash
# Установка с dev-зависимостями
pip install -e ".[dev]"

# Запуск тестов
pytest tests/ -v

# Только unit тесты
pytest tests/unit/ -v

# Только integration тесты
pytest tests/integration/ -v
```

## Hyperliquid API

### Эндпоинты

| Тип | Mainnet | Testnet |
|-----|---------|---------|
| Info | `https://api.hyperliquid.xyz/info` | `https://api.hyperliquid-testnet.xyz/info` |
| Exchange | `https://api.hyperliquid.xyz/exchange` | `https://api.hyperliquid-testnet.xyz/exchange` |

### Формат ордера

```json
{
  "type": "order",
  "orders": [
    {
      "a": 0,           // asset index
      "b": true,        // is buy
      "p": "67500.0",   // price
      "s": "0.1",       // size
      "r": false,       // reduce only
      "t": {"limit": {"tif": "Gtc"}}
    }
  ],
  "grouping": "na"
}
```

## Задачи для Claude

При работе с hlhandler:

1. **Изменения кода** — обновлять README.md и ARCHITECTURE.md
2. **Новые команды** — добавлять в cli.py и документацию
3. **Новые модели** — добавлять в models/ с Pydantic валидацией
4. **API клиенты** — наследовать от BaseClient
5. **Тесты** — писать для каждого нового функционала

## Примеры использования

### Создать тестовый сигнал

```bash
cat > /tmp/signal.json << 'EOF'
{
  "pair": "ETH",
  "side": "long",
  "order_type": "market",
  "size": 0.5,
  "leverage": 3,
  "stop_loss": 3200.0,
  "take_profit": 3800.0
}
EOF

hlhandler validate --signal /tmp/signal.json
```

### Проверить статус testnet

```bash
export HL_TESTNET_PRIVATE_KEY="0x..."
hlhandler status --network testnet
hlhandler positions --network testnet
```
