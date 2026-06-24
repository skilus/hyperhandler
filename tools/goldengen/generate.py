"""Golden vector generator for the Go port (SPEC-007, Phase 0).

Produces deterministic reference vectors for the byte-identical crypto core:

  testdata/golden/signer.json  — msgpack bytes, action hash, EIP-712 signature
  testdata/golden/hd.json      — BIP-39/44 mnemonic -> address/key derivation

Decision D5: signatures are taken from the *official* Hyperliquid Python SDK
(hyperliquid.utils.signing) as an independent oracle. Every vector is then
cross-checked against our own hyperhandler.signer.Signer to prove the local
reference matches the oracle before we freeze it for the Go implementation.

All keys/mnemonics here are public test values. NO real secrets.
"""

from __future__ import annotations

import json
from decimal import Decimal
from pathlib import Path
from typing import Any

import msgpack
from eth_account import Account
from eth_utils import to_hex

from hyperhandler.client.order_builder import OrderBuilder
from hyperhandler.models import OrderSide, OrderType, TradingSignal
from hyperhandler.models.order import Position
from hyperhandler.models.risk import RiskLevel, RiskReject
from hyperhandler.models.signal import SignalHorizon
from hyperhandler.risk import HLConfig, RISK_PROFILES, RiskCalculator

# Official Hyperliquid SDK — the oracle (D5).
from hyperliquid.utils.signing import (
    action_hash as hl_action_hash,
    construct_phantom_agent as hl_construct_phantom_agent,
    l1_payload as hl_l1_payload,
    sign_inner as hl_sign_inner,
)

# Our own implementation — must match the oracle byte-for-byte.
from hyperhandler.signer import Signer

Account.enable_unaudited_hdwallet_features()

REPO_ROOT = Path(__file__).resolve().parents[2]
GOLDEN_DIR = REPO_ROOT / "testdata" / "golden"

# Public test keys (NEVER real secrets).
KEY_A = "0x" + "a" * 64          # matches tests/unit/test_signer.py
KEY_B = "0x" + "1" * 64
VAULT = "0x1234567890123456789012345678901234567890"

# Hardhat/Foundry well-known test mnemonic — public, deterministic.
TEST_MNEMONIC = "test test test test test test test test test test test junk"
BIP44_BASE = "m/44'/60'/0'/0"

# A full order_builder-shaped action (entry + SL + TP) to exercise nested
# msgpack key ordering (a,b,p,s,r,t) that the Go structs must reproduce.
FULL_ORDER_ACTION: dict[str, Any] = {
    "type": "order",
    "orders": [
        {
            "a": 0,
            "b": True,
            "p": "67500",
            "s": "0.1",
            "r": False,
            "t": {"limit": {"tif": "Ioc"}},
        },
        {
            "a": 0,
            "b": False,
            "p": "63787.5",
            "s": "0.1",
            "r": True,
            "t": {
                "trigger": {
                    "isMarket": True,
                    "triggerPx": "64000",
                    "tpsl": "sl",
                }
            },
        },
        {
            "a": 0,
            "b": False,
            "p": "71437.5",
            "s": "0.1",
            "r": True,
            "t": {
                "trigger": {
                    "isMarket": True,
                    "triggerPx": "71000",
                    "tpsl": "tp",
                }
            },
        },
    ],
    "grouping": "normalTpsl",
}

# (label, private_key, action, nonce, vault, expires, is_mainnet)
SIGNER_CASES: list[tuple[str, str, dict[str, Any], int, str | None, int | None, bool]] = [
    (
        "order_mainnet",  # the test_signer.py U-SGN-01 gate example
        KEY_A,
        {
            "type": "order",
            "orders": [{"a": 0, "b": True, "p": "67500", "s": "0.1", "r": False}],
            "grouping": "na",
        },
        1699999999999,
        None,
        None,
        True,
    ),
    (
        "simple_testnet",
        KEY_A,
        {"type": "test", "data": "value"},
        1699999999999,
        None,
        None,
        False,
    ),
    (
        "order_with_vault",
        KEY_A,
        {"type": "order", "orders": []},
        1699999999999,
        VAULT,
        None,
        True,
    ),
    (
        "order_with_expires",
        KEY_A,
        {"type": "order", "orders": []},
        1699999999999,
        None,
        1699999999999 + 60000,
        True,
    ),
    (
        "order_vault_and_expires",
        KEY_B,
        {"type": "order", "orders": []},
        1234567890000,
        VAULT,
        1234567890000 + 60000,
        False,
    ),
    (
        "full_order_tpsl",
        KEY_B,
        FULL_ORDER_ACTION,
        1700000000000,
        None,
        None,
        True,
    ),
]


