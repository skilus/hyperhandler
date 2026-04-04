# Position Sizing Logic

This document describes how hyperhandler calculates position size, stop-loss, and leverage in **MANAGED** risk mode. In **MANUAL** mode the system only validates signal parameters against risk limits without modifying them.

## Overview

```
Signal ──▶ Circuit Breaker ──▶ ATR ──▶ Stop-Loss ──▶ Leverage ──▶ Position Size ──▶ TradeOrder
              │                                                       │
              │ risk_multiplier                                       │ budget constraint
              └───────────────────────────────────────────────────────┘
```

The pipeline takes a trading signal and produces a fully parameterized `TradeOrder` with size, leverage, stop-loss, and risk metrics — or rejects the signal with a specific reason.

## Step-by-Step Pipeline

### 1. Circuit Breaker Check

Before any calculation, the system checks trading eligibility.

**Inputs:** recent `TradeResult` history, current `account_value`.

| Condition | Level | Effect |
|-----------|-------|--------|
| Daily loss ≥ limit | HARD | Trading blocked (`risk_multiplier = 0`) |
| N consecutive losses ≥ hard threshold | HARD | Trading blocked |
| N consecutive losses ≥ soft threshold | SOFT | `risk_multiplier = 0.5` (half risk) |
| Otherwise | NONE | `risk_multiplier = 1.0` (normal) |

Thresholds per profile:

| Profile | Soft Threshold | Hard Threshold | Daily Loss Limit |
|---------|---------------|----------------|------------------|
| LOW     | 2 losses      | 4 losses       | 2%               |
| MEDIUM  | 3 losses      | 5 losses       | 3%               |
| HIGH    | 3 losses      | 6 losses       | 5%               |

The `risk_multiplier` from this step propagates into position sizing (step 5).

### 2. ATR Calculation

**Purpose:** Measure recent volatility to set a data-driven stop-loss.

**Inputs:** Historical candles from Hyperliquid API.

**Candle interval** depends on the signal's `horizon`:

| Horizon    | Interval | ATR Multiplier |
|------------|----------|----------------|
| `scalp`    | 15m      | 1.2×           |
| `intraday` | 1h       | 1.5×           |
| `swing`    | 4h       | 2.0×           |
| `position` | 1d       | 2.5×           |

**Algorithm:**

1. Compute True Range for each candle:
   ```
   TR = max(High - Low, |High - PrevClose|, |Low - PrevClose|)
   ```
2. If candle count ≥ ATR period (14): use **EMA** with `α = 2 / (period + 1)`
3. If candle count < period: fall back to **simple average** of available TRs
4. Reject if fewer than 2 candles available

**Output:** ATR value in price units (e.g., ATR = $500 for BTC means average 14-period range is $500).

### 3. Stop-Loss Calculation

**Purpose:** Set stop-loss at a volatility-adjusted distance from entry.

**Formula:**

```
base_distance    = ATR × horizon_multiplier
slippage_buffer  = base_distance × 0.5%
stop_distance    = base_distance + slippage_buffer

Long:  stop_price = entry_price - stop_distance
Short: stop_price = entry_price + stop_distance
```

**Example:** BTC at $50,000, ATR = $500, intraday horizon:
```
base_distance   = 500 × 1.5 = $750
slippage_buffer = 750 × 0.005 = $3.75
stop_distance   = $753.75
stop_price      = 50000 - 753.75 = $49,246.25
distance_pct    = 753.75 / 50000 = 1.5075%
```

### 4. Leverage Selection

**Purpose:** Choose leverage so that the liquidation price is safely beyond the stop-loss.

**Step 4a — Calculate max safe leverage:**

```
max_safe_leverage = floor(1 / (stop_distance_pct × 1.5))
```

The 1.5× safety factor ensures liquidation is at least 50% further than the stop.

**Step 4b — Apply constraints:**

```
leverage = min(max_safe_leverage, coin_max_leverage, profile_max_leverage)
leverage = max(leverage, 1)  // floor at 1×
```

**Step 4c — Validate stop vs. liquidation:**

