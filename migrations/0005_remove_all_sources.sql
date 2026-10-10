-- +goose Up

-- Operator decision: remove the source master rows outright rather than leaving
-- them flagged disabled (migration 0004). The catalog is now empty, so the
-- Sources screen lists nothing and a run reports that no source is configured.
--
-- What this does and does not touch:
--   * deletes all 13 job_sources rows (the master/reference data);
--   * CASCADE removes their children: job_source_listings (the source -> job
--     links), source_watchlist (per-user company boards) and
--     user_source_settings (per-user enable flags). Those rows cannot be
--     recreated by a rollback — they are operational/user data;
--   * does NOT delete vacancies themselves: `jobs` has no foreign key to
--     job_sources, so existing rows remain (without a source listing).
--
-- The adapters stay compiled in and callable: restoring a source is a matter of
-- re-inserting its row (migration Down does exactly that).

delete from job_sources;

-- +goose Down

-- Restore the source master rows, disabled, matching the state left by
-- migration 0004. Children removed by the CASCADE above (listings, watchlists,
-- per-user settings) are NOT restored — restore those from a database backup.
insert into job_sources (id, display_name, kind, requires_key, enabled_globally, attribution_text, attribution_url, backlink_required, notes)
values
    ('arbeitnow',  'Arbeitnow',  'feed',       false, false, 'Jobs from Arbeitnow',      'https://www.arbeitnow.com',     false, 'Free keyless API; 325 jobs/page; Europe + remote focus; link back requested'),
    ('remotive',   'Remotive',   'feed',       false, false, 'Jobs from Remotive',       'https://remotive.com',          false, 'Keyless; 24h delayed; attribution + link back required; must not be redistributed to other job boards'),
    ('remoteok',   'Remote OK',  'feed',       false, false, 'Jobs from Remote OK',      'https://remoteok.com',          true,  'Keyless; 100-job window; dofollow backlink REQUIRED or access is suspended'),
    ('jobicy',     'Jobicy',     'feed',       false, false, 'Jobs from Jobicy',         'https://jobicy.com',            false, 'Keyless; remote-only; salary fields often present'),
    ('himalayas',  'Himalayas',  'feed',       false, false, 'Jobs from Himalayas',      'https://himalayas.app',         false, 'Keyless; includes seniority, employment type and expiry'),
    ('greenhouse', 'Greenhouse', 'ats',        false, false, 'Company careers board',    'https://boards.greenhouse.io',  false, 'Public board API; requires a company watchlist'),
    ('lever',      'Lever',      'ats',        false, false, 'Company careers board',    'https://jobs.lever.co',         false, 'Public postings API; requires a company watchlist'),
    ('ashby',      'Ashby',      'ats',        false, false, 'Company careers board',    'https://jobs.ashbyhq.com',      false, 'Public posting API; workplaceType + compensation when published'),
    ('smartrecruiters', 'SmartRecruiters', 'ats', false, false, 'Company careers board', 'https://jobs.smartrecruiters.com', false,
     'Public postings API; requires a company watchlist (board identifier, e.g. SGS). List endpoint publishes no description; seniority and employment type are stated.'),
    ('recruitee', 'Recruitee', 'ats', false, false, 'Company careers board', 'https://recruitee.com', false,
     'Public offers API; requires a company watchlist (subdomain, e.g. channable). Publishes salary, remote/hybrid/on-site flags and expiry.'),
    ('personio', 'Personio', 'ats', false, false, 'Company careers board', 'https://www.personio.com', false,
     'Public XML feed; requires a company watchlist (subdomain, e.g. thermondo). Publishes seniority, employment type and full descriptions.'),
    ('adzuna',  'Adzuna',  'aggregator', true, false, 'Adzuna',  'https://www.adzuna.com',  false, 'Free key; 25/min, 250/day, 1000/week, 2500/month; label each advert "Adzuna" >=116x23px with link | No adapter in this build: requires an operator-supplied API key and an explicit decision to enable (see docs/sources.md).'),
    ('usajobs', 'USAJOBS', 'aggregator', true, false, 'USAJOBS', 'https://www.usajobs.gov', false, 'Free key + contact email in User-Agent; US federal roles only | No adapter in this build: requires an operator-supplied API key and an explicit decision to enable (see docs/sources.md).')
on conflict (id) do nothing;