def _oracle_signature(
    key: str, action: dict[str, Any], nonce: int,
    vault: str | None, expires: int | None, is_mainnet: bool,
) -> dict[str, Any]:
    """Sign via the official SDK (D5 oracle)."""
    wallet = Account.from_key(key)
    h = hl_action_hash(action, vault, nonce, expires)
    phantom = hl_construct_phantom_agent(h, is_mainnet)
    payload = hl_l1_payload(phantom)
    # sign_inner already returns {r, s} as 0x-hex strings and v as int.
    return hl_sign_inner(wallet, payload)


def build_signer_golden() -> dict[str, Any]:
    vectors = []
    for label, key, action, nonce, vault, expires, is_mainnet in SIGNER_CASES:
        oracle_hash = hl_action_hash(action, vault, nonce, expires)
        oracle_sig = _oracle_signature(key, action, nonce, vault, expires, is_mainnet)

        # Cross-check: our own signer must reproduce the oracle exactly.
        signer = Signer(key, is_mainnet=is_mainnet)
        ours_hash = signer._create_action_hash(action, vault, nonce, expires)
        ours_payload = signer.sign_action(
            action, nonce=nonce, vault_address=vault, expires_after=expires
        )
        assert ours_hash == oracle_hash, f"{label}: action_hash mismatch"
        assert ours_payload["signature"] == oracle_sig, f"{label}: signature mismatch"

        vectors.append({
            "label": label,
            "private_key": key,
            "address": signer.address,
            "is_mainnet": is_mainnet,
            "action": action,
            "nonce": nonce,
            "vault_address": vault,
            "expires_after": expires,
            "msgpack_hex": msgpack.packb(action).hex(),
            "action_hash": to_hex(oracle_hash),
            "signature": oracle_sig,
        })
    return {
        "_comment": "Golden signer vectors. Oracle: official hyperliquid SDK. "
                    "Cross-checked against hyperhandler.signer. Public test keys only.",
        "vectors": vectors,
    }


def build_hd_golden(count: int = 5) -> dict[str, Any]:
    accounts = []
    for i in range(count):
        path = f"{BIP44_BASE}/{i}"
        acct = Account.from_mnemonic(TEST_MNEMONIC, account_path=path)
        accounts.append({
            "index": i,
            "path": path,
            "address": acct.address,
            "private_key": acct.key.hex() if acct.key.hex().startswith("0x")
            else "0x" + acct.key.hex(),
        })
    return {
        "_comment": "Golden HD vectors. Public Hardhat test mnemonic only.",
        "mnemonic": TEST_MNEMONIC,
        "base_path": BIP44_BASE,
        "passphrase": "",
        "accounts": accounts,
    }


# Frozen float-path cases (SPEC-007 risk #1/#3). Prices chosen to exercise the
# 5-significant-figure formatting and Python's half-to-even round() boundaries.
SLIPPAGE_DEFAULT = Decimal("0.005")
# (price, is_buy, sz_decimals, is_spot)
SLIPPAGE_CASES: list[tuple[str, bool, int, bool]] = [
    ("67500", True, 3, False),
    ("67500", False, 3, False),
    ("1700.55", True, 2, False),
    ("1700.55", False, 2, False),
    ("0.123456", True, 0, False),
    ("0.123456", False, 0, False),
    ("2.675", True, 0, False),     # classic half-to-even float boundary
    ("2.675", False, 0, False),
    ("123456.789", True, 1, False),
    ("123456.789", False, 4, False),
    ("9.99995", True, 0, False),   # rounds up across a 9-cascade at 5 sig figs
    ("0.0000456789", True, 2, True),
    ("31415.926", False, 2, False),
    ("100000", True, 0, False),
]

