# Concierge

![concierge logo](./_img/logo.png)

A web application for temporarily hosting files publicly. Files are automatically deleted after a specified time-to-live (TTL) period, making it perfect for sharing files temporarily.

## Features

- **Temporary file hosting**: Upload files and get a shareable key
- **Automatic cleanup**: Files are automatically deleted after TTL expires
- **Active reference tracking**: Files won't be deleted while they're being downloaded
- **Configurable TTL**: Set custom expiration time per file
- **MIME type support**: Preserve or override file MIME types
- **Size limit**: Configurable file size limit (default: 5MB)
- **Optional Google login**: Multi-user mode with PostgreSQL, roles (`admin` / `guest`), and opaque sessions
- **Per-object ownership**: Each upload records `ownerUserId` in `info.yaml` for delete/stat scoping
- **Per-user API keys** (database mode): `concierge_…` secrets; `Authorization: Bearer` on protected routes; create/list/revoke via API or the `fe/` dashboard
- **React dashboard (`fe/`)**: List your luggage, upload with TTL, copy public links, delete, and manage API keys (session or legacy Bearer)

## Installation

### Build from source

```sh
go build -o concierge
```

### Docker

```sh
docker build -t concierge .
```

## Usage

### Start the server

```sh
# Default settings (port 8080, temp dir /tmp/concierge, 5MB limit)
./concierge

# Custom configuration
./concierge -p 9000 -t /tmp/myfiles -l 10485760  # 10MB limit
```

**Command-line flags:**

- `-p`: Port number (default: 8080)
- `-t`: Temporary directory path (default: /tmp/concierge)
- `-l`: File size limit in bytes (default: 5242880 = 5MB)
- `-r`: Release mode (quieter Gin, no Swagger UI)
- `-token`: Path to legacy bearer token file (default: /secret/token)

### Multi-user mode (Google OAuth + PostgreSQL)

When `CONCIERGE_DATABASE_URL` is set, the server enables Google OAuth, applies embedded SQL migrations on startup, and accepts any of:

- A valid **`concierge_session`** cookie (set after `GET /api/v1/auth/google` → Google → callback), or
- A **user API key**: `Authorization: Bearer concierge_…` (create with `POST /api/v1/api-keys` while signed in; the full secret is returned once), or
- The **legacy Bearer token** from `-token` / `CONCIERGE_TOKEN_PATH` (same as `TokenPath`; treated as **admin** for uploads, deletes, and stats).

Protected routes try the legacy file token first (if configured), then a `concierge_` API key, then the session cookie.

**Environment variables** (prefix `CONCIERGE_`):

| Variable | Required if DB | Description |
|----------|----------------|---------------|
| `TMP_DIR` | No | Overrides `-t` temp directory |
| `TOKEN_PATH` | No | Overrides `-token` legacy bearer token file path |
| `DATABASE_URL` | — | PostgreSQL DSN (enables OAuth + sessions) |
| `GOOGLE_CLIENT_ID` | Yes | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Yes | Google OAuth client secret |
| `OAUTH_REDIRECT_URL` | Yes | Must match Google console, e.g. `http://localhost:8080/api/v1/auth/google/callback` |
| `SESSION_SECRET` | Yes | At least 16 characters; used to sign OAuth `state` |
| `BOOTSTRAP_ADMIN_EMAILS` | No | Comma-separated emails; first login for each gets `admin` |
| `SESSION_TTL` | No | Session lifetime (Go duration, default `168h`) |
| `POST_LOGIN_REDIRECT` | No | Path after login (default `/`; must start with `/`). When using the Vite app in `fe/`, set this to that origin (for example `http://localhost:5173/`) so OAuth returns to the dashboard. |
| `COOKIE_SECURE` | No | `true` / `1` to set `Secure` on cookies (use behind HTTPS) |

**Roles:**

- **guest**: Upload (owned by self), delete own objects, `GET /stat` scoped to own keys.
- **admin**: Same as guest on own objects, plus delete any object, full `GET /stat`, `GET/PATCH /api/v1/admin/users`.

**Downloads** `GET /api/v1/luggage/:key` stay **public** (anyone with the key), same as single-token mode.

**Google Cloud Console:** Create OAuth 2.0 Web credentials; authorized redirect URI = `OAUTH_REDIRECT_URL`. Start login at `http://localhost:8080/api/v1/auth/google` (same path under your public base URL in production).

### Local database with Docker Compose

Start PostgreSQL only (for running `./concierge` on the host against `localhost:5432`):

```sh
docker compose up -d
```

Example DSN:

`postgres://concierge:concierge@localhost:5432/concierge?sslmode=disable`

Optional: run the app in Compose (needs Google OAuth and session secrets).

**Using a `.env` file (recommended):** From the repo root (the directory that contains `docker-compose.yml`), create a file named `.env`. Docker Compose loads it automatically for `${…}` substitution in the compose file—you do not need `export` in your shell.

Example `.env` for `docker compose --profile app up`:

```env
CONCIERGE_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
CONCIERGE_GOOGLE_CLIENT_SECRET=your-client-secret
CONCIERGE_SESSION_SECRET=change-me-at-least-16-chars
# Optional (defaults in compose match localhost:8080 callback):
# CONCIERGE_OAUTH_REDIRECT_URL=http://localhost:8080/api/v1/auth/google/callback
# CONCIERGE_BOOTSTRAP_ADMIN_EMAILS=you@example.com
```

