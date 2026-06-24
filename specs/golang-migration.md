# Миграция hyperhandler: Python → Go

**Version:** v1.0 (approved)
**Spec ID:** SPEC-007
**Created:** 2026-06-24 14:03
**Work Started:** 2026-06-24 15:00
**Work Completed:** —

> **Прогресс:** Фаза 0 ✅ (golden-генератор из офиц. SDK, каркас Go, Makefile+CI, гейт зелёный). Дальше — Фаза 1 (крипто-ядро).

---

## Цель

Полный перенос CLI-сервиса hyperhandler с Python на Go с сохранением функционального паритета CLI и **байт-идентичного** поведения на критичном пути подписи (EIP-712 + msgpack), чтобы Hyperliquid принимал ордера без изменений.

Спека составлена по результатам аудита текущего кода (~316 тестов, ~6.6k LOC) — см. раздел «Находки аудита».

### Scope

- Полный порт всех модулей: signer, order_builder, clients (info/exchange/vault), models, risk, wallet, storage, config, cli.
- Порт всех ~316 тестов на Go (`testing` + `testify`) + golden-векторы для крипто-ядра.
- Целевая версия Go-бинаря: **0.4.0** (заменяет Python-версию после прохождения e2e на testnet).

### Out of Scope

- **Brownfield-совместимость данных** (см. решение D4): Go НЕ обязан читать существующие SQLite-БД и keyring-секреты пользователей. Чистый старт.
- Новые фичи (JSON-output, параллелизация запросов и т.п.) — отдельные задачи после паритета.
- WebSocket (его нет и в Python-версии).

---

## Принятые решения

| # | Решение | Обоснование |
|---|---------|-------------|
| **D1** | **Big-bang переписать.** Python замораживается как эталон. | Проект небольшой, CLI без долгоживущего состояния — риск управляем. |
| **D2** | **SQLite-драйвер: `modernc.org/sqlite` (pure Go).** | Без cgo: простая кросс-компиляция, статический бинарь. |
| **D3** | **Полный порт ~316 тестов + golden-векторы.** | Сохраняем покрытие; крипто-ядро сверяется байт-в-байт. |
| **D4** | **Clean cutover** — Go не читает старые БД/keyring. | Убирает работу над isoformat/falsy→NULL/Keychain-ACL совместимостью. Пользователь перезаводит ключ/seed, история начинается заново. |
| **D5** | **Signer пишем сами на go-ethereum, golden-векторы берём из официального Python HL SDK** (не из нашего Python-кода). | Полный контроль, минимум зависимостей, но ядро проверено против независимого эталона. |
| **D6** | **Умеренный рефакторинг при порте** (слои + типы + ошибки/логи); ядро на критпути заморожено. | Переписываем всё равно с нуля — слои/типы стоят того же; ядро чистим за golden позже, чтобы не раздувать критпуть. |
| **D7** | **Латентные баги чиним в ходе порта**, каждый — отдельным тестом и в changelog. | Clean cutover снимает ограничения на изменение поведения; нет смысла консервировать болячки. |

---

## Находки аудита (валидация плана против кода)

### Подтверждено

- **msgpack key ordering критичен.** `order_builder.py:121,156` — в коде прямой комментарий `# Key order matters for msgpack hashing!`. Ключи ордера идут в фикс-порядке `a,b,p,s,r,t`. → В Go **обязательно структуры с фикс-порядком полей**, НЕ `map[string]any` (Go-мапы итерируются случайно → нестабильный хэш → невалидная подпись).
- **EIP-712 схема** (`signer.py`): msgpack(action) + nonce(8B BE) + vault flag(+addr) + (expires) → keccak256 → phantom agent `{source: "a"/"b", connectionId: hash}` → domain `Exchange/1/chainId 1337/0x0`. Соответствует документированной схеме HL.
- **Нет конкурентности.** Во всём `src/` ровно один `asyncio.run` (`cli.py:87`) и один `asyncio.sleep` (`base.py:163`). `gather`/`create_task` отсутствуют; даже независимые запросы в `manager.py:85-96` последовательны. → **Go: синхронный `net/http`, без горутин.**

### Критические риски (байт-идентичность)

1. **Два независимых пути округления на подписи:**
   - **Decimal-путь:** ATR EMA `alpha=2/15` (`calculator.py:72`), `_round_down` ROUND_DOWN (`calculator.py:471`), множественные деления.
   - **Float-путь:** `order_builder._slippage_price` (`:240-255`) — `float(px)` → `f"{px:.5g}"` (5 знач. цифр) → `round(x, n)`. **Python `round()` = банковское (half-to-even), Go `math.Round` = half-away-from-zero.** Требует ручного воспроизведения.