FORMAT_CASES: list[str] = [
    "1700",
    "1700.0",
    "1700.00000000",
    "0.1",
    "0.10000000",
    "1700.5",
    "0.123456789",     # quantized to 8 decimals
    "0.000000005",     # rounds at the 8th decimal (half-up via quantize default)
    "100",
    "0",
    "63787.5",
    "71437.5",
    "12345.6789",
]


# Full signal -> order-payload cases. These lock the *assembly* (key order
# a,b,p,s,r,t; entry + SL/TP legs; grouping) on top of the frozen float path.
# Each is hashed by the official SDK oracle and rebuilt in Go from the same
# signal recipe. nonce is fixed for determinism.
PAYLOAD_NONCE = 1700000000000
# (label, signal_kwargs, asset_index, current_price | None, sz_decimals)
PAYLOAD_CASES: list[tuple[str, dict[str, Any], int, str | None, int]] = [
    (
        "limit_long_entry_only",
        dict(pair="BTC", side=OrderSide.LONG, order_type=OrderType.LIMIT,
             entry_price=Decimal("67500"), size=Decimal("0.1"), leverage=5),
        0, None, 0,
    ),
    (
        "limit_long_full_tpsl",
        dict(pair="BTC", side=OrderSide.LONG, order_type=OrderType.LIMIT,
             entry_price=Decimal("67500"), size=Decimal("0.1"),
             stop_loss=Decimal("66000"), take_profit=Decimal("70000")),
        0, None, 0,
    ),
    (
        "limit_short_sl_only",
        dict(pair="BTC", side=OrderSide.SHORT, order_type=OrderType.LIMIT,
             entry_price=Decimal("67500"), size=Decimal("0.1"),
             stop_loss=Decimal("69000")),
        0, None, 0,
    ),
    (
        "limit_long_tp_only",
        dict(pair="BTC", side=OrderSide.LONG, order_type=OrderType.LIMIT,
             entry_price=Decimal("67500"), size=Decimal("0.1"),
             take_profit=Decimal("70000")),
        0, None, 0,
    ),
    (
        "market_long",
        dict(pair="ETH", side=OrderSide.LONG, order_type=OrderType.MARKET,
             size=Decimal("1.0"), leverage=10),
        1, "3500", 0,
    ),
    (
        "market_short",
        dict(pair="ETH", side=OrderSide.SHORT, order_type=OrderType.MARKET,
             size=Decimal("1.0")),
        1, "3500", 0,
    ),
]


def _payload_signal_json(kwargs: dict[str, Any]) -> dict[str, Any]:
    """Serialize the signal recipe so the Go test can rebuild it."""
    out: dict[str, Any] = {
        "pair": kwargs["pair"],
        "side": kwargs["side"].value,
        "order_type": kwargs["order_type"].value,
        "size": str(kwargs["size"]),
        "leverage": kwargs.get("leverage", 5),
    }
    for opt in ("entry_price", "stop_loss", "take_profit"):
        out[opt] = str(kwargs[opt]) if kwargs.get(opt) is not None else None
    return out


def build_payload_golden() -> list[dict[str, Any]]:
    builder = OrderBuilder(slippage=SLIPPAGE_DEFAULT)
    payloads = []
    for label, kwargs, asset_index, current_price, sz_decimals in PAYLOAD_CASES:
        signal = TradingSignal(**kwargs)
        action = builder.build_order_payload(
            signal,
            asset_index=asset_index,
            current_price=Decimal(current_price) if current_price else None,
            sz_decimals=sz_decimals,
        )
        oracle_hash = hl_action_hash(action, None, PAYLOAD_NONCE, None)
        payloads.append({
            "label": label,
            "signal": _payload_signal_json(kwargs),
            "asset_index": asset_index,
            "current_price": current_price,
            "sz_decimals": sz_decimals,
            "nonce": PAYLOAD_NONCE,
            "msgpack_hex": msgpack.packb(action).hex(),
            "action_hash": to_hex(oracle_hash),
        })
    return payloads


