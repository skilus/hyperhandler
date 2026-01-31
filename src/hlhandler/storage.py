"""SQLite storage for order and signal history."""

import json
import sqlite3
from contextlib import contextmanager
from dataclasses import asdict
from datetime import datetime
from decimal import Decimal
from pathlib import Path
from typing import Any, Generator

from hlhandler.models import OrderResult, OrderStatus, TradingSignal


class DecimalEncoder(json.JSONEncoder):
    """JSON encoder that handles Decimal types."""

    def default(self, obj: Any) -> Any:
        if isinstance(obj, Decimal):
            return str(obj)
        return super().default(obj)


class Storage:
    """SQLite storage for hlhandler history."""

    DEFAULT_PATH = Path.home() / ".hlhandler" / "history.db"

    def __init__(self, db_path: Path | None = None):
        """Initialize storage.

        Args:
            db_path: Path to SQLite database. Uses default if not provided.
        """
        self.db_path = db_path or self.DEFAULT_PATH
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init_db()

    def _init_db(self) -> None:
        """Initialize database schema."""
        with self._connection() as conn:
            conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS signals (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    network TEXT NOT NULL,
                    pair TEXT NOT NULL,
                    side TEXT NOT NULL,
                    order_type TEXT NOT NULL,
                    size TEXT NOT NULL,
                    leverage INTEGER NOT NULL,
                    entry_price TEXT,
                    stop_loss TEXT,
                    take_profit TEXT,
                    signal_json TEXT NOT NULL,
                    validated INTEGER DEFAULT 0,
                    executed INTEGER DEFAULT 0
                );

                CREATE TABLE IF NOT EXISTS orders (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    signal_id INTEGER,
                    network TEXT NOT NULL,
                    order_id INTEGER,
                    pair TEXT NOT NULL,
                    side TEXT NOT NULL,
                    order_type TEXT NOT NULL,
                    size TEXT NOT NULL,
                    price TEXT,
                    status TEXT NOT NULL,
                    filled_size TEXT,
                    avg_price TEXT,
                    error TEXT,
                    vault_address TEXT,
                    FOREIGN KEY (signal_id) REFERENCES signals(id)
                );

                CREATE INDEX IF NOT EXISTS idx_signals_pair ON signals(pair);
                CREATE INDEX IF NOT EXISTS idx_signals_created ON signals(created_at);
                CREATE INDEX IF NOT EXISTS idx_orders_signal ON orders(signal_id);
                CREATE INDEX IF NOT EXISTS idx_orders_pair ON orders(pair);
                CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
                """
            )

    @contextmanager
    def _connection(self) -> Generator[sqlite3.Connection, None, None]:
        """Context manager for database connection."""
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        try:
            yield conn
            conn.commit()
        finally:
            conn.close()

    def save_signal(
        self,
        signal: TradingSignal,
        network: str,
        validated: bool = False,
        executed: bool = False,
    ) -> int:
        """Save a trading signal to the database.

        Args:
            signal: The trading signal.
            network: Network name.
            validated: Whether the signal was validated.
            executed: Whether the signal was executed.

        Returns:
            Signal ID.
        """
        signal_json = json.dumps(signal.model_dump(), cls=DecimalEncoder)

        with self._connection() as conn:
            cursor = conn.execute(
                """
                INSERT INTO signals (
                    network, pair, side, order_type, size, leverage,
                    entry_price, stop_loss, take_profit, signal_json,
                    validated, executed
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    network,
                    signal.pair,
                    signal.side.value,
                    signal.order_type.value,
                    str(signal.size),
                    signal.leverage,
                    str(signal.entry_price) if signal.entry_price else None,
                    str(signal.stop_loss) if signal.stop_loss else None,
                    str(signal.take_profit) if signal.take_profit else None,
                    signal_json,
                    1 if validated else 0,
                    1 if executed else 0,
                ),
            )
            return cursor.lastrowid or 0

    def save_order(
        self,
        signal_id: int | None,
        network: str,
        pair: str,
        side: str,
        order_type: str,
        size: Decimal,
        price: Decimal | None,
        result: OrderResult,
        vault_address: str | None = None,
    ) -> int:
        """Save an order result to the database.

        Args:
            signal_id: Associated signal ID.
            network: Network name.
            pair: Trading pair.
            side: Order side.
            order_type: Order type.
            size: Order size.
            price: Order price.
            result: Order result.
            vault_address: Optional vault address.

        Returns:
            Order ID.
        """
        with self._connection() as conn:
            cursor = conn.execute(
                """
                INSERT INTO orders (
                    signal_id, network, order_id, pair, side, order_type,
                    size, price, status, filled_size, avg_price, error,
                    vault_address
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    signal_id,
                    network,
                    result.order_id,
                    pair,
                    side,
                    order_type,
                    str(size),
                    str(price) if price else None,
                    result.status.value,
                    str(result.filled_size),
                    str(result.avg_price) if result.avg_price else None,
                    result.error,
                    vault_address,
                ),
            )
            return cursor.lastrowid or 0

    def update_signal_executed(self, signal_id: int, executed: bool = True) -> None:
        """Update signal executed status.

        Args:
            signal_id: Signal ID.
            executed: Whether executed.
        """
        with self._connection() as conn:
            conn.execute(
                "UPDATE signals SET executed = ? WHERE id = ?",
                (1 if executed else 0, signal_id),
            )

    def get_recent_signals(
        self,
        limit: int = 50,
        network: str | None = None,
        pair: str | None = None,
    ) -> list[dict]:
        """Get recent signals.

        Args:
            limit: Maximum number of signals.
            network: Optional network filter.
            pair: Optional pair filter.

        Returns:
            List of signal records.
        """
        query = "SELECT * FROM signals WHERE 1=1"
        params: list[Any] = []

        if network:
            query += " AND network = ?"
            params.append(network)
        if pair:
            query += " AND pair = ?"
            params.append(pair)

        query += " ORDER BY created_at DESC LIMIT ?"
        params.append(limit)

        with self._connection() as conn:
            cursor = conn.execute(query, params)
            return [dict(row) for row in cursor.fetchall()]

    def get_recent_orders(
        self,
        limit: int = 50,
        network: str | None = None,
        pair: str | None = None,
        status: str | None = None,
    ) -> list[dict]:
        """Get recent orders.

        Args:
            limit: Maximum number of orders.
            network: Optional network filter.
            pair: Optional pair filter.
            status: Optional status filter.

        Returns:
            List of order records.
        """
        query = "SELECT * FROM orders WHERE 1=1"
        params: list[Any] = []

        if network:
            query += " AND network = ?"
            params.append(network)
        if pair:
            query += " AND pair = ?"
            params.append(pair)
        if status:
            query += " AND status = ?"
            params.append(status)

        query += " ORDER BY created_at DESC LIMIT ?"
        params.append(limit)

        with self._connection() as conn:
            cursor = conn.execute(query, params)
            return [dict(row) for row in cursor.fetchall()]

    def get_orders_by_signal(self, signal_id: int) -> list[dict]:
        """Get all orders for a signal.

        Args:
            signal_id: Signal ID.

        Returns:
            List of order records.
        """
        with self._connection() as conn:
            cursor = conn.execute(
                "SELECT * FROM orders WHERE signal_id = ? ORDER BY created_at",
                (signal_id,),
            )
            return [dict(row) for row in cursor.fetchall()]

    def get_signal(self, signal_id: int) -> dict | None:
        """Get a signal by ID.

        Args:
            signal_id: Signal ID.

        Returns:
            Signal record or None.
        """
        with self._connection() as conn:
            cursor = conn.execute(
                "SELECT * FROM signals WHERE id = ?",
                (signal_id,),
            )
            row = cursor.fetchone()
            return dict(row) if row else None

    def get_stats(self, network: str | None = None) -> dict:
        """Get statistics.

        Args:
            network: Optional network filter.

        Returns:
            Stats dict.
        """
        with self._connection() as conn:
            where = "WHERE network = ?" if network else ""
            params = (network,) if network else ()

            # Signal stats
            cursor = conn.execute(
                f"SELECT COUNT(*) as total, SUM(executed) as executed FROM signals {where}",
                params,
            )
            signal_row = cursor.fetchone()

            # Order stats
            cursor = conn.execute(
                f"""
                SELECT
                    COUNT(*) as total,
                    SUM(CASE WHEN status = 'filled' THEN 1 ELSE 0 END) as filled,
                    SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END) as rejected
                FROM orders {where}
                """,
                params,
            )
            order_row = cursor.fetchone()

            return {
                "signals": {
                    "total": signal_row["total"] if signal_row else 0,
                    "executed": signal_row["executed"] if signal_row else 0,
                },
                "orders": {
                    "total": order_row["total"] if order_row else 0,
                    "filled": order_row["filled"] if order_row else 0,
                    "rejected": order_row["rejected"] if order_row else 0,
                },
            }


# Global storage instance
_storage: Storage | None = None


def get_storage(db_path: Path | None = None) -> Storage:
    """Get or create the global storage instance."""
    global _storage
    if _storage is None or db_path is not None:
        _storage = Storage(db_path)
    return _storage
