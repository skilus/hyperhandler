// Package storage persists signals, orders, trade results and risk decisions
// via modernc.org/sqlite (pure-Go, no cgo). SPEC-007 Phase 5.
//
// Clean-cutover schema: Go does NOT read legacy Python databases. Decimal values
// are stored as TEXT via decimal.String() (zero serializes as "0", never NULL);
// only genuinely optional columns are nullable. trade_results carries a UNIQUE
// fill_id so reconciliation from exchange fills is idempotent across restarts
// (SPEC-007 B.5) — previously the collector deduped only in memory, so a restart
// re-inserted closed fills and corrupted the circuit breaker's PnL window.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shopspring/decimal"
	_ "modernc.org/sqlite"

	"github.com/skilus/hyperhandler/internal/models"
)

// schema is the clean-cutover DDL. Applied idempotently on open.
const schema = `
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

CREATE TABLE IF NOT EXISTS trade_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    signal_id INTEGER,
    network TEXT NOT NULL,
    fill_id TEXT UNIQUE,
    coin TEXT NOT NULL,
    side TEXT NOT NULL,
    entry_price TEXT NOT NULL,
    exit_price TEXT NOT NULL,
    size TEXT NOT NULL,
    pnl TEXT NOT NULL,
    fees TEXT NOT NULL,
    funding_paid TEXT NOT NULL,
    opened_at TIMESTAMP NOT NULL,
    closed_at TIMESTAMP NOT NULL,
    FOREIGN KEY (signal_id) REFERENCES signals(id)
);

CREATE TABLE IF NOT EXISTS risk_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    network TEXT NOT NULL,
    signal_id INTEGER,
    risk_mode TEXT NOT NULL,
    decision TEXT NOT NULL,
    reject_reason TEXT,
    coin TEXT NOT NULL,
    side TEXT NOT NULL,
    input_size TEXT,
    output_size TEXT,
    input_leverage INTEGER,
    output_leverage INTEGER,
    risk_pct TEXT,
    estimated_liq TEXT,
    details_json TEXT,
    FOREIGN KEY (signal_id) REFERENCES signals(id)
);

CREATE INDEX IF NOT EXISTS idx_trade_results_closed ON trade_results(closed_at);
CREATE INDEX IF NOT EXISTS idx_trade_results_network ON trade_results(network);
CREATE INDEX IF NOT EXISTS idx_trade_results_coin ON trade_results(coin);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_network ON risk_decisions(network);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_coin ON risk_decisions(coin);
`

// Storage is a SQLite-backed history store. It is safe for the single-threaded
// CLI use: the underlying *sql.DB pool is capped to one connection to avoid
// "database is locked" under SQLite. Construct with New and Close when done.
type Storage struct {
	db   *sql.DB
	path string
}

// DefaultPath returns ~/.hyperhandler/history.db.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".hyperhandler", "history.db"), nil
}

// New opens (creating if needed) the SQLite database at dbPath and applies the
// schema. An empty dbPath uses DefaultPath. The parent directory is created.
func New(dbPath string) (*Storage, error) {
	if dbPath == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		dbPath = p
	}
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite handles one writer at a time; a single pooled connection keeps the
	// CLI free of lock contention.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &Storage{db: db, path: dbPath}, nil
}

// Path returns the database file path.
func (s *Storage) Path() string { return s.path }

// Close closes the underlying database.
func (s *Storage) Close() error { return s.db.Close() }

