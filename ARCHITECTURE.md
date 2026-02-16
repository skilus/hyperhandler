# Архитектура hyperhandler

## Обзор

hyperhandler — CLI-сервис для автоматизации торговли на Hyperliquid DEX. Принимает торговые сигналы в формате JSON и транслирует их в исполняемые ордера через Hyperliquid API.

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Торговый       │────▶│  CLI-сервис      │────▶│  Hyperliquid    │
│  сигнал (JSON)  │     │  (валидация,     │     │  API            │
│                 │     │   подпись,       │     │                 │
└─────────────────┘     │   отправка)      │     └─────────────────┘
                        └──────────────────┘
                               │
                               ▼
                        ┌──────────────────┐
                        │  SQLite          │
                        │  (история)       │
                        └──────────────────┘
```

## Структура проекта

```
src/hyperhandler/
├── __init__.py              # Версия пакета
├── cli.py                   # Typer CLI, все команды
├── config.py                # Конфигурация (YAML + env)
├── signer.py                # Подпись запросов (EIP-712)
├── storage.py               # SQLite хранилище
├── utils.py                 # Утилиты (валидация ключей)
│
├── models/                  # Pydantic модели
│   ├── __init__.py
│   ├── signal.py            # TradingSignal, OrderSide, OrderType, SignalHorizon
│   ├── order.py             # OrderResult, Position, OpenOrder
│   ├── vault.py             # VaultInfo, VaultPosition, VaultDetails
│   ├── validator.py         # SignalValidator, ValidationConfig
│   └── risk.py              # RiskLevel, TradeOrder, TradeResult, CircuitBreakerStatus
│
├── risk/                    # Risk Management модуль
│   ├── __init__.py
│   ├── manager.py           # RiskManager — главный класс
│   ├── calculator.py        # ATR, position sizing, leverage selection
│   ├── circuit_breaker.py   # Circuit breaker (consecutive losses, daily limit)
│   ├── collector.py         # TradeResultCollector
│   └── config.py            # RiskProfile, HLConfig, ATR_SETTINGS
│
├── client/                  # API клиенты
│   ├── __init__.py
│   ├── base.py              # BaseClient (retry, error handling)
│   ├── info.py              # InfoClient (публичные данные)
│   ├── exchange.py          # ExchangeClient (торговые операции)
│   ├── vault.py             # VaultClient (vault операции)
│   └── order_builder.py     # Конвертация signal → API payload
│
└── wallet/                  # Управление ключами
    ├── __init__.py
    ├── manager.py           # WalletManager
    └── providers/           # Провайдеры ключей
        ├── __init__.py
        ├── base.py          # KeyProvider ABC
        ├── env.py           # EnvKeyProvider
        ├── hd.py            # HDWalletProvider (BIP-39)
        ├── keyring_provider.py  # KeyringProvider
        └── prompt.py        # PromptKeyProvider
```

## Компоненты

### 1. CLI (`cli.py`)

Typer-приложение с командами:

| Команда | Описание |
|---------|----------|
| `exec` | Исполнение торгового сигнала |
| `exec --risk-level` | Исполнение с автоматическим расчётом размера |
| `validate` | Валидация сигнала без исполнения |
| `positions` | Показать открытые позиции |
| `orders` | Показать открытые ордера |
| `status` | Статус аккаунта |
| `cancel` | Отмена ордеров |
| `faucet` | Запрос тестовых средств |
| `config *` | Управление конфигурацией |
| `wallet *` | HD wallet (seed phrase) |
| `vaults *` | Операции с vaults |
| `risk check` | Проверить сигнал через риск-менеджер |
| `risk status` | Статус риска и circuit breaker |
| `risk reset` | Сбросить circuit breaker |

### 2. Модели (`models/`)

#### TradingSignal

```python
class TradingSignal(BaseModel):
    pair: str              # Нормализуется к uppercase
    side: OrderSide        # long | short
    order_type: OrderType  # market | limit
    size: Decimal
    leverage: int = 5
    entry_price: Decimal | None
    stop_loss: Decimal | None
    take_profit: Decimal | None
    # Risk management fields (optional)
    confidence: float | None  # 0.0-1.0, affects position sizing
    horizon: SignalHorizon    # scalp | intraday | swing | position
    source: str | None        # Signal source identifier