def build_order_golden() -> dict[str, Any]:
    builder = OrderBuilder(slippage=SLIPPAGE_DEFAULT)

    slippage = []
    for price, is_buy, sz_decimals, is_spot in SLIPPAGE_CASES:
        result = builder._slippage_price(
            Decimal(price), is_buy, sz_decimals, is_spot=is_spot
        )
        slippage.append({
            "price": price,
            "is_buy": is_buy,
            "sz_decimals": sz_decimals,
            "is_spot": is_spot,
            "slippage": str(SLIPPAGE_DEFAULT),
            "result": str(result),
            "formatted": builder._format_price(result),
        })

    formatting = []
    for value in FORMAT_CASES:
        formatting.append({
            "value": value,
            "formatted_price": builder._format_price(Decimal(value)),
            "formatted_size": builder._format_size(Decimal(value)),
        })

    return {
        "_comment": "Golden order-builder float-path vectors (FROZEN). "
                    "slippage = _slippage_price; formatting = _format_price/_format_size; "
                    "payloads = build_order_payload msgpack/action_hash.",
        "slippage": slippage,
        "formatting": formatting,
        "payloads": build_payload_golden(),
    }


# ---------------------------------------------------------------------------
# Risk calculator golden vectors (SPEC-007 Phase 4).
#
# Exact reference values for the Decimal-heavy risk math: ATR EMA (alpha=2/15),
# stop-loss, liquidation, leverage selection, position sizing (incl. rejects),
# cumulative risk (correlation penalty + cascade buffer), funding, round-down.
# All computed with the MEDIUM profile + default HLConfig, matching the Go
# risk.NewCalculator(GetProfile(RiskMedium), DefaultHLConfig()).
# ---------------------------------------------------------------------------

RISK_CALC = RiskCalculator(RISK_PROFILES[RiskLevel.MEDIUM], HLConfig())


def _candle(h: str, l: str, c: str) -> dict[str, str]:
    return {"h": h, "l": l, "c": c}


def _consistent_candles(n: int) -> list[dict[str, str]]:
    return [_candle("102", "98", "100") for _ in range(n)]


def _sample_candles() -> list[dict[str, str]]:
    """The 20-candle fixture from test_risk_calculator (string OHLC)."""
    base = Decimal("100")
    out = []
    for i in range(20):
        high = base + Decimal("2") + Decimal(str(i % 3))
        low = base - Decimal("2") - Decimal(str(i % 2))
        close = base + Decimal(str(i % 2))
        out.append(_candle(str(high), str(low), str(close)))
    return out


ATR_CASES = [
    ("two_candles", [_candle("102", "98", "101"), _candle("104", "99", "103")], 14),
    ("short_series_sma", [
        _candle("102", "98", "100"),
        _candle("103", "97", "101"),
        _candle("105", "99", "102"),
    ], 14),
    ("consistent_tr_ema", _consistent_candles(20), 14),
    ("sample_fixture", _sample_candles(), 14),
]

# (entry, side, atr, horizon)
STOP_LOSS_CASES = [
    ("100", "long", "5", SignalHorizon.SCALP),
    ("100", "long", "5", SignalHorizon.INTRADAY),
    ("100", "long", "5", SignalHorizon.SWING),
    ("100", "long", "5", SignalHorizon.POSITION),
    ("100", "short", "5", SignalHorizon.INTRADAY),
    ("50000", "long", "1500", SignalHorizon.SWING),
    ("100", "long", "10", SignalHorizon.INTRADAY),
]

# (entry, leverage, side)
LIQ_CASES = [
    ("100", 5, "long"),
    ("100", 10, "long"),
    ("100", 20, "long"),
    ("100", 100, "long"),
    ("100", 10, "short"),
    ("50000", 10, "long"),
]

