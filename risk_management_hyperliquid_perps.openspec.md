# Risk Management Core Loop — Hyperliquid Perps
# OpenSpec v1.0

---

## 1. Overview

### 1.1 Purpose
Модуль управления рисками для копитрейдинга сигналов на Hyperliquid Perps.
Принимает торговый сигнал, рассчитывает безопасный размер позиции с учётом
состояния портфеля, и возвращает готовый к исполнению ордер или reject.

### 1.2 Target Exchange
**Hyperliquid** — onchain perps DEX на собственном L1.

Ключевые характеристики HL для risk manager:
- Collateral: **USDC** (единственный)
- Margin modes: **Cross** (дефолт) и **Isolated**
- Leverage: до 50x на BTC/ETH, варьируется по активам (см. `meta` endpoint)
- Fees: taker 0.045%, maker 0.015% (Tier 0, без стейкинга)
- Funding: каждый **час** (не 8h как на CEX)
- Стоп-лоссы: нативные TP/SL ордера через API
- Liquidation: по **mark price** (не last trade), deterministic
- API: REST + WebSocket, **Python SDK** (`hyperliquid-python-sdk`)
- Auth: private key signing (не API key/secret), поддержка **API wallets**
- Asset ID: index в `meta.universe` для perps

### 1.3 Scope — MVP
- Position sizing на основе ATR и risk budget
- ATR-based стоп-лоссы (нативные HL TP/SL ордера)
- Кумулятивный риск портфеля (cross margin awareness)
- Circuit breaker
- Funding rate cost estimation
- Интеграция через `hyperliquid-python-sdk`

### 1.4 Out of Scope (v1)
- Take-profit стратегии / trailing stop
- Portfolio margin mode
- HIP-3 markets
- Hyperps (pre-launch futures)
- Multi-account / vault trading
- Builder codes / fee optimization

---

## 2. Core Loop

```
Signal In
  │
  ▼
┌──────────────────────┐
│  1. VALIDATE SIGNAL  │──→ Reject: invalid ticker, stale price, duplicate
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  2. CIRCUIT BREAKER  │──→ Reject: consecutive losses, daily loss limit
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  3. GET MARKET DATA  │    ATR, mark price, funding rate, asset meta
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  4. CALC STOP-LOSS   │    ATR-based + slippage buffer
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  5. CHECK RISK       │──→ Reject: cumulative risk exceeded,
│     BUDGET           │    correlation limit, max positions
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  6. CALC POSITION    │    Size, leverage, margin required
│     SIZE             │──→ Reject: below min order, insufficient balance
└──────────┬───────────┘
           ▼
┌──────────────────────┐
│  7. ESTIMATE COSTS   │    Commission + funding cost projection
└──────────┬───────────┘
           ▼
  TradeOrder OUT
```

---

## 3. Data Models

### 3.1 Signal (Input)

```python
@dataclass
class Signal:
    coin: str                # HL naming: "BTC", "ETH", "SOL" (not "BTC/USDT")
    side: Side               # LONG | SHORT
    entry_price: float       # Ожидаемая цена входа
    confidence: float        # 0.0 - 1.0
    source: str              # ID источника сигнала (influencer/strategy)
    timestamp: float         # Unix timestamp (ms)
    leverage: int | None     # Желаемое плечо (None = use default from config)
```

### 3.2 HyperliquidState (from API)

```python
@dataclass
class HyperliquidState:
    """Snapshot состояния аккаунта на Hyperliquid.
    Источник: info.user_state(address) + info.meta()
    """
    # Account
    account_value: float          # Общая стоимость аккаунта в USDC
    margin_used: float            # Используемая маржа
    available_balance: float      # Свободный баланс (withdrawable)
    cross_margin_summary: dict    # Общая информация по cross margin

    # Positions
    open_positions: list[HLPosition]

    # Meta
    asset_meta: dict[str, AssetMeta]  # coin → {szDecimals, maxLeverage, ...}
```

### 3.3 HLPosition (Open Position)