```

Валидаторы:
- Нормализация пары (BTC-USD → BTC)
- entry_price обязателен для limit
- SL должен быть ниже entry для long, выше для short
- TP должен быть выше entry для long, ниже для short

#### SignalValidator

Проверяет сигнал против конфигурируемых лимитов:
- `max_position_size_usd`
- `max_leverage`
- `require_stop_loss`
- `allowed_pairs` (whitelist)
- `min_order_size`

### 3. API Клиенты (`client/`)

#### BaseClient

Базовый HTTP клиент с:
- Retry logic с exponential backoff
- Обработка ошибок API
- Rate limit handling

#### InfoClient

Публичные данные (не требуют подписи):
- `get_meta()` — метаданные рынков
- `get_all_mids()` — текущие цены
- `get_account_state()` — состояние аккаунта
- `get_open_orders()` — открытые ордера
- `get_positions()` — позиции
- `get_asset_index()` — индекс ассета (с кэшированием)

#### ExchangeClient

Торговые операции (требуют подписи):
- `place_order()` — разместить ордер
- `place_order_from_signal()` — ордер из сигнала (entry + SL + TP)
- `cancel_order()` — отменить ордер
- `set_leverage()` — установить плечо
- `close_position()` — закрыть позицию

#### VaultClient

Операции с vaults:
- `list_vaults()` — список публичных vaults
- `get_vault_details()` — детали vault
- `deposit_to_vault()` — депозит
- `withdraw_from_vault()` — вывод
- `get_my_vault_positions()` — мои позиции в vaults

#### OrderBuilder

Конвертация TradingSignal в API payload:
- Построение entry ордера (limit/market)
- Построение SL/TP trigger ордеров
- Расчёт slippage для market ордеров
- Группировка ордеров (`normalTpsl`)

### 4. Risk Management (`risk/`)

#### RiskManager

Главный класс для оценки риска сигнала:

```python
class RiskManager:
    def __init__(risk_level, risk_mode, hl_config)
    async def evaluate_signal(signal, info_client, address, trade_history) -> TradeOrder | RiskReject
    def evaluate_signal_with_data(...) -> TradeOrder | RiskReject  # Pure function for testing
```

**Risk Modes:**
- `MANUAL` — валидирует параметры сигнала, не меняет их
- `MANAGED` — автоматически рассчитывает size, leverage, stop-loss

#### RiskCalculator

Чистые функции для расчётов:
- `calculate_atr()` — ATR на основе EMA (чистый Python)
- `calculate_stop_loss()` — ATR-based стоп-лосс
- `estimate_liquidation_price()` — расчёт ликвидационной цены
- `select_leverage()` — выбор плеча с учётом stop distance
- `calculate_position_size()` — размер позиции из риск-бюджета
- `calculate_cumulative_risk()` — кумулятивный риск с корреляцией

#### CircuitBreaker

Отслеживание убытков и блокировка торговли:

```python
class CircuitBreaker:
    def check(trade_history, account_value) -> CircuitBreakerStatus
    def get_reject(status) -> RiskReject | None
```

**Триггеры:**
- `SOFT` — N consecutive losses → risk multiplier 0.5
- `HARD` — M consecutive losses или daily loss limit → блокировка

#### TradeResultCollector

Сбор результатов закрытых сделок:
- `collect_from_fills()` — reconcile из HL API
- `record_close()` — запись при закрытии через CLI

#### Risk Profiles

| Profile | Risk/Trade | Max Cumulative | Max Leverage | Soft Stop | Hard Stop |
|---------|------------|----------------|--------------|-----------|-----------|
| LOW | 1% | 4% | 5x | 2 losses | 4 losses |
| MEDIUM | 2% | 6% | 10x | 3 losses | 5 losses |
| HIGH | 3% | 10% | 20x | 3 losses | 6 losses |

### 5. Подпись (`signer.py`)

```python
class Signer:
    def __init__(private_key, is_mainnet=True)
    def sign_action(action, nonce, vault_address, expires_after) -> payload
    def sign_action_for_vault(action, vault_address, nonce) -> payload
