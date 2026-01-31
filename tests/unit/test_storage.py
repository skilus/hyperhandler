"""Tests for storage."""

import tempfile
from decimal import Decimal
from pathlib import Path

import pytest

from hlhandler.models import OrderResult, OrderSide, OrderStatus, OrderType, TradingSignal
from hlhandler.storage import Storage


@pytest.fixture
def temp_db():
    """Create a temporary database."""
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        yield Storage(db_path)


@pytest.fixture
def sample_signal():
    """Create a sample trading signal."""
    return TradingSignal(
        pair="BTC",
        side=OrderSide.LONG,
        order_type=OrderType.LIMIT,
        entry_price=Decimal("67500"),
        size=Decimal("0.1"),
        leverage=5,
        stop_loss=Decimal("66000"),
    )


@pytest.fixture
def sample_result():
    """Create a sample order result."""
    return OrderResult(
        success=True,
        order_id=123456,
        filled_size=Decimal("0.1"),
        avg_price=Decimal("67500"),
        status=OrderStatus.FILLED,
    )


class TestStorage:
    """Tests for Storage class."""

    def test_save_signal(self, temp_db, sample_signal):
        """Save signal to database."""
        signal_id = temp_db.save_signal(sample_signal, "testnet", validated=True)
        assert signal_id > 0

    def test_get_signal(self, temp_db, sample_signal):
        """Retrieve saved signal."""
        signal_id = temp_db.save_signal(sample_signal, "testnet")
        retrieved = temp_db.get_signal(signal_id)

        assert retrieved is not None
        assert retrieved["pair"] == "BTC"
        assert retrieved["side"] == "long"

    def test_save_order(self, temp_db, sample_signal, sample_result):
        """Save order to database."""
        signal_id = temp_db.save_signal(sample_signal, "testnet")
        order_id = temp_db.save_order(
            signal_id=signal_id,
            network="testnet",
            pair="BTC",
            side="long",
            order_type="limit",
            size=Decimal("0.1"),
            price=Decimal("67500"),
            result=sample_result,
        )
        assert order_id > 0

    def test_get_orders_by_signal(self, temp_db, sample_signal, sample_result):
        """Get orders associated with a signal."""
        signal_id = temp_db.save_signal(sample_signal, "testnet")

        temp_db.save_order(
            signal_id=signal_id,
            network="testnet",
            pair="BTC",
            side="long",
            order_type="entry",
            size=Decimal("0.1"),
            price=Decimal("67500"),
            result=sample_result,
        )

        orders = temp_db.get_orders_by_signal(signal_id)
        assert len(orders) == 1
        assert orders[0]["pair"] == "BTC"

    def test_update_signal_executed(self, temp_db, sample_signal):
        """Update signal executed status."""
        signal_id = temp_db.save_signal(sample_signal, "testnet", executed=False)
        temp_db.update_signal_executed(signal_id, True)

        retrieved = temp_db.get_signal(signal_id)
        assert retrieved["executed"] == 1

    def test_get_recent_signals(self, temp_db, sample_signal):
        """Get recent signals."""
        for _ in range(5):
            temp_db.save_signal(sample_signal, "testnet")

        signals = temp_db.get_recent_signals(limit=3)
        assert len(signals) == 3

    def test_get_recent_signals_with_filter(self, temp_db, sample_signal):
        """Get recent signals with network filter."""
        temp_db.save_signal(sample_signal, "testnet")
        temp_db.save_signal(sample_signal, "mainnet")

        signals = temp_db.get_recent_signals(network="testnet")
        assert all(s["network"] == "testnet" for s in signals)

    def test_get_recent_orders(self, temp_db, sample_signal, sample_result):
        """Get recent orders."""
        signal_id = temp_db.save_signal(sample_signal, "testnet")

        for _ in range(5):
            temp_db.save_order(
                signal_id=signal_id,
                network="testnet",
                pair="BTC",
                side="long",
                order_type="entry",
                size=Decimal("0.1"),
                price=Decimal("67500"),
                result=sample_result,
            )

        orders = temp_db.get_recent_orders(limit=3)
        assert len(orders) == 3

    def test_get_stats(self, temp_db, sample_signal, sample_result):
        """Get statistics."""
        signal_id = temp_db.save_signal(sample_signal, "testnet", executed=True)
        temp_db.save_order(
            signal_id=signal_id,
            network="testnet",
            pair="BTC",
            side="long",
            order_type="entry",
            size=Decimal("0.1"),
            price=Decimal("67500"),
            result=sample_result,
        )

        stats = temp_db.get_stats()
        assert stats["signals"]["total"] == 1
        assert stats["signals"]["executed"] == 1
        assert stats["orders"]["total"] == 1
        assert stats["orders"]["filled"] == 1

    def test_get_stats_with_network_filter(self, temp_db, sample_signal, sample_result):
        """Get statistics with network filter."""
        temp_db.save_signal(sample_signal, "testnet", executed=True)
        temp_db.save_signal(sample_signal, "mainnet", executed=False)

        stats = temp_db.get_stats(network="testnet")
        assert stats["signals"]["total"] == 1
        assert stats["signals"]["executed"] == 1

    def test_vault_address_stored(self, temp_db, sample_signal, sample_result):
        """Vault address is stored with order."""
        signal_id = temp_db.save_signal(sample_signal, "testnet")
        vault = "0x1234567890123456789012345678901234567890"

        temp_db.save_order(
            signal_id=signal_id,
            network="testnet",
            pair="BTC",
            side="long",
            order_type="entry",
            size=Decimal("0.1"),
            price=Decimal("67500"),
            result=sample_result,
            vault_address=vault,
        )

        orders = temp_db.get_orders_by_signal(signal_id)
        assert orders[0]["vault_address"] == vault
