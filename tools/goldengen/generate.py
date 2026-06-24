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
                    "slippage = _slippage_price; formatting = _format_price/_format_size.",
        "slippage": slippage,
        "formatting": formatting,
    }


def main() -> None:
    GOLDEN_DIR.mkdir(parents=True, exist_ok=True)

    signer_golden = build_signer_golden()
    hd_golden = build_hd_golden()
    order_golden = build_order_golden()

    (GOLDEN_DIR / "signer.json").write_text(
        json.dumps(signer_golden, indent=2, sort_keys=False) + "\n"
    )
    (GOLDEN_DIR / "hd.json").write_text(
        json.dumps(hd_golden, indent=2, sort_keys=False) + "\n"
    )
    (GOLDEN_DIR / "order.json").write_text(
        json.dumps(order_golden, indent=2, sort_keys=False) + "\n"
    )

    print(f"Wrote {len(signer_golden['vectors'])} signer vectors -> "
          f"{GOLDEN_DIR / 'signer.json'}")
    print(f"Wrote {len(hd_golden['accounts'])} HD vectors -> "
          f"{GOLDEN_DIR / 'hd.json'}")
    print(f"Wrote {len(order_golden['slippage'])} slippage + "
          f"{len(order_golden['formatting'])} formatting vectors -> "
          f"{GOLDEN_DIR / 'order.json'}")
    print("All vectors cross-checked: official SDK == hyperhandler.signer ✓")


if __name__ == "__main__":
    main()
