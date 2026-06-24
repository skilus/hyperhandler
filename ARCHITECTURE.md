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
cmd/hyperhandler/         # main — точка входа, делегирует в internal/cli
└── main.go               # version ldflag, cli.Execute

internal/
├── cli/                  # cobra CLI — тонкий слой, только рендер
│   ├── root.go           # дерево команд, resolveNetwork, --version
│   ├── trading.go        # exec / validate / cancel / faucet / status / ...
│   ├── risk.go           # risk check / status / reset
│   ├── vaults.go         # vaults list / info / deposit / withdraw / my-positions
│   ├── wallet.go         # wallet generate / import / list / use / delete
│   ├── config.go         # config set-key / remove-key / show-address / check
│   ├── helpers.go        # parseRiskLevel и пр.
│   └── render.go         # table/ANSI-рендер (замена Rich; NO_COLOR + isatty)
│
├── service/              # оркестрация, вынесенная из CLI (SPEC-007 A.1)
│   ├── service.go        # ParseSignal, ValidatorFromConfig, WalletAndSigner
│   ├── evaluate.go       # EvaluateSignal — async-обёртка над risk-ядром
│   ├── exec.go           # Executor.Exec — дискриминированный ExecResult
│   ├── cancel.go         # CancelOrders — фильтр открытых ордеров
│   └── risk.go           # RiskCheck / RiskStatus / ResetCircuitBreaker
│
├── models/               # типы домена (порт Pydantic-моделей)
│   ├── signal.go         # TradingSignal, OrderSide, OrderType, SignalHorizon
│   ├── order.go          # OrderResult, Position, OpenOrder
│   ├── vault.go          # VaultInfo, VaultPosition, VaultDetails
│   ├── validator.go      # SignalValidator, ValidationConfig
│   └── risk.go           # RiskLevel, TradeOrder, TradeResult, CircuitBreakerStatus
│
├── risk/                 # Risk Management (чистое ядро, инжектируемый clock)
│   ├── manager.go        # Manager.EvaluateSignalWithData
│   ├── calculator.go     # ATR, position sizing, leverage selection
│   ├── circuit_breaker.go# consecutive losses, daily limit
│   ├── collector.go      # TradeResultCollector
│   └── config.go         # RiskProfile, HLConfig, ATRSettings
│
├── client/               # HTTP-клиенты (sync net/http, retry-бэкофф)
│   ├── base.go           # BaseClient (retry, типизированные ошибки)
│   ├── info.go           # InfoClient (публичные данные)
│   ├── exchange.go       # ExchangeClient (торговые операции)
│   ├── vault.go          # VaultClient (vault операции)
│   └── order_builder.go  # сборка API-payload из сигнала
│
├── wallet/               # управление ключами
│   ├── manager.go        # WalletManager + цепочка провайдеров
│   ├── env.go            # env-провайдер (HL_*_PRIVATE_KEY)
│   ├── keyring.go        # системный keyring
│   ├── hdprovider.go     # HD-провайдер (BIP-39/BIP-44)
│   ├── prompt.go         # интерактивный prompt
│   └── keys.go           # валидация/нормализация ключей
│
├── signer/               # EIP-712 подпись (msgpack action hash)
├── storage/              # SQLite на modernc.org/sqlite (чистый Go, без cgo)
├── config/               # YAML + HL_-env (__ nesting), NetworkConfig
├── decimalx/             # decimal-хелперы (DivisionPrecision=28)
└── golden/               # загрузка golden-векторов для байт-в-байт тестов

