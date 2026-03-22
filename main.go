package main

import (
	"flag"
	"log"
)

func main() {
	configPath := flag.String("config", "config.toml", "config file")
	dbPath := flag.String("db", "muup.db", "database file")
	port := flag.Int("port", 8080, "HTTP port")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := OpenDB(*dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	if err := db.SyncMonitors(cfg.Monitors); err != nil {
		log.Fatalf("sync: %v", err)
	}

	monitors, err := db.GetMonitors()
	if err != nil {
		log.Fatalf("monitors: %v", err)
	}

	checker := NewChecker(db)
	checker.Start(monitors)
	defer checker.Stop()

	server, err := NewServer(db, checker)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	log.Printf("Monitoring %d endpoints on :%d", len(monitors), *port)
	log.Fatal(server.Start(*port))
}