```python
@dataclass
class HLPosition:
    coin: str
    side: Side
    size: float               # В базовой валюте (с учётом szDecimals)
    entry_price: float        # Средняя цена входа
    mark_price: float         # Текущая mark price (для PnL и ликвидации)
    liquidation_price: float  # Цена ликвидации (от HL)
    unrealized_pnl: float     # В USDC
    leverage: int
    margin_used: float        # Маржа под позицию
    funding_accrued: float    # Накопленный funding
    stop_loss: float | None   # Текущий SL ордер (если есть)

    # Для risk tracking (рассчитывается нами)
    risk_amount: float        # $ под риском = size * |entry - stop_loss|
    risk_pct: float           # % от account_value
    correlation_group: str    # "btc-correlated" | "alt-independent"
    opened_at: float          # Timestamp
```

### 3.4 AssetMeta (from HL meta endpoint)

```python
@dataclass
class AssetMeta:
    coin: str
    asset_id: int             # Index в universe (нужен для ордеров)
    sz_decimals: int          # Decimal places для размера
    max_leverage: int         # Макс. допустимое плечо
    only_isolated: bool       # Только isolated margin (некоторые мем-коины)

    # From market data
    mark_price: float
    funding_rate: float       # Текущий hourly funding rate
    open_interest: float
    day_volume: float         # 24h notional volume
```

### 3.5 TradeOrder (Output)

```python
@dataclass
class TradeOrder:
    coin: str
    asset_id: int             # HL asset index для API
    side: Side
    size: float               # Размер в базовой валюте (округлён до szDecimals)
    entry_price: float
    leverage: int
    margin_mode: str          # "cross" | "isolated"

    # Risk params
    stop_loss: float          # Будет выставлен как HL native TP/SL
    risk_amount: float        # USDC под риском
    risk_pct: float           # % от account_value
    cumulative_risk_after: float

    # Cost estimates
    estimated_commission: float     # Taker fee на вход
    estimated_funding_24h: float    # Проекция funding cost на 24h
    margin_required: float          # Сколько маржи заблокируется

    # Audit
    calculation_details: dict       # Все промежуточные значения
```

### 3.6 Reject (Output)

```python
@dataclass
class Reject:
    reason: RejectReason
    details: str
    signal: Signal
    suggested_action: str     # "wait" | "reduce_risk" | "close_positions" | "manual_reset"

class RejectReason(Enum):
    CIRCUIT_BREAKER_SOFT = "circuit_breaker_soft"
    CIRCUIT_BREAKER_HARD = "circuit_breaker_hard"
    DAILY_LOSS_LIMIT = "daily_loss_limit"
    RISK_BUDGET_EXCEEDED = "risk_budget_exceeded"
    CORRELATION_LIMIT = "correlation_limit"
    MAX_POSITIONS_REACHED = "max_positions_reached"
    INSUFFICIENT_MARGIN = "insufficient_margin"
    POSITION_TOO_SMALL = "position_too_small"          # Below HL min order ($10)
    DUPLICATE_POSITION = "duplicate_position"
    INVALID_COIN = "invalid_coin"
    STALE_SIGNAL = "stale_signal"                      # Entry deviated > threshold
    LEVERAGE_EXCEEDED = "leverage_exceeded"
    LIQUIDATION_TOO_CLOSE = "liquidation_too_close"    # SL beyond liq price
    HIGH_FUNDING_COST = "high_funding_cost"             # Funding eats >50% of risk
```

---

## 4. Calculation Logic

### 4.1 Stop-Loss (ATR-based)

**Данные:** candle snapshot из HL Info API (`candle_snapshot` endpoint).

```python
def calculate_atr(candles: list[dict], period: int = 14) -> float:
    """
    candles: from HL API, each has {t, T, s, i, o, c, h, l, v, n}
    o=open, c=close, h=high, l=low
    """
    true_ranges = []
    for i in range(1, len(candles)):
        high = float(candles[i]["h"])
        low = float(candles[i]["l"])
        prev_close = float(candles[i-1]["c"])
        tr = max(high - low, abs(high - prev_close), abs(low - prev_close))
        true_ranges.append(tr)

    # EMA для большей чувствительности к последним свечам
    if len(true_ranges) < period:
        return sum(true_ranges) / len(true_ranges)

    alpha = 2 / (period + 1)
    ema = true_ranges[0]
    for tr in true_ranges[1:]:
        ema = alpha * tr + (1 - alpha) * ema
    return ema
```