// decPtr renders an optional decimal to a nullable TEXT value.
func decPtr(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// strPtr renders an optional string to a nullable value.
func strPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// intPtr renders an optional int to a nullable value.
func intPtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// int64Ptr renders an optional int64 to a nullable value.
func int64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// boolToInt maps a bool to SQLite's 0/1 integer convention.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const tsLayout = time.RFC3339Nano

// SaveSignal persists a trading signal and returns its row ID. Mirrors
// storage.py:save_signal.
func (s *Storage) SaveSignal(signal *models.TradingSignal, network string, validated, executed bool) (int64, error) {
	signalJSON, err := json.Marshal(signal)
	if err != nil {
		return 0, fmt.Errorf("encode signal: %w", err)
	}

	res, err := s.db.Exec(
		`INSERT INTO signals (
            network, pair, side, order_type, size, leverage,
            entry_price, stop_loss, take_profit, signal_json,
            validated, executed
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		network,
		signal.Pair,
		string(signal.Side),
		string(signal.OrderType),
		signal.Size.String(),
		signal.Leverage,
		decPtr(signal.EntryPrice),
		decPtr(signal.StopLoss),
		decPtr(signal.TakeProfit),
		string(signalJSON),
		boolToInt(validated),
		boolToInt(executed),
	)
	if err != nil {
		return 0, fmt.Errorf("insert signal: %w", err)
	}
	return res.LastInsertId()
}

// SaveOrder persists an order result and returns its row ID. Mirrors
// storage.py:save_order.
func (s *Storage) SaveOrder(
	signalID *int64,
	network, pair, side, orderType string,
	size decimal.Decimal,
	price *decimal.Decimal,
	result models.OrderResult,
	vaultAddress *string,
) (int64, error) {
	var avgPrice any
	if result.AvgPrice != nil {
		avgPrice = result.AvgPrice.String()
	}
	var errStr any
	if result.Error != "" {
		errStr = result.Error
	}

	res, err := s.db.Exec(
		`INSERT INTO orders (
            signal_id, network, order_id, pair, side, order_type,
            size, price, status, filled_size, avg_price, error,
            vault_address
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		int64Ptr(signalID),
		network,
		int64Ptr(result.OrderID),
		pair,
		side,
		orderType,
		size.String(),
		decPtr(price),
		string(result.Status),
		result.FilledSize.String(),
		avgPrice,
		errStr,
		strPtr(vaultAddress),
	)
	if err != nil {
		return 0, fmt.Errorf("insert order: %w", err)
	}
	return res.LastInsertId()
}

// UpdateSignalExecuted flips a signal's executed flag. Mirrors
// storage.py:update_signal_executed.
func (s *Storage) UpdateSignalExecuted(signalID int64, executed bool) error {
	_, err := s.db.Exec(
		"UPDATE signals SET executed = ? WHERE id = ?",
		boolToInt(executed), signalID,
	)
	return err
}

// SaveTradeResult persists a closed trade for circuit-breaker tracking and
// returns its row ID. It is idempotent on fill_id: a result carrying a FillID
// that already exists is not re-inserted, and the existing row ID is returned
// (SPEC-007 B.5). Manual closes (nil FillID) are always inserted, since SQLite
// treats NULLs as distinct under a UNIQUE constraint. Mirrors
// storage.py:save_trade_result.
func (s *Storage) SaveTradeResult(result models.TradeResult, network string) (int64, error) {
	if result.FillID != nil {
		// Fast path: return the existing row without inserting a duplicate.
		var existing int64
		err := s.db.QueryRow(
			"SELECT id FROM trade_results WHERE fill_id = ?", *result.FillID,
		).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return 0, fmt.Errorf("lookup fill_id: %w", err)
		}
	}

	res, err := s.db.Exec(
		`INSERT INTO trade_results (
            signal_id, network, fill_id, coin, side, entry_price, exit_price,
            size, pnl, fees, funding_paid, opened_at, closed_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(fill_id) DO NOTHING`,
		int64Ptr(result.SignalID),
		network,
		strPtr(result.FillID),
		result.Coin,
		result.Side,
		result.EntryPrice.String(),
		result.ExitPrice.String(),
		result.Size.String(),
		result.Pnl.String(),
		result.Fees.String(),
		result.FundingPaid.String(),
		result.OpenedAt.UTC().Format(tsLayout),
		result.ClosedAt.UTC().Format(tsLayout),
	)
	if err != nil {
		return 0, fmt.Errorf("insert trade_result: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 && result.FillID != nil {
		// Lost a race with a concurrent insert of the same fill_id.
		var existing int64
		if err := s.db.QueryRow(
			"SELECT id FROM trade_results WHERE fill_id = ?", *result.FillID,
		).Scan(&existing); err != nil {
			return 0, fmt.Errorf("lookup fill_id after conflict: %w", err)
		}
		return existing, nil
	}
	return res.LastInsertId()
}

// GetRecentTradeResults returns the most recently closed trade results for a
// network. Mirrors storage.py:get_recent_trade_results without a coin filter;
// use GetRecentTradeResultsByCoin to filter by coin.
func (s *Storage) GetRecentTradeResults(network string, limit int) ([]models.TradeResult, error) {
	return s.getTradeResults(network, limit, nil)
}

// GetRecentTradeResultsByCoin is GetRecentTradeResults restricted to one coin.
func (s *Storage) GetRecentTradeResultsByCoin(network string, limit int, coin string) ([]models.TradeResult, error) {
	return s.getTradeResults(network, limit, &coin)
}

func (s *Storage) getTradeResults(network string, limit int, coin *string) ([]models.TradeResult, error) {
	query := "SELECT id, signal_id, fill_id, coin, side, entry_price, exit_price, size, pnl, fees, funding_paid, opened_at, closed_at FROM trade_results WHERE network = ?"
	args := []any{network}
	if coin != nil {
		query += " AND coin = ?"
		args = append(args, *coin)
	}
	query += " ORDER BY closed_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trade_results: %w", err)
	}
	defer rows.Close()

	var results []models.TradeResult
	for rows.Next() {
		var (
			id                                                        int64
			signalID                                                  sql.NullInt64
			fillID                                                    sql.NullString
			coinVal, side                                             string
			entry, exit, size, pnl, fees, funding, openedAt, closedAt string
		)
		if err := rows.Scan(&id, &signalID, &fillID, &coinVal, &side, &entry, &exit, &size, &pnl, &fees, &funding, &openedAt, &closedAt); err != nil {
			return nil, fmt.Errorf("scan trade_result: %w", err)
		}
		opened, err := parseTime(openedAt)
		if err != nil {
			return nil, fmt.Errorf("parse opened_at: %w", err)
		}
		closed, err := parseTime(closedAt)
		if err != nil {
			return nil, fmt.Errorf("parse closed_at: %w", err)
		}
		tr := models.TradeResult{
			ID:          int64Val(id),
			Coin:        coinVal,
			Side:        side,
			EntryPrice:  mustDec(entry),
			ExitPrice:   mustDec(exit),
			Size:        mustDec(size),
			Pnl:         mustDec(pnl),
			Fees:        mustDec(fees),
			FundingPaid: mustDec(funding),
			OpenedAt:    opened,
			ClosedAt:    closed,
		}
		if signalID.Valid {
			tr.SignalID = &signalID.Int64
		}
		if fillID.Valid {
			fv := fillID.String
			tr.FillID = &fv
		}
		results = append(results, tr)
	}
	return results, rows.Err()
}