```

Схема подписи (Hyperliquid L1 Action):

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. ACTION HASH                                                  │
│    msgpack(action) + nonce(8 bytes) + vault_flag + vault_addr   │
│    → keccak256                                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. PHANTOM AGENT                                                │
│    {                                                            │
│      "source": "a" (mainnet) | "b" (testnet),                  │
│      "connectionId": action_hash (bytes32)                      │
│    }                                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. EIP-712 TYPED DATA                                           │
│    domain: {name: "Exchange", version: "1", chainId: 1337}     │
│    types: Agent(source: string, connectionId: bytes32)          │
│    message: phantom_agent                                       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. SIGNATURE                                                    │
│    eth_account.sign_message(encode_typed_data(full_message))    │
│    → {r, s, v}                                                  │
└─────────────────────────────────────────────────────────────────┘
```

Защита от replay attacks:
- **nonce**: timestamp в миллисекундах
- **source**: "a" для mainnet, "b" для testnet — подписи не переносимы между сетями

### 6. Wallet (`wallet/`)

#### WalletManager

Цепочка провайдеров для получения приватного ключа:

```
Env → Keyring → Prompt
```

#### Провайдеры

| Провайдер | Источник |
|-----------|----------|
| EnvKeyProvider | `HL_{NETWORK}_PRIVATE_KEY`, `HL_PRIVATE_KEY` |
| KeyringProvider | Системный keychain |
| HDWalletProvider | BIP-39 seed phrase → BIP-44 derivation |
| PromptKeyProvider | Интерактивный ввод |

#### HD Wallet

```
Seed Phrase (12/24 words)
         │
         ▼ BIP-39
    Master Key
         │
         ▼ BIP-44: m/44'/60'/0'/0/{index}
    Derived Keys (0, 1, 2, ...)
```

Команды:
- `wallet generate` — создать новый seed phrase
- `wallet import` — импортировать существующий
- `wallet list` — показать derived адреса
- `wallet use --index N` — получить ключ для индекса N

### 7. Storage (`storage.py`)

SQLite база данных (`~/.hyperhandler/history.db`):

**Таблица signals:**
- id, created_at, network, pair, side, order_type
- size, leverage, entry_price, stop_loss, take_profit
- signal_json, validated, executed

**Таблица orders:**
- id, created_at, signal_id, network
- order_id, pair, side, order_type
- size, price, status, filled_size, avg_price
- error, vault_address

**Таблица trade_results:** (для circuit breaker)
- id, signal_id, network, coin, side
- entry_price, exit_price, size, pnl, fees, funding_paid
- opened_at, closed_at

**Таблица risk_decisions:** (audit log)
- id, created_at, network, signal_id
- risk_mode, decision, reject_reason
- coin, side, input_size, output_size
- risk_pct, estimated_liq, details_json

### 8. Config (`config.py`)

```python
class Config:
    # Загрузка из ~/.hyperhandler/config.yaml
    # Override из env vars (HL_*)

class Settings:
    network: str = "mainnet"
    trading: TradingSettings

class NetworkConfig:
    name: str
    api_url: str
    ws_url: str
```

## Потоки данных

### Исполнение сигнала (Manual Mode)

```
1. CLI: exec --signal signal.json
   │
2. ├── Загрузка JSON
   │
3. ├── Парсинг в TradingSignal
   │   └── Валидация полей (Pydantic)
   │
4. ├── SignalValidator.validate()
   │   └── Проверка лимитов
   │
5. ├── WalletManager.get_private_key()
   │   └── Env → Keyring → Prompt
   │
6. ├── RiskManager.evaluate_signal()  ← NEW
   │   ├── CircuitBreaker.check()
   │   ├── Validate risk limits
   │   └── Return TradeOrder | RiskReject
   │
7. ├── InfoClient.get_asset_index()
   │   └── (кэшируется)
   │
8. ├── InfoClient.get_mid_price() [для market]
   │
9. ├── ExchangeClient.set_leverage()
   │
10.├── OrderBuilder.build_order_payload()
   │   └── Entry + SL + TP
   │
11.├── Signer.sign_action()
   │
12.├── ExchangeClient._post("exchange", payload)
   │
13.├── Storage.save_signal(), save_order(), save_risk_decision()
   │
14.└── Вывод результата
```

