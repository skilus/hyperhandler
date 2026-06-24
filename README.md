# hyperhandler

CLI-сервис для автоматизации торговли на Hyperliquid DEX.

## Возможности

- Исполнение торговых сигналов (market/limit ордера)
- Автоматическая установка Stop-Loss и Take-Profit
- **Risk Management** — автоматический расчёт размера позиции и стоп-лосса
- **Circuit Breaker** — автоматическая остановка торговли при серии убытков
- Поддержка Vault-трейдинга (копитрейдинг)
- Работа с mainnet и testnet
- Мониторинг позиций и ордеров
- Безопасное хранение ключей (keyring)

## Установка

Реализация на Go (SPEC-007). Статический бинарь без cgo — драйвер SQLite
`modernc.org/sqlite` чистый Go, поэтому кросс-компиляция работает «из коробки».

```bash
# Клонирование
git clone <repository-url>
cd hyperhandler

# Сборка статического бинаря в ./bin/hyperhandler
make build

# Или установить в $GOBIN
go install ./cmd/hyperhandler
```

Требуется Go 1.25+. Кросс-компиляция релизных бинарей под все платформы
(linux/darwin/windows, amd64/arm64) — `make release` (артефакты в `dist/`).

## Быстрый старт

### 1. Настройка ключа

```bash
# Через системный keyring (рекомендуется)
hyperhandler config set-key --network testnet

# Или через переменную окружения
export HL_TESTNET_PRIVATE_KEY="0x..."
```

### 2. Проверка конфигурации

```bash
hyperhandler config check
hyperhandler config show-address
```

### 3. Исполнение сигнала

```bash
# Создать файл сигнала
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

# Валидация
hyperhandler validate --signal signal.json

# Исполнение
hyperhandler exec --signal signal.json --network testnet

# Исполнение с риск-менеджментом (автоматический расчёт размера)
hyperhandler exec --signal signal.json --risk-level medium --network testnet
```

## CLI команды

### Торговля

```bash
# Исполнить сигнал (manual mode — параметры из сигнала)
hyperhandler exec --signal signal.json [--network mainnet|testnet] [--vault 0x...]

# Исполнить с риск-менеджментом (managed mode — автоматический расчёт)
hyperhandler exec --signal signal.json --risk-level medium --dry-run

# Валидация без исполнения
hyperhandler validate --signal signal.json

# Из stdin
echo '{"pair":"BTC",...}' | hyperhandler exec
```

### Риск-менеджмент

```bash
# Проверить сигнал без исполнения (показывает расчёты)
hyperhandler risk check --signal signal.json --risk-level medium

# Статус риска (позиции, circuit breaker, daily PnL)
hyperhandler risk status

# Сбросить circuit breaker вручную
hyperhandler risk reset --yes
```

**Risk Levels:**
| Уровень | Risk per Trade | Max Leverage | Max Positions | Daily Loss Limit |
|---------|----------------|--------------|---------------|------------------|
| `low` | 1% | 5x | 3 | 2% |
| `medium` | 2% | 10x | 5 | 3% |
| `high` | 3% | 20x | 8 | 5% |

### Мониторинг

```bash
# Статус аккаунта
hyperhandler status

# Открытые позиции
hyperhandler positions

# Открытые ордера
hyperhandler orders
```

### Управление ордерами

```bash
# Отменить по ID
hyperhandler cancel --order-id 123456

# Отменить все для пары
hyperhandler cancel --pair BTC

# Отменить все
hyperhandler cancel --all
```

### Vault операции

```bash
# Список публичных vaults
hyperhandler vaults list --min-tvl 100000 --min-apr 20

# Детали vault
hyperhandler vaults info 0x...

# Депозит/вывод
hyperhandler vaults deposit --vault 0x... --amount 1000
hyperhandler vaults withdraw --vault 0x... --shares 0.5

# Мои позиции в vaults
hyperhandler vaults my-positions
```

### Конфигурация

```bash
# Сохранить ключ в keyring
hyperhandler config set-key --network mainnet

# Удалить ключ
hyperhandler config remove-key --network mainnet

# Показать адреса
hyperhandler config show-address

# Проверить конфигурацию
hyperhandler config check
```

### HD Wallet (Seed Phrase)

```bash
# Сгенерировать новый seed phrase (12 или 24 слова)
hyperhandler wallet generate --words 12
hyperhandler wallet generate --words 24 --save  # сохранить в keyring

# Импортировать существующий seed phrase
hyperhandler wallet import --network testnet

# Показать derived адреса
hyperhandler wallet list --count 10

# Получить приватный ключ для конкретного индекса
hyperhandler wallet use --index 0

# Удалить seed phrase из keyring
hyperhandler wallet delete --network testnet
```

### Testnet

```bash
# Запросить тестовые средства
hyperhandler faucet --network testnet
```

## Формат сигнала

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
| `confidence` | float | Нет | Уверенность 0.0-1.0 (влияет на размер в managed mode) |
| `horizon` | enum | Нет | `scalp`, `intraday`, `swing`, `position` |
| `source` | string | Нет | ID источника сигнала |

## Конфигурация

### Файл конфигурации

Расположение: `~/.hyperhandler/config.yaml`

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

### Переменные окружения

| Переменная | Описание |
|------------|----------|
| `HL_NETWORK` | Сеть по умолчанию |
| `HL_PRIVATE_KEY` | Приватный ключ (для любой сети) |
| `HL_MAINNET_PRIVATE_KEY` | Приватный ключ для mainnet |
| `HL_TESTNET_PRIVATE_KEY` | Приватный ключ для testnet |

## Разработка

```bash
# Запуск всех тестов
make test          # go test ./... -count=1

# С покрытием по пакетам
make cover         # go test ./... -cover

# Статический анализ
make vet           # go vet ./...
make lint          # golangci-lint run ./...

# Форматирование
gofmt -w .
```

### Тесты

- Unit/integration — встроены в каждый пакет (`*_test.go`), HTTP-зависимости
  мокаются через `httptest`.
- Golden-векторы (`testdata/golden/`) — байт-в-байт сверка подписи EIP-712,
  msgpack-payload и HD-деривации против официального HL SDK (оракул).
- E2E на реальном testnet — отдельный ручной прогон (`exec`/`cancel`/`faucet`/
  `vaults`/`risk`).

### Линтер

Конфигурация в `.golangci.yml` (golangci-lint v2): стандартный набор линтеров,
preset `std-error-handling` для идиоматичных `Close`/`Fprintf`, отключён ST1005
(сообщения об ошибках сохранены byte-for-byte в паритете с Python).

## Архитектура

См. [ARCHITECTURE.md](ARCHITECTURE.md)

## Лицензия

MIT