**Стоп-лосс:**
```
stop_distance = ATR * atr_multiplier
stop_with_buffer = stop_distance * (1 + slippage_pct)

LONG:  stop_price = entry_price - stop_with_buffer
SHORT: stop_price = entry_price + stop_with_buffer
```

**Candle timeframe выбор:**
| Signal horizon      | Candle TF | ATR period | ATR multiplier |
|---------------------|-----------|------------|----------------|
| Scalp (<4h)         | 15m       | 14         | 1.2            |
| Intraday (4h-24h)   | 1h        | 14         | 1.5            |
| Swing (1d-7d)       | 4h        | 14         | 2.0            |
| Position (>7d)      | 1d        | 14         | 2.5            |

> MVP: используем **1h** как дефолт (подходит для большинства influencer-сигналов).

**Валидация стоп-лосса vs ликвидация:**
```python
# Стоп-лосс ДОЛЖЕН быть ближе к entry чем liquidation price
# Иначе ликвидация наступит раньше стопа

# LONG:  assert stop_price > liquidation_price
# SHORT: assert stop_price < liquidation_price

# Margin of safety: стоп должен быть минимум на 2% дальше от ликвидации
liq_buffer = abs(stop_price - liquidation_price) / entry_price
if liq_buffer < 0.02:
    # → Reject(LIQUIDATION_TOO_CLOSE)
    pass
```

### 4.2 Position Sizing

```python
def calculate_position_size(
    account_value: float,       # USDC
    risk_per_trade: float,      # e.g. 0.02
    entry_price: float,
    stop_price: float,
    leverage: int,
    sz_decimals: int,           # HL szDecimals для округления
    taker_fee: float,           # 0.00045 (Tier 0)
    available_balance: float,
) -> PositionSize | Reject:

    # 1. Risk amount
    risk_amount = account_value * risk_per_trade

    # 2. Stop distance (per unit)
    stop_distance = abs(entry_price - stop_price)

    # 3. Raw position size (в базовой валюте)
    raw_size = risk_amount / stop_distance

    # 4. Notional value
    notional = raw_size * entry_price

    # 5. Round-trip commission
    commission = notional * taker_fee * 2  # open + close

    # 6. Adjust risk for commission
    adjusted_risk = risk_amount - commission
    if adjusted_risk <= 0:
        return Reject(POSITION_TOO_SMALL)

    adjusted_size = adjusted_risk / stop_distance

    # 7. Margin check
    margin_required = (adjusted_size * entry_price) / leverage
    if margin_required > available_balance:
        # Reduce to fit balance
        max_size = (available_balance * leverage) / entry_price
        adjusted_size = min(adjusted_size, max_size)

    # 8. Round to szDecimals (HL requirement)
    adjusted_size = round_down(adjusted_size, sz_decimals)

    # 9. Min order check ($10 on HL)
    if adjusted_size * entry_price < 10.0:
        return Reject(POSITION_TOO_SMALL)

    return PositionSize(
        size=adjusted_size,
        notional=adjusted_size * entry_price,
        margin_required=(adjusted_size * entry_price) / leverage,
        risk_amount=adjusted_size * stop_distance,
        commission=adjusted_size * entry_price * taker_fee * 2,
    )
```

**Округление (critical для HL):**
```python
import math

def round_down(value: float, decimals: int) -> float:
    """HL требует точное количество decimals в sz.
    BTC: szDecimals=5, ETH: szDecimals=4, мемы: szDecimals=0
    Всегда округляем ВНИЗ чтобы не превысить маржу.
    """
    factor = 10 ** decimals
    return math.floor(value * factor) / factor
```

### 4.3 Leverage Selection

