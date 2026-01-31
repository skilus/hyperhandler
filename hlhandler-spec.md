# Спецификация CLI-сервиса hlhandler

## 1. Обзор проекта

### 1.1 Назначение

CLI-сервис для автоматизации торговли на Hyperliquid DEX. Принимает торговые сигналы и транслирует их в исполняемые ордера через Hyperliquid API.

### 1.2 Ключевые возможности

- Исполнение торговых сигналов (market/limit ордера)
- Автоматическая установка Stop-Loss и Take-Profit
- Поддержка Vault-трейдинга (копитрейдинг)
- Работа с mainnet и testnet
- Мониторинг позиций и ордеров

### 1.3 Технологический стек

| Компонент | Технология |
|-----------|------------|
| Язык | Python 3.11+ |
| CLI-фреймворк | Typer |
| HTTP-клиент | httpx (async) |
| Валидация | Pydantic |
| Вывод | Rich |
| Хранилище | SQLite |
| SDK | hyperliquid-python-sdk |

---

## 2. Архитектура

### 2.1 Общая схема

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
                        │  Локальное       │
                        │  хранилище       │
                        │  (история, логи) │
                        └──────────────────┘
```

### 2.2 Компоненты

| Компонент | Ответственность |
|-----------|-----------------|
| CLI Parser | Парсинг аргументов и stdin |
| Signal Validator | Валидация сигнала, проверка лимитов |
| Wallet Manager | Управление ключами, подпись транзакций |
| Order Builder | Формирование ордеров (основной + SL/TP) |
| Hyperliquid Client | Взаимодействие с API |
| Position Tracker | Отслеживание открытых позиций |
| Vault Manager | Работа с vaults (копитрейдинг) |

### 2.3 Структура проекта

```
hlhandler/
├── src/
│   └── hlhandler/
│       ├── __init__.py
│       ├── cli.py              # Typer CLI
│       ├── config.py           # Конфигурация
│       ├── models/
│       │   ├── __init__.py
│       │   ├── signal.py       # TradingSignal
│       │   ├── order.py        # Order, OrderResult
│       │   └── vault.py        # VaultInfo, VaultPosition
│       ├── client/
│       │   ├── __init__.py
│       │   ├── base.py         # HyperliquidClient
│       │   ├── info.py         # Info API
│       │   ├── exchange.py     # Exchange API
│       │   └── vault.py        # Vault API
│       ├── signer.py           # EIP-712 подпись
│       ├── storage.py          # SQLite история
│       └── utils.py
├── tests/
├── config/
│   └── config.example.yaml
├── pyproject.toml
└── README.md
```

---

## 3. Формат торгового сигнала

### 3.1 JSON-схема

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

### 3.2 Поля сигнала

| Поле | Тип | Обязательное | Описание |
|------|-----|--------------|----------|
| `pair` | string | Да | Символ ассета (BTC, ETH, SOL) |
| `side` | enum | Да | Направление: `long` или `short` |
| `order_type` | enum | Да | Тип: `market` или `limit` |
| `entry_price` | decimal | Для limit | Цена входа |
| `size` | decimal | Да | Размер позиции |
| `leverage` | int | Нет | Плечо (по умолчанию из конфига) |
| `stop_loss` | decimal | Нет | Цена стоп-лосса |
| `take_profit` | decimal | Нет | Цена тейк-профита |

### 3.3 Pydantic модель

```python
from decimal import Decimal
from enum import Enum
from pydantic import BaseModel, field_validator

class OrderSide(str, Enum):
    LONG = "long"
    SHORT = "short"

class OrderType(str, Enum):
    MARKET = "market"
    LIMIT = "limit"

class TradingSignal(BaseModel):
    pair: str
    side: OrderSide
    order_type: OrderType
    size: Decimal
    leverage: int = 5
    entry_price: Decimal | None = None
    stop_loss: Decimal | None = None
    take_profit: Decimal | None = None

    @field_validator('pair')
    @classmethod
    def normalize_pair(cls, v: str) -> str:
        return v.upper().replace('-USD', '').replace('-PERP', '')

    @field_validator('entry_price')
    @classmethod
    def require_entry_for_limit(cls, v, info):
        if info.data.get('order_type') == OrderType.LIMIT and v is None:
            raise ValueError('entry_price required for limit orders')
        return v
```

---

## 4. Hyperliquid API

### 4.1 Эндпоинты

| Тип | Mainnet | Testnet |
|-----|---------|---------|
| Info API | `https://api.hyperliquid.xyz/info` | `https://api.hyperliquid-testnet.xyz/info` |
| Exchange API | `https://api.hyperliquid.xyz/exchange` | `https://api.hyperliquid-testnet.xyz/exchange` |
| WebSocket | `wss://api.hyperliquid.xyz/ws` | `wss://api.hyperliquid-testnet.xyz/ws` |

Все запросы — POST с JSON body.

### 4.2 Info API (публичные данные)

#### Метаданные рынков

```python
# Request
{"type": "meta"}

# Response
{
    "universe": [
        {
            "name": "BTC",
            "szDecimals": 5,
            "maxLeverage": 50,
            "onlyIsolated": false
        }
    ]
}
```

#### Состояние аккаунта

