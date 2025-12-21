package main

import "time"

// Monitor represents an endpoint to check
type Monitor struct {
	ID         int
	Name       string
	URL        string
	Interval   int // seconds between checks
	Timeout    int // milliseconds
	Expected   int // expected HTTP status code
	Enabled    bool
	SkipVerify bool // ignore TLS certificate errors
}

// Check represents a single check result
type Check struct {
	ID        int
	MonitorID int
	Timestamp time.Time
	Up        bool
	LatencyMs int
	Error     string
}

// CheckDaily represents aggregated daily stats
type CheckDaily struct {
	MonitorID  int
	Day        string
	UptimePct  float64
	AvgLatency int
	TotalCheck int
	UpCount    int
}

// MonitorStats holds calculated stats for display
type MonitorStats struct {
	Monitor        Monitor
	CurrentUp      bool
	StreakStart    time.Time
	StreakDuration time.Duration
	Uptime24h      float64
	Uptime7d       float64
	Uptime30d      float64
	AvgLatency24h  int
	AvgLatency7d   int
	RecentChecks   []Check
	LatencyHistory []int
}