testdata/golden/          # эталонные векторы (оракул — официальный HL SDK)
```

> Стек: **Go 1.25+**, cobra (CLI), `net/http` (sync-клиент), shopspring/decimal,
> modernc.org/sqlite (pure-Go SQLite, без cgo), go-ethereum/crypto + go-bip39
> (подпись и HD-кошелёк). Имена методов — PascalCase (`GetMeta` вместо
> `get_meta`); поведение байт-в-байт совпадает с Python-оригиналом (golden).

## Компоненты

### 1. CLI (`internal/cli`)

cobra-приложение (24 команды). Слой тонкий: парсит флаги, вызывает
`internal/service` и рендерит структурированный результат. Команды:

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

### 2. Модели (`internal/models`)

#### TradingSignal

```go
type TradingSignal struct {
    Pair       string           // нормализуется к uppercase (BTC-USD → BTC)
    Side       OrderSide        // long | short
    OrderType  OrderType        // market | limit
    Size       decimal.Decimal
    Leverage   int              // по умолчанию 5
    EntryPrice *decimal.Decimal
    StopLoss   *decimal.Decimal
    TakeProfit *decimal.Decimal
    // Risk management поля (опциональны)
    Confidence *float64         // 0.0-1.0, влияет на sizing в managed mode
    Horizon    SignalHorizon    // scalp | intraday | swing | position
    Source     *string          // идентификатор источника сигнала
}
// Конструктор NewTradingSignal выполняет нормализацию и cross-field проверки.
```

Валидаторы (`NewTradingSignal`):
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

### 3. API Клиенты (`internal/client`)

#### BaseClient

Базовый HTTP клиент с:
- Retry logic с exponential backoff
- Обработка ошибок API
- Rate limit handling

#### InfoClient

Публичные данные (не требуют подписи):
- `GetMeta()` — метаданные рынков
- `GetAllMids()` — текущие цены
- `GetAccountState()` — состояние аккаунта
- `GetOpenOrders()` — открытые ордера
- `GetPositions()` — позиции
- `GetAssetIndex()` — индекс ассета (с кэшированием)

#### ExchangeClient

Торговые операции (требуют подписи):
- `PlaceOrder()` — разместить ордер
- `PlaceOrderFromSignal()` — ордер из сигнала (entry + SL + TP)
- `CancelOrder()` — отменить ордер
- `SetLeverage()` — установить плечо
- `ClosePosition()` — закрыть позицию

#### VaultClient

Операции с vaults:
- `ListVaults()` — список публичных vaults
- `GetVaultDetails()` — детали vault
- `DepositToVault()` — депозит
- `WithdrawFromVault()` — вывод
- `GetMyVaultPositions()` — мои позиции в vaults

#### OrderBuilder

Конвертация TradingSignal в API payload:
- Построение entry ордера (limit/market)
- Построение SL/TP trigger ордеров
- Расчёт slippage для market ордеров
- Группировка ордеров (`normalTpsl`)

### 4. Risk Management (`internal/risk`)

#### RiskManager

Главный класс для оценки риска сигнала:

```go
type Manager struct { /* RiskLevel, RiskMode, Profile, HLConfig, CircuitBreaker, Calculator */ }
func NewManager(level RiskLevel, mode RiskMode, clock func() time.Time) *Manager
// Чистое ядро (детерминированно, тестируемо) — данные подаёт service-слой:
func (m *Manager) EvaluateSignalWithData(in EvaluateInput) (*TradeOrder, *RiskReject)
```

> Async-фетч данных (account/market/candles) вынесен в `service.EvaluateSignal`
> (SPEC-007 A.1), который вызывает чистое `EvaluateSignalWithData`.

**Risk Modes:**
- `MANUAL` — валидирует параметры сигнала, не меняет их
- `MANAGED` — автоматически рассчитывает size, leverage, stop-loss

#### RiskCalculator

Чистые функции для расчётов:
- `CalculateATR()` — ATR на основе EMA (чистый Python)
- `CalculateStopLoss()` — ATR-based стоп-лосс
- `EstimateLiquidationPrice()` — расчёт ликвидационной цены
- `SelectLeverage()` — выбор плеча с учётом stop distance
- `CalculatePositionSize()` — размер позиции из риск-бюджета
- `CalculateCumulativeRisk()` — кумулятивный риск с корреляцией

#### CircuitBreaker

Отслеживание убытков и блокировка торговли:

```go
func NewCircuitBreaker(profile RiskProfile) *CircuitBreaker
func (cb *CircuitBreaker) Check(history []TradeResult, accountValue decimal.Decimal) CircuitBreakerStatus
func (cb *CircuitBreaker) GetReject(status CircuitBreakerStatus) *RiskReject
```

**Триггеры:**
- `SOFT` — N consecutive losses → risk multiplier 0.5
- `HARD` — M consecutive losses или daily loss limit → блокировка

#### TradeResultCollector

Сбор результатов закрытых сделок:
- `CollectFromFills()` — reconcile из HL API
- `RecordClose()` — запись при закрытии через CLI

#### Risk Profiles

| Profile | Risk/Trade | Max Cumulative | Max Leverage | Soft Stop | Hard Stop |
|---------|------------|----------------|--------------|-----------|-----------|
| LOW | 1% | 4% | 5x | 2 losses | 4 losses |
| MEDIUM | 2% | 6% | 10x | 3 losses | 5 losses |
| HIGH | 3% | 10% | 20x | 3 losses | 6 losses |

### 5. Подпись (`internal/signer`)

```go
func New(privateKey string, isMainnet bool) (*Signer, error)
func (s *Signer) SignAction(action any, nonce int64, vaultAddress *string, expiresAfter *int64) (Payload, error)
func (s *Signer) Address() string
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
│    crypto.Sign(keccak256(EIP-712 digest), privKey) → {r, s, v}  │
│    → {r, s, v}                                                  │
└─────────────────────────────────────────────────────────────────┘
```

Защита от replay attacks:
- **nonce**: timestamp в миллисекундах
- **source**: "a" для mainnet, "b" для testnet — подписи не переносимы между сетями

### 6. Wallet (`internal/wallet`)

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

### 7. Storage (`internal/storage`)

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

### 8. Config (`internal/config`)

```go
// Load читает ~/.hyperhandler/config.yaml и применяет env-override (HL_*,
// "__" — вложенность). Без синглтона.
func Load(path string) (*Config, error)
func (c *Config) NetworkConfig(name string) (NetworkConfig, error)