Estimated liquidation price:
```
liq_distance_pct = max(1/leverage - 0.5%, 1%)    // 0.5% = HL maintenance margin

Long:  liq_price = entry × (1 - liq_distance_pct)
Short: liq_price = entry × (1 + liq_distance_pct)
```

Validation rule: stop must be at least **2% of entry price** away from liquidation.

If validation fails, leverage is reduced further using `select_leverage_for_stop()`:
```
required_liq_distance = stop_distance_pct + 2%
leverage = floor(1 / (required_liq_distance + 0.5%))
```

**Example** (continuing from step 3):
```
stop_distance_pct = 1.5075%
max_safe = floor(1 / (0.015075 × 1.5)) = floor(44.2) = 44
coin_max = 50, profile_max (MEDIUM) = 10
→ leverage = 10

liq_distance = 1/10 - 0.005 = 0.095 = 9.5%
liq_price = 50000 × 0.905 = $45,250
buffer = |49246.25 - 45250| / 50000 = 7.99% ≥ 2% ✓
```

### 5. Position Size Calculation

**Purpose:** Size the position so that hitting the stop-loss loses exactly the risk budget.

**Step 5a — Determine risk budget:**

```
confidence_factor = clamp(signal.confidence, 0.3, 1.0)    // default 1.0
risk_pct          = profile.risk_per_trade × confidence_factor × cb_risk_multiplier
risk_amount       = account_value × risk_pct
```

| Profile | Base Risk | × Confidence 0.5 | × CB Soft (0.5) | Both |
|---------|-----------|-------------------|-----------------|------|
| LOW     | 1%        | 0.5%              | 0.5%            | 0.25%|
| MEDIUM  | 2%        | 1.0%              | 1.0%            | 0.50%|
| HIGH    | 3%        | 1.5%              | 1.5%            | 0.75%|

**Step 5b — Apply cumulative budget constraint:**

Before sizing, the system checks how much risk budget remains across the portfolio:

```
available_budget = max_cumulative_risk × account_value - existing_adjusted_risk
risk_amount = min(risk_amount, available_budget)
```

**Step 5c — Calculate raw size:**

```
stop_distance = |entry_price - stop_price|
raw_size      = risk_amount / stop_distance
```

**Step 5d — Deduct commission:**

```
notional   = raw_size × entry_price
commission = notional × taker_fee × 2        // open + close, taker_fee = 0.045%
adjusted_risk = risk_amount - commission
adjusted_size = adjusted_risk / stop_distance
```

**Step 5e — Apply margin constraint:**

```
margin_required = (adjusted_size × entry_price) / leverage
if margin_required > available_balance:
    adjusted_size = (available_balance × leverage) / entry_price
```

**Step 5f — Round down:**

Size is rounded down to the asset's `szDecimals` (e.g., 5 for BTC = 0.00001 precision).

**Step 5g — Final validation:**

```
final_notional = adjusted_size × entry_price
if final_notional < $10:    → REJECT (POSITION_TOO_SMALL)
```

**Example** (continuing, $10,000 account, MEDIUM, confidence 0.8):
```
risk_pct    = 0.02 × 0.8 × 1.0 = 1.6%
risk_amount = 10000 × 0.016 = $160
stop_distance = |50000 - 49246.25| = $753.75
raw_size    = 160 / 753.75 = 0.21224 BTC
notional    = 0.21224 × 50000 = $10,612
commission  = 10612 × 0.00045 × 2 = $9.55
adj_risk    = 160 - 9.55 = $150.45
adj_size    = 150.45 / 753.75 = 0.19960 BTC
margin      = 0.19960 × 50000 / 10 = $998 (< $5000 available ✓)
rounded     = 0.19960 (szDecimals=5)
notional    = $9,980 (< $10 min? No → ✓)
```

### 6. Cumulative Risk Check

**Purpose:** Ensure total portfolio risk stays within limits.

**Correlation groups** (positions in the same group receive a penalty):

| Group       | Assets                        |
|-------------|-------------------------------|
| btc-major   | BTC, ETH                      |
| l1-alt      | SOL, AVAX, SUI, APT, SEI      |
| defi        | AAVE, UNI, MKR, CRV, DYDX     |
| meme        | DOGE, SHIB, PEPE, WIF, BONK   |
| ai          | FET, RNDR, TAO, NEAR           |

