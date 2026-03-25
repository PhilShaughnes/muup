package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds the HTTP server dependencies
type Server struct {
	db      *DB
	checker *Checker
	tmpl    *template.Template
}

// MonitorStats holds calculated stats for display
type MonitorStats struct {
	Monitor         Monitor
	CurrentUp       bool
	StreakStart     time.Time
	StreakDuration  time.Duration
	MedianLatency   int
	MaxLatency      int
	StateChanges    []StateChange
	SelectedRange   string
	StatusBlips     string
	StatusGraph     string // 24-block aggregated graph for details
	LastCheckTime   time.Time
}

// NewServer creates a new web server
func NewServer(db *DB, checker *Checker) (*Server, error) {
	funcMap := template.FuncMap{
		"formatStreak":   FormatStreak,
		"formatLatency":  FormatLatency,
		"formatTime":     FormatTime,
		"formatDuration": FormatDuration,
		"sub":            func(a, b int) int { return a - b },
		"timeBetween":    func(newer, older time.Time) time.Duration { return older.Sub(newer) },
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{db: db, checker: checker, tmpl: tmpl}, nil
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	// Routes
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/monitors", s.handleMonitors)
	mux.HandleFunc("/monitors/", s.handleMonitorDetails)

	log.Printf("Starting server on :%d", port)
	return http.ListenAndServe(":"+strconv.Itoa(port), mux)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.tmpl.ExecuteTemplate(w, "layout.html", nil)
}

func (s *Server) handleMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.db.GetMonitors()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var stats []MonitorStats
	for _, m := range monitors {
		up, start, dur := s.db.GetStreak(m.ID)
		median, max, _ := s.db.GetLatencyStats(m.ID, 24*time.Hour)
		statusBlips := s.checker.GetStatusBlips(m.ID)

		stats = append(stats, MonitorStats{
			Monitor:        m,
			CurrentUp:      up,
			StreakStart:    start,
			StreakDuration: dur,
			MedianLatency:  median,
			MaxLatency:     max,
			StatusBlips:    statusBlips,
		})
	}

	s.tmpl.ExecuteTemplate(w, "monitors.html", stats)
}

func (s *Server) handleMonitorDetails(w http.ResponseWriter, r *http.Request) {
	// Parse ID from /monitors/123
	idStr := strings.TrimPrefix(r.URL.Path, "/monitors/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	m, err := s.db.GetMonitor(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Get time range from query parameter (default to 24h)
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	// Parse time range
	var duration time.Duration
	switch timeRange {
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	case "6mo":
		duration = 180 * 24 * time.Hour
	case "1y":
		duration = 365 * 24 * time.Hour
	default:
		duration = 24 * time.Hour
		timeRange = "24h"
	}

	up, start, dur := s.db.GetStreak(id)
	median, max, _ := s.db.GetLatencyStats(id, duration)
	stateChanges, _ := s.db.GetStateChanges(id, duration, 50)
	statusGraph := buildStatusGraph(stateChanges, duration, up)

	stats := MonitorStats{
		Monitor:        *m,
		CurrentUp:      up,
		StreakStart:    start,
		StreakDuration: dur,
		MedianLatency:  median,
		MaxLatency:     max,
		StateChanges:   stateChanges,
		SelectedRange:  timeRange,
		StatusGraph:    statusGraph,
		LastCheckTime:  s.checker.GetLastCheckTime(id),
	}

	s.tmpl.ExecuteTemplate(w, "details.html", stats)
}

// FormatStreak formats a duration as "up 10mo 2d" or "down 2h 15m"
func FormatStreak(d time.Duration, up bool) string {
	prefix := "up"
	if !up {
		prefix = "down"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%s %ds", prefix, int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%s %dm", prefix, int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%s %dh %dm", prefix, int(d.Hours()), int(d.Minutes())%60)
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%s %dd %dh", prefix, int(d.Hours()/24), int(d.Hours())%24)
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%s %dmo %dd", prefix, int(d.Hours()/24/30), int(d.Hours()/24)%30)
	default:
		return fmt.Sprintf("%s %dy %dmo", prefix, int(d.Hours()/24/365), int(d.Hours()/24/30)%12)
	}
}

// FormatLatency formats latency in ms
func FormatLatency(ms int) string {
	if ms == 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", ms)
}

// FormatTime formats a timestamp as HH:MM
func FormatTime(t time.Time) string {
	return t.Format("15:04")
}

// FormatDuration formats a duration in a compact way (e.g., "6m", "2h", "3d")
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// buildStatusGraph creates a 24-block aggregated status graph
// Each block shows the state at that point in time
func buildStatusGraph(changes []StateChange, timeRange time.Duration, currentUp bool) string {
	const blocks = 24
	now := time.Now()
	rangeStart := now.Add(-timeRange)
	blockDuration := timeRange / blocks

	result := make([]rune, blocks)

	// For each block, check state at that time
	for i := 0; i < blocks; i++ {
		blockTime := rangeStart.Add(time.Duration(i) * blockDuration)

		// Find the most recent state change before this block time
		state := currentUp
		for j := 0; j < len(changes); j++ {
			if changes[j].Timestamp.Before(blockTime) {
				state = changes[j].NewState
				break
			}
		}

		if state {
			result[i] = '■'
		} else {
			result[i] = '□'
		}
	}

	return string(result)
}
