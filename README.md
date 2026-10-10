# JobSeeker

An automated job-discovery platform: upload a CV once, configure what you want,
and a scheduled search collects vacancies from permitted job sources, normalises
them, removes duplicates across portals, and scores each one against your
confirmed profile — with an explanation for every point awarded.

**React 18 + TypeScript (Vite)** frontend, **Go 1.25** backend, **PostgreSQL**,
a cron-scheduled search process, Docker Compose deployment.

## The rule this codebase is built around

The system never invents data and never hides uncertainty.

- A field a source did not publish is stored as unknown (`NULL` / `"unknown"`)
  and reported as `unknown` in the match explanation. It is never assumed to
  match, and never assumed to be absent either.
- Extracted CV values are **drafts**. They live separately from confirmed values
  and cannot influence matching until you confirm them.
- `/api/v1/searches/runs/{id}` reports per-source statistics and failures, so a
  source that broke is visible rather than silently reducing coverage.
- Coverage limits are documented in [`docs/sources.md`](docs/sources.md) with the
  evidence behind each claim. Sources that cannot be used legally are not scraped.

## Quick start (local)

Prerequisites: Go 1.25+, Node 20+/pnpm, and PostgreSQL 15 (either the isolated dev
cluster below or your own instance).

```bash
cd JobSeeker

# 1. Isolated local PostgreSQL on port 5433 (trust auth, localhost only).
#    Uses the Homebrew Postgres binaries; nothing else on your machine is touched.
make dev-db-init

# 2. Configuration (never commit .env)
cp .env.example .env

# 3. Schema
make migrate-up

# 4. API on 127.0.0.1:8080
make run-api
# ...and in a second terminal, the cron scheduler
make run-scheduler

# 5. Frontend on http://localhost:5173
make web-install && make web-dev
```

Docker Compose is the deployment path (`make compose-up`); it publishes
PostgreSQL on host port 5433 so it does not collide with an existing local
instance.

### One origin, always

The SPA calls the API on **its own origin** (`/api/v1/...`). `pnpm dev` proxies
`/api`, `/healthz` and `/readyz` to the backend, and in production Caddy does the
same. Keep it that way: a session cookie is sent only to the site that set it, so
if the page is on `localhost:5173` and the API is on `127.0.0.1:8080`, the browser
stores the login cookie and then never sends it — which looks exactly like "login
does nothing, it just bounces back to the login form".

