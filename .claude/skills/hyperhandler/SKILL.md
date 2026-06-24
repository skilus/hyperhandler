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

Go-реализация (порт с Python, SPEC-007; паритет подтверждён golden-векторами).

```
cmd/hyperhandler/main.go   # entrypoint → cli.Execute
internal/
├── cli/                # cobra-команды (тонкий слой) + table/ANSI-рендер
├── service/            # оркестрация: ParseSignal, Executor.Exec, Cancel, Risk*
├── models/             # signal, order, vault, validator, risk
├── risk/               # manager, calculator, circuit_breaker, collector, config
├── client/             # base, info, exchange, vault, order_builder
├── wallet/             # manager + провайдеры env/keyring/hd/prompt
├── signer/             # EIP-712 подпись (msgpack action hash)
├── storage/            # SQLite (modernc.org/sqlite, без cgo)
├── config/             # YAML + HL_-env
├── decimalx/           # decimal-хелперы (DivisionPrecision=28)
└── golden/             # загрузчик golden-векторов
testdata/golden/        # эталонные векторы (оракул — официальный HL SDK)
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

### Базовые правила (NewTradingSignal)

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
  default_slippage: 0.005   # проскальзывание market-ордеров (exec)
  max_retries: 3            # ретраи HTTP-клиента (429/5xx/сеть)
  retry_delay: 1.0          # базовая задержка бэкоффа, сек

security:
  max_position_size_usd: 10000
  max_leverage: 20
  require_stop_loss: false
```

> Секция `trading` подключена к рантайму: `default_slippage` идёт в slippage
> market-ордеров (`exec`), `max_retries`/`retry_delay` — в retry-бэкофф всех
> HTTP-клиентов. Дефолты совпадают со встроенными (0.005 / 3 / 1.0s).

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
# Сборка статического бинаря в ./bin/
make build

# Тесты / покрытие / линтер
make test            # go test ./... -count=1
make cover           # go test ./... -cover
make lint            # golangci-lint (.golangci.yml)

# Кросс-компиляция релизных бинарей в ./dist/
make release
```

### Тесты

- Один `*_test.go` на пакет; HTTP мокается через `net/http/httptest`,
  время — через инжектируемый `clock`.
- Golden-векторы (`testdata/golden/`) — байт-в-байт сверка подписи EIP-712,
  msgpack-payload и HD-деривации против официального HL SDK.
- E2E на реальном testnet — отдельный ручной прогон
  (`exec`/`cancel`/`faucet`/`vaults`/`risk`).

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
2. **Новые команды** — добавлять в `internal/cli` (cobra) + оркестрацию в `internal/service`
3. **Новые модели** — добавлять в `internal/models`
4. **API клиенты** — встраивать `client.BaseClient` (retry logic)
5. **Тесты** — `*_test.go` рядом с пакетом; `make lint` должен быть чистым
6. **Версионирование** — `-ldflags "-X main.version=..."` (SemVer 2.0.0)
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
