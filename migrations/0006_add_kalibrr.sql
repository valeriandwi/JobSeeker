-- +goose Up

-- Kalibrr enters the catalog as a known source that this build does not fetch.
--
-- Why it is not enabled:
--   1. No adapter is shipped for it (see below).
--   2. Kalibrr's Terms of Use do not permit reuse of their materials without
--      written permission: "copy, distribute ... or disseminate in any manner
--      ... materials from our website or Application without written
--      permission from Kalibrr", with "automated extraction, scraping" named
--      under Account Monitoring and Acceptable Use.
--
-- The access path itself is verified and documented (keyless JSON at
-- GET https://www.kalibrr.com/kjs/job_board/search?limit=&offset=&text=,
-- ~1,525 listings, country=Indonesia -> 1,403, country=Philippines -> 798), so
-- enabling it is a business decision, not an engineering one. Until then the
-- UI reports it as unavailable instead of pretending to search it.
--
-- This mirrors the adzuna/usajobs rows: present, visible, not searched.

insert into job_sources (id, display_name, kind, requires_key, enabled_globally, attribution_text, attribution_url, backlink_required, notes)
values
    ('kalibrr', 'Kalibrr', 'feed', false, false, 'Jobs from Kalibrr', 'https://www.kalibrr.com', false,
     'Indonesia + Philippines coverage. Access verified: keyless JSON feed (GET /kjs/job_board/search). No adapter in this build: Kalibrr Terms of Use require written permission to reuse their materials and name "automated extraction, scraping" as misuse. Enable only with written permission or an explicit operator decision (see docs/sources.md).')
on conflict (id) do nothing;

-- +goose Down

delete from job_source_listings where source_id = 'kalibrr';
delete from source_watchlist where source_id = 'kalibrr';
delete from user_source_settings where source_id = 'kalibrr';
delete from job_sources where id = 'kalibrr';
