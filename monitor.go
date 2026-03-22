package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"
)

// Monitor represents an endpoint to check
type Monitor struct {
	ID         int
	Name       string
	URL        string
	Interval   int  // seconds between checks
	Timeout    int  // milliseconds
	Expected   int  // expected HTTP status code
	Enabled    bool
	SkipVerify bool // ignore TLS certificate errors
}

// Checker manages HTTP checks for all monitors
type Checker struct {
	db           *DB
	mu           sync.RWMutex
	running      map[int]chan struct{} // stop channels per monitor ID
	hourlyStats  map[int]*HourlyStats  // in-memory hourly aggregation
	recentChecks map[int]*RingBuffer   // ring buffer for last 20 checks
	statsMu      sync.Mutex
}

// HourlyStats tracks running statistics for the current hour
type HourlyStats struct {
	Hour      time.Time
	Latencies []int // all latencies this hour for median calculation
	Max       int
}

// RingBuffer stores last N check results
type RingBuffer struct {
	checks [10]bool // true = up, false = down
	index  int
	count  int
}

// Add adds a check result to the ring buffer
func (rb *RingBuffer) Add(up bool) {
	rb.checks[rb.index] = up
	rb.index = (rb.index + 1) % 10
	if rb.count < 10 {
		rb.count++
	}
}

// String returns a visual representation (■ = up, □ = down)
func (rb *RingBuffer) String() string {
	if rb.count == 0 {
		return "□□□□□□□□□□" // No data yet
	}

	result := make([]rune, 10)
	for i := 0; i < 10; i++ {
		if i >= rb.count {
			result[i] = '□' // No data yet
		} else {
			// Read from oldest to newest
			idx := (rb.index - rb.count + i + 10) % 10
			if rb.checks[idx] {
				result[i] = '■'
			} else {
				result[i] = '□'
			}
		}
	}
	return string(result)
}

func NewChecker(db *DB) *Checker {
	return &Checker{
		db:           db,
		running:      make(map[int]chan struct{}),
		hourlyStats:  make(map[int]*HourlyStats),
		recentChecks: make(map[int]*RingBuffer),
	}
}

// Start begins checking all monitors
func (c *Checker) Start(monitors []Monitor) {
	for _, m := range monitors {
		c.StartMonitor(m)
	}
}

// StartMonitor starts a goroutine that checks a single monitor on its interval
func (c *Checker) StartMonitor(m Monitor) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.running[m.ID]; exists {
		return
	}

	stop := make(chan struct{})
	c.running[m.ID] = stop

	go func(m Monitor, stop chan struct{}) {
		c.check(m) // Run immediately on start
		ticker := time.NewTicker(time.Duration(m.Interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				c.check(m)
			}
		}
	}(m, stop)
}

// check performs a single HTTP check and saves state changes + latency
func (c *Checker) check(m Monitor) {
	transport := &http.Transport{}
	if m.SkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(m.Timeout) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	start := time.Now()
	resp, err := client.Get(m.URL)
	latency := int(time.Since(start).Milliseconds())

	var up bool
	var errMsg string

	if err != nil {
		errMsg = err.Error()
		log.Printf("[%s] DOWN: %v", m.Name, err)
	} else {
		resp.Body.Close()
		up = resp.StatusCode == m.Expected
		if !up {
			errMsg = http.StatusText(resp.StatusCode)
			log.Printf("[%s] DOWN: got %d, expected %d", m.Name, resp.StatusCode, m.Expected)
		}
	}

	// Check if state changed
	lastState, err := c.db.GetLastState(m.ID)
	if err != nil {
		log.Printf("[%s] failed to get last state: %v", m.Name, err)
	}

	// Save state change if it changed
	if up != lastState || err != nil {
		if err := c.db.SaveStateChange(m.ID, up, errMsg); err != nil {
			log.Printf("[%s] failed to save state change: %v", m.Name, err)
		}
		if up {
			log.Printf("[%s] UP (was down)", m.Name)
		}
	}

	// Update hourly latency stats (only for successful checks)
	if up {
		c.updateHourlyStats(m.ID, latency)
	}

	// Add to ring buffer
	c.statsMu.Lock()
	if c.recentChecks[m.ID] == nil {
		c.recentChecks[m.ID] = &RingBuffer{}
	}
	c.recentChecks[m.ID].Add(up)
	c.statsMu.Unlock()
}

// updateHourlyStats updates in-memory hourly statistics and persists when hour changes
func (c *Checker) updateHourlyStats(monitorID, latency int) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	now := time.Now().UTC()
	currentHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)

	stats, exists := c.hourlyStats[monitorID]

	// If hour changed or first time, persist old stats and create new
	if !exists || !stats.Hour.Equal(currentHour) {
		if exists {
			// Persist previous hour's stats
			median := calculateMedian(stats.Latencies)
			if err := c.db.UpsertHourlyLatency(monitorID, stats.Hour, median, stats.Max, len(stats.Latencies)); err != nil {
				log.Printf("failed to save hourly stats: %v", err)
			}
		}
		// Start new hour
		c.hourlyStats[monitorID] = &HourlyStats{
			Hour:      currentHour,
			Latencies: []int{latency},
			Max:       latency,
		}
	} else {
		// Update current hour
		stats.Latencies = append(stats.Latencies, latency)
		if latency > stats.Max {
			stats.Max = latency
		}
	}
}

// calculateMedian returns the median value from a slice of ints
func calculateMedian(values []int) int {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]int, len(values))
	copy(sorted, values)
	slices.Sort(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// GetStatusBlips returns the visual status for a monitor
func (c *Checker) GetStatusBlips(monitorID int) string {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	if rb, ok := c.recentChecks[monitorID]; ok {
		return rb.String()
	}
	return "□□□□□□□□□□" // No data yet
}

// Stop stops all monitor goroutines
func (c *Checker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Persist any remaining hourly stats before stopping
	c.statsMu.Lock()
	for monitorID, stats := range c.hourlyStats {
		median := calculateMedian(stats.Latencies)
		if err := c.db.UpsertHourlyLatency(monitorID, stats.Hour, median, stats.Max, len(stats.Latencies)); err != nil {
			log.Printf("failed to save final hourly stats: %v", err)
		}
	}
	c.statsMu.Unlock()

	for _, stop := range c.running {
		close(stop)
	}
	c.running = make(map[int]chan struct{})
}
