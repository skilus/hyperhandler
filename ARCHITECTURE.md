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
│   ├── signal.py            # TradingSignal, OrderSide, OrderType
│   ├── order.py             # OrderResult, Position, OpenOrder
│   ├── vault.py             # VaultInfo, VaultPosition, VaultDetails
│   └── validator.py         # SignalValidator, ValidationConfig
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
| `validate` | Валидация сигнала без исполнения |
| `positions` | Показать открытые позиции |
| `orders` | Показать открытые ордера |
| `status` | Статус аккаунта |
| `cancel` | Отмена ордеров |
| `faucet` | Запрос тестовых средств |
| `config *` | Управление конфигурацией |
| `wallet *` | HD wallet (seed phrase) |
| `vaults *` | Операции с vaults |

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

### 4. Подпись (`signer.py`)

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

### 5. Wallet (`wallet/`)

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

### 6. Storage (`storage.py`)

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

### 7. Config (`config.py`)

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

### Исполнение сигнала

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
6. ├── InfoClient.get_asset_index()
   │   └── (кэшируется)
   │
7. ├── InfoClient.get_mid_price() [для market]
   │
8. ├── ExchangeClient.set_leverage()
   │
9. ├── OrderBuilder.build_order_payload()
   │   └── Entry + SL + TP
   │
10.├── Signer.sign_action()
   │
11.├── ExchangeClient._post("exchange", payload)
   │
12.├── Storage.save_signal(), save_order()
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
├── unit/                    # Unit тесты
│   ├── test_models.py       # TradingSignal, OrderResult
│   ├── test_validator.py    # SignalValidator
│   ├── test_order_builder.py
│   ├── test_signer.py
│   ├── test_config.py
│   ├── test_storage.py
│   └── test_wallet.py
└── integration/             # Integration тесты (mocked HTTP)
    ├── conftest.py          # respx fixtures
    ├── test_info_client.py
    ├── test_exchange_client.py
    └── test_vault_client.py
```

Маркеры pytest:
- `unit` — быстрые тесты без внешних зависимостей
- `integration` — тесты с mocked HTTP
- `e2e` — тесты на реальном testnet
- `vault` — vault-related тесты

## Безопасность

1. **Хранение ключей**: приватные ключи в системном keychain или env vars, никогда в конфиге
2. **Валидация сигналов**: проверка лимитов перед исполнением
3. **Подпись**: EIP-712 typed data signing, nonce (timestamp) предотвращает replay attacks, разные source для mainnet/testnet
