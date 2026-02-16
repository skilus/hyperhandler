---
name: hyperhandler
description: Work with hyperhandler - CLI service for Hyperliquid DEX trading automation. Use when creating trading signals, validating orders, working with vaults, or modifying hyperhandler code.
argument-hint: [command] [options]
---

# hyperhandler Development Skill

Этот skill помогает работать с проектом hyperhandler — CLI-сервисом для автоматизации торговли на Hyperliquid DEX.

## Основные возможности

- ✅ Исполнение торговых сигналов (market/limit ордера)
- ✅ Автоматическая установка Stop-Loss и Take-Profit
- ✅ **Risk Management** — автоматический расчёт размера позиции (ATR-based)
- ✅ **Circuit Breaker** — блокировка торговли при серии убытков
- ✅ Vault-трейдинг (копитрейдинг)
- ✅ HD Wallet с BIP-39/BIP-44 деривацией
- ✅ Мониторинг позиций и ордеров
- ✅ Безопасное хранение ключей (keyring + env vars)
- ✅ SQLite история всех операций
- ✅ EIP-712 подпись с защитой от replay attacks
- ✅ Mainnet и Testnet поддержка

## Документация проекта

- Обзор архитектуры: [ARCHITECTURE.md](../../../ARCHITECTURE.md)
- Руководство пользователя: [README.md](../../../README.md)
- Правила разработки: [CLAUDE.md](../../../CLAUDE.md)

## Структура проекта

```
src/hyperhandler/
├── cli.py              # Typer CLI commands
├── config.py           # YAML + env configuration
├── signer.py           # EIP-712 request signing
├── storage.py          # SQLite history storage
├── models/             # Pydantic models
│   ├── signal.py       # TradingSignal, SignalHorizon
│   ├── order.py        # OrderResult, Position
│   ├── vault.py        # VaultInfo
│   ├── validator.py    # SignalValidator
│   └── risk.py         # RiskLevel, TradeOrder, TradeResult
├── client/             # API clients
│   ├── base.py         # BaseClient with retry
│   ├── info.py         # Public data + candles, funding
│   ├── exchange.py     # Trading operations
│   ├── vault.py        # Vault operations
│   └── order_builder.py
├── risk/               # Risk Management module
│   ├── manager.py      # RiskManager (main entry)
│   ├── calculator.py   # ATR, position sizing, leverage
│   ├── circuit_breaker.py  # Consecutive losses, daily limit
│   ├── collector.py    # TradeResultCollector
│   └── config.py       # RiskProfile, HLConfig
└── wallet/             # Key management
    ├── manager.py      # WalletManager
    └── providers/      # Env, Keyring, Prompt, HD
```

## Команды

### Торговля

```bash
# Исполнить сигнал (manual mode — параметры из сигнала)
hyperhandler exec --signal signal.json [--network mainnet|testnet] [--vault 0x...]

# Исполнить с риск-менеджментом (managed mode — автоматический расчёт)
hyperhandler exec --signal signal.json --risk-level medium

# Валидация без исполнения
hyperhandler validate --signal signal.json
```

### Риск-менеджмент

```bash
# Проверить сигнал без исполнения (показывает все расчёты)
hyperhandler risk check --signal signal.json --risk-level medium

# Статус риска (позиции, circuit breaker, daily PnL)
hyperhandler risk status

# Сбросить circuit breaker вручную
hyperhandler risk reset --yes
```

**Risk Levels:**

| Уровень | Risk/Trade | Max Leverage | Max Positions | Circuit Breaker |
|---------|------------|--------------|---------------|-----------------|
| `low` | 1% | 5x | 3 | 4 losses |
| `medium` | 2% | 10x | 5 | 5 losses |
| `high` | 3% | 20x | 8 | 6 losses |

**Risk Modes:**

- `MANUAL` — валидирует параметры сигнала, не меняет их (default)
- `MANAGED` — автоматически рассчитывает size, leverage, stop-loss (--risk-level)

### Мониторинг

```bash
hyperhandler status      # Статус аккаунта
hyperhandler positions   # Открытые позиции
hyperhandler orders      # Открытые ордера
```

### Управление ордерами

```bash
hyperhandler cancel --order-id 123456  # Отменить по ID
hyperhandler cancel --pair BTC         # Отменить все для пары
hyperhandler cancel --all              # Отменить все
```

### Vault операции

```bash
hyperhandler vaults list --min-tvl 100000
hyperhandler vaults info 0x...
hyperhandler vaults deposit --vault 0x... --amount 1000
hyperhandler vaults withdraw --vault 0x... --shares 0.5
hyperhandler vaults my-positions
```

### HD Wallet (Seed Phrase)

```bash
hyperhandler wallet generate [--words 12|24] [--save]
hyperhandler wallet import --network testnet
hyperhandler wallet list [--count 5]
hyperhandler wallet use --index 0
hyperhandler wallet delete
```