### Исполнение сигнала (Managed Mode)

```
1. CLI: exec --signal signal.json --risk-level medium
   │
2. ├── Загрузка JSON
   │
3. ├── Парсинг в TradingSignal
   │
4. ├── SignalValidator.validate()
   │
5. ├── WalletManager.get_private_key()
   │
6. ├── RiskManager.evaluate_signal()  ← MANAGED MODE
   │   ├── CircuitBreaker.check()
   │   ├── InfoClient.get_candles() → ATR
   │   ├── RiskCalculator.calculate_stop_loss()
   │   ├── RiskCalculator.select_leverage()
   │   ├── RiskCalculator.calculate_position_size()
   │   ├── RiskCalculator.calculate_cumulative_risk()
   │   └── Return TradeOrder (with calculated values)
   │
7. ├── CLI: показать diff (Signal → Calculated)
   │
8. ├── ExchangeClient.set_leverage(calculated)
   │
9. ├── OrderBuilder.build_order_payload(TradeOrder)
   │
10.├── Signer.sign_action()
   │
11.├── ExchangeClient._post("exchange", payload)
   │
12.├── Storage.save_*()
   │
13.└── Вывод результата
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

### Формат подписанного запроса

```json
{
  "action": {...},
  "nonce": 1699999999999,
  "signature": {"r": "0x...", "s": "0x...", "v": 27}
}
```

## Тестирование

```
tests/
├── conftest.py              # Общие fixtures
├── unit/                    # Unit тесты (~240)
│   ├── test_models.py       # TradingSignal, OrderResult
│   ├── test_validator.py    # SignalValidator
│   ├── test_order_builder.py
│   ├── test_signer.py
│   ├── test_config.py
│   ├── test_storage.py
│   ├── test_wallet.py
│   ├── test_cli.py
│   ├── test_risk_calculator.py   # ATR, position sizing, leverage
│   ├── test_risk_manager.py      # Manual/Managed modes
│   └── test_circuit_breaker.py   # Consecutive losses, daily limit
└── integration/             # Integration тесты (~75, mocked HTTP)
    ├── conftest.py          # respx fixtures, info_request_router
    ├── test_info_client.py
    ├── test_exchange_client.py
    ├── test_vault_client.py
    ├── test_risk_manager.py     # Groups A, B (15 tests)
    ├── test_risk_storage.py     # Group C (6 tests)
    ├── test_risk_collector.py   # Group D (5 tests)
    ├── test_risk_cli.py         # Groups E, F (9 tests)
    ├── test_risk_precision.py   # Group G (5 tests)
    └── test_risk_e2e.py         # Group H (2 tests)
```

### Risk Integration Tests (SPEC-005)

42 теста в 8 группах:

| Группа | Описание | Тестов |
|--------|----------|--------|
| A | RiskManager MANUAL mode | 7 |
| B | RiskManager MANAGED mode | 8 |
| C | Storage integration | 6 |
| D | TradeResultCollector | 5 |
| E | CLI risk commands | 4 |
| F | exec with --risk-level | 5 |
| G | Precision & Rounding | 5 |
| H | E2E Risk Lifecycle | 2 |

Маркеры pytest:
- `unit` — быстрые тесты без внешних зависимостей
- `integration` — тесты с mocked HTTP
- `e2e` — тесты на реальном testnet
- `vault` — vault-related тесты

## Безопасность

1. **Хранение ключей**: приватные ключи в системном keychain или env vars, никогда в конфиге
2. **Валидация сигналов**: проверка лимитов перед исполнением
3. **Подпись**: EIP-712 typed data signing, nonce (timestamp) предотвращает replay attacks, разные source для mainnet/testnet