2. **shopspring `DivisionPrecision=16` vs Python-контекст 28 знаков.** Поднять точность Go-decimal до 28 для совпадения хвостов делений.
3. **`_format_price/_format_size`** (`order_builder.py:259-279`): `quantize(1e-8).normalize()` + `f"{x:f}"` — нормализация убирает хвостовые нули, `:f` гасит научную нотацию. Воспроизвести в shopspring руками.
4. **Нет golden-векторов в текущих тестах.** `test_signer.py` проверяет только структуру (r/s/v, детерминизм, v∈{27,28}), не пинит конкретную подпись. → Перед использованием Python как оракула сверить с официальным HL SDK (решение D5).
5. **HD-деривация** (`hd.py:72`): `Account.from_mnemonic(account_path=...)`, BIP39 passphrase `""`. Нужны golden-векторы mnemonic→address для go-ethereum-hdwallet.
6. **r/s в подписи — минимальный hex, не zero-padded** (находка Фазы 0). `signer.py:199-200` использует `to_hex(signed.r)` (eth_utils), который рендерит 256-битное число минимальным hex: длина может быть < 64 символов и **нечётной** (напр. вектор `simple_testnet`: `s` нечётной длины). HL восстанавливает подписанта по integer-значению r/s, поэтому zero-padding не влияет на приём ордера. Golden-сверка в Go должна сравнивать **integer-значения** r/s (нормализовать), а не строки. Если в Фазе 1 нужен строковый паритет — воспроизвести минимальный-hex формат `to_hex` (обрезка ведущих нулевых нибблов).

### Упрощено благодаря clean cutover (D4)

- **Storage**: проектируем чистую схему; НЕ воспроизводим isoformat-формат дат и falsy→NULL семантику (`storage.py:180-182`). Decimal пишем как TEXT через `.String()`, ноль → `"0"` (а не NULL).
- **Wallet**: пользователь перезаводит ключ/seed → проблема Keychain ACL cross-app снимается.

### Прочее (учесть при порте)

- **CLI содержит бизнес-логику**, не только парсинг: оркестрация `exec` (`cli.py:198-322`), «виртуальная сделка» для reset CB (`cli.py:1402-1415`), фильтрация cancel (`cli.py:517-541`) → вынести в service-слой.
- **Контракт паритета CLI**: 24 команды; `HL_NETWORK` биндится везде **кроме** 4 `config`-команд; дефолт-сеть `testnet` для `faucet`+`risk`, иначе `mainnet`; `-s` = 4 разных флага в разных командах; `vaults info` — единственный позиционный аргумент.
- **Retry** (`base.py:87-163`): экспонента `retry_delay * 2^n`, base 1.0s, **без jitter**, макс 4 попытки; retry только на 429/5xx/timeout/network; `Retry-After` игнорируется.
- **HL error mapping** — подстрочный поиск по сообщению (`base.py:165-176`): `signature`→SignatureError, `margin|insufficient`→InsufficientMarginError, `not found|unknown`→AssetNotFoundError. Хрупко, воспроизвести порядок.
- **Все денежные значения на проводе — строки** (`str(amount)`), не числа.

---

## Зоны рефакторинга (умеренный, D6)

Принцип «strangler»: сначала faithful-порт + зелёные golden, затем чистка внутренностей за тестами.

### 🔴 Заморозить (портировать дословно, чистить потом)
- EIP-712 схема, порядок msgpack-ключей.
- `calculator.py` математика (ATR EMA, `_round_down`, cascade-buffer).
- `order_builder._slippage_price` float-путь (5 знач. цифр + банковское округление).

> Эти места НЕЛЬЗЯ «улучшать» (напр. slippage-float → Decimal) до прохождения golden — это изменит подпись/числа.

### 🟢 Рефакторить при порте (A, B, C)

**A. Слои и типы**
1. **Service-слой**: вынести из CLI оркестрацию `exec` (`cli.py:198-322`), фильтрацию `cancel` (`:517-541`), reset CB (`:1402-1415`), сборку validator-конфига. CLI — тонкий.
2. **Типизированные DTO HL API** вместо `dict.get(...)` и мутации `asset_meta["_asset_id"]` (`manager.py:90`).
3. **Discriminated результаты** вместо `isinstance(tuple)`/`hasattr(...,"risk_mode")` (`cli.py:274-288`) и union `TradeOrder | RiskReject`.
4. **Убрать order-type-по-индексу** (`cli.py:294,312`) — явная типизация entry/sl/tp.

