package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type MarketConfig struct {
	Symbol, CoreMarket, RHMarket string
	Window                       time.Duration
	Low, High                    float64
}

func initDB(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS users(chat_id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS markets(symbol TEXT PRIMARY KEY, core_market TEXT NOT NULL, rh_market TEXT NOT NULL, window_seconds INTEGER NOT NULL, low_pct REAL NOT NULL, high_pct REAL NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS notification_schedule(symbol TEXT PRIMARY KEY, last_in_range_sent_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS samples(symbol TEXT NOT NULL, ts INTEGER NOT NULL, core_price REAL NOT NULL, rh_price REAL NOT NULL, spread_pct REAL NOT NULL, PRIMARY KEY(symbol,ts));
CREATE INDEX IF NOT EXISTS samples_symbol_ts ON samples(symbol,ts);`)
	return err
}

func (b *Bot) authorized(ctx context.Context, chatID, userID int64) bool {
	if len(b.cfg.AllowedUsers) > 0 {
		if !b.cfg.AllowedUsers[userID] {
			return false
		}
		_, _ = b.db.ExecContext(ctx, "INSERT OR IGNORE INTO users(chat_id,user_id,created_at) VALUES(?,?,?)", chatID, userID, time.Now().Unix())
		return true
	}
	var owner int64
	err := b.db.QueryRowContext(ctx, "SELECT user_id FROM users LIMIT 1").Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = b.db.ExecContext(ctx, "INSERT OR IGNORE INTO users(chat_id,user_id,created_at) VALUES(?,?,?)", chatID, userID, time.Now().Unix())
		return err == nil
	}
	return err == nil && owner == userID
}

func (b *Bot) saveMarket(ctx context.Context, symbol, core, rh string, window int, low, high float64) error {
	_, err := b.db.ExecContext(ctx, `INSERT INTO markets(symbol,core_market,rh_market,window_seconds,low_pct,high_pct,enabled,updated_at)
VALUES(?,?,?,?,?,?,1,?) ON CONFLICT(symbol) DO UPDATE SET core_market=excluded.core_market,rh_market=excluded.rh_market,window_seconds=excluded.window_seconds,low_pct=excluded.low_pct,high_pct=excluded.high_pct,enabled=1,updated_at=excluded.updated_at`, symbol, core, rh, window*60, low, high, time.Now().Unix())
	if err != nil {
		return err
	}
	_, err = b.db.ExecContext(ctx, `INSERT INTO notification_schedule(symbol,last_in_range_sent_at) VALUES(?,0)
ON CONFLICT(symbol) DO UPDATE SET last_in_range_sent_at=0`, symbol)
	return err
}

func (b *Bot) deleteMarket(ctx context.Context, symbol string) (bool, error) {
	result, err := b.db.ExecContext(ctx, "DELETE FROM markets WHERE symbol=?", symbol)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (b *Bot) markets(ctx context.Context) ([]MarketConfig, error) {
	rows, err := b.db.QueryContext(ctx, "SELECT symbol,core_market,rh_market,window_seconds,low_pct,high_pct FROM markets WHERE enabled=1 ORDER BY symbol")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []MarketConfig
	for rows.Next() {
		var c MarketConfig
		var seconds int
		if err := rows.Scan(&c.Symbol, &c.CoreMarket, &c.RHMarket, &seconds, &c.Low, &c.High); err != nil {
			return nil, err
		}
		c.Window = time.Duration(seconds) * time.Second
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (b *Bot) saveSample(ctx context.Context, symbol string, ts int64, core, rh, spread float64) error {
	_, err := b.db.ExecContext(ctx, "INSERT OR REPLACE INTO samples(symbol,ts,core_price,rh_price,spread_pct) VALUES(?,?,?,?,?)", symbol, ts, core, rh, spread)
	return err
}

func (b *Bot) medianSpread(ctx context.Context, symbol string, window time.Duration, now int64) (float64, int, error) {
	rows, err := b.db.QueryContext(ctx, "SELECT spread_pct FROM samples WHERE symbol=? AND ts>=? ORDER BY spread_pct", symbol, now-int64(window/time.Second))
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	values := make([]float64, 0)
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			return 0, 0, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(values) == 0 {
		return 0, 0, nil
	}
	// Values are already sorted by SQLite. Average the two center values for an even count.
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		median = (values[middle-1] + values[middle]) / 2
	}
	return median, len(values), nil
}

func (b *Bot) lastInRangeNotification(ctx context.Context, symbol string) (int64, error) {
	var sentAt int64
	err := b.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT last_in_range_sent_at FROM notification_schedule WHERE symbol=?),0)", symbol).Scan(&sentAt)
	return sentAt, err
}

func (b *Bot) setLastInRangeNotification(ctx context.Context, symbol string, sentAt int64) error {
	_, err := b.db.ExecContext(ctx, `INSERT INTO notification_schedule(symbol,last_in_range_sent_at) VALUES(?,?)
ON CONFLICT(symbol) DO UPDATE SET last_in_range_sent_at=excluded.last_in_range_sent_at`, symbol, sentAt)
	return err
}

func (b *Bot) pruneSamples(ctx context.Context, symbol string, before int64) error {
	_, err := b.db.ExecContext(ctx, "DELETE FROM samples WHERE symbol=? AND ts<?", symbol, before)
	return err
}