```python
# Request
{
    "type": "clearinghouseState",
    "user": "0x..."
}

# Response
{
    "marginSummary": {
        "accountValue": "10000.0",
        "totalMarginUsed": "500.0",
        "totalNtlPos": "2500.0",
        "totalRawUsd": "10000.0"
    },
    "assetPositions": [
        {
            "position": {
                "coin": "BTC",
                "szi": "0.1",
                "entryPx": "67500.0",
                "positionValue": "6750.0",
                "unrealizedPnl": "125.0",
                "leverage": {"type": "cross", "value": 5}
            }
        }
    ]
}
```

#### Открытые ордера

```python
# Request
{"type": "openOrders", "user": "0x..."}

# Response
[
    {
        "coin": "BTC",
        "oid": 123456,
        "side": "B",
        "limitPx": "67000.0",
        "sz": "0.1",
        "timestamp": 1699999999999
    }
]
```

#### Текущие цены

```python
# Request
{"type": "allMids"}

# Response
{"BTC": "67550.5", "ETH": "3450.25"}
```

### 4.3 Exchange API (торговые операции)

Все операции требуют EIP-712 подписи.

#### Общая структура запроса

```python
{
    "action": {...},
    "nonce": 1699999999999,
    "signature": {"r": "0x...", "s": "0x...", "v": 27},
    "vaultAddress": null  # опционально для vault trading
}
```

#### Лимитный ордер

```python
{
    "type": "order",
    "orders": [
        {
            "a": 0,              # asset index
            "b": true,           # buy
            "p": "67500.0",      # price
            "s": "0.1",          # size
            "r": false,          # reduce only
            "t": {"limit": {"tif": "Gtc"}}
        }
    ],
    "grouping": "na"
}
```

**Time-in-Force:**
- `Gtc` — Good-til-canceled
- `Ioc` — Immediate-or-cancel (market)
- `Alo` — Add-liquidity-only (post-only)

#### Market ордер

```python
{
    "type": "order",
    "orders": [
        {
            "a": 0,
            "b": true,
            "p": "68000.0",      # slippage price
            "s": "0.1",
            "r": false,
            "t": {"limit": {"tif": "Ioc"}}
        }
    ],
    "grouping": "na"
}
```

#### Stop-Loss / Take-Profit

```python
{
    "type": "order",
    "orders": [
        {
            "a": 0,
            "b": false,
            "p": "65000.0",
            "s": "0.1",
            "r": true,
            "t": {
                "trigger": {
                    "triggerPx": "66000.0",
                    "isMarket": true,
                    "tpsl": "sl"  # или "tp"
                }
            }
        }
    ],
    "grouping": "na"
}
```

#### Связанные ордера (Entry + SL + TP)

```python
{
    "type": "order",
    "orders": [
        # Entry
        {"a": 0, "b": true, "p": "67500.0", "s": "0.1", "r": false,
         "t": {"limit": {"tif": "Gtc"}}},
        # Stop-Loss
        {"a": 0, "b": false, "p": "65500.0", "s": "0.1", "r": true,
         "t": {"trigger": {"triggerPx": "66000.0", "isMarket": true, "tpsl": "sl"}}},
        # Take-Profit
        {"a": 0, "b": false, "p": "69500.0", "s": "0.1", "r": true,
         "t": {"trigger": {"triggerPx": "70000.0", "isMarket": true, "tpsl": "tp"}}}
    ],
    "grouping": "normalTpsl"
}
```

#### Отмена ордера

```python
{
    "type": "cancel",
    "cancels": [{"a": 0, "o": 123456}]
}
```

#### Установка плеча

```python
{
    "type": "updateLeverage",
    "asset": 0,
    "isCross": true,
    "leverage": 10
}
```

### 4.4 EIP-712 подпись

```python
DOMAIN = {
    "name": "HyperliquidSignTransaction",
    "version": "1",
    "chainId": 1337,
    "verifyingContract": "0x0000000000000000000000000000000000000000"
}
```

---

## 5. Vault API (копитрейдинг)

### 5.1 Концепция

Vault — смарт-контрактный аккаунт для копитрейдинга:
- Пользователи депозитят средства
- Лидер (owner) торгует от имени vault
- Прибыль распределяется пропорционально долям

```
┌─────────────────────────────────────────────┐
│                    VAULT                    │
│                                             │
│  User A (30%) + User B (50%) + User C (20%) │
│                    │                        │
│                    ▼                        │
│              Pool: $100k                    │
│                    │                        │
│                    ▼                        │
│             Vault Leader                    │
│         (торгует от имени vault)            │
└─────────────────────────────────────────────┘
```

### 5.2 API методы

#### Список vaults

```python
# Request
{"type": "vaults"}

# Response
[
    {
        "vault": "0x...",
        "name": "Alpha Trader Vault",
        "leader": "0x...",
        "tvl": "1500000.0",
        "apr": "45.2",
        "followers": 342,
        "maxCapacity": "5000000.0",
        "isPublic": true
    }
]
```

#### Детали vault

```python
# Request
{"type": "vaultDetails", "vault": "0x..."}

# Response
{
    "vault": "0x...",
    "name": "Alpha Trader Vault",
    "leader": "0x...",
    "portfolio": {
        "accountValue": "1500000.0",
        "positions": [...]
    },
    "followerState": {
        "shares": "0.025",
        "depositedAmount": "35000.0",
        "currentValue": "37500.0"
    },
    "lockupPeriod": 86400,
    "profitShare": "10.0"
}
```

#### Депозит в vault

```python
{
    "type": "vaultDeposit",
    "vault": "0x...",
    "usd": "10000.0"
}
```

