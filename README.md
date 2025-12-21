# muup

A micro uptime monitor (~1000 lines of Go). Inspired by Uptime Kuma but much simpler.

## Features

- HTTP endpoint monitoring with configurable intervals
- HTMX-powered web dashboard (no JavaScript framework)
- Retro terminal aesthetic (black on mustard yellow)
- SQLite storage with automatic data rollup
- Click-to-expand monitor details with uptime stats and latency sparklines
- Self-signed certificate support (`skip_verify`)

## Quick Start

```bash
# Build and run
go build .
./muup --config config.toml

# Or with just
just build
just run
```

Open http://localhost:8080

## Configuration

Create `config.toml`:

```toml
[server]
port = 8080

[[monitor]]
name = "Google"
url = "https://www.google.com"
interval = 30       # seconds between checks
timeout = 5000      # request timeout in milliseconds
expected = 200      # expected HTTP status code

[[monitor]]
name = "Internal Service"
url = "https://192.168.1.100:8443"
skip_verify = true  # ignore TLS certificate errors
```

Config is read-only at startup. Restart muup to apply changes.

## CLI Options

```bash
muup [options]
  --config PATH    Config file (default: config.toml)
  --db PATH        SQLite database (default: muup.db)
  --port PORT      Override config port
```

## Web UI

The dashboard shows all monitors with:
- **Status indicator** (green/red dot)
- **Current streak** ("up 3d 5h" or "down 2m")
- **Average latency** (24h)

Click a monitor row to expand details:
- Uptime percentages (24h, 7d, 30d)
- Response time stats with Unicode sparkline graph
- Recent check history
- "Check Now" button for immediate check

The UI auto-refreshes every 30 seconds via HTMX polling.

## Data Storage

### Two-tier retention

**Raw checks** (`checks` table)
- Every individual check result
- Kept for 7 days
- Used for recent history and streak calculation

**Daily rollups** (`checks_daily` table)
- Aggregated daily stats (uptime %, avg latency, check counts)
- Kept for 365 days
- Used for long-term uptime reporting

### Rollup process

A background worker runs daily at midnight UTC:
1. Aggregates yesterday's raw checks into a single daily record per monitor
2. Deletes raw checks older than 7 days

This keeps the database small while preserving historical uptime data.

## Themes

Edit CSS variables in `static/style.css`. Three presets included:

```css
/* Black on mustard (default) */
--bg: #f4e04d; --fg: #1a1a1a; --border: #8a7a00;

/* Amber CRT */
--bg: #1a0800; --fg: #ffb000; --border: #663300;

/* Green terminal */
--bg: #0c0c0c; --fg: #00ff00; --border: #333;
```

## Deployment

Deploy to a server (e.g., Wyse 3040 thin client):

```bash
just deploy          # Cross-compile for Linux and scp to server
just deploy-service  # Install systemd user service
just ship            # Deploy + restart service
just logs            # Tail service logs
```

The `justfile` targets a host called `wx.lan` - edit as needed.

### Systemd service

```ini
[Unit]
Description=muup uptime monitor

[Service]
ExecStart=%h/muup/muup --config %h/muup/config.toml --db %h/muup/muup.db
Restart=always

[Install]
WantedBy=default.target
```

## Architecture

```
main.go      - CLI flags, startup orchestration
config.go    - TOML config loading
models.go    - Data structures
db.go        - SQLite setup, migrations, queries
checker.go   - HTTP check logic, goroutine scheduler
rollup.go    - Daily aggregation worker
render.go    - Template helpers (streak formatting, sparklines)
web.go       - HTTP server, HTMX route handlers
templates/   - HTML templates (layout, monitors list, details)
static/      - CSS
```

## License

MIT