# (stop, liq, entry, side)
VALIDATE_STOP_CASES = [
    ("95", "90", "100", "long"),
    ("89", "90", "100", "long"),
    ("105", "110", "100", "short"),
    ("111", "110", "100", "short"),
    ("90.5", "90", "100", "long"),
    ("93", "90", "100", "long"),
]

# (stop_distance_pct, max_coin)
SELECT_LEVERAGE_CASES = [
    ("0.15", 50),
    ("0.01", 5),
    ("0.01", 50),
    ("0.5", 50),
    ("0", 50),
]

# (stop, entry, side, max_coin)
SELECT_LEVERAGE_FOR_STOP_CASES = [
    ("95", "100", "long", 50),
    ("105", "100", "short", 50),
    ("90", "100", "long", 3),
]

# (label, kwargs) for calculate_position_size
POSITION_SIZE_CASES = [
    ("normal", dict(account_value="10000", available_balance="5000",
                    entry_price="100", stop_price="95", leverage=10, sz_decimals=2)),
    ("margin_constrained", dict(account_value="10000", available_balance="100",
                                entry_price="100", stop_price="95", leverage=10, sz_decimals=2)),
    ("too_small", dict(account_value="50", available_balance="5",
                       entry_price="50000", stop_price="45000", leverage=2, sz_decimals=5)),
    ("zero_stop", dict(account_value="10000", available_balance="5000",
                       entry_price="100", stop_price="100", leverage=10, sz_decimals=2)),
    ("confidence_full", dict(account_value="10000", available_balance="5000",
                             entry_price="100", stop_price="95", leverage=10, sz_decimals=2,
                             confidence=1.0)),
    ("confidence_half", dict(account_value="10000", available_balance="5000",
                             entry_price="100", stop_price="95", leverage=10, sz_decimals=2,
                             confidence=0.5)),
    ("confidence_clamped_low", dict(account_value="10000", available_balance="5000",
                                    entry_price="100", stop_price="95", leverage=10, sz_decimals=2,
                                    confidence=0.1)),
    ("risk_multiplier_half", dict(account_value="10000", available_balance="5000",
                                  entry_price="100", stop_price="95", leverage=10, sz_decimals=2,
                                  risk_multiplier="0.5")),
    ("max_risk_budget", dict(account_value="10000", available_balance="5000",
                             entry_price="100", stop_price="95", leverage=10, sz_decimals=2,
                             max_risk_amount="50")),
    ("rounding_3dp", dict(account_value="10000", available_balance="5000",
                          entry_price="100", stop_price="95", leverage=10, sz_decimals=3)),
]


def _pos(coin: str, risk_amount: str | None, size: str = "1",
         entry: str = "100") -> Position:
    return Position(
        coin=coin,
        size=Decimal(size),
        entry_price=Decimal(entry),
        position_value=Decimal(size) * Decimal(entry),
        unrealized_pnl=Decimal("0"),
        leverage=10,
        leverage_type="cross",
        risk_amount=Decimal(risk_amount) if risk_amount is not None else None,
    )


# (label, positions, new_risk_amount, new_coin, account_value)
CUMULATIVE_RISK_CASES = [
    ("single_btc", [_pos("BTC", "500")], "0", "BTC", "10000"),
    ("correlated_btc_eth", [_pos("BTC", "100"), _pos("ETH", "100")], "0", "SOL", "10000"),
    ("independent_doge_aave", [_pos("DOGE", "50")], "50", "AAVE", "10000"),
    ("exceeds_limit", [_pos("BTC", "500")], "500", "ETH", "10000"),
    ("empty_budget", [], "0", "BTC", "10000"),
    ("three_correlated", [_pos("SOL", "100"), _pos("AVAX", "100"), _pos("SUI", "100")],
     "0", "APT", "10000"),
]

# (size, entry, side, funding_rate, risk_amount, hold_hours)
FUNDING_CASES = [
    ("1", "50000", "long", "0.0001", "500", 24),
    ("1", "50000", "short", "0.0001", "500", 24),
    ("1", "50000", "long", "0.001", "500", 24),
    ("2", "3000", "short", "0.0002", "300", 8),
]