#### Вывод из vault

```python
{
    "type": "vaultWithdraw",
    "vault": "0x...",
    "shares": "0.5"
}
```

#### Торговля от имени vault

```python
{
    "action": {"type": "order", "orders": [...], "grouping": "na"},
    "nonce": 1699999999999,
    "signature": {...},
    "vaultAddress": "0x..."  # ключевое поле
}
```

#### Создание vault

```python
{
    "type": "createVault",
    "name": "My Trading Vault",
    "description": "Algorithmic strategy",
    "isPublic": true,
    "maxCapacity": "1000000.0",
    "lockupPeriod": 86400,
    "profitShare": "10.0"
}
```

### 5.3 Модель распределения прибыли

```
Прибыль vault: $10,000
Доля лидера (profit share 10%): $1,000

Оставшаяся прибыль $9,000 распределяется по shares:
- User A (30%): $2,700
- User B (50%): $4,500
- User C (20%): $1,800
```

---

## 6. CLI интерфейс

### 6.1 Основные команды

```bash
# Исполнение сигнала
hlhandler exec --signal signal.json
hlhandler exec --signal signal.json --vault 0x...  # от имени vault

# Исполнение из stdin
echo '{"pair":"ETH",...}' | hlhandler exec

# Валидация без исполнения
hlhandler validate --signal signal.json

# Позиции
hlhandler positions
hlhandler positions --vault 0x...

# Ордера
hlhandler orders
hlhandler cancel --order-id 123456
hlhandler cancel --all

# Статус аккаунта
hlhandler status
```

### 6.2 Vault команды

```bash
# Список публичных vaults
hlhandler vaults list --min-tvl 100000 --min-apr 20

# Детали vault
hlhandler vaults info 0x...

# Депозит/вывод
hlhandler vaults deposit --vault 0x... --amount 5000
hlhandler vaults withdraw --vault 0x... --shares 0.5

# Мои позиции в vaults
hlhandler vaults my-positions

# Создать vault (для лидера)
hlhandler vaults create \
  --name "My Strategy" \
  --profit-share 10 \
  --lockup 24h \
  --public
```

### 6.3 Сетевые команды

```bash
# Указание сети
hlhandler --network testnet exec --signal signal.json
hlhandler --network mainnet positions

# Переменная окружения
export HL_NETWORK=testnet

# Faucet (только testnet)
hlhandler --network testnet faucet

# Проверка подключения
hlhandler --network testnet status
```

---

## 7. Конфигурация

### 7.1 Файл конфигурации

Расположение: `~/.hlhandler/config.yaml`

```yaml
# Дефолтная сеть
default_network: testnet

networks:
  testnet:
    api_url: https://api.hyperliquid-testnet.xyz
    ws_url: wss://api.hyperliquid-testnet.xyz/ws
    wallet: 0x...

  mainnet:
    api_url: https://api.hyperliquid.xyz
    ws_url: wss://api.hyperliquid.xyz/ws
    wallet: 0x...

# Торговые настройки
settings:
  default_leverage: 5
  max_slippage_percent: 0.5
  log_level: info

# Безопасность
security:
  max_position_size_usd: 10000
  max_leverage: 20
  require_stop_loss: true
```

### 7.2 Переменные окружения

| Переменная | Описание |
|------------|----------|
| `HL_NETWORK` | Сеть по умолчанию |
| `HL_PRIVATE_KEY` | Приватный ключ |
| `HL_CONFIG_PATH` | Путь к конфигу |

---

## 8. Интерфейсы (абстракции)

### 8.1 HyperliquidClient

```python
from abc import ABC, abstractmethod
from decimal import Decimal

@dataclass
class OrderResult:
    success: bool
    order_id: int | None
    filled_size: Decimal
    avg_price: Decimal | None
    error: str | None

class HyperliquidClient(ABC):

    @abstractmethod
    async def get_account_state(self, address: str) -> dict:
        """Состояние аккаунта"""
        pass

    @abstractmethod
    async def get_asset_index(self, symbol: str) -> int:
        """Индекс ассета по символу"""
        pass

    @abstractmethod
    async def get_mid_price(self, symbol: str) -> Decimal:
        """Текущая средняя цена"""
        pass

    @abstractmethod
    async def set_leverage(
        self,
        asset: int,
        leverage: int,
        cross: bool = True
    ) -> bool:
        """Установить плечо"""
        pass

    @abstractmethod
    async def place_order(
        self,
        asset: int,
        is_buy: bool,
        size: Decimal,
        price: Decimal,
        order_type: OrderType,
        reduce_only: bool = False
    ) -> OrderResult:
        """Разместить ордер"""
        pass

    @abstractmethod
    async def place_order_with_tpsl(
        self,
        asset: int,
        is_buy: bool,
        size: Decimal,
        entry_price: Decimal,
        order_type: OrderType,
        stop_loss: Decimal | None,
        take_profit: Decimal | None
    ) -> list[OrderResult]:
        """Ордер со SL/TP"""
        pass

    @abstractmethod
    async def cancel_order(self, asset: int, order_id: int) -> bool:
        """Отменить ордер"""
        pass

    @abstractmethod
    async def get_open_orders(self, address: str) -> list[dict]:
        """Открытые ордера"""
        pass

    @abstractmethod
    async def get_positions(self, address: str) -> list[dict]:
        """Открытые позиции"""
        pass
```