```python
def select_leverage(
    coin: str,
    max_leverage: int,          # From HL meta
    stop_distance_pct: float,   # stop_distance / entry_price
    config_max_leverage: int,   # User's max allowed leverage
) -> int:
    """
    Выбираем плечо так, чтобы ликвидация была ДАЛЬШЕ стопа.

    Approximate HL liq distance (cross margin, simplified):
    liq_distance ≈ 1 / leverage

    Мы хотим: liq_distance > stop_distance * safety_factor
    → leverage < 1 / (stop_distance_pct * safety_factor)
    """
    SAFETY_FACTOR = 1.5  # Ликвидация на 50% дальше стопа

    max_safe_leverage = int(1 / (stop_distance_pct * SAFETY_FACTOR))
    max_safe_leverage = max(1, max_safe_leverage)

    leverage = min(
        max_safe_leverage,
        max_leverage,          # HL max для монеты
        config_max_leverage,   # Пользовательский лимит
    )
    return leverage
```

### 4.4 Cumulative Risk (Cross Margin Awareness)

> **Critical:** В cross margin на HL все позиции делят один пул маржи.
> Убыток по одной позиции снижает available margin для остальных.
> Каскадная ликвидация — реальный риск.

```python
def calculate_cumulative_risk(
    open_positions: list[HLPosition],
    new_risk_amount: float,
    new_coin: str,
    account_value: float,
    correlation_factor: float,
) -> CumulativeRisk:

    # 1. Базовый кумулятивный риск
    total_risk = sum(p.risk_amount for p in open_positions)

    # 2. Корреляционная поправка
    groups = group_positions_by_correlation(open_positions, new_coin)
    adjusted_risk = 0.0
    for group_name, positions in groups.items():
        group_risk = sum(p.risk_amount for p in positions)
        n = len(positions)
        if n > 1:
            penalty = 1 + (n - 1) * correlation_factor
            group_risk *= penalty
        adjusted_risk += group_risk

    # 3. Cross margin cascade buffer
    cascade_buffer = 0.0
    for group_name, positions in groups.items():
        if len(positions) > 1:
            cascade_buffer += sum(p.risk_amount for p in positions) * 0.1

    total_adjusted_risk = adjusted_risk + cascade_buffer + new_risk_amount

    return CumulativeRisk(
        raw_risk=total_risk + new_risk_amount,
        adjusted_risk=total_adjusted_risk,
        risk_pct=total_adjusted_risk / account_value,
        available_budget=max(0, MAX_CUMULATIVE_RISK * account_value - total_adjusted_risk),
        within_limit=total_adjusted_risk / account_value <= MAX_CUMULATIVE_RISK,
    )
```

**Correlation groups (v1 — static):**
```python
CORRELATION_MAP = {
    "btc-major": ["BTC", "ETH"],
    "l1-alt": ["SOL", "AVAX", "SUI", "APT", "SEI"],
    "defi": ["AAVE", "UNI", "MKR", "CRV", "DYDX"],
    "meme": ["DOGE", "SHIB", "PEPE", "WIF", "BONK"],
    "ai": ["FET", "RNDR", "TAO", "NEAR"],
}

def get_correlation_group(coin: str) -> str:
    for group, coins in CORRELATION_MAP.items():
        if coin in coins:
            return group
    return f"independent-{coin}"
```

### 4.5 Funding Rate Cost

> HL funding — **каждый час**. Для positions > 24h funding может быть значительным.

```python
def estimate_funding_cost(
    size: float,
    entry_price: float,
    side: Side,
    current_funding_rate: float,  # Hourly rate from HL
    risk_amount: float,
    hold_hours: int = 24,
) -> FundingEstimate:
    """
    funding_rate > 0: longs pay shorts
    funding_rate < 0: shorts pay longs
    """
    notional = size * entry_price
    hourly_cost = notional * current_funding_rate

    if side == Side.LONG:
        projected_cost = hourly_cost * hold_hours
    else:
        projected_cost = -hourly_cost * hold_hours

    funding_eats_risk_pct = max(0, projected_cost) / risk_amount if risk_amount > 0 else 0

    return FundingEstimate(
        hourly_cost=abs(hourly_cost) if projected_cost > 0 else 0,
        hourly_income=abs(hourly_cost) if projected_cost < 0 else 0,
        projected_24h=projected_cost,
        funding_eats_risk_pct=funding_eats_risk_pct,
    )

# Reject если funding слишком дорогой:
# if funding_eats_risk_pct > max_funding_risk_pct → Reject(HIGH_FUNDING_COST)
```

