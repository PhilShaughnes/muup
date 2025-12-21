package main

import (
	"embed"
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

// NewServer creates a new web server
func NewServer(db *DB, checker *Checker) (*Server, error) {
	funcMap := template.FuncMap{
		"formatStreak":  FormatStreak,
		"formatUptime":  FormatUptime,
		"formatLatency": FormatLatency,
		"sparkline":     GenerateSparkline,
		"minMax":        MinMax,
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
	mux.HandleFunc("/check/", s.handleCheckNow)

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
		stats = append(stats, MonitorStats{
			Monitor:        m,
			CurrentUp:      up,
			StreakStart:    start,
			StreakDuration: dur,
			AvgLatency24h:  s.db.GetAvgLatency(m.ID, 24*time.Hour),
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

	up, start, dur := s.db.GetStreak(id)
	checks, _ := s.db.GetRecentChecks(id, 20)
	latencies := s.db.GetLatencyHistory(id, 24)

	stats := MonitorStats{
		Monitor:        *m,
		CurrentUp:      up,
		StreakStart:    start,
		StreakDuration: dur,
		Uptime24h:      s.db.GetUptime(id, 24*time.Hour),
		Uptime7d:       s.db.GetUptime(id, 7*24*time.Hour),
		Uptime30d:      s.db.GetUptime(id, 30*24*time.Hour),
		AvgLatency24h:  s.db.GetAvgLatency(id, 24*time.Hour),
		AvgLatency7d:   s.db.GetAvgLatency(id, 7*24*time.Hour),
		RecentChecks:   checks,
		LatencyHistory: latencies,
	}

	s.tmpl.ExecuteTemplate(w, "details.html", stats)
}

func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// Parse ID from /check/123
	idStr := strings.TrimPrefix(r.URL.Path, "/check/")
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

	// Perform check
	s.checker.CheckNow(*m)

	// Return updated details
	s.handleMonitorDetails(w, r)
}