### 8.2 HyperliquidVaultClient

```python
@dataclass
class VaultInfo:
    address: str
    name: str
    leader: str
    tvl: Decimal
    apr: Decimal
    profit_share: Decimal
    lockup_period: int
    is_public: bool

@dataclass
class VaultPosition:
    vault: str
    shares: Decimal
    deposited: Decimal
    current_value: Decimal
    pnl: Decimal
    pnl_percent: Decimal

class HyperliquidVaultClient(ABC):

    @abstractmethod
    async def list_vaults(
        self,
        min_tvl: Decimal | None = None,
        min_apr: Decimal | None = None
    ) -> list[VaultInfo]:
        """Список публичных vaults"""
        pass

    @abstractmethod
    async def get_vault_details(self, vault_address: str) -> dict:
        """Детали vault"""
        pass

    @abstractmethod
    async def deposit_to_vault(
        self,
        vault_address: str,
        amount_usd: Decimal
    ) -> bool:
        """Депозит в vault"""
        pass

    @abstractmethod
    async def withdraw_from_vault(
        self,
        vault_address: str,
        shares: Decimal
    ) -> bool:
        """Вывод из vault"""
        pass

    @abstractmethod
    async def get_my_vault_positions(
        self,
        user_address: str
    ) -> list[VaultPosition]:
        """Мои позиции во всех vaults"""
        pass

    @abstractmethod
    async def create_vault(
        self,
        name: str,
        description: str,
        is_public: bool,
        max_capacity: Decimal,
        lockup_period: int,
        profit_share: Decimal
    ) -> str:
        """Создать vault"""
        pass

    @abstractmethod
    async def trade_as_vault(
        self,
        vault_address: str,
        signal: TradingSignal
    ) -> list[OrderResult]:
        """Торговать от имени vault"""
        pass
```

---

## 9. Testnet

### 9.1 Различия сетей

| Параметр | Mainnet | Testnet |
|----------|---------|---------|
| API URL | api.hyperliquid.xyz | api.hyperliquid-testnet.xyz |
| UI | app.hyperliquid.xyz | app.hyperliquid-testnet.xyz |
| Chain ID | 1337 | 1337 |
| Средства | Реальные | Faucet |
| Ликвидность | Высокая | Ограниченная |

### 9.2 Faucet API

```python
{"type": "faucet", "user": "0x..."}
```

### 9.3 Workflow разработки

```
1. Разработка      →  testnet + unit tests
2. Интеграционные  →  testnet с реальными ордерами
3. Staging         →  testnet, полный флоу
4. Production      →  mainnet с малыми суммами
5. Full production →  mainnet
```

---

## 10. Обработка ошибок

### 10.1 Коды ошибок API

| Код | Описание | Действие |
|-----|----------|----------|
| `INVALID_SIGNATURE` | Неверная подпись | Проверить ключ |
| `INSUFFICIENT_MARGIN` | Недостаточно маржи | Уменьшить размер |
| `INVALID_PRICE` | Цена вне допустимого | Пересчитать цену |
| `ORDER_NOT_FOUND` | Ордер не найден | Обновить список |
| `RATE_LIMITED` | Превышен лимит | Подождать |

### 10.2 Rate Limits

- REST API: 1200 requests/min
- WebSocket: без явных лимитов

---

## 11. Безопасность

### 11.1 Хранение ключей

- Приватный ключ в переменной окружения или keyring
- Никогда не хранить в конфиге или коде
- Опционально: аппаратный кошелёк через WalletConnect

### 11.2 Валидация сигналов

- Проверка максимального размера позиции
- Проверка максимального плеча
- Обязательный stop-loss (опционально)
- Whitelist разрешённых пар

---

## 12. Дальнейшее развитие

### 12.1 Возможные расширения

- WebSocket для real-time обновлений
- Telegram/Discord интеграция для сигналов
- Web UI для мониторинга
- Backtesting модуль
- Multi-account поддержка

### 12.2 Интеграции

- TradingView webhooks
- Telegram боты
- Custom signal providers

---

## 13. Тестирование

### 13.1 Стратегия тестирования

| Уровень | Инструменты | Покрытие |
|---------|-------------|----------|
| Unit | pytest, pytest-asyncio | Модели, валидация, утилиты |
| Integration | pytest, respx (mock HTTP) | Клиенты API, подпись |
| E2E | pytest, testnet | Полный флоу на реальном API |

### 13.2 Структура тестов

```
tests/
├── conftest.py                 # Общие fixtures
├── unit/
│   ├── test_models.py
│   ├── test_signal_validator.py
│   ├── test_order_builder.py
│   ├── test_signer.py
│   └── test_config.py
├── integration/
│   ├── conftest.py             # Mock fixtures
│   ├── test_info_client.py
│   ├── test_exchange_client.py
│   └── test_vault_client.py
└── e2e/
    ├── conftest.py             # Testnet fixtures
    ├── test_order_flow.py
    ├── test_position_management.py
    └── test_vault_operations.py
```

---

## 14. Unit тесты

### 14.1 Модели (test_models.py)

#### TradingSignal