Then:

```sh
docker compose --profile app up --build
```

To use another path instead of `./.env`, pass it explicitly:

```sh
docker compose --env-file /path/to/secrets.env --profile app up --build
```

Do not commit `.env` (it holds secrets). You can keep the same variable names when running `./concierge` on the host against the Compose database.

**Using your shell only:**

```sh
export CONCIERGE_GOOGLE_CLIENT_ID=...
export CONCIERGE_GOOGLE_CLIENT_SECRET=...
export CONCIERGE_SESSION_SECRET=$(openssl rand -hex 16)
docker compose --profile app up --build
```

### React app (`fe/`)

The **`fe/`** directory is a Vite + React + TypeScript UI that calls the same JSON and multipart endpoints as the API (`GET /api/v1/stat`, `POST /api/v1/luggage`, `DELETE /api/v1/luggage/:key`, `POST /api/v1/auth/logout`, `GET/POST/DELETE /api/v1/api-keys`) with **`credentials: 'include'`** for the session cookie.

```sh
cd fe
npm install
npm run dev
```

By default, `npm run dev` proxies **`/api`** to **`http://127.0.0.1:8080`**. Run `./concierge` on that port (or set `VITE_DEV_API_PROXY` in `fe/.env`; see `fe/.env.example`). Set **`CONCIERGE_POST_LOGIN_REDIRECT`** to your Vite origin (for example `http://localhost:5173/`) so Google OAuth returns to the app after login.

```sh
cd fe
npm run build
```

writes static assets to **`fe/dist/`** for deployment behind any static file host or reverse proxy that forwards `/api` to Concierge.

### API base URL

HTTP handlers live under **`/api/v1`**. The site root **`/`** serves the static landing page (`web/index.html`).

### Upload a file

With **legacy token** (when configured):

```sh
curl -X POST http://localhost:8080/api/v1/luggage \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@example.txt"
```

With **session cookie** (after browser login), send the cookie (e.g. `curl -b cookies.txt`).

```sh
# Basic upload (default TTL: 3 minutes)
curl -X POST http://localhost:8080/api/v1/luggage \
  -F "file=@example.txt"

# Upload with custom MIME type and TTL
curl -X POST http://localhost:8080/api/v1/luggage \
  -F "file=@example.txt" \
  -F "mime=text/plain" \
  -F "ttl=5"
```

**Response:**

```json
{
  "key": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"
}
```

### Delete a file

```sh
curl -X DELETE http://localhost:8080/api/v1/luggage/KEY \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Fetch a file

```sh
# Download using the key from upload response
curl http://localhost:8080/api/v1/luggage/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6 -o downloaded.txt

# Or open in browser
open http://localhost:8080/api/v1/luggage/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
```

### Log out (clears server session)

```sh
curl -X POST http://localhost:8080/api/v1/auth/logout -b cookies.txt -c cookies.txt
```

### User API keys (database mode)

After signing in with Google (session cookie), create a key (optional JSON `label`). The response includes `key` **once**; the server stores only a hash.

```sh
curl -s -X POST http://localhost:8080/api/v1/api-keys \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"label":"my laptop"}'
```

List or revoke (no secret shown):

```sh
curl -s http://localhost:8080/api/v1/api-keys -b cookies.txt
curl -s -X DELETE "http://localhost:8080/api/v1/api-keys/1" -b cookies.txt
```

Use the key like the legacy token:

```sh
curl -s http://localhost:8080/api/v1/stat \
  -H "Authorization: Bearer concierge_…"
```

### Complete example

```sh
# 1. Start the server
./concierge

# 2. Upload a file
KEY=$(curl -s -X POST http://localhost:8080/api/v1/luggage \
  -F "file=@document.pdf" \
  -F "mime=application/pdf" \
  -F "ttl=15" | jq -r '.key')

echo "File uploaded! Key: $KEY"

# 3. Share the URL
echo "Share this URL: http://localhost:8080/api/v1/luggage/$KEY"

# 4. Download the file
curl http://localhost:8080/api/v1/luggage/$KEY -o downloaded.pdf
```

## How it works

1. When you upload a file via `POST /api/v1/luggage`, it generates a unique key and stores the file in a temporary directory
2. The file metadata (MIME type, filename, optional `ownerUserId`) is stored in `info.yaml` alongside the file
3. A background goroutine waits for the TTL period, then checks if there are any active downloads
4. If no active references exist, the file is deleted. If downloads are in progress, deletion is delayed until all downloads complete
5. Active reference counting ensures files aren't deleted while being served
6. With PostgreSQL enabled, users, opaque sessions, and API key hashes are stored in the database; OAuth `state` is HMAC-signed with `SESSION_SECRET`

## Documentation maintenance

Code changes that affect behavior, flags, APIs, or how the project is built or run should be reflected **immediately** in **`README.md`** (this file) and **`AGENTS.md`** (guidance for coding agents). Update both in the same change whenever practical so they do not drift from the codebase.

## Notes

- Files are stored in the temporary directory specified by the `-t` flag or `CONCIERGE_TMP_DIR`
- Each file gets its own directory named after its key
- The server uses file locking to handle concurrent access in multi-instance deployments
- Default TTL is 3 minutes if not specified
- Maximum file size is 5MB by default (configurable via `-l`)
- SQL migrations are embedded under `internal/store/migrations` and applied on startup when `DATABASE_URL` is set
