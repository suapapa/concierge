# AGENTS.md — Concierge

Guidance for coding agents working in this repository.

## Documentation (mandatory with code changes)

Whenever a change alters **user-visible behavior**, **HTTP APIs**, **CLI flags**, **defaults**, **build or run steps**, **project layout**, or **conventions** documented here or in `README.md`, update **`AGENTS.md`** and **`README.md` in the same editing session / pull request** so both stay accurate. Treat doc updates as part of the code change, not optional follow-up.

- **`README.md`**: end-user facing overview, install/usage examples, feature list.
- **`AGENTS.md`**: agent-oriented architecture, commands, and repo conventions.

If only one file seems affected (e.g. a new flag), still check the other for stale references (flags, paths, endpoints) and fix them.

## What this is

**Concierge** is a small Gin HTTP service for temporary file hosting: uploads go under a configurable temp directory with per-object TTL, YAML sidecar metadata (`info.yaml`, including `ownerUserId` when authenticated), and a file-backed active reference map so objects are not deleted while downloads are in progress.

With **`CONCIERGE_DATABASE_URL`** set, the service adds **Google OAuth**, **PostgreSQL** (`users`, `sessions`, `api_keys`, `user_daily_uploads` for quota counters), **opaque session cookies** (`concierge_session`), **per-user API keys** (`concierge_…` bearer secrets, stored hashed), **roles** (`admin` / `guest`), and **per-user upload quotas** on `users` (pool size, single-file max, daily upload count; UTC day). Downloads `GET /api/v1/luggage/:key` remain public for anyone with the key.

Module path: `github.com/suapapa/concierge`.

## Layout

| Path | Role |
|------|------|
| `main.go` | Process entry: flags, `config.ApplyEnv()`, signal-aware root `context`, HTTP routes: public `GET /luggage/:key`, OAuth, protected group (session, `Bearer concierge_…` API key, or legacy Bearer), `GET/POST/DELETE /api-keys`, admin group (`GET/PATCH /admin/users`), Swagger in non-release mode |
| `handler.go` | Gin handlers on `Handlers`; authz for delete/stat; upload quota checks (DB users); admin user/quota APIs |
| `web/` | Static site root (`index.html`); lightweight landing page |
| `fe/` | Vite + React + TypeScript dashboard: `GET /api/v1/stat`, upload, delete, API key UI, copy public URLs, admin “Users & quotas” when `GET /admin/users` succeeds; `npm run dev` proxies `/api` to Concierge (see `fe/.env.example`) |
| `internal/config` | `Config`, flags, `ApplyEnv()` for `CONCIERGE_*` variables |
| `internal/store` | PostgreSQL pool, embedded migrations, user/session/API-key CRUD, `LookupAPIKey`, per-user quotas + daily upload reservation |
| `internal/auth` | Google OAuth start/callback, signed OAuth state (HMAC + `SESSION_SECRET`), `RequireUserOrLegacy` (legacy Bearer → API key → session) / `RequireAdmin` |
| `internal/activerefs` | Advisory lock + YAML persistence for per-key download counts |
| `internal/luggage` | Core behavior: upload (with owner), `ReadFileInfo`, `Delete`, open/get lease, stats (optional owner filter), health, TTL expiry goroutines tied to app `context` |
| `docs/` | Generated Swagger (`swag`); do not hand-edit `docs/docs.go` |
| `docker-compose.yml` | Local **PostgreSQL**; optional **`concierge`** service via `--profile app`. Compose reads repo-root **`.env`** for `${CONCIERGE_*}` interpolation (or **`docker compose --env-file …`**); copy **`.env.sample`** to `.env` and fill secrets. Both services use **`restart: unless-stopped`** so they come back after a host reboot unless you stopped them explicitly. |

New application logic belongs under `internal/`; keep `main` and handlers as wiring unless there is a strong reason to grow them.

## Commands (run from repo root)

```sh
go vet ./...
go test -race ./...
golangci-lint run ./...   # or: make lint
```

Frontend (`fe/`): `npm install`, `npm run dev`, `npm run build` (TypeScript check + Vite bundle to `fe/dist/`).

Regenerate API docs after changing handler Swagger comments or `internal/luggage` / `internal/store` types referenced in annotations:

```sh
make swagger
# requires: go install github.com/swaggo/swag/cmd/swag@latest
```

`swag init` **must** include `--parseInternal` (see `Makefile`) so definitions under `internal/` resolve.

## Conventions

- **Context**: Blocking/domain boundaries accept `context.Context`; background work uses the process-level context cancelled on SIGINT/SIGTERM.
- **Errors**: Prefer sentinel errors in `internal/luggage` (e.g. `ErrNotFound`) and `errors.Is` in handlers for HTTP status mapping. Wrap with `%w` where appropriate.
- **Keys**: Luggage keys are restricted to safe characters (alphanumeric, `_`, `-`); reject path segments and `..`.
- **Concurrency**: TTL cleanup goroutines must respect cancellation; do not spawn unbounded goroutines without a clear shutdown path.
- **Docker**: The build stage uses `COPY . .` and `swag init ... --parseInternal`; adding packages only under `internal/` does not require Dockerfile path tweaks. The runtime image copies the binary plus `web/` so `GET /` can serve `web/index.html`.

## Configuration surface

- CLI flags: `-t` temp dir, `-l` max upload bytes, `-p` port, `-r` release mode, `-token` legacy bearer token file path (see `main.go` / README).
- Environment: `CONCIERGE_*` — see `internal/config/env.go` and README for `DATABASE_URL`, Google OAuth, `SESSION_SECRET`, `BOOTSTRAP_ADMIN_EMAILS`, `SESSION_TTL`, `POST_LOGIN_REDIRECT`, `COOKIE_SECURE`, `TMP_DIR`, `TOKEN_PATH`.
- **Legacy bearer token**: optional file at `-token` / `TokenPath` default `/secret/token`. When present, its value is matched first against `Authorization: Bearer …` and, if it matches, the request is treated as **admin** (user id 0 for uploads). In DB mode, non-matching `Bearer` values starting with `concierge_` are looked up as **user API keys** (role from `users.role`); otherwise the **session cookie** is used. Missing or empty token file means no legacy token.
- **Migrations**: Embedded SQL under `internal/store/migrations`; `Store.Migrate` runs on startup when the database is enabled. Migrations use `IF NOT EXISTS` so they are safe to re-run.

## Skills / deeper Go guidance

Project-local Go conventions and workflow expectations are summarized in `.agents/skills/golang-pro/SKILL.md` (vet → golangci-lint → tests with `-race`, table-driven tests, etc.). Prefer aligning non-trivial Go changes with that skill.