| ID | Тесткейс | Входные данные | Ожидаемый результат |
|----|----------|----------------|---------------------|
| U-SIG-01 | Валидный long limit сигнал | `{"pair": "BTC", "side": "long", "order_type": "limit", "entry_price": 67500, "size": 0.1}` | Успешная валидация |
| U-SIG-02 | Валидный short market сигнал | `{"pair": "ETH", "side": "short", "order_type": "market", "size": 1.0}` | Успешная валидация |
| U-SIG-03 | Нормализация пары BTC-USD | `{"pair": "BTC-USD", ...}` | `signal.pair == "BTC"` |
| U-SIG-04 | Нормализация пары в lowercase | `{"pair": "btc", ...}` | `signal.pair == "BTC"` |
| U-SIG-05 | Limit без entry_price | `{"pair": "BTC", "order_type": "limit", "size": 0.1}` | ValidationError |
| U-SIG-06 | Отрицательный size | `{"size": -0.1, ...}` | ValidationError |
| U-SIG-07 | Нулевой size | `{"size": 0, ...}` | ValidationError |
| U-SIG-08 | Некорректный side | `{"side": "buy", ...}` | ValidationError |
| U-SIG-09 | Leverage по умолчанию | `{...}` без leverage | `signal.leverage == 5` |
| U-SIG-10 | SL выше entry для long | `{"side": "long", "entry_price": 100, "stop_loss": 110}` | ValidationError |
| U-SIG-11 | SL ниже entry для short | `{"side": "short", "entry_price": 100, "stop_loss": 90}` | ValidationError |
| U-SIG-12 | TP ниже entry для long | `{"side": "long", "entry_price": 100, "take_profit": 90}` | ValidationError |
| U-SIG-13 | TP выше entry для short | `{"side": "short", "entry_price": 100, "take_profit": 110}` | ValidationError |
| U-SIG-14 | Leverage превышает максимум | `{"leverage": 100, ...}` | ValidationError |
| U-SIG-15 | Полный сигнал со всеми полями | Все поля заполнены корректно | Успешная валидация |

#### OrderResult

| ID | Тесткейс | Входные данные | Ожидаемый результат |
|----|----------|----------------|---------------------|
| U-ORD-01 | Успешный результат | `success=True, order_id=123` | Валидный объект |
| U-ORD-02 | Неуспешный с ошибкой | `success=False, error="..."` | Валидный объект |
| U-ORD-03 | Частичное исполнение | `filled_size < requested_size` | Валидный объект |

#### VaultInfo / VaultPosition

| ID | Тесткейс | Входные данные | Ожидаемый результат |
|----|----------|----------------|---------------------|
| U-VLT-01 | Валидный VaultInfo | Все поля корректны | Успешная валидация |
| U-VLT-02 | Отрицательный TVL | `tvl=-1000` | ValidationError |
| U-VLT-03 | profit_share > 100 | `profit_share=150` | ValidationError |
| U-VLT-04 | Расчёт PnL процента | `deposited=1000, current=1100` | `pnl_percent == 10.0` |

### 14.2 Валидатор сигналов (test_signal_validator.py)

| ID | Тесткейс | Условия | Ожидаемый результат |
|----|----------|---------|---------------------|
| U-VAL-01 | Размер позиции в пределах лимита | `size_usd < max_position_size` | Валидация пройдена |
| U-VAL-02 | Размер позиции превышает лимит | `size_usd > max_position_size` | ValidationError |
| U-VAL-03 | Leverage в пределах лимита | `leverage <= max_leverage` | Валидация пройдена |
| U-VAL-04 | Leverage превышает лимит | `leverage > max_leverage` | ValidationError |
| U-VAL-05 | SL обязателен (настройка вкл) | `require_stop_loss=True`, SL отсутствует | ValidationError |
| U-VAL-06 | SL опционален (настройка выкл) | `require_stop_loss=False`, SL отсутствует | Валидация пройдена |
| U-VAL-07 | Пара в whitelist | `pair in allowed_pairs` | Валидация пройдена |
| U-VAL-08 | Пара не в whitelist | `pair not in allowed_pairs` | ValidationError |
| U-VAL-09 | Пустой whitelist (все разрешены) | `allowed_pairs=[]` | Валидация пройдена |
| U-VAL-10 | Минимальный размер ордера | `size < min_order_size` | ValidationError |

### 14.3 Order Builder (test_order_builder.py)

| ID | Тесткейс | Входные данные | Ожидаемый результат |
|----|----------|----------------|---------------------|
| U-BLD-01 | Limit long order | `side=long, type=limit, price=67500` | `b=True, tif=Gtc` |
| U-BLD-02 | Limit short order | `side=short, type=limit, price=67500` | `b=False, tif=Gtc` |
| U-BLD-03 | Market long order | `side=long, type=market` | `b=True, tif=Ioc` |
| U-BLD-04 | Market short order | `side=short, type=market` | `b=False, tif=Ioc` |
| U-BLD-05 | SL для long позиции | `side=long, sl=66000` | `b=False, tpsl=sl, triggerPx=66000` |
| U-BLD-06 | SL для short позиции | `side=short, sl=68000` | `b=True, tpsl=sl, triggerPx=68000` |
| U-BLD-07 | TP для long позиции | `side=long, tp=70000` | `b=False, tpsl=tp, triggerPx=70000` |
| U-BLD-08 | TP для short позиции | `side=short, tp=65000` | `b=True, tpsl=tp, triggerPx=65000` |
| U-BLD-09 | Связанные ордера (entry+SL+TP) | Полный сигнал | `grouping=normalTpsl`, 3 ордера |
| U-BLD-10 | Только entry (без SL/TP) | Без SL и TP | `grouping=na`, 1 ордер |
| U-BLD-11 | Расчёт slippage price (long) | `price=100, slippage=0.5%` | `p=100.5` |
| U-BLD-12 | Расчёт slippage price (short) | `price=100, slippage=0.5%` | `p=99.5` |
| U-BLD-13 | Asset index маппинг | `pair="BTC"` | `a=0` |
| U-BLD-14 | Reduce only флаг для SL/TP | SL или TP ордер | `r=True` |