### 4.6 Circuit Breaker

```python
def check_circuit_breaker(
    trade_history: list[TradeResult],
    account_value: float,
    config: dict,
) -> CircuitBreakerStatus:

    # 1. Consecutive losses
    consecutive_losses = 0
    for trade in reversed(trade_history):
        if trade.pnl < 0:
            consecutive_losses += 1
        else:
            break

    # 2. Daily P&L
    today_start = get_utc_day_start()
    today_trades = [t for t in trade_history if t.closed_at >= today_start]
    daily_pnl = sum(t.pnl for t in today_trades)
    daily_loss_pct = abs(min(0, daily_pnl)) / account_value

    # 3. Decision
    if daily_loss_pct >= config["daily_loss_limit"]:
        return CircuitBreakerStatus(
            active=True, level="HARD", risk_multiplier=0.0,
            reason=f"Daily loss {daily_loss_pct:.1%} >= {config['daily_loss_limit']:.1%}",
        )

    if consecutive_losses >= config["hard_stop_losses"]:
        return CircuitBreakerStatus(
            active=True, level="HARD", risk_multiplier=0.0,
            reason=f"{consecutive_losses} consecutive losses (hard: {config['hard_stop_losses']})",
        )

    if consecutive_losses >= config["soft_stop_losses"]:
        return CircuitBreakerStatus(
            active=True, level="SOFT", risk_multiplier=0.5,
            reason=f"{consecutive_losses} consecutive losses (soft: {config['soft_stop_losses']})",
        )

    return CircuitBreakerStatus(active=False, level="NONE", risk_multiplier=1.0)
```

---

## 5. Hyperliquid API Integration

### 5.1 SDK Setup

```python
from hyperliquid.info import Info
from hyperliquid.exchange import Exchange
from hyperliquid.utils import constants

# Testnet
API_URL = constants.TESTNET_API_URL  # https://api.hyperliquid-testnet.xyz
# Mainnet
# API_URL = constants.MAINNET_API_URL  # https://api.hyperliquid.xyz

info = Info(API_URL, skip_ws=True)
exchange = Exchange(wallet, API_URL)  # wallet = eth_account.Account
```

### 5.2 Market Data Adapter

