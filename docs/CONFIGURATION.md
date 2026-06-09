# Configuration Reference

All configuration is supplied via **`config.json`** and command-line
flags only — no environment variables are read.

Running the binary in a directory without a `config.json` automatically
creates one with safe defaults (no sources, watching disabled), so you
can do all further configuration from the **Settings** tab in the web
UI without touching a terminal.

> **Bootstrap vs. runtime settings.** `listen_addr`, `db_path`,
> `allowed_log_roots`, and `auth` are read on every start.
> `watch`, `keywords`, `sources`, and `geo.enabled` are only read on the
> **very first** launch (to seed the database) and are edited from the
> **Settings** tab thereafter — later edits to those fields in
> `config.json` have no effect.

## Full Example

```json
{
  "listen_addr": ":8080",
  "db_path": "./data/stats.db",
  "allowed_log_roots": ["/var/log"],
  "watch": true,
  "keywords": ["login", "admin", "api", "search"],
  "sources": [
    {
      "name": "nginx-main",
      "type": "file",
      "path": "/var/log/nginx/access.log",
      "read_compressed": false,
      "format": { "engine": "nginx", "preset": "combined" }
    }
  ],
  "auth": {
    "bootstrap_admin": { "username": "admin", "password": "change-me-on-first-login" },
    "require_account": false,
    "session_ttl_hours": 24,
    "cookie_secure": false
  },
  "geo": { "enabled": true }
}
```

## Top-Level Fields

| Field | Description | Default |
|---|---|---|
| `listen_addr` | HTTP server listen address | `:8080` |
| `db_path` | SQLite database file path | `./data/stats.db` |
| `allowed_log_roots` | Filesystem prefixes that sources added from the Settings UI may point at | – |
| `watch` | Enable real-time log monitoring | `false` |
| `keywords` | List of keywords to track | `[]` |
| `sources` | List of log sources to ingest from | `[]` |
| `auth` | Account-system settings (below) | – |
| `geo` | IP geolocation / world-map settings (below) | – |

## Sources

Each entry in `sources` describes one input. Common fields:

| Field | Description |
|---|---|
| `name` | Human-readable label (optional) |
| `type` | `file`, `dir`, or `syslog` |
| `format.engine` | `nginx`, `apache`, `custom`, or `auto` |
| `format.preset` | For `nginx`/`apache`: e.g. `combined`, `common`, `vhost_combined` |
| `format.pattern` | For `custom`: log pattern using Nginx-style `$variable` tokens |

### File sources (`type: "file"`)

| Field | Description |
|---|---|
| `path` | Path to the live access log file |
| `read_compressed` | If `true`, also import sibling `*.gz` files once on startup |

### Directory sources (`type: "dir"`)

| Field | Description |
|---|---|
| `path` | Directory to scan |
| `pattern` | Filename glob matched against the basename (e.g. `access*.log*`); empty matches every file |
| `recursive` | If `true`, descend into subdirectories |
| `read_compressed` | If `true`, also one-shot import any matching `*.gz` archives |

Directory sources resume safely across restarts: rotation
(rename + recreate or copytruncate) is detected and never causes lines
to be re-ingested or skipped. Plain files are live-tailed; compressed
archives matching the glob are imported once when discovered.

### Syslog sources (`type: "syslog"`)

| Field | Description |
|---|---|
| `addr` | Listen address (e.g. `:1514`) |
| `proto` | `udp`, `tcp`, or `both` |

To send Nginx logs via syslog, add to your Nginx config:

```nginx
access_log syslog:server=127.0.0.1:1514,facility=local7,tag=nginx combined;
```

## Log Formats

### Nginx / Apache presets

Use `format.engine: "nginx"` or `"apache"` with a `preset`:

- `combined` (default):
  `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"`
- `common`
- `vhost_combined`:
  `$host $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"`

Apache logs are currently supported by reading log files (or via
syslog). Apache `%`-style LogFormat tokens are not yet supported — see
[TODO.md](TODO.md).

### Custom formats

Set `format.engine` to `"custom"` and provide a `pattern` using
Nginx-style `$variable` tokens, e.g.:

```json
"format": {
  "engine": "custom",
  "pattern": "$remote_addr - $remote_user [$time_local] \"$request\" $status $body_bytes_sent \"$http_referer\" \"$http_user_agent\""
}
```

Supported variables: `$remote_addr`, `$remote_user`, `$time_local`,
`$msec`, `$request`, `$request_method`, `$request_uri`, `$uri`,
`$status`, `$body_bytes_sent`, `$bytes_sent`, `$http_referer`,
`$http_user_agent`, `$http_host`, `$host`, `$server_name`,
`$request_time`, `$upstream_response_time`. Unknown variables are
tolerated (matched and discarded). See the
[full variable reference](custom-format-variables.md).

### Compressed logs

Setting `read_compressed: true` on a file source imports rotated `.gz`
archives in the same directory once on startup. `.bz2` / `.xz` support
is tracked in [TODO.md](TODO.md).

## Authentication (`auth`)

| Field | Description | Default |
|---|---|---|
| `auth.bootstrap_admin` | `{username, password}` used to seed the first admin account | – |
| `auth.require_account` | Force login even before any user exists | `false` |
| `auth.session_ttl_hours` | Session lifetime | `24` |
| `auth.cookie_secure` | Set the session cookie's `Secure` flag — turn on when serving over HTTPS | `false` |

Until the first user account is created the dashboard runs in
**no-account mode**: anyone reaching the UI can administer the server.
Create your first user from the **Users** tab (or seed one via
`auth.bootstrap_admin`) to require login. Once at least one user
exists, the bootstrap block is ignored and can be removed.

Setting `auth.require_account: true` makes login mandatory from the
start: if no account exists at launch (and `bootstrap_admin` provides
no usable password), an `admin` account with a random password is
created and the credentials are printed to the server log (look for the
boxed banner), so the dashboard is never reachable anonymously.

Sessions are HttpOnly cookies; non-GET API calls also require the
`X-CSRF-Token` header (the dashboard JS handles this automatically).

## Geolocation (`geo`)

| Field | Description | Default |
|---|---|---|
| `geo.enabled` | Enable background IP geolocation (also toggleable from the **Settings** tab) | `true` |
| `geo.endpoint` | Override the provider URL template (`{ip}` is substituted) | ip-api.com |

See [DEVELOPMENT.md](DEVELOPMENT.md#geolocation-internals) for how
lookups and caching work.