**B. Корректность (латентные баги — чинить, D7)**
5. **UNIQUE на `fill_id`** в `trade_results` + idempotent upsert — сейчас дедуп in-memory (`collector.py:26`), при рестарте дубли искажают PnL circuit breaker'а.
6. **Явная retry-идемпотентность** для `/exchange`: ретрай переотправляет тот же nonce (replay-protected), не пере-подписывает; не ретраить write вслепую. Зафиксировать тестом.
7. **`risk status` — реальный профиль** вместо хардкода MEDIUM (`cli.py`).
8. **Inject clock** в `circuit_breaker`/`manager`/`collector` — детерминизм тестов.

**C. Ошибки, логи, конфиг**
9. **Типизированные ошибки** (`errors.Is/As`): строку HL-ошибки парсим на границе (`base.py:165-176`) в sentinel, внутри — типы.
10. **`log/slog`** структурно; риск-решения с полями.
11. **`context.Context`** сквозь клиенты (таймауты/отмена).
12. **DI вместо синглтонов** `_config`/`_storage`; единый decimal-контекст; убрать falsy-Decimal идиомы.

### ⏸️ Отложено за паритет (НЕ в этой миграции)
Версионирование схемы БД, money-тип-обёртка, errgroup-параллелизация 3 info-запросов (`manager.py:85-96`), пересмотр доменных границ. Открыть отдельными тасками после паритета.

## Карта зависимостей Python → Go

| Python | Go | Риск |
|--------|-----|------|
| `eth-account` (signing) | `github.com/ethereum/go-ethereum` crypto | 🔴 |
| `eth-account` HD | `github.com/miguelmota/go-ethereum-hdwallet` | 🔴 |
| `msgpack` | `github.com/vmihailenco/msgpack/v5` (структуры, фикс-порядок) | 🔴 |
| `eth-utils` keccak | geth `crypto.Keccak256` | 🟢 |
| `Decimal` | `github.com/shopspring/decimal` (DivisionPrecision=28) | 🟡 |
| `typer` | `github.com/spf13/cobra` | 🟢 |
| `rich` | `lipgloss` + `olekukonko/tablewriter` | 🟢 |
| `httpx` (async) | stdlib `net/http` (sync) | 🟢 |
| `pydantic` | structs + `go-playground/validator/v10` + конструкторы-фабрики | 🟡 |
| SQLite | `modernc.org/sqlite` | 🟢 |
| `keyring` | `github.com/zalando/go-keyring` | 🟢 |
| `pyyaml` | `gopkg.in/yaml.v3` | 🟢 |
| `pytest`/`respx` | `testing`+`testify` / `httptest` | 🟢 |

---

## Целевая структура

```
hyperhandler/
├── cmd/hyperhandler/main.go
├── internal/
│   ├── cli/            # cobra-команды (тонкий слой)
│   ├── service/        # оркестрация exec/cancel/risk-reset (вынесено из cli.py)
│   ├── signer/         # EIP-712 + msgpack action hash  🔴
│   ├── order/          # order_builder (float slippage path) 🔴
│   ├── client/         # base(retry) + info/exchange/vault, sync net/http
│   ├── models/         # signal, order, vault, validator, risk
│   ├── risk/           # manager, calculator, circuit_breaker, collector, config
│   ├── wallet/         # manager + providers (env, keyring, prompt, hd)
│   ├── storage/        # modernc.org/sqlite
│   └── config/         # YAML + env (HL_ prefix, __ nesting)
├── testdata/golden/    # векторы из официального HL SDK
├── go.mod
└── Makefile
```

---

## Последовательность реализации (фазы)

### Фаза 0 — Эталон и каркас (1–1.5 дня)
- Установить **официальный Python HL SDK**, написать генератор golden-векторов в `testdata/golden/`: для фиксированных (ключ, action, nonce, vault, expires) → msgpack-байты (hex), action_hash (hex), подпись {r,s,v}.
- Golden-векторы HD: mnemonic → адреса по путям `m/44'/60'/0'/0/{i}`.
- Тестовые ключи — только фейковые/testnet, **не реальные секреты**.
- `go mod init`, каркас пакетов, Makefile, CI-скелет.
- **Гейт:** генератор воспроизводит подпись из примера в `test_signer.py`.

### Фаза 1 — Крипто-ядро 🔴 (3 дня) — критический путь
- `signer`: msgpack(структуры) → конкатенация → keccak256 → phantom agent → EIP-712 sign на go-ethereum.
- `order`: порт `order_builder` включая **float slippage path** (5 знач. цифр + half-to-even round) и `_format_price/_format_size`.
- `wallet/hd`: BIP-39/44 деривация.
- Зафиксировать Go-decimal контекст (DivisionPrecision=28).
- **Гейт фазы:** ВСЕ golden-векторы Фазы 0 проходят байт-в-байт. Без этого дальше не идём.