```python
class HyperliquidMarketData:
    """Адаптер для получения market data из HL."""

    def __init__(self, info: Info, address: str):
        self.info = info
        self.address = address
        self._meta_cache = None
        self._meta_ts = 0

    def get_meta(self) -> dict:
        """Кэшируем meta на 5 минут."""
        if time.time() - self._meta_ts > 300:
            self._meta_cache = self.info.meta()
            self._meta_ts = time.time()
        return self._meta_cache

    def get_asset_id(self, coin: str) -> int:
        """HL требует asset index, не ticker string."""
        meta = self.get_meta()
        for i, asset in enumerate(meta["universe"]):
            if asset["name"] == coin:
                return i
        raise ValueError(f"Unknown coin: {coin}")

    def get_asset_meta(self, coin: str) -> AssetMeta:
        meta = self.get_meta()
        asset_id = self.get_asset_id(coin)
        universe = meta["universe"][asset_id]

        all_mids = self.info.all_mids()
        mark_price = float(all_mids.get(coin, 0))

        perp_meta = self.info.meta_and_asset_ctxs()
        ctx = perp_meta[1][asset_id]

        return AssetMeta(
            coin=coin,
            asset_id=asset_id,
            sz_decimals=universe["szDecimals"],
            max_leverage=universe["maxLeverage"],
            only_isolated=universe.get("onlyIsolated", False),
            mark_price=mark_price,
            funding_rate=float(ctx["funding"]),
            open_interest=float(ctx["openInterest"]),
            day_volume=float(ctx["dayNtlVlm"]),
        )

    def get_candles(self, coin: str, interval: str = "1h", lookback: int = 100) -> list[dict]:
        end_time = int(time.time() * 1000)
        return self.info.candles_snapshot(coin, interval, end_time, lookback)

    def get_user_state(self) -> HyperliquidState:
        state = self.info.user_state(self.address)
        positions = []
        for ap in state.get("assetPositions", []):
            pos = ap["position"]
            size = float(pos["szi"])
            if size != 0:
                positions.append(HLPosition(
                    coin=pos["coin"],
                    side=Side.LONG if size > 0 else Side.SHORT,
                    size=abs(size),
                    entry_price=float(pos["entryPx"]),
                    mark_price=float(pos.get("markPx", 0)),
                    liquidation_price=float(pos.get("liquidationPx") or 0),
                    unrealized_pnl=float(pos["unrealizedPnl"]),
                    leverage=int(pos["leverage"]["value"]),
                    margin_used=float(pos.get("marginUsed", 0)),
                    funding_accrued=float(pos.get("cumFunding", {}).get("sinceOpen", 0)),
                    stop_loss=None,
                    risk_amount=0.0,
                    risk_pct=0.0,
                    correlation_group=get_correlation_group(pos["coin"]),
                    opened_at=0.0,
                ))
        return HyperliquidState(
            account_value=float(state["marginSummary"]["accountValue"]),
            margin_used=float(state["marginSummary"]["totalMarginUsed"]),
            available_balance=float(state["withdrawable"]),
            cross_margin_summary=state.get("crossMarginSummary", {}),
            open_positions=positions,
            asset_meta={},
        )
```

### 5.3 Order Execution (reference — outside risk manager scope)

```python
def execute_trade_order(exchange: Exchange, order: TradeOrder) -> dict:
    """Reference implementation. Not part of risk manager."""

    # 1. Set leverage
    exchange.update_leverage(order.leverage, order.coin, is_cross=True)

    # 2. Market order
    result = exchange.market_open(
        coin=order.coin,
        is_buy=order.side == Side.LONG,
        sz=order.size,
        slippage=0.01,
    )

    # 3. Native HL stop-loss (trigger on mark price)
    if result["status"] == "ok":
        exchange.order(
            coin=order.coin,
            is_buy=order.side == Side.SHORT,  # SL = opposite side
            sz=order.size,
            limit_px=order.stop_loss,
            order_type={"trigger": {
                "triggerPx": str(order.stop_loss),
                "isMarket": True,
                "tpsl": "sl",
            }},
            reduce_only=True,
        )

    return result
```

---

## 6. Configuration

### 6.1 Risk Profiles

```python
RISK_PROFILES = {
    RiskLevel.LOW: {
        "risk_per_trade": 0.01,
        "max_cumulative_risk": 0.04,
        "daily_loss_limit": 0.02,
        "atr_multiplier": 2.0,
        "max_open_positions": 3,
        "max_leverage": 5,
        "correlation_factor": 0.4,
        "soft_stop_losses": 2,
        "hard_stop_losses": 4,
        "max_funding_risk_pct": 0.3,
        "candle_timeframe": "4h",
    },
    RiskLevel.MEDIUM: {
        "risk_per_trade": 0.02,
        "max_cumulative_risk": 0.06,
        "daily_loss_limit": 0.03,
        "atr_multiplier": 1.5,
        "max_open_positions": 5,
        "max_leverage": 10,
        "correlation_factor": 0.3,
        "soft_stop_losses": 3,
        "hard_stop_losses": 5,
        "max_funding_risk_pct": 0.5,
        "candle_timeframe": "1h",
    },
    RiskLevel.HIGH: {
        "risk_per_trade": 0.03,
        "max_cumulative_risk": 0.10,
        "daily_loss_limit": 0.05,
        "atr_multiplier": 1.2,
        "max_open_positions": 8,
        "max_leverage": 20,
        "correlation_factor": 0.25,
        "soft_stop_losses": 3,
        "hard_stop_losses": 6,
        "max_funding_risk_pct": 0.7,
        "candle_timeframe": "1h",
    },
}
```

