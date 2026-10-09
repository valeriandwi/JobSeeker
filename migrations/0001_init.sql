-- +goose Up

-- JobSeeker initial schema.
-- Conventions:
--   * timestamptz everywhere; UTC in the database, timezone handled at presentation.
--   * every user-owned table carries user_id and is always queried with that scope.
--   * *_norm columns hold canonicalized text used for duplicate detection.
--   * jobs is the canonical vacancy; job_source_listings keeps one row per portal.

create extension if not exists pg_trgm;

-- ---------------------------------------------------------------- users/auth

create table users (
    id            uuid primary key default gen_random_uuid(),
    email         text        not null,
    password_hash text        not null,
    display_name  text        not null default '',
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);

-- Case-insensitive uniqueness of email without citext.
create unique index users_email_lower_key on users (lower(email));

create table sessions (
    id           uuid primary key default gen_random_uuid(),
    user_id      uuid        not null references users (id) on delete cascade,
    token_hash   bytea       not null unique,
    user_agent   text        not null default '',
    ip           inet,
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null
);

create index sessions_user_idx on sessions (user_id);
create index sessions_expires_idx on sessions (expires_at);

-- ------------------------------------------------------------------ resumes

create table resumes (
    id                uuid primary key default gen_random_uuid(),
    user_id           uuid        not null references users (id) on delete cascade,
    original_filename text        not null,
    content_type      text        not null,
    size_bytes        bigint      not null,
    sha256            text        not null,
    storage_path      text        not null,
    status            text        not null default 'uploaded'
        check (status in ('uploaded', 'parsed', 'failed')),
    parse_error       text        not null default '',
    uploaded_at       timestamptz not null default now(),
    deleted_at        timestamptz
);

create index resumes_user_idx on resumes (user_id) where deleted_at is null;

-- Machine extraction output, immutable, kept separate from user-confirmed data.
-- draft is a map of field_key -> {value, provenance, confidence}.
create table resume_extractions (
    id                uuid primary key default gen_random_uuid(),
    resume_id         uuid        not null references resumes (id) on delete cascade,
    user_id           uuid        not null references users (id) on delete cascade,
    extractor         text        not null,
    extractor_version text        not null,
    draft             jsonb       not null default '{}'::jsonb,
    created_at        timestamptz not null default now()
);

create index resume_extractions_resume_idx on resume_extractions (resume_id);

-- ------------------------------------------------------- candidate profiles

-- Values here are user-confirmed (or explicitly accepted) only.
create table candidate_profiles (
    id                  uuid primary key default gen_random_uuid(),
    user_id             uuid        not null unique references users (id) on delete cascade,
    full_name           text        not null default '',
    headline            text        not null default '',
    current_title       text        not null default '',
    years_experience    numeric(4,1),
    seniority           text        not null default 'unknown'
        check (seniority in ('intern', 'junior', 'mid', 'senior', 'lead', 'principal', 'unknown')),
    skills              jsonb       not null default '[]'::jsonb,
    industries          jsonb       not null default '[]'::jsonb,
    education           jsonb       not null default '[]'::jsonb,
    certifications      jsonb       not null default '[]'::jsonb,
    languages           jsonb       not null default '[]'::jsonb,
    projects            jsonb       not null default '[]'::jsonb,
    source_resume_id    uuid        references resumes (id) on delete set null,
    version             integer     not null default 1,
    confirmed_at        timestamptz,
    created_at          timestamptz not null default now(),
    updated_at          timestamptz not null default now()
);

-- ------------------------------------------------------- search preferences

create table search_preferences (
    id                uuid primary key default gen_random_uuid(),
    user_id           uuid        not null unique references users (id) on delete cascade,
    job_titles        jsonb       not null default '[]'::jsonb,
    keywords          jsonb       not null default '[]'::jsonb,
    excluded_keywords jsonb       not null default '[]'::jsonb,
    -- [{country, region, city, mandatory}]
    locations         jsonb       not null default '[]'::jsonb,
    -- [{value: "remote"|"onsite"|"hybrid", mandatory}]
    work_arrangements jsonb       not null default '[]'::jsonb,
    employment_types  jsonb       not null default '[]'::jsonb,
    seniority_levels  jsonb       not null default '[]'::jsonb,
    min_salary        numeric(14,2),
    salary_currency   char(3),
    salary_period     text        check (salary_period is null or salary_period in ('year', 'month', 'day', 'hour')),
    min_score         integer     not null default 0 check (min_score between 0 and 100),
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);

create table search_schedules (
    id              uuid primary key default gen_random_uuid(),
    user_id         uuid        not null unique references users (id) on delete cascade,
    cron_spec       text        not null default '0 8 * * *',
    timezone        text        not null default 'Asia/Jakarta',
    enabled         boolean     not null default true,
    last_run_at     timestamptz,
    next_run_at     timestamptz,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now()
);

-- ------------------------------------------------------------- job sources

