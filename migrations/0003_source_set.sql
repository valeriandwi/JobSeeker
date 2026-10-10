-- +goose Up

-- Sea/global coverage via public ATS board APIs. These are the same class of
-- source as greenhouse/lever/ashby: a public read API the vendor publishes for
-- the company's own careers page, reached through a per-user company watchlist.
--
-- Deliberately NOT wired: LinkedIn (robots.txt Disallow: /jobs-guest/ and terms
-- prohibiting automated collection), Glints (WAF 403 on every endpoint) and
-- Kalibrr (open JSON API, but terms require written permission to reuse their
-- materials). Workable is omitted too: its public widget endpoint returns no
-- jobs and the SPI v3 API answers 401 invalid_token, i.e. it is key-gated.
-- See docs/sources.md for the evidence behind each decision.

insert into job_sources (id, display_name, kind, requires_key, attribution_text, attribution_url, backlink_required, notes)
values
    ('smartrecruiters', 'SmartRecruiters', 'ats', false, 'Company careers board', 'https://jobs.smartrecruiters.com', false,
     'Public postings API; requires a company watchlist (board identifier, e.g. SGS). List endpoint publishes no description; seniority and employment type are stated.'),
    ('recruitee', 'Recruitee', 'ats', false, 'Company careers board', 'https://recruitee.com', false,
     'Public offers API; requires a company watchlist (subdomain, e.g. channable). Publishes salary, remote/hybrid/on-site flags and expiry.'),
    ('personio', 'Personio', 'ats', false, 'Company careers board', 'https://www.personio.com', false,
     'Public XML feed; requires a company watchlist (subdomain, e.g. thermondo). Publishes seniority, employment type and full descriptions.')
on conflict (id) do nothing;

-- +goose Down

-- Remove dependants first so the rollback cannot fail on a foreign key.
delete from source_watchlist where source_id in ('smartrecruiters', 'recruitee', 'personio');
delete from user_source_settings where source_id in ('smartrecruiters', 'recruitee', 'personio');
delete from job_sources where id in ('smartrecruiters', 'recruitee', 'personio');
