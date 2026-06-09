# Deployment Guide

How to run Web Request Attribution as a long-lived service. Three
common setups, from simplest to most involved:

1. [Docker Compose](#option-1-docker-compose-recommended) — recommended
2. [Plain Docker](#option-2-plain-docker)
3. [Binary + systemd](#option-3-binary--systemd-no-docker)

Then: [putting it behind HTTPS](#serving-over-https-reverse-proxy),
[importing old logs](#importing-historical-logs), and
[updating](#updating).

## Before You Start

- The dashboard listens on port **8080** by default (`listen_addr`).
- All data lives in one SQLite file (`./data/stats.db` by default) —
  back up the `data/` directory and `config.json` and you've backed up
  everything.
- The service needs **read access** to your web server's log files
  (e.g. `/var/log/nginx/`).
- Until you create a user account, anyone who can reach the port can
  administer the dashboard. On a public server, either set
  `auth.require_account: true` before exposing it, or create an account
  immediately after the first start. See
  [Authentication](CONFIGURATION.md#authentication-auth).

## Option 1: Docker Compose (recommended)

```bash
git clone https://github.com/moehoshio/WebRequestAttribution.git
cd WebRequestAttribution
cp config.example.json config.json   # edit to taste
docker-compose up -d
```

The bundled [`docker-compose.yml`](../docker-compose.yml) builds the
image and mounts:

- `./data` → `/app/data` (the database — persists across restarts)
- `/var/log/nginx` → `/var/log/nginx` (read-only, the logs to analyze)
- `./config.json` → `/app/config.json` (read-only)

If your logs live elsewhere, edit the `volumes:` section accordingly
and make sure the `path` in `config.json` matches the path **inside**
the container.

Check it's running and view the startup log (this is where a generated
admin password would be printed):

```bash
docker-compose ps
docker-compose logs -f
```

## Option 2: Plain Docker

```bash
docker build -t web-req-attr .
docker run -d \
  --name web-req-attr \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /var/log/nginx:/var/log/nginx:ro \
  -v "$(pwd)/data":/app/data \
  -v "$(pwd)/config.json":/app/config.json:ro \
  web-req-attr
```

## Option 3: Binary + systemd (no Docker)

1. Download the binary for your platform from the
   [latest release](https://github.com/moehoshio/WebRequestAttribution/releases/latest)
   (or [build from source](DEVELOPMENT.md#building-from-source)) and
   install it:

   ```bash
   sudo install -m 755 web-req-attr-linux-amd64 /usr/local/bin/web-req-attr
   sudo mkdir -p /etc/web-req-attr /var/lib/web-req-attr
   ```

2. Create `/etc/web-req-attr/config.json` (start from
   [`config.example.json`](../config.example.json)); point `db_path` at
   the data directory:

   ```json
   {
     "listen_addr": ":8080",
     "db_path": "/var/lib/web-req-attr/stats.db",
     "watch": true,
     "sources": [
       {
         "name": "nginx",
         "type": "file",
         "path": "/var/log/nginx/access.log",
         "format": { "engine": "nginx", "preset": "combined" }
       }
     ]
   }
   ```

3. Create `/etc/systemd/system/web-req-attr.service`:

   ```ini
   [Unit]
   Description=Web Request Attribution
   After=network.target

   [Service]
   ExecStart=/usr/local/bin/web-req-attr -config /etc/web-req-attr/config.json
   WorkingDirectory=/var/lib/web-req-attr
   Restart=on-failure
   # Run as a dedicated user that can read your web server's logs.
   # On Debian/Ubuntu, membership in the "adm" group usually suffices:
   User=www-data
   SupplementaryGroups=adm
   NoNewPrivileges=true
   ProtectSystem=full

   [Install]
   WantedBy=multi-user.target
   ```

4. Enable and start:

   ```bash
   sudo chown -R www-data /var/lib/web-req-attr
   sudo systemctl daemon-reload
   sudo systemctl enable --now web-req-attr
   sudo systemctl status web-req-attr
   journalctl -u web-req-attr -f   # startup log / generated admin password
   ```

## Serving over HTTPS (reverse proxy)

Don't expose port 8080 directly on the internet — put it behind your
existing web server with TLS. Example Nginx server block:

```nginx
server {
    listen 443 ssl;
    server_name stats.example.com;

    # ssl_certificate / ssl_certificate_key ... (e.g. via certbot)

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

When serving over HTTPS, set in `config.json`:

```json
"auth": { "cookie_secure": true }
```

and restrict the local port to localhost by setting
`"listen_addr": "127.0.0.1:8080"`.

## Importing Historical Logs

A live source only picks up lines written after it starts watching. To
ingest existing logs once:

```bash
web-req-attr -import /var/log/nginx/access.log
```

Or set `read_compressed: true` on a file source to also import rotated
`*.gz` archives in the same directory on first startup. With Docker,
run the import inside the container:

```bash
docker-compose exec web-req-attr /app/web-req-attr -import /var/log/nginx/access.log
```

Importing is idempotent for watched sources — file positions are
tracked in the database, so restarts never re-ingest or skip lines.

## Updating

- **Docker Compose:** `git pull && docker-compose up -d --build`
- **Binary:** stop the service, replace the binary, start it again.
  The SQLite schema is migrated automatically on startup.

Your data is safe across updates as long as the `data/` directory (or
wherever `db_path` points) is preserved.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Dashboard loads but shows no data | The source `path` is wrong, or the process can't read the log file (check permissions / Docker volume mounts) |
| "No geolocation data yet" on the world map | IPs are resolved gradually in the background; also requires outbound network access |
| Forgot the admin password | `auth.bootstrap_admin` is ignored once users exist — delete the rows in the `users` table of the SQLite DB (the next start re-seeds from `bootstrap_admin` or prints a generated password), or start fresh with a new `db_path` |
| Port already in use | Change `listen_addr` in `config.json` |