### Фаза 2 — Модели и валидация (1.5 дня)
- Structs: signal, order, vault, risk; теги JSON/YAML.
- Конструкторы-фабрики `NewTradingSignal(...) (*T, error)`: `normalize_pair` (order-sensitive replace), `validate_prices`, Field-ограничения (`size>0`, `leverage 1..50`, `confidence 0..1`).
- `SignalValidator` — порядок правил из `validator.py:35-96`.

### Фаза 3 — HTTP-клиенты (1.5 дня, де-рискована)
- `client/base`: sync `net/http` + retry (экспонента без jitter, 4 попытки), `context.Context`, типизированные ошибки (парсинг HL-строки на границе → sentinel).
- **Явная retry-идемпотентность** для `/exchange` (тот же nonce при ретрае; не ретраить write вслепую) — B.6, с тестом.
- `info`/`exchange`/`vault`: все эндпоинты (POST `/info`|`/exchange`, операция в JSON `type`). Тесты через `httptest.Server`.

### Фаза 4 — Risk-модуль (3–4 дня) — самый плотный
- `calculator` (ATR EMA, position sizing, `_round_down`, cumulative/cascade — `calculator.py` самый сложный файл проекта), `manager`, `circuit_breaker` (чистая функция от истории), `collector`, `config` (профили, Decimal-константы из строк).
- Инжектируемый `now`/clock для детерминизма тестов.
- Golden-значения расчётов из Python.

### Фаза 5 — Storage + Wallet (1.5 дня, упрощено D4)
- `storage` (modernc): чистая схема (clean cutover — без байт-совместимости старых БД). Decimal → TEXT `.String()`, ноль → `"0"`. **UNIQUE на `fill_id`** в `trade_results` + idempotent upsert (B.5).
- `wallet/manager` + providers (env, keyring, prompt). Сервис-имена keyring можно задать заново.

### Фаза 6 — CLI + service-слой (2.5 дня)
- Вынести оркестрацию (`exec`, `cancel`, `risk reset`) в `internal/service` (A.1); discriminated результаты (A.3).
- **`risk status` — реальный профиль** вместо хардкода MEDIUM (B.7).
- Cobra-дерево: 24 команды с точным контрактом флагов/дефолтов/env (см. находки). lipgloss/tablewriter вместо Rich.

### Фаза 7 — Тесты, e2e, документация (2.5 дня)
- Допортировать оставшиеся unit/integration тесты.
- e2e против Hyperliquid **testnet**: exec, cancel, faucet, vaults.
- Обновить README, ARCHITECTURE, CLAUDE.md, Claude Skill под Go; кросс-компиляция (goreleaser/Makefile).

**Итого ≈ 16–21 рабочий день.** Критический путь — Фаза 1.

---

## Риски и митигации

| Риск | Митигация |
|------|-----------|
| msgpack байты ≠ Python | структуры с фикс-порядком; golden из офиц. SDK |
| EIP-712 подпись расходится → биржа отвергает | байт-в-байт golden + e2e на testnet до замены |
| Float slippage round (half-even vs half-away) | ручная реализация банковского округления + golden |
| Decimal-точность (деления, ATR 2/15) | DivisionPrecision=28, golden-значения расчётов |
| HD даёт другие адреса | golden-векторы mnemonic→address |
| Потеря фич CLI | чек-лист 24 команд + контракт флагов из аудита |

---

## Критерии приёмки

- [ ] Все golden-векторы (signer, msgpack, HD) проходят байт-в-байт.
- [ ] Заморожённое ядро (🔴) не рефакторилось до прохождения golden.
- [ ] Порт всех ~316 тестов на Go, зелёные.
- [ ] Латентные баги исправлены и покрыты тестами: UNIQUE `fill_id` (дедуп при рестарте), retry-идемпотентность `/exchange`, реальный профиль в `risk status`.
- [ ] Service-слой: CLI не содержит оркестрации/бизнес-логики.
- [ ] e2e на testnet: exec/cancel/faucet/vaults/risk проходят против реального API.
- [ ] Паритет CLI: все 24 команды, флаги, дефолты сети, env-binding совпадают с Python.
- [ ] Статический бинарь собирается без cgo; кросс-компиляция работает.
- [ ] README/ARCHITECTURE/CLAUDE.md/Skill обновлены под Go.
- [ ] Линтер (`golangci-lint`) чистый.