create table job_sources (
    id                  text primary key,
    display_name        text        not null,
    kind                text        not null check (kind in ('feed', 'ats', 'aggregator')),
    requires_key        boolean     not null default false,
    enabled_globally    boolean     not null default true,
    attribution_text    text        not null default '',
    attribution_url     text        not null default '',
    backlink_required   boolean     not null default false,
    notes               text        not null default '',
    created_at          timestamptz not null default now()
);

create table user_source_settings (
    user_id    uuid        not null references users (id) on delete cascade,
    source_id  text        not null references job_sources (id) on delete cascade,
    enabled    boolean     not null default true,
    created_at timestamptz not null default now(),
    primary key (user_id, source_id)
);

-- Company watchlist for ATS boards (greenhouse/lever/ashby).
create table source_watchlist (
    id         uuid primary key default gen_random_uuid(),
    user_id    uuid        not null references users (id) on delete cascade,
    source_id  text        not null references job_sources (id) on delete cascade,
    board_slug text        not null,
    company    text        not null default '',
    created_at timestamptz not null default now(),
    unique (user_id, source_id, board_slug)
);

-- ------------------------------------------------------- jobs and listings

create table jobs (
    id                  uuid primary key default gen_random_uuid(),
    canonical_key       text        not null unique,
    title               text        not null,
    title_norm          text        not null default '',
    company             text        not null default '',
    company_norm        text        not null default '',
    location_city       text,
    location_region     text,
    location_country    text,
    work_arrangement    text        not null default 'unknown'
        check (work_arrangement in ('remote', 'onsite', 'hybrid', 'unknown')),
    employment_type     text        not null default 'unknown',
    seniority           text        not null default 'unknown',
    salary_min          numeric(14,2),
    salary_max          numeric(14,2),
    salary_currency     char(3),
    salary_period       text,
    description_summary text        not null default '',
    published_at        timestamptz,
    expires_at          timestamptz,
    status              text        not null default 'new'
        check (status in ('new', 'active', 'saved', 'applied', 'interviewing', 'rejected', 'expired', 'removed', 'unverified')),
    first_discovered_at timestamptz not null default now(),
    last_verified_at    timestamptz,
    verification_failures integer   not null default 0,
    created_at          timestamptz not null default now(),
    updated_at          timestamptz not null default now(),
    -- Explicit attributes vs unknown: callers must never assume a value.
    attributes_verified jsonb       not null default '{}'::jsonb,
    search_tsv          tsvector generated always as (
        to_tsvector('english',
            coalesce(title, '') || ' ' || coalesce(company, '') || ' ' ||
            coalesce(description_summary, ''))
    ) stored
);

create index jobs_status_published_idx on jobs (status, published_at desc nulls last);
create index jobs_first_discovered_idx on jobs (first_discovered_at desc);
create index jobs_search_tsv_idx on jobs using gin (search_tsv);
create index jobs_title_trgm_idx on jobs using gin (title_norm gin_trgm_ops);
create index jobs_company_trgm_idx on jobs using gin (company_norm gin_trgm_ops);

create table job_source_listings (
    id             uuid primary key default gen_random_uuid(),
    job_id         uuid        not null references jobs (id) on delete cascade,
    source_id      text        not null references job_sources (id) on delete cascade,
    source_job_id  text        not null,
    url            text        not null,
    url_hash       text        not null,
    apply_url      text,
    raw_payload    jsonb       not null default '{}'::jsonb,
    first_seen_at  timestamptz not null default now(),
    last_seen_at   timestamptz not null default now(),
    last_status    text        not null default 'active'
        check (last_status in ('active', 'expired', 'removed', 'unverified')),
    is_active      boolean     not null default true,
    unique (source_id, source_job_id),
    unique (source_id, url_hash)
);

create index job_source_listings_job_idx on job_source_listings (job_id);
create index job_source_listings_active_idx on job_source_listings (source_id, is_active);

-- Scoring is per (job, user, profile version) so results are reproducible and
-- invalidated only when the profile actually changes.
create table job_matches (
    id              uuid primary key default gen_random_uuid(),
    job_id          uuid        not null references jobs (id) on delete cascade,
    user_id         uuid        not null references users (id) on delete cascade,
    profile_version integer     not null,
    score           integer     not null check (score between 0 and 100),
    explanation     jsonb       not null default '[]'::jsonb,
    hard_constraint_violation boolean not null default false,
    computed_at     timestamptz not null default now(),
    unique (job_id, user_id, profile_version)
);

create index job_matches_user_score_idx on job_matches (user_id, score desc);

-- --------------------------------------------------------- user job states

create table saved_jobs (
    user_id  uuid        not null references users (id) on delete cascade,
    job_id   uuid        not null references jobs (id) on delete cascade,
    saved_at timestamptz not null default now(),
    primary key (user_id, job_id)
);

create table applications (
    id           uuid primary key default gen_random_uuid(),
    user_id      uuid        not null references users (id) on delete cascade,
    job_id       uuid        not null references jobs (id) on delete cascade,
    status       text        not null default 'saved'
        check (status in ('saved', 'applied', 'interviewing', 'rejected', 'accepted', 'withdrawn')),
    applied_at   timestamptz,
    follow_up_at timestamptz,
    notes        text        not null default '',
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    unique (user_id, job_id)
);