### 14.4 Signer (test_signer.py)

| ID | Тесткейс | Входные данные | Ожидаемый результат |
|----|----------|----------------|---------------------|
| U-SGN-01 | Валидная подпись ордера | Корректный payload | Подпись с r, s, v |
| U-SGN-02 | Детерминированность подписи | Один и тот же payload | Идентичные подписи |
| U-SGN-03 | Разные payload — разные подписи | Разные payloads | Разные подписи |
| U-SGN-04 | Невалидный приватный ключ | `key="invalid"` | SignerError |
| U-SGN-05 | Корректный nonce (timestamp) | — | `nonce` близок к текущему времени |
| U-SGN-06 | EIP-712 domain корректен | — | `chainId=1337` |

### 14.5 Config (test_config.py)

| ID | Тесткейс | Условия | Ожидаемый результат |
|----|----------|---------|---------------------|
| U-CFG-01 | Загрузка валидного конфига | Корректный YAML | Config объект |
| U-CFG-02 | Дефолтные значения | Минимальный конфиг | Дефолты применены |
| U-CFG-03 | Переопределение из env | `HL_NETWORK=mainnet` | `config.network == mainnet` |
| U-CFG-04 | Отсутствующий файл | Файл не существует | ConfigError или дефолты |
| U-CFG-05 | Невалидный YAML | Синтаксическая ошибка | ConfigError |
| U-CFG-06 | Выбор сети testnet | `network=testnet` | Корректные URL testnet |
| U-CFG-07 | Выбор сети mainnet | `network=mainnet` | Корректные URL mainnet |

---

## 15. Integration тесты

### 15.1 Info Client (test_info_client.py)

Используем `respx` для мока HTTP запросов.

| ID | Тесткейс | Mock Response | Ожидаемый результат |
|----|----------|---------------|---------------------|
| I-INF-01 | get_meta успех | `{"universe": [...]}` | Список ассетов |
| I-INF-02 | get_meta пустой ответ | `{"universe": []}` | Пустой список |
| I-INF-03 | get_account_state успех | Валидный JSON | AccountState объект |
| I-INF-04 | get_account_state несуществующий адрес | `{"error": "..."}` | Пустое состояние или ошибка |
| I-INF-05 | get_all_mids успех | `{"BTC": "67500", ...}` | Dict[str, Decimal] |
| I-INF-06 | get_open_orders пустой | `[]` | Пустой список |
| I-INF-07 | get_open_orders с ордерами | `[{...}, {...}]` | Список ордеров |
| I-INF-08 | get_asset_index существующий | meta с BTC | `0` |
| I-INF-09 | get_asset_index несуществующий | meta без XYZ | AssetNotFoundError |
| I-INF-10 | HTTP timeout | Timeout | TimeoutError, retry |
| I-INF-11 | HTTP 500 | Server error | APIError, retry |
| I-INF-12 | HTTP 429 rate limit | Rate limited | RateLimitError, backoff |
| I-INF-13 | Невалидный JSON ответ | `"not json"` | ParseError |

### 15.2 Exchange Client (test_exchange_client.py)

| ID | Тесткейс | Mock Response | Ожидаемый результат |
|----|----------|---------------|---------------------|
| I-EXC-01 | place_order успех | `{"status": "ok", "response": {...}}` | OrderResult(success=True) |
| I-EXC-02 | place_order insufficient margin | `{"status": "err", "response": "..."}` | OrderResult(success=False, error) |
| I-EXC-03 | place_order invalid signature | `{"status": "err", ...}` | SignatureError |
| I-EXC-04 | place_order_with_tpsl успех | 3 ордера созданы | List[OrderResult] len=3 |
| I-EXC-05 | cancel_order успех | `{"status": "ok"}` | True |
| I-EXC-06 | cancel_order not found | `{"status": "err", ...}` | False или OrderNotFoundError |
| I-EXC-07 | set_leverage успех | `{"status": "ok"}` | True |
| I-EXC-08 | set_leverage invalid value | `{"status": "err", ...}` | False или ValidationError |
| I-EXC-09 | Подпись включена в запрос | — | Request содержит signature |
| I-EXC-10 | Nonce уникален | Два запроса | Разные nonce |
| I-EXC-11 | vaultAddress передаётся | `vault="0x..."` | Request содержит vaultAddress |

### 15.3 Vault Client (test_vault_client.py)

| ID | Тесткейс | Mock Response | Ожидаемый результат |
|----|----------|---------------|---------------------|
| I-VLT-01 | list_vaults успех | `[{...}, {...}]` | List[VaultInfo] |
| I-VLT-02 | list_vaults с фильтром min_tvl | — | Отфильтрованный список |
| I-VLT-03 | get_vault_details успех | Полный JSON | VaultDetails объект |
| I-VLT-04 | get_vault_details not found | `{"error": ...}` | VaultNotFoundError |
| I-VLT-05 | deposit_to_vault успех | `{"status": "ok"}` | True |
| I-VLT-06 | deposit_to_vault insufficient balance | `{"status": "err", ...}` | InsufficientBalanceError |
| I-VLT-07 | withdraw_from_vault успех | `{"status": "ok"}` | True |
| I-VLT-08 | withdraw_from_vault lockup period | `{"status": "err", ...}` | LockupPeriodError |
| I-VLT-09 | create_vault успех | `{"vault": "0x..."}` | Адрес vault |
| I-VLT-10 | trade_as_vault успех | — | List[OrderResult] |

