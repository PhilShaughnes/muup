package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the database connection
type DB struct{ *sql.DB }

// OpenDB opens the database and runs migrations
func OpenDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			url TEXT NOT NULL,
			interval INTEGER DEFAULT 30,
			timeout INTEGER DEFAULT 5000,
			expected INTEGER DEFAULT 200,
			enabled BOOLEAN DEFAULT 1,
			skip_verify BOOLEAN DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			up BOOLEAN NOT NULL,
			latency_ms INTEGER,
			error TEXT,
			FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_checks_monitor_time ON checks(monitor_id, timestamp DESC);
		CREATE TABLE IF NOT EXISTS checks_daily (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			day DATE NOT NULL,
			uptime_pct REAL NOT NULL,
			avg_latency INTEGER NOT NULL,
			total_checks INTEGER NOT NULL,
			up_count INTEGER NOT NULL,
			FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
			UNIQUE(monitor_id, day)
		);
		CREATE INDEX IF NOT EXISTS idx_checks_daily_monitor ON checks_daily(monitor_id, day DESC);
	`)
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

// SyncMonitors upserts monitors from config into the database
func (db *DB) SyncMonitors(monitors []MonitorConfig) error {
	for _, m := range monitors {
		_, err := db.Exec(`
			INSERT INTO monitors (name, url, interval, timeout, expected, enabled, skip_verify)
			VALUES (?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(name) DO UPDATE SET
				url=excluded.url,
				interval=excluded.interval,
				timeout=excluded.timeout,
				expected=excluded.expected,
				enabled=1,
				skip_verify=excluded.skip_verify`,
			m.Name, m.URL, m.Interval, m.Timeout, m.Expected, m.SkipVerify)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetMonitors returns all enabled monitors
func (db *DB) GetMonitors() ([]Monitor, error) {
	rows, err := db.Query(`
		SELECT id, name, url, interval, timeout, expected, enabled, COALESCE(skip_verify, 0)
		FROM monitors WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		var m Monitor
		if err := rows.Scan(&m.ID, &m.Name, &m.URL, &m.Interval, &m.Timeout, &m.Expected, &m.Enabled, &m.SkipVerify); err != nil {
			return nil, err
		}
		monitors = append(monitors, m)
	}
	return monitors, rows.Err()
}

// GetMonitor returns a single monitor by ID
func (db *DB) GetMonitor(id int) (*Monitor, error) {
	var m Monitor
	err := db.QueryRow(`
		SELECT id, name, url, interval, timeout, expected, enabled, COALESCE(skip_verify, 0)
		FROM monitors WHERE id=?`, id).
		Scan(&m.ID, &m.Name, &m.URL, &m.Interval, &m.Timeout, &m.Expected, &m.Enabled, &m.SkipVerify)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveCheck inserts a check result
func (db *DB) SaveCheck(monitorID int, up bool, latencyMs int, errMsg string) error {
	_, err := db.Exec(`
		INSERT INTO checks (monitor_id, up, latency_ms, error)
		VALUES (?, ?, ?, ?)`,
		monitorID, up, latencyMs, errMsg)
	return err
}

// GetRecentChecks returns the N most recent checks for a monitor
func (db *DB) GetRecentChecks(monitorID, limit int) ([]Check, error) {
	rows, err := db.Query(`
		SELECT id, monitor_id, timestamp, up, latency_ms, COALESCE(error, '')
		FROM checks WHERE monitor_id=?
		ORDER BY timestamp DESC LIMIT ?`,
		monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []Check
	for rows.Next() {
		var c Check
		var ts string
		if err := rows.Scan(&c.ID, &c.MonitorID, &ts, &c.Up, &c.LatencyMs, &c.Error); err != nil {
			return nil, err
		}
		c.Timestamp, _ = time.Parse(time.RFC3339, ts)
		if c.Timestamp.IsZero() {
			c.Timestamp, _ = time.ParseInLocation("2006-01-02 15:04:05", ts, time.UTC)
		}
		if c.Timestamp.IsZero() {
			c.Timestamp = time.Now().UTC()
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

// GetStreak calculates the current up/down streak for a monitor
func (db *DB) GetStreak(monitorID int) (up bool, start time.Time, duration time.Duration) {
	checks, err := db.GetRecentChecks(monitorID, 1)
	if err != nil || len(checks) == 0 {
		return false, time.Now(), 0
	}

	currentStatus := checks[0].Up
	now := time.Now().UTC()

	// Walk backwards through checks to find where streak started
	rows, err := db.Query(`
		SELECT timestamp, up FROM checks
		WHERE monitor_id=? ORDER BY timestamp DESC`,
		monitorID)
	if err != nil {
		return currentStatus, now, 0
	}
	defer rows.Close()

	var lastMatchTime time.Time
	for rows.Next() {
		var ts string
		var checkUp bool
		if err := rows.Scan(&ts, &checkUp); err != nil || checkUp != currentStatus {
			break
		}
		lastMatchTime, _ = time.Parse(time.RFC3339, ts)
		if lastMatchTime.IsZero() {
			lastMatchTime, _ = time.ParseInLocation("2006-01-02 15:04:05", ts, time.UTC)
		}
	}

	if !lastMatchTime.IsZero() {
		return currentStatus, lastMatchTime, now.Sub(lastMatchTime)
	}
	return currentStatus, now, 0
}

// GetUptime calculates uptime percentage for the given time window
func (db *DB) GetUptime(monitorID int, since time.Duration) float64 {
	var total, up int
	err := db.QueryRow(`
		SELECT COUNT(*), SUM(CASE WHEN up THEN 1 ELSE 0 END)
		FROM checks
		WHERE monitor_id=? AND timestamp > datetime('now', ?)`,
		monitorID, fmt.Sprintf("-%d seconds", int(since.Seconds()))).Scan(&total, &up)
	if err != nil || total == 0 {
		return 0
	}
	return float64(up) / float64(total) * 100
}

// GetAvgLatency calculates average latency for successful checks in the time window
func (db *DB) GetAvgLatency(monitorID int, since time.Duration) int {
	var avg sql.NullFloat64
	db.QueryRow(`
		SELECT AVG(latency_ms) FROM checks
		WHERE monitor_id=? AND up=1 AND timestamp > datetime('now', ?)`,
		monitorID, fmt.Sprintf("-%d seconds", int(since.Seconds()))).Scan(&avg)
	if !avg.Valid {
		return 0
	}
	return int(avg.Float64)
}

// GetLatencyHistory returns latency values for the sparkline graph
func (db *DB) GetLatencyHistory(monitorID int, hours int) []int {
	rows, err := db.Query(`
		SELECT latency_ms FROM checks
		WHERE monitor_id=? AND up=1 AND timestamp > datetime('now', ?)
		ORDER BY timestamp ASC`,
		monitorID, fmt.Sprintf("-%d hours", hours))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var latencies []int
	for rows.Next() {
		var l int
		if err := rows.Scan(&l); err == nil {
			latencies = append(latencies, l)
		}
	}
	return latencies
}
