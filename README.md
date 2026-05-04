# Concierge

![concierge logo](./_img/logo.png)

Temporary public file hosting: each upload gets a key and TTL; files are removed after expiry unless a download is still in progress. With PostgreSQL (e.g. via Docker Compose), you also get Google sign-in, per-user API keys, roles, and quotas.

## Features

- TTL-based cleanup with active-download protection  
- Optional **Google OAuth + PostgreSQL**: sessions, `concierge_…` API keys, admin/guest, per-user quotas  
- **React UI** in the image at `/` when built (`fe/` → `fe/dist`)  
- Public downloads: `GET /api/v1/luggage/:key` (anyone with the key)

## Quick start: Docker Compose

From the repo root (where `docker-compose.yml` lives).

### PostgreSQL only

Use this when you run `./concierge` on the host and point it at `localhost:5432`.

```sh
docker compose up -d
```

Example DSN for the app:

`postgres://concierge:concierge@localhost:5432/concierge?sslmode=disable`

Services use `restart: unless-stopped` (they come back after a reboot unless you stopped the stack).

### App + database (recommended)

1. Copy **`.env.sample`** to **`.env`** and set at least:

   - `CONCIERGE_GOOGLE_CLIENT_ID`
   - `CONCIERGE_GOOGLE_CLIENT_SECRET`
   - `CONCIERGE_SESSION_SECRET` (≥ 16 characters)

   Optional: `CONCIERGE_OAUTH_REDIRECT_URL` (defaults to `http://localhost:8080/api/v1/auth/google/callback` in Compose), `CONCIERGE_BOOTSTRAP_ADMIN_EMAILS` (comma-separated; first login becomes admin).

2. Start the stack:

```sh
docker compose --profile app up --build
```

3. Open **http://localhost:8080** — UI and API are same origin. Start Google login at **http://localhost:8080/api/v1/auth/google** (redirect URI must match Google Cloud Console).

Use another env file path:

```sh
docker compose --env-file /path/to/secrets.env --profile app up --build
```

Do not commit `.env`. The runtime image runs **`docker-entrypoint.sh`**: it creates **`CONCIERGE_TMP_DIR`** (default **`/app/concierge_archive`**) and `chown`s it for the non-root app user so the bundled volume stays writable.

## Other ways to run

| Goal | Command / note |
|------|----------------|
| Build binary | `go build -o concierge` |
| Local binary | `./concierge` — defaults: port **8080**, temp **`/tmp/concierge`**, max upload **10 MiB** |
| Build image only | `docker build -t concierge .` |
| Flags | `-p` port, `-t` temp dir, `-l` max bytes, `-r` release (no Swagger UI), `-token` legacy bearer file (default `/secret/token`) |

With **`CONCIERGE_DATABASE_URL`** set, protected routes accept (in order): legacy bearer file token (if configured), **`Authorization: Bearer concierge_…`** API key, then **`concierge_session`** cookie after OAuth. Downloads stay public. Roles and quotas: see **`AGENTS.md`**.

Full **`CONCIERGE_*`** list: **`internal/config/env.go`** and **`.env.sample`**.

## Frontend (`fe/`)

```sh
cd fe && npm install && npm run dev
```

Proxies **`/api`** to **`http://127.0.0.1:8080`** by default (`fe/.env.example`). For OAuth while using Vite on another origin, set **`CONCIERGE_POST_LOGIN_REDIRECT`** (e.g. `http://localhost:5173/`). Production bundle: `npm run build` → **`fe/dist/`** (also baked into the Docker image).

## API sketch

Base path: **`/api/v1`**. If **`fe/dist/index.html`** is missing at startup, **`/`** serves **`web/index.html`**.

```sh
# Upload (anonymous or with auth as above)
curl -s -X POST http://localhost:8080/api/v1/luggage -F "file=@example.txt" -F "ttl=5"

# Download (public)
curl -O http://localhost:8080/api/v1/luggage/KEY
```

Swagger UI is available in non-release mode (`-r` not set).

## Docs for contributors

Behavior, flags, and layout for agents: **`AGENTS.md`**. When you change user-visible run steps or APIs, keep **README** and **AGENTS** in sync.