### 15.4 CLI Integration (test_cli_integration.py)

| ID | Тесткейс | Команда | Ожидаемый результат |
|----|----------|---------|---------------------|
| I-CLI-01 | exec с файлом сигнала | `hlhandler exec --signal f.json` | Вызов place_order |
| I-CLI-02 | exec из stdin | `echo {...} \| hlhandler exec` | Вызов place_order |
| I-CLI-03 | validate успех | `hlhandler validate --signal f.json` | Exit code 0 |
| I-CLI-04 | validate ошибка | Невалидный сигнал | Exit code 1, сообщение |
| I-CLI-05 | positions вывод | `hlhandler positions` | Таблица позиций |
| I-CLI-06 | orders вывод | `hlhandler orders` | Таблица ордеров |
| I-CLI-07 | --network флаг | `--network testnet` | Использован testnet URL |
| I-CLI-08 | --vault флаг | `--vault 0x...` | vaultAddress в запросе |
| I-CLI-09 | Несуществующий файл сигнала | `--signal nofile.json` | Exit code 1, ошибка |
| I-CLI-10 | Невалидный JSON в файле | Битый JSON | Exit code 1, parse error |

---

## 16. E2E тесты (Testnet)

**Предусловия:**
- Аккаунт на testnet с балансом (faucet)
- Переменные окружения: `HL_TESTNET_PRIVATE_KEY`, `HL_TESTNET_ADDRESS`

### 16.1 Order Flow (test_order_flow.py)

| ID | Тесткейс | Шаги | Ожидаемый результат |
|----|----------|------|---------------------|
| E-ORD-01 | Limit long order полный цикл | 1. Создать limit long<br>2. Проверить в open orders<br>3. Отменить<br>4. Проверить отмену | Ордер создан, виден, отменён |
| E-ORD-02 | Limit short order полный цикл | Аналогично E-ORD-01 для short | Ордер создан, виден, отменён |
| E-ORD-03 | Market long order исполнение | 1. Создать market long<br>2. Проверить позицию<br>3. Закрыть позицию | Позиция открыта и закрыта |
| E-ORD-04 | Market short order исполнение | Аналогично E-ORD-03 для short | Позиция открыта и закрыта |
| E-ORD-05 | Order с SL | 1. Создать order с SL<br>2. Проверить trigger order | Entry + SL ордера созданы |
| E-ORD-06 | Order с TP | 1. Создать order с TP<br>2. Проверить trigger order | Entry + TP ордера созданы |
| E-ORD-07 | Order с SL и TP | 1. Создать order с SL+TP<br>2. Проверить все ордера | Entry + SL + TP созданы |
| E-ORD-08 | Отмена несуществующего ордера | Отменить order_id=999999999 | Ошибка или False |
| E-ORD-09 | Дублирование ордера | Создать два одинаковых ордера | Оба созданы |
| E-ORD-10 | Leverage изменение | 1. Установить leverage=10<br>2. Проверить | Leverage применён |

### 16.2 Position Management (test_position_management.py)

| ID | Тесткейс | Шаги | Ожидаемый результат |
|----|----------|------|---------------------|
| E-POS-01 | Открытие позиции | Market order → проверка позиции | Позиция в списке |
| E-POS-02 | Увеличение позиции | Открыть → добавить → проверить size | Size увеличен |
| E-POS-03 | Частичное закрытие | Открыть 0.1 → закрыть 0.05 | Size = 0.05 |
| E-POS-04 | Полное закрытие | Открыть → закрыть полностью | Позиция отсутствует |
| E-POS-05 | Переворот позиции | Long → Short (большим размером) | Short позиция |
| E-POS-06 | PnL расчёт | Открыть → подождать → проверить unrealizedPnl | PnL присутствует |
| E-POS-07 | Несколько позиций | Открыть BTC + ETH | Обе позиции в списке |
| E-POS-08 | Cross margin | Установить cross → открыть | leverage.type == "cross" |
| E-POS-09 | Isolated margin | Установить isolated → открыть | leverage.type == "isolated" |

### 16.3 Vault Operations (test_vault_operations.py)

| ID | Тесткейс | Шаги | Ожидаемый результат |
|----|----------|------|---------------------|
| E-VLT-01 | Список публичных vaults | Запросить список | Непустой список VaultInfo |
| E-VLT-02 | Детали vault | Получить детали существующего | VaultDetails с позициями |
| E-VLT-03 | Депозит в vault | 1. Баланс до<br>2. Депозит<br>3. Проверить shares | Shares > 0 |
| E-VLT-04 | Вывод из vault | 1. Депозит<br>2. Подождать lockup<br>3. Withdraw | Баланс восстановлен |
| E-VLT-05 | Торговля от имени vault (лидер) | 1. Создать vault<br>2. Депозит<br>3. Trade as vault | Позиция в vault |
| E-VLT-06 | Просмотр позиций vault | Запросить позиции vault | Список позиций |
| E-VLT-07 | Мои позиции во vaults | Депозит → my_vault_positions | VaultPosition в списке |

