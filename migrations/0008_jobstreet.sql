-- +goose Up

-- Operator decision: JobStreet by SEEK (SEEK Limited) is added and switched on
-- WITH the access restrictions in full view. This is a deliberate, informed
-- exception — not an oversight — and anyone re-enabling this row inherits it.
--
--   * robots.txt (id.jobstreet.com, `User-agent: *`) disallows `*/job/`, `*?`,
--     `/graphql` and — exactly the endpoint the adapter uses — `/api/jobsearch/`.
--   * The Website Terms and Conditions (SEEK Limited; covers "SEEK, JobStreet by
--     SEEK or JobsDB by SEEK"; last updated 20 July 2026) state: "You may not use
--     data mining, robots, screen scraping, or similar automated data gathering,
--     extraction or publication tools on our websites and apps ... except with
--     our prior written consent", and "our websites and apps are designed for
--     individual human use. Except where we have provided a specific technical
--     interface (such as an API) ... you must not use ... any automated process,
--     script, tool, bot, scraper or other technical means to ... access, query,
--     search, copy, collect, mine or harvest information". That exception is
--     SEEK's developer API: a hirer/ATS surface needing partner onboarding, not a
--     seeker-side listing feed.
--   * Individual job pages answer a Cloudflare JS challenge ("Just a moment...",
--     HTTP 403 to non-browser clients). The adapter does NOT attempt to pass that
--     challenge. The listing URL it emits (/job/{id}) is therefore INFERRED from
--     the board's path shape and was NOT verified by fetch.
--
-- Verified endpoint behaviour (probed live 2026-10-10):
--   * siteKey selects the market: ID-Main / MY-Main / SG-Main on
--     id./my./sg.jobstreet.com. "software engineer" -> 1419 (ID), 2262 (MY),
--     5420 (SG).
--   * `keywords` = server-side search; `where` = place filter that genuinely
--     narrows results (Jakarta 804, Bandung 55).
--   * pageSize ceiling 100 (200 -> HTTP 400); `page` is 1-based.
--   * The list payload carries a teaser (not the full ad), salaryLabel,
--     workTypes, workArrangements, locations and classifications, but NO
--     seniority and NO expiry — those stay unknown rather than guessed.
--   * Rate limited to 20 requests/minute in the registry (one every 3s), the
--     tightest budget in the build.

insert into job_sources (id, display_name, kind, requires_key, enabled_globally, attribution_text, attribution_url, backlink_required, notes)
values (
    'jobstreet',
    'JobStreet by SEEK',
    'aggregator',
    false,
    true,
    'Jobs by JobStreet by SEEK',
    'https://www.jobstreet.com',
    true,
    'ENABLED BY OPERATOR DECISION (migration 0008) with the SEEK/JobStreet Terms and Conditions in view: automated gathering/extraction requires prior written consent and the site is stated to be for individual human use; robots.txt also disallows /api/jobsearch/ and */job/. Coverage: Indonesia (ID-Main), Malaysia (MY-Main), Singapore (SG-Main) — one market per configured location. Filters: keywords=, where= (place). Listing URL /job/{id} is INFERRED and unverified: job pages sit behind a Cloudflare challenge the adapter does not attempt to pass. Payload has teaser (no full ad), salaryLabel, workTypes, workArrangements, locations, classifications; NO seniority, NO expiry. Rate limit 20/min.'
);

-- +goose Down

delete from job_sources where id = 'jobstreet';
