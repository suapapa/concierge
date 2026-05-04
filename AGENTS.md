# AGENTS.md — Concierge

Guidance for coding agents working in this repository.

## Documentation (mandatory with code changes)

Whenever a change alters **user-visible behavior**, **HTTP APIs**, **CLI flags**, **defaults**, **build or run steps**, **project layout**, or **conventions** documented here or in `README.md`, update **`AGENTS.md`** and **`README.md` in the same editing session / pull request** so both stay accurate. Treat doc updates as part of the code change, not optional follow-up.

- **`README.md`**: end-user facing overview, install/usage examples, feature list.
- **`AGENTS.md`**: agent-oriented architecture, commands, and repo conventions.

If only one file seems affected (e.g. a new flag), still check the other for stale references (flags, paths, endpoints) and fix them.

## What this is

**Concierge** is a small Gin HTTP service for temporary file hosting: uploads go under a configurable temp directory with per-object TTL, YAML sidecar metadata (`info.yaml`), and a file-backed active reference map so objects are not deleted while downloads are in progress.

Module path: `github.com/suapapa/concierge`.

## Layout

| Path | Role |
|------|------|
| `main.go` | Process entry: flags, signal-aware root `context`, HTTP server lifecycle, auth middleware, API group under `/api/v1`, root `GET /` from `web/index.html`, Swagger wiring in non-release mode |
| `handler.go` | Gin handlers on `Handlers`; thin HTTP layer delegating to `internal/luggage` |
| `web/` | Static site root (`index.html`); replace or extend as the frontend grows |
| `internal/config` | Flag-driven `Config`, path derivation for active refs |
| `internal/activerefs` | Advisory lock + YAML persistence for per-key download counts |
| `internal/luggage` | Core behavior: upload, open/get lease, stats, health, TTL expiry goroutines tied to app `context` |
| `docs/` | Generated Swagger (`swag`); do not hand-edit `docs/docs.go` |

New application logic belongs under `internal/`; keep `main` and handlers as wiring unless there is a strong reason to grow them.

## Commands (run from repo root)

```sh
go vet ./...
go test -race ./...
golangci-lint run ./...   # or: make lint
```

Regenerate API docs after changing handler Swagger comments or `internal/luggage` types referenced in annotations:

```sh
make swagger
# requires: go install github.com/swaggo/swag/cmd/swag@latest
```

`swag init` **must** include `--parseInternal` (see `Makefile`) so definitions under `internal/luggage` resolve.

## Conventions

- **Context**: Blocking/domain boundaries accept `context.Context`; background work uses the process-level context cancelled on SIGINT/SIGTERM.
- **Errors**: Prefer sentinel errors in `internal/luggage` (e.g. `ErrNotFound`) and `errors.Is` in handlers for HTTP status mapping. Wrap with `%w` where appropriate.
- **Keys**: Luggage keys are restricted to safe characters (alphanumeric, `_`, `-`); reject path segments and `..`.
- **Concurrency**: TTL cleanup goroutines must respect cancellation; do not spawn unbounded goroutines without a clear shutdown path.
- **Docker**: The build stage uses `COPY . .` and `swag init ... --parseInternal`; adding packages only under `internal/` does not require Dockerfile path tweaks.

## Configuration surface

- CLI flags: `-t` temp dir, `-l` max upload bytes, `-p` port, `-r` release mode (see `main.go` / README).
- Optional bearer token: `main` reads `Config.TokenPath` (default `/secret/token` from `internal/config.Default()`). There is no `-token` flag yet; add one in `main` if needed. Missing or empty token file disables auth on non-GET requests.

## Skills / deeper Go guidance

Project-local Go conventions and workflow expectations are summarized in `.agents/skills/golang-pro/SKILL.md` (vet → golangci-lint → tests with `-race`, table-driven tests, etc.). Prefer aligning non-trivial Go changes with that skill.
