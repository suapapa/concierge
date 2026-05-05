# Concierge

![concierge logo](./_img/logo.png)

Temporary public file hosting: each upload gets a key and TTL stored as **`luggage_objects.expires_at`**; a **DB-driven sweep** removes expired payloads from disk (and metadata rows) when no download is in progress (in-flight counts live in PostgreSQL **`luggage_active_refs`** so multiple app instances stay consistent). **PostgreSQL is required** for sessions, per-object metadata (`luggage_objects`), Google sign-in, per-user API keys, roles, and quotas.

## Features

- TTL-based cleanup via **`expires_at` sweeps** (configurable interval; set interval **`0`** to rely only on an external job) with active-download protection (DB-backed ref counts across instances)  
- **Google OAuth + PostgreSQL**: sessions, `concierge_…` API keys, admin/guest, per-user quotas; object metadata in the database (payload bytes stay on disk)  
- **React UI** in the image at `/` when built (`fe/` → `fe/dist`)  
- Public downloads: `GET /api/v1/luggage/:key` (anyone with the key)

## Quick start: Docker Compose

From the repo root (where `docker-compose.yml` lives).

### PostgreSQL only

Use this when you run `./concierge` on the host and point it at `localhost:5432`.

```sh
docker compose up -d
```

Example DSN for the app (also set Google OAuth and session env — see **`.env.sample`**):

`postgres://concierge:concierge@localhost:5432/concierge?sslmode=disable`

Services use `restart: unless-stopped` (they come back after a reboot unless you stopped the stack).

### App + database (recommended)

1. Copy **`.env.sample`** to **`.env`** and set at least:

   - `CONCIERGE_DATABASE_URL` (or rely on Compose defaults for the app service)
   - `CONCIERGE_GOOGLE_CLIENT_ID`
   - `CONCIERGE_GOOGLE_CLIENT_SECRET`
   - `CONCIERGE_SESSION_SECRET` (≥ 16 characters)

   Optional: `CONCIERGE_OAUTH_REDIRECT_URL` (defaults to `http://localhost:8080/api/v1/auth/google/callback` in the binary and in Compose), `CONCIERGE_BOOTSTRAP_ADMIN_EMAILS` (comma-separated; first login becomes admin).

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

### Upgrading from yaml sidecars

If an older deployment stored metadata only in `info.yaml` under the temp directory, set **`CONCIERGE_LUGGAGE_BACKFILL=1`** once at startup to import rows into **`luggage_objects`** and remove each `info.yaml`, then unset the variable. See **`AGENTS.md`**.

## Other ways to run

| Goal | Command / note |
|------|----------------|
| Prebuilt image (after merge to `main` or a git tag push) | **`docker pull ghcr.io/suapapa/concierge:latest`** — multi-arch (**`linux/amd64`**, **`linux/arm64`**); tags also include `main`, `sha-<abbrev>`, and the **git tag** (e.g. `v1.2.3`) when you push that tag. [.github/workflows/docker-publish.yml](.github/workflows/docker-publish.yml). Forks use `ghcr.io/<your-org>/concierge`. Private repos may need `docker login ghcr.io` or a public package. |
| Build binary | `go build -o concierge` |
| Local binary | **`CONCIERGE_DATABASE_URL`** and OAuth/session env are required — see **`.env.sample`**. Defaults: port **8080**, temp **`./concierge_archive/`**, max upload **10 MiB** |
| Build image only | `docker build -t concierge .` |
| Flags | `-p` port, `-t` temp dir, `-l` max bytes, `-r` release (no Swagger UI), `-token` legacy bearer file (default `/secret/token`), **`-luggage-expiry-sweep-interval`** (default `1m`, **`0`** disables in-process sweeps), **`-luggage-expiry-sweep-once`** (sweep expired luggage then exit; no HTTP — use for Kubernetes **CronJob** with the same image/DSN/tmp volume), **`-luggage-expiry-sweep-batch`** (default `500`) |

Protected routes accept (in order): legacy bearer file token (if configured), **`Authorization: Bearer concierge_…`** API key, then **`concierge_session`** cookie after OAuth. Downloads stay public. Roles and quotas: see **`AGENTS.md`**.

Full **`CONCIERGE_*`** list: **`internal/config/env.go`** and **`.env.sample`**. Luggage expiry: **`CONCIERGE_LUGGAGE_EXPIRY_SWEEP_INTERVAL`** (Go duration, e.g. `2m`; `0` disables the in-process ticker), **`CONCIERGE_LUGGAGE_EXPIRY_SWEEP_ONCE`**, **`CONCIERGE_LUGGAGE_EXPIRY_SWEEP_BATCH`**. Cleanup can lag by up to one sweep interval after `expires_at`.

**CronJob example** (same DB and temp volume as the app; adjust image/registry):

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: concierge-luggage-expiry
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: sweep
              image: ghcr.io/suapapa/concierge:latest
              args: ["-luggage-expiry-sweep-once", "-t", "/data/concierge_archive"]
              env:
                - name: CONCIERGE_DATABASE_URL
                  valueFrom: { secretKeyRef: { name: concierge, key: database-url } }
          restartPolicy: OnFailure
```

## Frontend (`fe/`)

```sh
cd fe && npm install && npm run dev
```

Proxies **`/api`** to **`http://127.0.0.1:8080`** by default (`fe/.env.example`). For OAuth while using Vite on another origin, set **`CONCIERGE_POST_LOGIN_REDIRECT`** (e.g. `http://localhost:5173/`). Production bundle: `npm run build` → **`fe/dist/`** (also baked into the Docker image).

## API sketch

Base path: **`/api/v1`**. The dashboard is the Vite bundle under **`fe/dist/`** (or **`CONCIERGE_STATIC_UI_DIR`**); if **`index.html`** is missing there at startup, **`GET /`** is not served until you run **`npm run build`** in **`fe/`**.

```sh
# Upload (requires session / API key / legacy bearer as configured)
curl -s -X POST http://localhost:8080/api/v1/luggage -F "file=@example.txt" -F "ttl=5"

# List your objects (session / API key / legacy bearer); each key may include `expiresAt`
curl -s http://localhost:8080/api/v1/stat

# Download (public)
curl -O http://localhost:8080/api/v1/luggage/KEY
```

Swagger UI is available in non-release mode (`-r` not set).

## Docs for contributors

Behavior, flags, and layout for agents: **`AGENTS.md`**. When you change user-visible run steps or APIs, keep **README** and **AGENTS** in sync.
