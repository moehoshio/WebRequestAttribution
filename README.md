# Web Request Attribution

🌐 **English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

See who is visiting your website — without sending any data to third parties.

Web Request Attribution reads your web server's (Nginx / Apache) access
logs and turns them into a clean dashboard: visitor trends, top pages,
browsers, status codes, a world map of request origins, and more. It is
a single small program with everything built in — no database server,
no separate frontend, nothing else to install.

## Screenshots

| Dark Mode | Light Mode |
|:-:|:-:|
| ![Dark Mode](docs/screenshot-dark.png) | ![Light Mode](docs/screenshot-light.png) |

## Highlights

- 🚀 **One file, zero dependencies** — download a single program and run it
- 📊 **Built-in dashboard** — open your browser and the stats are there
- 🗺️ **World map** — see which countries your requests come from (free, no API key); hover a bubble for details
- 📡 **Live updates** — new log lines show up in the dashboard automatically
- 🔍 **Powerful filters** — drill down by IP, page, domain, browser, OS, status code, keyword, and date; prefix a value with `!` to exclude it (multiple exclusions supported)
- 🔐 **Optional login** — run it open on your own machine, or require accounts on a server
- 🌐 **4 languages** — English, 繁體中文, 简体中文, 日本語
- 🐳 **Docker ready** — one command to deploy

## Get Started in 3 Steps

**1. Download** the file for your system from the
[latest release](https://github.com/moehoshio/WebRequestAttribution/releases/latest):

| Your system | Download |
|---|---|
| Linux (x86_64) | `web-req-attr-linux-amd64` |
| Linux (ARM, e.g. Raspberry Pi) | `web-req-attr-linux-arm64` |
| macOS (Intel) | `web-req-attr-darwin-amd64` |
| macOS (Apple Silicon) | `web-req-attr-darwin-arm64` |
| Windows | `web-req-attr-windows-amd64.exe` |

**2. Run it** (Linux/macOS — on Windows just double-click the `.exe`):

```bash
chmod +x web-req-attr-linux-amd64
./web-req-attr-linux-amd64
```

**3. Open** <http://localhost:8080> in your browser. Done! 🎉

On first run the program creates a `config.json` for you. You can set
everything else up — which log files to watch, keywords to track —
right from the **Settings** tab in your browser.

## Point It at Your Logs

To analyze your web server's traffic, tell it where your access log is.
Either add the log source from the **Settings** tab in the browser, or
edit `config.json`:

```json
{
  "listen_addr": ":8080",
  "db_path": "./data/stats.db",
  "watch": true,
  "sources": [
    {
      "name": "my-website",
      "type": "file",
      "path": "/var/log/nginx/access.log",
      "format": { "engine": "nginx", "preset": "combined" }
    }
  ]
}
```

In plain words:

- `listen_addr` — the port the dashboard runs on (`:8080` → http://localhost:8080)
- `db_path` — where the collected statistics are stored (a single file)
- `watch` — keep reading new log lines as they arrive
- `sources` — the log file(s) to read. The example above is the standard
  Nginx setup; for Apache use `"engine": "apache"`.

Restart the program after editing, and your traffic appears in the
dashboard. A ready-made example with comments is in
[`config.example.json`](config.example.json), and every option is
explained in the [configuration reference](docs/CONFIGURATION.md).

### Bring in your old logs

Watching only picks up *new* lines. To load the history you already
have, run once:

```bash
./web-req-attr-linux-amd64 -import /var/log/nginx/access.log
```

## Turn On Login

Out of the box (with no account configured) anyone who can open the
page can use the dashboard — fine on your own computer, **not** fine on
a public server. To require login, add this to `config.json` before
the first start:

```json
"auth": {
  "require_account": true
}
```

If you don't set a password, a random one is generated for the `admin`
user and printed in the program's startup output — sign in with it,
then change it from the **Users** tab. More details in the
[configuration reference](docs/CONFIGURATION.md#authentication-auth).

## Run It as a Service

For keeping it running permanently on a server, the
**[deployment guide](docs/DEPLOYMENT.md)** walks through every option
step by step. The short version with Docker:

```bash
git clone https://github.com/moehoshio/WebRequestAttribution.git
cd WebRequestAttribution
cp config.example.json config.json   # edit to point at your logs
docker-compose up -d
```

The guide also covers running without Docker (systemd), putting the
dashboard behind HTTPS, and updating safely.

## Frequently Asked Questions

**Does my data leave my server?**
No. Everything is stored locally in a single SQLite file. The only
optional outside call is the IP-to-country lookup for the world map
(can be turned off in Settings).

**My log format is unusual — can it still be parsed?**
Yes. Besides the standard Nginx/Apache formats, you can describe any
line format with a custom pattern. See the
[configuration reference](docs/CONFIGURATION.md#custom-formats).

**The world map says "no geolocation data yet".**
Locations are looked up gradually in the background — give it a few
minutes after the first import.

## Documentation

| Document | What's inside |
|---|---|
| [Deployment guide](docs/DEPLOYMENT.md) | Docker, systemd, HTTPS, updating, troubleshooting |
| [Configuration reference](docs/CONFIGURATION.md) | Every `config.json` option explained |
| [Developer guide](docs/DEVELOPMENT.md) | Building from source, HTTP API, project internals |
| [Contributing](CONTRIBUTING.md) | How to contribute code |

## License

MIT License
