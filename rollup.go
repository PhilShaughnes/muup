package main

import (
	"log"
	"time"
)

// StartRollup starts the background rollup worker
func StartRollup(db *DB) {
	go func() {
		// Run once at startup
		runRollup(db)

		// Then run daily
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			runRollup(db)
		}
	}()
}

func runRollup(db *DB) {
	log.Println("Running rollup...")

	// Aggregate checks older than 7 days into daily summaries
	_, err := db.Exec(`
		INSERT OR REPLACE INTO checks_daily (monitor_id, day, uptime_pct, avg_latency, total_checks, up_count)
		SELECT 
			monitor_id,
			date(timestamp) as day,
			CAST(SUM(CASE WHEN up THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100,
			COALESCE(AVG(CASE WHEN up THEN latency_ms END), 0),
			COUNT(*),
			SUM(CASE WHEN up THEN 1 ELSE 0 END)
		FROM checks
		WHERE timestamp < datetime('now', '-7 days')
		GROUP BY monitor_id, date(timestamp)
	`)
	if err != nil {
		log.Printf("Rollup aggregation failed: %v", err)
		return
	}

	// Delete old raw checks
	result, err := db.Exec(`DELETE FROM checks WHERE timestamp < datetime('now', '-7 days')`)
	if err != nil {
		log.Printf("Rollup cleanup failed: %v", err)
		return
	}
	deleted, _ := result.RowsAffected()

	// Delete daily summaries older than 365 days
	db.Exec(`DELETE FROM checks_daily WHERE day < date('now', '-365 days')`)

	log.Printf("Rollup complete: %d raw checks archived", deleted)
}