### 16.4 Error Scenarios (test_error_scenarios.py)

| ID | Тесткейс | Условия | Ожидаемый результат |
|----|----------|---------|---------------------|
| E-ERR-01 | Insufficient margin | Ордер больше баланса | Понятная ошибка |
| E-ERR-02 | Invalid price (слишком далеко) | Цена ±50% от рынка | Ошибка или отклонение |
| E-ERR-03 | Size ниже минимума | `size=0.00001` | Ошибка min size |
| E-ERR-04 | Неподдерживаемый ассет | `pair="INVALID"` | AssetNotFoundError |
| E-ERR-05 | Rate limit handling | 100 запросов подряд | Корректный backoff |
| E-ERR-06 | Network timeout | — | Retry с exponential backoff |
| E-ERR-07 | Невалидная подпись | Испорченный ключ | SignatureError |

### 16.5 CLI E2E (test_cli_e2e.py)

| ID | Тесткейс | Команда | Ожидаемый результат |
|----|----------|---------|---------------------|
| E-CLI-01 | Полный цикл через CLI | `hlhandler --network testnet exec ...` | Ордер создан |
| E-CLI-02 | Faucet запрос | `hlhandler --network testnet faucet` | Баланс увеличен |
| E-CLI-03 | Status проверка | `hlhandler --network testnet status` | Информация об аккаунте |
| E-CLI-04 | Positions после сделки | exec → positions | Позиция отображается |
| E-CLI-05 | Cancel через CLI | exec → cancel --order-id | Ордер отменён |
| E-CLI-06 | Vault list | `hlhandler vaults list` | Таблица vaults |

---

## 17. Fixtures и утилиты

### 17.1 Общие fixtures (conftest.py)

```python
import pytest
from decimal import Decimal

@pytest.fixture
def valid_long_signal():
    return {
        "pair": "BTC",
        "side": "long",
        "order_type": "limit",
        "entry_price": Decimal("67500"),
        "size": Decimal("0.1"),
        "leverage": 5,
        "stop_loss": Decimal("66000"),
        "take_profit": Decimal("70000")
    }

@pytest.fixture
def valid_short_signal():
    return {
        "pair": "ETH",
        "side": "short",
        "order_type": "market",
        "size": Decimal("1.0"),
        "leverage": 10
    }

@pytest.fixture
def mock_meta_response():
    return {
        "universe": [
            {"name": "BTC", "szDecimals": 5, "maxLeverage": 50},
            {"name": "ETH", "szDecimals": 4, "maxLeverage": 50}
        ]
    }

@pytest.fixture
def mock_account_state():
    return {
        "marginSummary": {
            "accountValue": "10000.0",
            "totalMarginUsed": "500.0"
        },
        "assetPositions": []
    }
```

### 17.2 E2E fixtures (e2e/conftest.py)

```python
import pytest
import os

@pytest.fixture(scope="session")
def testnet_client():
    """Клиент для testnet с реальными credentials"""
    from hlhandler.client import HyperliquidClient
    from hlhandler.config import NetworkConfig
    
    private_key = os.environ["HL_TESTNET_PRIVATE_KEY"]
    return HyperliquidClient(
        network=NetworkConfig.testnet(),
        private_key=private_key
    )

@pytest.fixture(scope="session")
def testnet_address():
    return os.environ["HL_TESTNET_ADDRESS"]

@pytest.fixture
async def clean_positions(testnet_client, testnet_address):
    """Закрыть все позиции перед/после теста"""
    yield
    # Cleanup: закрыть все открытые позиции
    positions = await testnet_client.get_positions(testnet_address)
    for pos in positions:
        await testnet_client.close_position(pos["coin"])

@pytest.fixture
async def clean_orders(testnet_client, testnet_address):
    """Отменить все ордера перед/после теста"""
    yield
    # Cleanup: отменить все открытые ордера
    orders = await testnet_client.get_open_orders(testnet_address)
    for order in orders:
        await testnet_client.cancel_order(order["a"], order["oid"])
```

### 17.3 Маркеры pytest

```python
# pytest.ini или pyproject.toml
[tool.pytest.ini_options]
markers = [
    "unit: Unit tests (fast, no external deps)",
    "integration: Integration tests (mocked HTTP)",
    "e2e: End-to-end tests (requires testnet)",
    "slow: Slow tests (>5 sec)",
    "vault: Vault-related tests"
]

# Запуск
# pytest -m unit           # Только unit
# pytest -m "not e2e"      # Всё кроме e2e
# pytest -m e2e            # Только e2e (testnet)
```

---

## Приложение A: Примеры использования

### Полный флоу исполнения сигнала

```bash
# 1. Создать файл сигнала
cat > signal.json << 'EOF'
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
EOF

# 2. Валидация
hlhandler --network testnet validate --signal signal.json

# 3. Исполнение
hlhandler --network testnet exec --signal signal.json

# 4. Проверка позиций
hlhandler --network testnet positions

# 5. Проверка ордеров (SL/TP)
hlhandler --network testnet orders
```

### Pipeline с внешним источником сигналов

```bash
# Получить сигнал из webhook и исполнить
curl -s https://signals.example.com/latest | hlhandler exec

# Исполнить от имени vault
curl -s https://signals.example.com/latest | hlhandler exec --vault 0x...
```
