package main

import (
	"database/sql"
	"fmt"
	"slices"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the database connection
type DB struct{ *sql.DB }

// StateChange represents a monitor state transition (up/down)
type StateChange struct {
	ID        int
	MonitorID int
	Timestamp time.Time
	NewState  bool // true = up, false = down
	Error     string
}

// HourlyLatency stores aggregated latency metrics per hour
type HourlyLatency struct {
	ID            int
	MonitorID     int
	Hour          time.Time
	MedianLatency int
	MaxLatency    int
	SampleCount   int
}

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
		CREATE TABLE IF NOT EXISTS state_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			new_state BOOLEAN NOT NULL,
			error TEXT,
			FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_state_changes_monitor_time ON state_changes(monitor_id, timestamp DESC);
		CREATE TABLE IF NOT EXISTS hourly_latency (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			monitor_id INTEGER NOT NULL,
			hour DATETIME NOT NULL,
			median_latency INTEGER NOT NULL,
			max_latency INTEGER NOT NULL,
			sample_count INTEGER NOT NULL,
			FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
			UNIQUE(monitor_id, hour)
		);
		CREATE INDEX IF NOT EXISTS idx_hourly_latency_monitor ON hourly_latency(monitor_id, hour DESC);
	`)
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

// parseTimestamp parses SQLite timestamp formats
func parseTimestamp(ts string) time.Time {
	t, _ := time.Parse(time.RFC3339, ts)
	if t.IsZero() {
		t, _ = time.ParseInLocation("2006-01-02 15:04:05", ts, time.UTC)
	}
	return t
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

// SaveStateChange records a state transition (up/down)
func (db *DB) SaveStateChange(monitorID int, newState bool, errMsg string) error {
	_, err := db.Exec(`
		INSERT INTO state_changes (monitor_id, new_state, error)
		VALUES (?, ?, ?)`,
		monitorID, newState, errMsg)
	return err
}

// GetLastState returns the most recent state for a monitor
func (db *DB) GetLastState(monitorID int) (bool, error) {
	var state bool
	err := db.QueryRow(`
		SELECT new_state FROM state_changes
		WHERE monitor_id=?
		ORDER BY timestamp DESC LIMIT 1`,
		monitorID).Scan(&state)
	if err == sql.ErrNoRows {
		return false, nil // Default to down if no history
	}
	return state, err
}

// GetStateChanges returns state changes for a monitor within a time range
func (db *DB) GetStateChanges(monitorID int, since time.Duration, limit int) ([]StateChange, error) {
	rows, err := db.Query(`
		SELECT id, monitor_id, timestamp, new_state, COALESCE(error, '')
		FROM state_changes
		WHERE monitor_id=? AND timestamp > datetime('now', ?)
		ORDER BY timestamp DESC LIMIT ?`,
		monitorID, fmt.Sprintf("-%d seconds", int(since.Seconds())), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []StateChange
	for rows.Next() {
		var sc StateChange
		var ts string
		if err := rows.Scan(&sc.ID, &sc.MonitorID, &ts, &sc.NewState, &sc.Error); err != nil {
			return nil, err
		}
		sc.Timestamp = parseTimestamp(ts)
		changes = append(changes, sc)
	}
	return changes, rows.Err()
}

// GetStreak calculates the current up/down streak for a monitor
func (db *DB) GetStreak(monitorID int) (up bool, start time.Time, duration time.Duration) {
	var ts string
	err := db.QueryRow(`
		SELECT new_state, timestamp FROM state_changes
		WHERE monitor_id=?
		ORDER BY timestamp DESC LIMIT 1`,
		monitorID).Scan(&up, &ts)

	if err == sql.ErrNoRows {
		return false, time.Now(), 0
	}
	if err != nil {
		return false, time.Now(), 0
	}

	start = parseTimestamp(ts)
	if start.IsZero() {
		start = time.Now()
	}

	duration = time.Since(start)
	return up, start, duration
}

// UpsertHourlyLatency updates or inserts hourly latency statistics
func (db *DB) UpsertHourlyLatency(monitorID int, hour time.Time, medianLatency, maxLatency, sampleCount int) error {
	_, err := db.Exec(`
		INSERT INTO hourly_latency (monitor_id, hour, median_latency, max_latency, sample_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(monitor_id, hour) DO UPDATE SET
			median_latency=excluded.median_latency,
			max_latency=excluded.max_latency,
			sample_count=excluded.sample_count`,
		monitorID, hour.Format("2006-01-02 15:00:00"), medianLatency, maxLatency, sampleCount)
	return err
}

// GetLatencyStats returns median and max latency for a time range
func (db *DB) GetLatencyStats(monitorID int, since time.Duration) (median, max int, err error) {
	// Get all hourly medians and calculate the median of medians (approximate p50)
	rows, err := db.Query(`
		SELECT median_latency, max_latency
		FROM hourly_latency
		WHERE monitor_id=? AND hour > datetime('now', ?)
		ORDER BY hour ASC`,
		monitorID, fmt.Sprintf("-%d seconds", int(since.Seconds())))
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var medians []int
	maxLatency := 0
	for rows.Next() {
		var med, mx int
		if err := rows.Scan(&med, &mx); err == nil {
			medians = append(medians, med)
			if mx > maxLatency {
				maxLatency = mx
			}
		}
	}

	if len(medians) == 0 {
		return 0, 0, nil
	}

	// Calculate median of medians (approximate p50)
	slices.Sort(medians)
	medianLatency := medians[len(medians)/2]
	if len(medians)%2 == 0 && len(medians) > 0 {
		medianLatency = (medians[len(medians)/2-1] + medians[len(medians)/2]) / 2
	}

	return medianLatency, maxLatency, nil
}