type Settings struct {
    Network string // "mainnet" по умолчанию
    Trading TradingSettings
}

type NetworkConfig struct {
    Name   string
    APIURL string
    WSURL  string
}
```

## Потоки данных

### Исполнение сигнала (Manual Mode)

```
1. CLI: exec --signal signal.json
   │
2. ├── Загрузка JSON
   │
3. ├── Парсинг в TradingSignal
   │   └── Валидация полей (NewTradingSignal)
   │
4. ├── SignalValidator.Validate()
   │   └── Проверка лимитов
   │
5. ├── WalletManager.GetPrivateKey()
   │   └── Env → Keyring → Prompt
   │
6. ├── RiskManager.EvaluateSignal()  ← NEW
   │   ├── CircuitBreaker.check()
   │   ├── Validate risk limits
   │   └── Return TradeOrder | RiskReject
   │
7. ├── InfoClient.GetAssetIndex()
   │   └── (кэшируется)
   │
8. ├── InfoClient.GetMidPrice() [для market]
   │
9. ├── ExchangeClient.SetLeverage()
   │
10.├── OrderBuilder.BuildOrderPayload()
   │   └── Entry + SL + TP
   │
11.├── Signer.SignAction()
   │
12.├── ExchangeClient._post("exchange", payload)
   │
13.├── Storage.SaveSignal(), save_order(), save_risk_decision()
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
5. ├── WalletManager.GetPrivateKey()
   │
6. ├── RiskManager.EvaluateSignal()  ← MANAGED MODE
   │   ├── CircuitBreaker.check()
   │   ├── InfoClient.GetCandles() → ATR
   │   ├── RiskCalculator.CalculateStopLoss()
   │   ├── RiskCalculator.SelectLeverage()
   │   ├── RiskCalculator.CalculatePositionSize()
   │   ├── RiskCalculator.CalculateCumulativeRisk()
   │   └── Return TradeOrder (with calculated values)
   │
7. ├── CLI: показать diff (Signal → Calculated)
   │
8. ├── ExchangeClient.SetLeverage(calculated)
   │
9. ├── OrderBuilder.BuildOrderPayload(TradeOrder)
   │
10.├── Signer.SignAction()
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

Тесты — стандартный `go test`, по одному `*_test.go` рядом с каждым пакетом.
HTTP-зависимости мокаются через `net/http/httptest`; время — через
инжектируемый `clock` (детерминизм).

```
internal/<pkg>/<pkg>_test.go   # unit/integration по месту
├── signer/signer_test.go       # подпись против golden-векторов
├── order/payload_test.go       # msgpack payload / action hash байт-в-байт
├── models/…_test.go            # TradingSignal, SignalValidator
├── client/{base,info,exchange,vault}_test.go   # httptest-моки
├── risk/{calculator,manager,circuit_breaker,collector}_test.go
├── storage/storage_test.go     # SQLite, идемпотентный insert (UNIQUE fill_id)
├── wallet/{wallet,hd,hdprovider}_test.go
├── config/config_test.go
├── service/service_test.go, service_extra_test.go   # оркестрация + httptest HL
└── cli/{cli,render}_test.go     # дерево команд, контракт флагов, рендер

testdata/golden/                # эталонные векторы (оракул — официальный HL SDK)
└── internal/golden/            # загрузчик golden в тестах
```

Категории:
- **Golden (байт-в-байт)** — подпись EIP-712, msgpack-payload, HD-деривация
  сверяются с официальным HL SDK. Заморожённое ядро (🔴) не рефакторится до
  прохождения golden.
- **Unit/integration** — логика моделей, risk-расчёты, клиенты (httptest),
  storage, service, CLI.
- **E2E на testnet** — ручной прогон против реального API: `exec`/`cancel`/
  `faucet`/`vaults`/`risk` (см. SPEC-007 Фаза 7).

Запуск: `make test` (всё), `make cover` (покрытие по пакетам).

## Безопасность

1. **Хранение ключей**: приватные ключи в системном keychain или env vars, никогда в конфиге
2. **Валидация сигналов**: проверка лимитов перед исполнением
3. **Подпись**: EIP-712 typed data signing, nonce (timestamp) предотвращает replay attacks, разные source для mainnet/testnet