create table dismissed_jobs (
    user_id      uuid        not null references users (id) on delete cascade,
    job_id       uuid        not null references jobs (id) on delete cascade,
    dismissed_at timestamptz not null default now(),
    primary key (user_id, job_id)
);

-- ------------------------------------------------------------- run history

create table search_runs (
    id              uuid primary key default gen_random_uuid(),
    user_id         uuid        not null references users (id) on delete cascade,
    schedule_id     uuid        references search_schedules (id) on delete set null,
    trigger         text        not null check (trigger in ('manual', 'schedule', 'retry')),
    status          text        not null default 'running'
        check (status in ('running', 'succeeded', 'partial', 'failed', 'aborted')),
    idempotency_key text,
    started_at      timestamptz not null default now(),
    finished_at     timestamptz,
    error_summary   text        not null default '',
    stats           jsonb       not null default '{}'::jsonb
);

create index search_runs_user_started_idx on search_runs (user_id, started_at desc);
create unique index search_runs_idempotency_key on search_runs (user_id, idempotency_key)
    where idempotency_key is not null;

create table search_run_items (
    id             uuid primary key default gen_random_uuid(),
    run_id         uuid        not null references search_runs (id) on delete cascade,
    source_id      text        not null,
    status         text        not null check (status in ('ok', 'partial', 'error', 'skipped')),
    fetched        integer     not null default 0,
    accepted       integer     not null default 0,
    rejected       integer     not null default 0,
    duplicated     integer     not null default 0,
    new_jobs       integer     not null default 0,
    updated_jobs   integer     not null default 0,
    duration_ms    integer     not null default 0,
    error          text        not null default '',
    started_at     timestamptz not null default now(),
    finished_at    timestamptz
);

create index search_run_items_run_idx on search_run_items (run_id);

-- ------------------------------------------------------------ notifications

create table notification_preferences (
    user_id        uuid primary key references users (id) on delete cascade,
    email_enabled  boolean     not null default false,
    min_score      integer     not null default 70 check (min_score between 0 and 100),
    digest         text        not null default 'daily'
        check (digest in ('off', 'daily', 'weekly')),
    channels       jsonb       not null default '[]'::jsonb,
    updated_at     timestamptz not null default now()
);

-- Source catalog: seeded so the app can render coverage and attribution
-- obligations before any run happens.
insert into job_sources (id, display_name, kind, requires_key, attribution_text, attribution_url, backlink_required, notes) values
    ('arbeitnow',  'Arbeitnow',  'feed',       false, 'Jobs from Arbeitnow',        'https://www.arbeitnow.com',       false, 'Free keyless API; 325 jobs/page; Europe + remote focus; link back requested'),
    ('remotive',   'Remotive',   'feed',       false, 'Jobs from Remotive',         'https://remotive.com',            false, 'Keyless; 24h delayed; attribution + link back required; must not be redistributed to other job boards'),
    ('remoteok',   'Remote OK',  'feed',       false, 'Jobs from Remote OK',        'https://remoteok.com',            true,  'Keyless; 100-job window; dofollow backlink REQUIRED or access is suspended'),
    ('jobicy',     'Jobicy',     'feed',       false, 'Jobs from Jobicy',           'https://jobicy.com',              false, 'Keyless; remote-only; salary fields often present'),
    ('himalayas',  'Himalayas',  'feed',       false, 'Jobs from Himalayas',        'https://himalayas.app',           false, 'Keyless; includes seniority, employment type and expiry'),
    ('greenhouse', 'Greenhouse', 'ats',        false, 'Company careers board',      'https://boards.greenhouse.io',    false, 'Public board API; requires a company watchlist'),
    ('lever',      'Lever',      'ats',        false, 'Company careers board',      'https://jobs.lever.co',           false, 'Public postings API; requires a company watchlist'),
    ('ashby',      'Ashby',      'ats',        false, 'Company careers board',      'https://jobs.ashbyhq.com',        false, 'Public posting API; workplaceType + compensation when published'),
    ('adzuna',     'Adzuna',     'aggregator', true,  'Adzuna',                     'https://www.adzuna.com',          false, 'Free key; 25/min, 250/day, 1000/week, 2500/month; label each advert "Adzuna" >=116x23px with link'),
    ('usajobs',    'USAJOBS',    'aggregator', true,  'USAJOBS',                    'https://www.usajobs.gov',         false, 'Free key + contact email in User-Agent; US federal roles only')
on conflict (id) do nothing;

-- +goose Down
drop table if exists search_run_items;
drop table if exists search_runs;
drop table if exists dismissed_jobs;
drop table if exists applications;
drop table if exists saved_jobs;
drop table if exists job_matches;
drop table if exists job_source_listings;
drop table if exists jobs;
drop table if exists source_watchlist;
drop table if exists user_source_settings;
drop table if exists job_sources;
drop table if exists search_schedules;
drop table if exists search_preferences;
drop table if exists candidate_profiles;
drop table if exists resume_extractions;
drop table if exists resumes;
drop table if exists sessions;
drop table if exists users;
