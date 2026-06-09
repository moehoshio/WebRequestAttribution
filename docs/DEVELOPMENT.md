# Developer Guide

This document covers building from source, the command-line interface,
the HTTP API, and the project layout. For contribution workflow and
testing requirements, see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Building from Source

Prerequisites: **Go 1.24+** (the SQLite driver is pure Go, so CGO and a
C compiler are not required).

```bash
git clone https://github.com/moehoshio/WebRequestAttribution.git
cd WebRequestAttribution
go build -o web-req-attr ./cmd/
```

For a smaller, fully static binary:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o web-req-attr ./cmd/
```

Cross-compile by setting `GOOS` / `GOARCH`, e.g.:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o web-req-attr-linux-arm64 ./cmd/
```

Run the test suite:

```bash
go test ./...
```

There is also a convenience script, [`deploy.sh`](../deploy.sh), that
builds from source (or via Docker when Go is unavailable) and creates a
default `config.json` and `data/` directory.

## Command-Line Flags

| Flag | Description | Default |
|---|---|---|
| `-config` | Path to the config file | `config.json` |
| `-import` | Import a log file once and exit | – |
| `-import-format` | Parser engine for `-import`: `auto`, `nginx`, `apache`, `custom` | `auto` |
| `-import-preset` | Parser preset for `-import` (e.g. `combined`, `common`) | – |
| `-import-pattern` | Custom pattern for `-import` when `-import-format=custom` | – |

Example one-shot import:

```bash
./web-req-attr -import /var/log/nginx/access.log
```

## HTTP API

All `/api/*` endpoints are gated behind the session cookie (unless the
server is in no-account mode). Non-GET calls additionally require the
`X-CSRF-Token` header matching the CSRF cookie (double-submit pattern);
the bundled dashboard JS handles this automatically.

### GET /api/stats

Statistics summary. Supported query-parameter filters:

| Parameter | Description |
|---|---|
| `start` | Start date (`YYYY-MM-DD`) |
| `end` | End date (`YYYY-MM-DD`) |
| `ip` | IP address (fuzzy) |
| `path` | Path (fuzzy) |
| `domain` | Domain (fuzzy) |
| `query` | Query string (fuzzy) |
| `method` | HTTP method |
| `status` | HTTP status code |
| `os` | Operating system |
| `browser` | Browser |
| `keyword` | Keyword |

**Exclusions:** every filter above also supports negative matching.
Prefix a value with `!` to exclude matching rows, and combine several
exclusions with commas or by repeating the parameter — they are AND-ed
together. A parameter may mix one positive value with any number of
exclusions. Examples:

```
/api/stats?path=!/health
/api/stats?path=!/health,!/metrics
/api/stats?path=/api,!/api/internal&status=!404&browser=!Bot
```

Response example:

```json
{
  "total_requests": 12345,
  "top_paths": [{"name": "/api/users", "count": 500}],
  "top_ips": [{"name": "192.168.1.1", "count": 300}],
  "top_domains": [{"name": "example.com", "count": 200}],
  "top_os": [{"name": "Windows", "count": 5000}],
  "top_browsers": [{"name": "Chrome", "count": 8000}],
  "top_keywords": [{"name": "api", "count": 1500}],
  "status_codes": [{"name": "200", "count": 10000}],
  "requests_per_day": [{"date": "2023-10-10", "count": 500}]
}
```

### GET /api/requests

Paginated request list. Accepts the same filters as `/api/stats` plus:

| Parameter | Description |
|---|---|
| `limit` | Results per page (default 100) |
| `offset` | Offset |

Each row also includes `country` and `country_code` once the source IP
has been geolocated.

### GET /api/geo

Per-country request counts with a representative coordinate for the
world map. Honours the same filters as `/api/stats`.

```json
{
  "countries": [
    {"country_code": "US", "country": "United States", "lat": 37.4, "lon": -122.0, "count": 2}
  ],
  "total": 2
}
```

### GET /api/realtime

Per-minute request counts for the live trend chart. Accepts the same
filters as `/api/stats` plus `minutes` (window size).

## Project Layout

```
cmd/                 Entry point and the embedded single-page dashboard (cmd/static/index.html)
internal/api/        HTTP handlers for /api/*
internal/auth/       Accounts, sessions, CSRF
internal/config/     Bootstrap config loading (config.json)
internal/runtimeconfig/  Runtime-tunable settings persisted in SQLite
internal/parser/     Log-line parsers (nginx, apache, custom, auto-detect)
internal/geo/        Background IP → country resolution and caching
internal/watcher/    File / directory / syslog ingestion with rotation tracking
docs/                Documentation
```

## How Ingestion Works

- **File sources** are live-tailed. Per-file position (inode, offset,
  size, fingerprint) is tracked in the `file_state` table, so log
  rotation (rename + recreate or copytruncate) is detected and lines are
  never re-ingested or skipped across restarts.
- **Directory sources** scan for files matching a glob; plain files are
  tailed, and `*.gz` archives are imported once when discovered (when
  `read_compressed` is true) and then ignored.
- **Syslog sources** listen on UDP/TCP and parse the message payload
  with the configured engine.

## Geolocation Internals

Source IPs are resolved in the background to a country (and region
where available) using the free [ip-api.com](https://ip-api.com)
service — no API key required — and cached in SQLite so each IP is only
looked up once. Private, loopback, and reserved addresses are never
sent upstream. Resolution is best-effort: with no outbound network the
rest of the dashboard keeps working and the map shows whatever has
already been resolved. The provider URL can be overridden with
`geo.endpoint` (`{ip}` is substituted).

## Related Documents

- [Configuration reference](CONFIGURATION.md)
- [Deployment guide](DEPLOYMENT.md)
- [Custom format variable reference](custom-format-variables.md)
- [TODO / roadmap](TODO.md)