// SaveRiskDecision persists a risk-manager audit record and returns its row ID.
// Mirrors storage.py:save_risk_decision.
func (s *Storage) SaveRiskDecision(decision models.RiskDecisionLog, network string) (int64, error) {
	details := map[string]any{
		"mark_price":         decision.MarkPrice.String(),
		"funding_rate":       decision.FundingRate.String(),
		"atr_value":          decPtr(decision.ATRValue),
		"consecutive_losses": decision.ConsecutiveLosses,
		"daily_pnl_pct":      decision.DailyPnlPct.String(),
		"account_value":      decision.AccountValue.String(),
		"available_balance":  decision.AvailableBalance.String(),
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return 0, fmt.Errorf("encode risk details: %w", err)
	}

	var rejectReason any
	if decision.RejectReason != nil {
		rejectReason = string(*decision.RejectReason)
	}

	res, err := s.db.Exec(
		`INSERT INTO risk_decisions (
            network, risk_mode, decision, reject_reason, coin, side,
            input_size, output_size, input_leverage, output_leverage,
            risk_pct, estimated_liq, details_json
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		network,
		string(decision.RiskMode),
		decision.Decision,
		rejectReason,
		decision.Coin,
		decision.Side,
		decPtr(decision.InputSize),
		decPtr(decision.OutputSize),
		intPtr(decision.InputLeverage),
		intPtr(decision.OutputLeverage),
		decPtr(decision.CumulativeRiskAfterPct),
		decPtr(decision.EstimatedLiquidation),
		string(detailsJSON),
	)
	if err != nil {
		return 0, fmt.Errorf("insert risk_decision: %w", err)
	}
	return res.LastInsertId()
}

// SignalRecord is a persisted signal row. Decimal columns are TEXT; optional
// columns are pointers.
type SignalRecord struct {
	ID         int64
	CreatedAt  string
	Network    string
	Pair       string
	Side       string
	OrderType  string
	Size       string
	Leverage   int
	EntryPrice *string
	StopLoss   *string
	TakeProfit *string
	SignalJSON string
	Validated  bool
	Executed   bool
}

// OrderRecord is a persisted order row.
type OrderRecord struct {
	ID           int64
	CreatedAt    string
	SignalID     *int64
	Network      string
	OrderID      *int64
	Pair         string
	Side         string
	OrderType    string
	Size         string
	Price        *string
	Status       string
	FilledSize   *string
	AvgPrice     *string
	Error        *string
	VaultAddress *string
}

func scanSignal(rows *sql.Rows) (SignalRecord, error) {
	var (
		r         SignalRecord
		entry, sl sql.NullString
		tp        sql.NullString
		validated int
		executed  int
	)
	if err := rows.Scan(
		&r.ID, &r.CreatedAt, &r.Network, &r.Pair, &r.Side, &r.OrderType,
		&r.Size, &r.Leverage, &entry, &sl, &tp, &r.SignalJSON, &validated, &executed,
	); err != nil {
		return r, err
	}
	r.EntryPrice = nullStr(entry)
	r.StopLoss = nullStr(sl)
	r.TakeProfit = nullStr(tp)
	r.Validated = validated != 0
	r.Executed = executed != 0
	return r, nil
}

const signalCols = "id, created_at, network, pair, side, order_type, size, leverage, entry_price, stop_loss, take_profit, signal_json, validated, executed"

// GetSignal returns a single signal by ID, or nil if not found. Mirrors
// storage.py:get_signal.
func (s *Storage) GetSignal(signalID int64) (*SignalRecord, error) {
	rows, err := s.db.Query("SELECT "+signalCols+" FROM signals WHERE id = ?", signalID)
	if err != nil {
		return nil, fmt.Errorf("query signal: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	rec, err := scanSignal(rows)
	if err != nil {
		return nil, fmt.Errorf("scan signal: %w", err)
	}
	return &rec, nil
}

// GetRecentSignals returns recent signals, newest first, optionally filtered by
// network and pair. Mirrors storage.py:get_recent_signals.
func (s *Storage) GetRecentSignals(limit int, network, pair *string) ([]SignalRecord, error) {
	query := "SELECT " + signalCols + " FROM signals WHERE 1=1"
	var args []any
	if network != nil {
		query += " AND network = ?"
		args = append(args, *network)
	}
	if pair != nil {
		query += " AND pair = ?"
		args = append(args, *pair)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query signals: %w", err)
	}
	defer rows.Close()

	var out []SignalRecord
	for rows.Next() {
		rec, err := scanSignal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan signal: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanOrder(rows *sql.Rows) (OrderRecord, error) {
	var (
		r                                 OrderRecord
		signalID, orderID                 sql.NullInt64
		price, filled, avg, errStr, vault sql.NullString
	)
	if err := rows.Scan(
		&r.ID, &r.CreatedAt, &signalID, &r.Network, &orderID, &r.Pair, &r.Side,
		&r.OrderType, &r.Size, &price, &r.Status, &filled, &avg, &errStr, &vault,
	); err != nil {
		return r, err
	}
	if signalID.Valid {
		r.SignalID = &signalID.Int64
	}
	if orderID.Valid {
		r.OrderID = &orderID.Int64
	}
	r.Price = nullStr(price)
	r.FilledSize = nullStr(filled)
	r.AvgPrice = nullStr(avg)
	r.Error = nullStr(errStr)
	r.VaultAddress = nullStr(vault)
	return r, nil
}

const orderCols = "id, created_at, signal_id, network, order_id, pair, side, order_type, size, price, status, filled_size, avg_price, error, vault_address"

// GetRecentOrders returns recent orders, newest first, optionally filtered by
// network, pair and status. Mirrors storage.py:get_recent_orders.
func (s *Storage) GetRecentOrders(limit int, network, pair, status *string) ([]OrderRecord, error) {
	query := "SELECT " + orderCols + " FROM orders WHERE 1=1"
	var args []any
	if network != nil {
		query += " AND network = ?"
		args = append(args, *network)
	}
	if pair != nil {
		query += " AND pair = ?"
		args = append(args, *pair)
	}
	if status != nil {
		query += " AND status = ?"
		args = append(args, *status)
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query orders: %w", err)
	}
	defer rows.Close()

	var out []OrderRecord
	for rows.Next() {
		rec, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetOrdersBySignal returns all orders for a signal, oldest first. Mirrors
// storage.py:get_orders_by_signal.
func (s *Storage) GetOrdersBySignal(signalID int64) ([]OrderRecord, error) {
	rows, err := s.db.Query("SELECT "+orderCols+" FROM orders WHERE signal_id = ? ORDER BY created_at", signalID)
	if err != nil {
		return nil, fmt.Errorf("query orders by signal: %w", err)
	}
	defer rows.Close()

	var out []OrderRecord
	for rows.Next() {
		rec, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// SignalStats summarizes the signals table.
type SignalStats struct {
	Total    int
	Executed int
}

// OrderStats summarizes the orders table.
type OrderStats struct {
	Total    int
	Filled   int
	Rejected int
}

// Stats is the combined history summary. Mirrors storage.py:get_stats.
type Stats struct {
	Signals SignalStats
	Orders  OrderStats
}

// GetStats returns aggregate counts, optionally filtered by network. Mirrors
// storage.py:get_stats.
func (s *Storage) GetStats(network *string) (Stats, error) {
	var stats Stats

	where := ""
	var args []any
	if network != nil {
		where = "WHERE network = ?"
		args = append(args, *network)
	}

	var sigTotal, sigExecuted sql.NullInt64
	if err := s.db.QueryRow(
		"SELECT COUNT(*), SUM(executed) FROM signals "+where, args...,
	).Scan(&sigTotal, &sigExecuted); err != nil {
		return stats, fmt.Errorf("signal stats: %w", err)
	}
	stats.Signals.Total = int(sigTotal.Int64)
	stats.Signals.Executed = int(sigExecuted.Int64)

	var ordTotal, ordFilled, ordRejected sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT COUNT(*),
            SUM(CASE WHEN status = 'filled' THEN 1 ELSE 0 END),
            SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END)
        FROM orders `+where, args...,
	).Scan(&ordTotal, &ordFilled, &ordRejected); err != nil {
		return stats, fmt.Errorf("order stats: %w", err)
	}
	stats.Orders.Total = int(ordTotal.Int64)
	stats.Orders.Filled = int(ordFilled.Int64)
	stats.Orders.Rejected = int(ordRejected.Int64)

	return stats, nil
}

// nullStr converts a sql.NullString to *string.
func nullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// int64Val returns a pointer to v.
func int64Val(v int64) *int64 { return &v }

// mustDec parses a TEXT decimal column; malformed input degrades to zero (our
// own writes are always well-formed via decimal.String()).
func mustDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// parseTime parses a stored timestamp, tolerating both our RFC3339Nano writes
// and SQLite's CURRENT_TIMESTAMP ("2006-01-02 15:04:05") default.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(tsLayout, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}