- Recommended: leave `VITE_API_BASE_URL` **unset** and browse one host
  (http://localhost:5173).
- If you must point at an absolute API URL, it has to be the **same site** as the
  page: `localhost:5173` → `http://localhost:8080` works; → `http://127.0.0.1:8080`
  does not.
- Backend override for the dev proxy: `VITE_API_PROXY_TARGET` in `web/.env`.

## Verifying it works

```bash
make test              # unit tests
make test-race         # unit tests under the race detector
make test-integration  # unit + PostgreSQL integration tests (needs dev-db)
make web-test          # frontend tests
make smoke             # end-to-end: HTTP API + a real search run
```

`make smoke` drives the real HTTP surface: register → upload a real PDF CV →
confirm the profile → configure preferences and a schedule → **run a live search
against the job sources** → read back scored vacancies, duplicate handling, run
history, tracking and logout. Each step asserts on the response, so a regression
fails the run instead of printing a warning.

## How a search works

1. The scheduler ticks every 30s and asks the database which schedules are due
   (`search_schedules`), computing fire times in each user's timezone.
2. A due schedule takes a **PostgreSQL advisory lock** keyed on the schedule id,
   so running two API/scheduler instances cannot run the same schedule twice.
3. `search.Runner` loads the confirmed profile and preferences, builds one query,
   and calls each enabled adapter with a per-source timeout and rate limit. Each
   source contributes up to **1000 listings per run** (`PerSourceLimit`);
   paginated adapters walk as many pages as that needs (100/page, ceiling of 20
   pages) and stop early when the feed reports no more results.
4. Every listing is normalised and upserted. Duplicate detection has three tiers:
   same source id → same canonical URL hash → fuzzy title/company/location
   similarity. Cross-portal duplicates merge into one canonical vacancy while
   one row per portal is kept for attribution.
5. Each touched vacancy is scored 0–100 against the profile; the explanation is
   stored keyed by profile version, so scores are reproducible and invalidated
   when the profile actually changes.
6. Listings that did not come back from a source are marked `unverified`. Expiry
   requires either a source-published deadline passing or repeated misses across a
   72-hour grace window — a single failed search never expires a vacancy.
7. The run closes as `succeeded`, `partial` or `failed`, with per-source counters.

## Layout

```
cmd/api            HTTP server
cmd/scheduler      cron loop (separate process)
cmd/migrate        migration runner
internal/
  api/             routing, middleware, handlers
  auth/            Argon2id passwords, server-side sessions
  candidate/       confirmed profile, preferences, resume metadata
  resume/          PDF/DOCX text extraction + deterministic profile extraction
  sources/         adapter contract, HTTP fetcher, 13 source adapters, fixtures
  matching/        hard constraints + weighted scoring with explanations
  jobs/            canonical vacancies, dedup, lifecycle, job queries
  search/          run orchestration, source settings, run history, cron maths
  scheduler/       due-schedule loop with advisory locking
  tracker/         saved jobs, applications, dismissals
  storage/         pgx pool/transactions, private document storage
  platform/        config, structured logging with redaction, HTTP plumbing
migrations/        plain-SQL schema history
web/               React + TypeScript SPA
deploy/            Dockerfiles, compose, Caddyfile
docs/              source feasibility and obligations
scripts/smoke.sh   end-to-end verification
```

## Security and privacy

- Argon2id password hashing; sessions are opaque 256-bit tokens stored only as
  SHA-256 digests and delivered in `HttpOnly` + `SameSite=Lax` cookies.
- Every user-owned query is scoped by the authenticated user id from the session,
  never from the request body or query string.
- CVs are parsed **locally**; no content is sent to a third-party service, and
  contact details found in a CV are deliberately not stored in the profile.
- Uploaded files live outside any static root, named by UUID, mode `0600`, with a
  size limit, magic-byte checks, a DOCX decompression-bomb guard, and
  path-traversal protection on read.
- JSON bodies are decoded strictly (unknown fields rejected), rate limits apply to
  auth and write endpoints, and internal error detail never reaches the client.
- Structured logs redact anything that looks like a credential, a request body, or
  CV text.

## Attribution obligations

Sources are used under their published terms, and those terms are part of the
product: Remote OK requires a follow backlink (`backlink_required: true`, rendered
as a normal link), Remotive requires attribution and forbids redistribution,
Arbeitnow asks for a link back, and Adzuna requires a labelled, linked logo. See
[`docs/sources.md`](docs/sources.md).

## Deploy (Docker Compose)

The whole stack — Postgres, migrations, API, scheduler, and the SPA served by
Caddy — is in `deploy/docker-compose.yml`:

```bash
cp .env.example .env      # then review it
docker compose --env-file .env -f deploy/docker-compose.yml up -d --build
curl -I http://localhost/healthz    # /readyz is the DB-aware check
```

- **`--env-file .env` is required.** Compose reads `.env` from the *project
  directory*, which is this compose file's directory (`deploy/`) when `-f` points
  there — not your shell's cwd. Without the flag the stack fails with
  *"required variable POSTGRES_PASSWORD is missing a value"*.
- **`POSTGRES_PASSWORD` is required** by the stack and is interpolated into the
  containers' `DATABASE_URL`. Use a long random value containing no `@ : / #`
  (it sits inside a URL) — `openssl rand -hex 24`.
- **`migrate` runs to completion before `api` and `scheduler` start**, so the API
  never serves a half-migrated schema. The API never migrates on its own.
- **`scheduler` is a separate service and must be running**, or no schedule ever
  fires — the API only serves requests.
- The SPA is served from the same origin as `/api`, which is what keeps the
  session cookie first-party. Never bake a different host into
  `VITE_API_BASE_URL`.
- **`POSTGRES_HOST_PORT`** (default 5433) is where the container's Postgres is
  published for host access. The isolated dev cluster from `make dev-db-init`
  also uses 5433 — set `POSTGRES_HOST_PORT=5434` to run both, or
  `make dev-db-down` first. The app never uses this port: containers talk over
  the compose network.
- Uploaded CVs live in the `cvs` volume (`DATA_DIR`). Back that up along with the
  database: a volume is not a backup.

### HTTPS on a real domain

1. Point the domain's A record at the server.
2. In `deploy/Caddyfile`, replace `:80 {` with `yourdomain.com {`.
3. In `deploy/docker-compose.yml`, publish `"443:443"` alongside `"80:80"`.
4. In `.env`: `COOKIE_SECURE=true`, `CORS_ALLOWED_ORIGINS=https://yourdomain.com`,
   and `VITE_API_BASE_URL=https://yourdomain.com` (baked in at build time).

Caddy obtains and renews the certificate automatically.

## Status

Implemented and verified: everything above.

### Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Signed in, but every reload lands on `/login` | Fixed in code (session state is now read straight from the `me` query instead of being mirrored into component state, which left a render window where "loading finished" and "user is null" were both true). If you see it again, check you are on one origin — see "One origin, always". |
| Login appears to succeed, then bounces back | The cookie is being stored for one host and requested from another (`localhost` vs `127.0.0.1` are different sites). Keep the page and the API on the same site. |
| `401` from `/api/v1/...` in a browser tab | Expected: user-scoped endpoints need the session cookie, and a bare tab has none. Log in through the SPA, or use `curl -c jar …/auth/login` then `curl -b jar …/me`. |
| `address already in use` on 8080/5173 | Something already runs there: `lsof -nP -iTCP:8080 -sTCP:LISTEN`. |
| Job list empty | No run has completed for your account, or all sources are disabled — check Search History for the run status. |
| Few results, `high_match_count: 0` | The default 70/100 threshold is strict, and job titles must resemble your configured target titles. Adjust `job_titles` and the minimum score in Search Config. |
| ATS sources report `skipped` | `greenhouse`, `lever` and `ashby` need company board slugs in the watchlist. |
| `go: downloading go1.26.0` | A module was fetched without the toolchain pin: `export GOTOOLCHAIN=local`. |
| `required variable POSTGRES_PASSWORD is missing a value` | Compose reads `.env` from the *project directory* (`deploy/` when you pass `-f deploy/docker-compose.yml`). Run it with `--env-file .env`, and make sure `POSTGRES_PASSWORD` is set — it is required, not optional. |
| A scheduled run reports `no adapter available in this build` | The **scheduler** process is stale. `go run` and containers compile once at startup, so a source added after it started is unknown to it. Restart it (`make run-scheduler`, or `docker compose … restart scheduler`). The same applies to the API process. |
| Cron never fires | Nothing is running `cmd/scheduler`. The API does not run schedules — see "Deploy (Docker Compose)". |
| `port is already allocated` / `address already in use` on 5433 | The isolated dev cluster owns it. Set `POSTGRES_HOST_PORT=5434` in `.env`, or `make dev-db-down`. |

Deliberately **not** implemented: automatic applications, employer contact, and
scraping of any source whose terms prohibit it. Email digests (Phase 2 of the
plan) are not built yet — the scheduler records new vacancies, but no mail is sent.