### Конфигурация

```bash
hyperhandler config set-key --network mainnet
hyperhandler config remove-key --network mainnet
hyperhandler config show-address
hyperhandler config check
```

### Testnet

```bash
# Запросить тестовые средства
hyperhandler faucet --network testnet
```

## Формат торгового сигнала

### Базовый формат (Manual Mode)

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

### Расширенный формат (Managed Mode)

```json
{
  "pair": "BTC",
  "side": "long",
  "order_type": "market",
  "size": 0.1,
  "confidence": 0.8,
  "horizon": "intraday",
  "source": "signal-provider-1"
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|--------------|----------|
| `pair` | string | Да | Символ ассета (BTC, ETH, SOL) |
| `side` | enum | Да | `long` или `short` |
| `order_type` | enum | Да | `market` или `limit` |
| `entry_price` | decimal | Для limit | Цена входа |
| `size` | decimal | Да* | Размер позиции (*игнорируется в managed mode) |
| `leverage` | int | Нет | Плечо (по умолчанию 5) |
| `stop_loss` | decimal | Нет | Цена стоп-лосса |
| `take_profit` | decimal | Нет | Цена тейк-профита |
| `confidence` | float | Нет | 0.0-1.0, влияет на размер в managed mode |
| `horizon` | enum | Нет | `scalp`, `intraday`, `swing`, `position` |
| `source` | string | Нет | ID источника сигнала |

## Правила валидации сигнала

### Базовые правила (Pydantic)

1. `entry_price` обязателен для `limit` ордеров
2. Stop-loss должен быть ниже entry для long, выше для short
3. Take-profit должен быть выше entry для long, ниже для short
4. Пара нормализуется к uppercase (BTC-USD → BTC)

### Конфигурационные лимиты (SignalValidator)

Проверяются из `~/.hyperhandler/config.yaml`:

```yaml
security:
  max_position_size_usd: 10000    # Максимальный размер позиции в USD
  max_leverage: 20                # Максимальное плечо
  require_stop_loss: false        # Обязательность SL
  allowed_pairs: []               # Whitelist пар (пусто = все)
  min_order_size: null            # Минимальный размер ордера
```

## Конфигурация и хранение

### Файл конфигурации

`~/.hyperhandler/config.yaml`:

```yaml
network: mainnet

trading:
  default_slippage: 0.01
  max_retries: 3
  retry_delay: 1.0

security:
  max_position_size_usd: 10000
  max_leverage: 20
  require_stop_loss: false
```

### База данных

`~/.hyperhandler/history.db` (SQLite):

- **signals** — все принятые сигналы (JSON, validated, executed)
- **orders** — все отправленные ордера (order_id, status, filled_size, error)
- **trade_results** — результаты закрытых сделок (для circuit breaker)
- **risk_decisions** — audit log риск-решений (input/output, reject reason)

### Переменные окружения

```bash
HL_NETWORK=mainnet              # Сеть по умолчанию
HL_PRIVATE_KEY=0x...            # Ключ для любой сети
HL_MAINNET_PRIVATE_KEY=0x...    # Ключ для mainnet
HL_TESTNET_PRIVATE_KEY=0x...    # Ключ для testnet
```

## Безопасность

1. **Приватные ключи**: хранятся в системном keyring или env vars, НИКОГДА в config.yaml
2. **Валидация сигналов**: проверка лимитов перед исполнением (SignalValidator)
3. **EIP-712 подпись**: typed data signing с nonce (timestamp)
4. **Защита от replay**: разные `source` для mainnet ("a") и testnet ("b")
5. **SQLite история**: все операции логируются для аудита

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

# E2E тесты (реальный testnet)
pytest tests/ -v -m e2e
```

### Маркеры pytest

- `unit` — быстрые тесты без внешних зависимостей
- `integration` — тесты с mocked HTTP (respx)
- `e2e` — тесты на реальном testnet
- `vault` — vault-related тесты

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

При работе с hyperhandler:

1. **Изменения кода** — обновлять README.md, ARCHITECTURE.md и этот SKILL.md
2. **Новые команды** — добавлять в cli.py и документацию
3. **Новые модели** — добавлять в models/ с Pydantic валидацией
4. **API клиенты** — наследовать от BaseClient (retry logic)
5. **Тесты** — писать для каждого нового функционала
6. **Версионирование** — обновлять версию в pyproject.toml и __init__.py (SemVer 2.0.0)
7. **Коммиты** — без Co-Authored-By, с conventional commits (feat:, fix:, refactor:)

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

hyperhandler validate --signal /tmp/signal.json
```

### Проверить статус testnet

```bash
export HL_TESTNET_PRIVATE_KEY="0x..."
hyperhandler status --network testnet
hyperhandler positions --network testnet
```