Assets not in any group are treated as independent.

**Calculation:**

```
For each correlation group with N > 1 positions:
    penalty = 1 + (N - 1) × correlation_factor
    group_adjusted_risk = group_raw_risk × penalty
    cascade_buffer += group_raw_risk × 10%

total_adjusted = sum(group_adjusted_risks) + cascade_buffer + new_position_risk
```

Correlation factors: LOW = 0.4, MEDIUM = 0.3, HIGH = 0.25.

**Limits:**

| Profile | Max Cumulative Risk |
|---------|-------------------|
| LOW     | 4%                |
| MEDIUM  | 6%                |
| HIGH    | 10%               |

If `total_adjusted > account_value × max_cumulative_risk` → REJECT.

### 7. Funding Cost Check

**Purpose:** Reject if funding payments would erode the risk budget.

```
notional         = size × entry_price
hourly_payment   = notional × funding_rate
projected_24h    = hourly_payment × 24        // long pays, short receives (positive rate)
funding_risk_pct = projected_24h / risk_amount
```

If `funding_risk_pct > max_funding_risk_pct` → REJECT.

| Profile | Max Funding Risk % |
|---------|--------------------|
| LOW     | 30%                |
| MEDIUM  | 50%                |
| HIGH    | 70%                |

## Output: TradeOrder

When all checks pass, the system produces a `TradeOrder`:

```
TradeOrder:
  coin:                    "BTC"
  side:                    "long"
  size:                    0.19960          ← calculated
  entry_price:             50000.00
  leverage:                10               ← selected
  stop_loss:               49246.25         ← ATR-based
  margin_mode:             "cross"
  risk_amount:             $150.45          ← $ at risk
  risk_pct:                1.50%            ← % of account
  cumulative_risk_after:   1.50%
  estimated_liquidation:   $45,250
  estimated_commission:    $9.55
  estimated_funding_24h:   $2.40
  margin_required:         $998.00
  risk_mode:               MANAGED
  size_source:             "calculated"
  sl_source:               "calculated"
```

## Rejection Reasons

| Reason | When | Suggested Action |
|--------|------|------------------|
| `circuit_breaker_hard` | Too many consecutive losses | `manual_reset` |
| `daily_loss_limit` | Daily loss exceeds limit | `wait` |
| `stale_signal` | Entry price deviation > 1% | `wait` |
| `duplicate_position` | Same-side position already open | `wait` |
| `max_positions_reached` | At position count limit | `close_positions` |
| `atr_unavailable` | Not enough candles or zero ATR | `wait` |
| `liquidation_too_close` | Stop beyond liquidation | `reduce_risk` |
| `risk_budget_exceeded` | Cumulative risk over limit | `reduce_risk` / `close_positions` |
| `insufficient_margin` | Not enough free margin | `reduce_risk` |
| `position_too_small` | Notional < $10 | `wait` |
| `leverage_exceeded` | Leverage over limit (MANUAL) | `reduce_risk` |
| `high_funding_cost` | Funding eats too much risk | `wait` |

## MANUAL vs MANAGED Comparison

| Aspect | MANUAL | MANAGED |
|--------|--------|---------|
| Position size | From signal | Calculated from risk budget |
| Stop-loss | From signal (optional) | ATR-based (always set) |
| Leverage | From signal (validated) | Auto-selected for stop distance |
| Risk per trade | Validated against limits | Precisely controlled |
| ATR / Candles | Not needed (unless no SL) | Required |
| Use case | Experienced traders | Automated / strategy signals |

## Source Files

| File | Responsibility |
|------|---------------|
| `risk/manager.py` | Pipeline orchestration, mode routing |
| `risk/calculator.py` | ATR, stop-loss, leverage, sizing, cumulative risk |
| `risk/circuit_breaker.py` | Loss tracking, soft/hard stops |
| `risk/config.py` | Risk profiles, ATR settings, HL config, correlation map |
| `risk/collector.py` | Trade result collection for CB |
| `models/risk.py` | All data models (TradeOrder, TradeResult, etc.) |
