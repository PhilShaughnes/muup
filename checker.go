package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"sync"
	"time"
)

// Checker manages HTTP checks for all monitors
type Checker struct {
	db      *DB
	mu      sync.RWMutex
	running map[int]chan struct{} // stop channels per monitor ID
}

func NewChecker(db *DB) *Checker {
	return &Checker{db: db, running: make(map[int]chan struct{})}
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

// CheckNow performs an immediate check (used by "Check Now" button)
func (c *Checker) CheckNow(m Monitor) {
	c.check(m)
}

// check performs a single HTTP check and saves the result
func (c *Checker) check(m Monitor) {
	transport := &http.Transport{}
	if m.SkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(m.Timeout) * time.Millisecond,
		// Don't follow redirects - we want to check the actual endpoint
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

	if err := c.db.SaveCheck(m.ID, up, latency, errMsg); err != nil {
		log.Printf("[%s] save failed: %v", m.Name, err)
	}
}

// Stop stops all monitor goroutines
func (c *Checker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, stop := range c.running {
		close(stop)
	}
	c.running = make(map[int]chan struct{})
}