### 6.2 Hyperliquid-Specific Config

```python
HL_CONFIG = {
    # Fees (Tier 0, no staking discount)
    "taker_fee": 0.00045,         # 0.045%
    "maker_fee": 0.00015,         # 0.015%

    # Order constraints
    "min_order_value": 10.0,      # $10 USDC minimum
    "max_slippage": 0.01,         # 1% for market orders
    "slippage_buffer": 0.005,     # 0.5% added to stop calculations

    # Margin
    "default_margin_mode": "cross",
    "liq_safety_buffer": 0.02,    # SL must be 2% away from liq price

    # Signal validation
    "max_entry_deviation": 0.01,  # 1% max deviation from mark price
    "max_signal_age_s": 300,      # 5 min max signal age

    # API
    "api_url": "https://api.hyperliquid-testnet.xyz",
}
```

---

## 7. Interface

```python
class RiskManager:
    """
    Stateless risk evaluator.
    Все данные приходят через аргументы — нет internal state.
    Позволяет тестировать и бэктестить через один интерфейс.
    """

    def __init__(self, risk_level: RiskLevel, hl_config: dict = HL_CONFIG):
        self.profile = RISK_PROFILES[risk_level]
        self.hl_config = hl_config

    def evaluate_signal(
        self,
        signal: Signal,
        hl_state: HyperliquidState,
        asset_meta: AssetMeta,
        candles: list[dict],
        trade_history: list[TradeResult],
    ) -> TradeOrder | Reject:
        """Core entry point. Pure function."""

    # Sub-methods (public for testing)
    def validate_signal(self, signal, hl_state, asset_meta) -> Reject | None
    def check_circuit_breaker(self, trade_history, account_value) -> CircuitBreakerStatus
    def calculate_atr(self, candles, period=14) -> float
    def calculate_stop_loss(self, entry, side, atr) -> StopLoss
    def validate_stop_vs_liquidation(self, stop, liq_price, entry, side) -> bool
    def calculate_cumulative_risk(self, positions, new_risk, new_coin, acct_val) -> CumulativeRisk
    def calculate_position_size(self, acct_val, risk_pct, entry, stop, leverage, sz_dec) -> PositionSize
    def select_leverage(self, coin, max_lev, stop_dist_pct) -> int
    def estimate_funding_cost(self, size, entry, side, funding_rate) -> FundingEstimate
```

---

## 8. Edge Cases

| Case | Handling |
|------|----------|
| ATR = 0 (no volatility) | Reject — cannot calculate stop |
| Coin not in HL universe | Reject(INVALID_COIN) |
| `onlyIsolated` coin | Auto-switch margin_mode to "isolated" |
| Mark price deviates > 1% from signal entry | Reject(STALE_SIGNAL) |
| SL beyond liquidation price | Reject(LIQUIDATION_TOO_CLOSE) |
| Funding rate > 0.1% hourly | Warning + Reject if > threshold |
| Duplicate position (same coin+side) | Reject(DUPLICATE_POSITION) — v1 |
| Opposite position on same coin | Reject — manual close first |
| Available balance = 0 | Reject(INSUFFICIENT_MARGIN) |
| Cross margin cascade loss | Tracked via cascade buffer |
| HL API timeout/error | Raise MarketDataError, caller retries |
| sz_decimals rounding → size = 0 | Reject(POSITION_TOO_SMALL) |
| Max leverage for coin < safe leverage | Use coin's max, recalculate |

---

## 9. Observability

```python
@dataclass
class RiskDecisionLog:
    timestamp: float
    signal: Signal
    decision: str               # "approved" | "rejected"
    reject_reason: str | None

    # Market snapshot
    mark_price: float
    atr_value: float
    funding_rate: float

    # Calculations
    stop_price: float
    stop_distance_pct: float
    leverage_selected: int
    position_size: float
    notional_value: float
    margin_required: float

    # Risk state
    risk_per_trade_pct: float
    cumulative_risk_before_pct: float
    cumulative_risk_after_pct: float
    open_positions_count: int
    consecutive_losses: int
    daily_pnl_pct: float

    # Costs
    estimated_commission: float
    estimated_funding_24h: float

    # HL specific
    account_value: float
    available_balance: float
    liquidation_price: float | None
```