# (value, decimals)
ROUND_DOWN_CASES = [
    ("1.999", 2),
    ("1.991", 2),
    ("9.99", 0),
    ("0.123456789", 8),
    ("0.000000005", 8),
    ("1234.5678", 3),
    ("100", 0),
]


def build_risk_golden() -> dict[str, Any]:
    atr = []
    for label, candles, period in ATR_CASES:
        atr.append({
            "label": label,
            "candles": candles,
            "period": period,
            "result": str(RISK_CALC.calculate_atr(candles, period)),
        })

    stop_loss = []
    for entry, side, atr_val, horizon in STOP_LOSS_CASES:
        r = RISK_CALC.calculate_stop_loss(
            Decimal(entry), side, Decimal(atr_val), horizon
        )
        stop_loss.append({
            "entry": entry, "side": side, "atr": atr_val, "horizon": horizon.value,
            "price": str(r.price), "distance": str(r.distance),
            "distance_pct": str(r.distance_pct), "atr_value": str(r.atr_value),
            "atr_multiplier": str(r.atr_multiplier),
        })

    liquidation = []
    for entry, leverage, side in LIQ_CASES:
        liquidation.append({
            "entry": entry, "leverage": leverage, "side": side,
            "result": str(RISK_CALC.estimate_liquidation_price(
                Decimal(entry), leverage, side)),
        })

    validate_stop = []
    for stop, liq, entry, side in VALIDATE_STOP_CASES:
        validate_stop.append({
            "stop": stop, "liq": liq, "entry": entry, "side": side,
            "valid": RISK_CALC.validate_stop_vs_liquidation(
                Decimal(stop), Decimal(liq), Decimal(entry), side),
        })

    select_leverage = []
    for spct, max_coin in SELECT_LEVERAGE_CASES:
        r = RISK_CALC.select_leverage(Decimal(spct), max_coin)
        select_leverage.append({
            "stop_distance_pct": spct, "max_coin": max_coin,
            "leverage": r.leverage, "max_safe": r.max_safe,
            "max_coin_out": r.max_coin, "max_config": r.max_config, "reason": r.reason,
        })

    select_leverage_for_stop = []
    for stop, entry, side, max_coin in SELECT_LEVERAGE_FOR_STOP_CASES:
        r = RISK_CALC.select_leverage_for_stop(
            Decimal(stop), Decimal(entry), side, max_coin)
        select_leverage_for_stop.append({
            "stop": stop, "entry": entry, "side": side, "max_coin": max_coin,
            "leverage": r.leverage, "max_safe": r.max_safe,
            "max_coin_out": r.max_coin, "max_config": r.max_config, "reason": r.reason,
        })

    position_size = []
    for label, kwargs in POSITION_SIZE_CASES:
        call = dict(
            account_value=Decimal(kwargs["account_value"]),
            available_balance=Decimal(kwargs["available_balance"]),
            entry_price=Decimal(kwargs["entry_price"]),
            stop_price=Decimal(kwargs["stop_price"]),
            leverage=kwargs["leverage"],
            sz_decimals=kwargs["sz_decimals"],
        )
        if "confidence" in kwargs:
            call["confidence"] = kwargs["confidence"]
        if "risk_multiplier" in kwargs:
            call["risk_multiplier"] = Decimal(kwargs["risk_multiplier"])
        if "max_risk_amount" in kwargs:
            call["max_risk_amount"] = Decimal(kwargs["max_risk_amount"])
        result = RISK_CALC.calculate_position_size(**call)
        entry = {"label": label, "input": kwargs}
        if isinstance(result, RiskReject):
            entry["is_reject"] = True
            entry["reject_reason"] = result.reason.value
        else:
            entry["is_reject"] = False
            entry["result"] = {
                "size": str(result.size), "notional": str(result.notional),
                "margin_required": str(result.margin_required),
                "risk_amount": str(result.risk_amount), "risk_pct": str(result.risk_pct),
                "commission_estimate": str(result.commission_estimate),
            }
        position_size.append(entry)

    cumulative_risk = []
    for label, positions, new_risk, new_coin, account in CUMULATIVE_RISK_CASES:
        r = RISK_CALC.calculate_cumulative_risk(
            positions, Decimal(new_risk), new_coin, Decimal(account))
        cumulative_risk.append({
            "label": label,
            "positions": [
                {"coin": p.coin, "risk_amount": str(p.risk_amount)
                 if p.risk_amount is not None else None}
                for p in positions
            ],
            "new_risk_amount": new_risk, "new_coin": new_coin, "account_value": account,
            "raw_risk": str(r.raw_risk), "adjusted_risk": str(r.adjusted_risk),
            "risk_pct": str(r.risk_pct), "available_budget": str(r.available_budget),
            "within_limit": r.within_limit,
            "correlation_groups": r.correlation_groups,
        })

    funding = []
    for size, entry, side, rate, risk, hold in FUNDING_CASES:
        r = RISK_CALC.estimate_funding_cost(
            Decimal(size), Decimal(entry), side, Decimal(rate), Decimal(risk), hold)
        funding.append({
            "size": size, "entry": entry, "side": side, "funding_rate": rate,
            "risk_amount": risk, "hold_hours": hold,
            "hourly_rate": str(r.hourly_rate), "hourly_cost": str(r.hourly_cost),
            "hourly_income": str(r.hourly_income), "projected_24h": str(r.projected_24h),
            "funding_eats_risk_pct": str(r.funding_eats_risk_pct),
        })

    round_down = []
    for value, decimals in ROUND_DOWN_CASES:
        round_down.append({
            "value": value, "decimals": decimals,
            "result": str(RISK_CALC._round_down(Decimal(value), decimals)),
        })

    return {
        "_comment": "Golden risk-calculator vectors (SPEC-007 Phase 4). "
                    "MEDIUM profile + default HLConfig. Decimal values as strings, "
                    "compared by value in Go.",
        "profile": "medium",
        "atr": atr,
        "stop_loss": stop_loss,
        "liquidation": liquidation,
        "validate_stop": validate_stop,
        "select_leverage": select_leverage,
        "select_leverage_for_stop": select_leverage_for_stop,
        "position_size": position_size,
        "cumulative_risk": cumulative_risk,
        "funding": funding,
        "round_down": round_down,
    }


