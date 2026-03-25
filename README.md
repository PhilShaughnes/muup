# muup

A micro uptime monitor (~900 lines of Go). Inspired by Uptime Kuma but much simpler.

## Features

- HTTP endpoint monitoring with configurable intervals
- HTMX-powered web dashboard (no JavaScript framework)
- Retro terminal aesthetic (black on mustard yellow)
- SQLite storage — state changes + hourly latency aggregates
- Click-to-expand monitor details with time-range selector (24h to 1y)
- Self-signed certificate support (`skip_verify`)

## Quick Start

```bash
go build .
./muup --config config.toml

# Or with just
just run
```

Open http://localhost:8080

## Configuration

```toml
[[monitor]]
name = "Google"
url = "https://www.google.com"
interval = 30       # seconds between checks (default: 30)
timeout = 5000      # request timeout in milliseconds (default: 5000)
expected = 200      # expected HTTP status code (default: 200)

[[monitor]]
name = "Internal Service"
url = "https://192.168.1.100:8443"
skip_verify = true  # ignore TLS certificate errors
```

Config is read-only at startup. Restart muup to apply changes.

## CLI Options

```
muup [options]
  --config PATH    Config file (default: config.toml)
  --db PATH        SQLite database (default: muup.db)
  --port PORT      HTTP port (default: 8080)
```

## Web UI

The dashboard shows all monitors with:
- **Status dot** (up/down)
- **Current streak** ("up 3d 5h" or "down 2m")
- **Recent check blips** (last 10 checks as ■/□)
- **p50 latency** (24h)

Click a monitor row to expand details:
- Status graph (24 blocks across selected time range)
- Median and max latency for the selected range
- State change log with downtime durations
- Time range selector: 24h / 7d / 30d / 6mo / 1y

The dashboard auto-refreshes every 30 seconds via HTMX polling.

## Data Storage

**`state_changes` table** — one row per transition (up→down or down→up). Very low write volume regardless of check frequency.

**`hourly_latency` table** — latency stats (median, max, sample count) aggregated per hour per monitor. Accumulated in memory and flushed when the hour rolls over.

This keeps the database small indefinitely — a busy monitor with frequent checks produces the same write rate as a quiet one.

## Architecture

```
main.go      - CLI flags, startup orchestration
config.go    - TOML config loading
database.go  - SQLite setup, migrations, queries
monitor.go   - HTTP check logic, goroutine scheduler, hourly latency aggregation
web.go       - HTTP server, HTMX route handlers, template helpers
templates/   - HTML templates (layout, monitors list, details)
static/      - CSS
```

## Deployment

```bash
just deploy          # Cross-compile for Linux and rsync to wx.lan
just deploy-service  # Install and enable systemd user service
just ship            # deploy + restart
just logs            # Tail service logs
```

## License

MIT
