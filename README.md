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
   and calls each enabled adapter with a per-source timeout and rate limit.
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
  sources/         adapter contract, HTTP fetcher, 8 source adapters, fixtures
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

## Status

Implemented and verified: everything above.

Deliberately **not** implemented: automatic applications, employer contact, and
scraping of any source whose terms prohibit it. Email digests (Phase 2 of the
plan) are not built yet — the scheduler records new vacancies, but no mail is sent.