---

## 10. Testing

### 10.1 Unit Tests

```
# Signal validation
test_reject_invalid_coin
test_reject_stale_signal_entry_deviation
test_reject_stale_signal_age
test_reject_duplicate_position_same_side
test_reject_leverage_exceeded

# ATR & Stop-loss
test_atr_calculation_basic
test_atr_with_gaps
test_stop_loss_long / test_stop_loss_short
test_stop_loss_with_slippage_buffer
test_stop_vs_liquidation_too_close / _ok

# Position sizing
test_position_size_basic
test_position_size_with_commission_deduction
test_position_size_margin_limited
test_position_size_rounds_to_sz_decimals
test_position_size_below_minimum_rejected
test_position_size_btc_5_decimals / meme_0_decimals

# Leverage
test_leverage_selection_wide_stop / tight_stop
test_leverage_capped_by_coin_max / by_config

# Cumulative risk
test_cumulative_single_position
test_cumulative_correlated_penalty / uncorrelated_no_penalty
test_cumulative_budget_exceeded / cascade_buffer

# Funding
test_funding_long_positive_costs / short_positive_earns
test_funding_high_rate_reject

# Circuit breaker
test_cb_inactive / soft_reduces / hard_rejects / daily_loss
test_cb_win_resets

# Full flow
test_full_approve_btc_long
test_full_reject_risk_exceeded
test_full_reduced_risk_after_losses
```

### 10.2 Property-Based (Hypothesis)

```
risk_amount <= account_value * risk_per_trade
cumulative_risk <= max_cumulative_risk (after approve)
stop_loss closer to entry than liquidation_price
position_size * entry >= min_order_value (if approved)
position_size rounded to sz_decimals
margin_required <= available_balance (if approved)
leverage <= min(config_max, coin_max)
```

---

## 11. Implementation Plan

### Phase 1 — Core Logic (Week 1-2)
- [ ] Data models (dataclasses)
- [ ] ATR from HL candles
- [ ] Stop-loss + liq validation
- [ ] Position sizing with sz_decimals
- [ ] Leverage selection
- [ ] Simple cumulative risk
- [ ] Circuit breaker
- [ ] `evaluate_signal()` orchestrator
- [ ] Unit tests
- [ ] HL market data adapter (read-only)

### Phase 2 — Integration (Week 3-4)
- [ ] Testnet connection + user_state
- [ ] Candle fetching + caching
- [ ] Correlation groups
- [ ] Funding cost estimation + reject
- [ ] Cross margin cascade buffer
- [ ] Decision logging
- [ ] Integration tests (testnet)
- [ ] Property-based tests

### Phase 3 — Hardening (Week 5-6)
- [ ] Order execution wrapper
- [ ] WebSocket for real-time prices
- [ ] Fee tier awareness
- [ ] Isolated margin for onlyIsolated coins
- [ ] Backtest adapter
- [ ] Metrics dashboard

---

## 12. Dependencies

```
hyperliquid-python-sdk        # Official HL SDK
eth-account                   # Wallet/signing
numpy                         # ATR
pydantic >= 2.0               # Validation (optional)
pytest + hypothesis           # Testing
```

---

## 13. Open Questions

1. **Margin mode** — Cross для мажоров, isolated для мемов? Или единый подход?

2. **Trade history storage** — SQLite (простой) vs PostgreSQL (multi-user) vs in-memory?

3. **Signal deduplication** — Reject если уже есть позиция на coin+side? Или разрешить добавление через N часов?

4. **API wallet** — Рекомендуется для бота (не может выводить средства). Использовать всегда?

5. **Rate limits** — HL 1200 req/min. WebSocket для цен, REST для ордеров?

6. **Testnet quirks** — Другие тикеры/ликвидность. Нужен testnet-specific flag?

7. **Fee tier tracking** — Отслеживать 14d volume для динамической комиссии или зафиксировать Tier 0?
