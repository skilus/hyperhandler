# hlhandler

CLI-сервис для автоматизации торговли на Hyperliquid DEX.

## Возможности

- Исполнение торговых сигналов (market/limit ордера)
- Автоматическая установка Stop-Loss и Take-Profit
- Поддержка Vault-трейдинга (копитрейдинг)
- Работа с mainnet и testnet
- Мониторинг позиций и ордеров
- Безопасное хранение ключей (keyring)

## Установка

```bash
# Клонирование
git clone <repository-url>
cd hlhandler

# Создание виртуального окружения
python3 -m venv .venv
source .venv/bin/activate

# Установка с dev-зависимостями
pip install -e ".[dev]"
```

## Быстрый старт

### 1. Настройка ключа

```bash
# Через системный keyring (рекомендуется)
hlhandler config set-key --network testnet

# Или через переменную окружения
export HL_TESTNET_PRIVATE_KEY="0x..."
```

### 2. Проверка конфигурации

```bash
hlhandler config check
hlhandler config show-address
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
hlhandler validate --signal signal.json

# Исполнение
hlhandler exec --signal signal.json --network testnet
```

## CLI команды

### Торговля

```bash
# Исполнить сигнал
hlhandler exec --signal signal.json [--network mainnet|testnet] [--vault 0x...]

# Валидация без исполнения
hlhandler validate --signal signal.json

# Из stdin
echo '{"pair":"BTC",...}' | hlhandler exec
```

### Мониторинг

```bash
# Статус аккаунта
hlhandler status

# Открытые позиции
hlhandler positions

# Открытые ордера
hlhandler orders
```

### Управление ордерами

```bash
# Отменить по ID
hlhandler cancel --order-id 123456

# Отменить все для пары
hlhandler cancel --pair BTC

# Отменить все
hlhandler cancel --all
```

### Vault операции

```bash
# Список публичных vaults
hlhandler vaults list --min-tvl 100000 --min-apr 20

# Детали vault
hlhandler vaults info 0x...

# Депозит/вывод
hlhandler vaults deposit --vault 0x... --amount 1000
hlhandler vaults withdraw --vault 0x... --shares 0.5

# Мои позиции в vaults
hlhandler vaults my-positions
```

### Конфигурация

```bash
# Сохранить ключ в keyring
hlhandler config set-key --network mainnet

# Удалить ключ
hlhandler config remove-key --network mainnet

# Показать адреса
hlhandler config show-address

# Проверить конфигурацию
hlhandler config check
```

### HD Wallet (Seed Phrase)

```bash
# Сгенерировать новый seed phrase (12 или 24 слова)
hlhandler wallet generate --words 12
hlhandler wallet generate --words 24 --save  # сохранить в keyring

# Импортировать существующий seed phrase
hlhandler wallet import --network testnet

# Показать derived адреса
hlhandler wallet list --count 10

# Получить приватный ключ для конкретного индекса
hlhandler wallet use --index 0

# Удалить seed phrase из keyring
hlhandler wallet delete --network testnet
```

### Testnet

```bash
# Запросить тестовые средства
hlhandler faucet --network testnet
```

## Формат сигнала

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

## Конфигурация

### Файл конфигурации

Расположение: `~/.hlhandler/config.yaml`

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
# Установка dev-зависимостей
pip install -e ".[dev]"

# Запуск тестов
pytest tests/ -v

# Только unit тесты
pytest tests/unit/ -v

# Только integration тесты
pytest tests/integration/ -v
```

## Архитектура

См. [ARCHITECTURE.md](ARCHITECTURE.md)

## Лицензия

MIT