def main() -> None:
    GOLDEN_DIR.mkdir(parents=True, exist_ok=True)

    signer_golden = build_signer_golden()
    hd_golden = build_hd_golden()
    order_golden = build_order_golden()
    risk_golden = build_risk_golden()

    (GOLDEN_DIR / "signer.json").write_text(
        json.dumps(signer_golden, indent=2, sort_keys=False) + "\n"
    )
    (GOLDEN_DIR / "hd.json").write_text(
        json.dumps(hd_golden, indent=2, sort_keys=False) + "\n"
    )
    (GOLDEN_DIR / "order.json").write_text(
        json.dumps(order_golden, indent=2, sort_keys=False) + "\n"
    )
    (GOLDEN_DIR / "risk.json").write_text(
        json.dumps(risk_golden, indent=2, sort_keys=False) + "\n"
    )

    print(f"Wrote {len(signer_golden['vectors'])} signer vectors -> "
          f"{GOLDEN_DIR / 'signer.json'}")
    print(f"Wrote {len(hd_golden['accounts'])} HD vectors -> "
          f"{GOLDEN_DIR / 'hd.json'}")
    print(f"Wrote {len(order_golden['slippage'])} slippage + "
          f"{len(order_golden['formatting'])} formatting + "
          f"{len(order_golden['payloads'])} payload vectors -> "
          f"{GOLDEN_DIR / 'order.json'}")
    print(f"Wrote {len(risk_golden['atr'])} atr + {len(risk_golden['position_size'])} "
          f"position_size + {len(risk_golden['cumulative_risk'])} cumulative_risk "
          f"(+ stop/liq/leverage/funding/round_down) risk vectors -> "
          f"{GOLDEN_DIR / 'risk.json'}")
    print("All vectors cross-checked: official SDK == hyperhandler.signer ✓")


if __name__ == "__main__":
    main()
