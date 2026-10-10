-- +goose Up

-- Operator decision: Kalibrr is switched on and given an adapter.
--
-- This is a deliberate, informed exception, not an oversight. Kalibrr's Terms of
-- Use require written permission to reuse their materials ("copy, distribute ...
-- or disseminate in any manner ... materials from our website or Application
-- without written permission from Kalibrr") and name "automated extraction,
-- scraping" under Account Monitoring and Acceptable Use. The operator was shown
-- that language and asked for the source anyway. Anyone re-enabling this source
-- inherits that decision.
--
-- What the feed actually covers (probed live):
--   * text  -> server-side keyword search ("Software Engineer" -> 272)
--   * location -> place-name filter ("Jakarta" -> 665, +text -> 174)
--   * country -> honoured ONLY for Indonesia (1,395) and Philippines (791);
--     any other value is IGNORED and returns the full feed (1,514), so the
--     adapter never sends an unsupported value.
--   * SG and MY: zero listings. Requests for those markets return Indonesian
--     and Philippine results; the adapter therefore cannot serve them.

update job_sources
set enabled_globally = true,
    notes = notes || ' | ENABLED BY OPERATOR DECISION (migration 0007) with the Kalibrr ToS language in view: written permission is required to reuse their materials and "automated extraction, scraping" is named as misuse. Coverage: Indonesia + Philippines only (SG/MY have no listings). Filters: text=, location=; country= honoured for ID/PH only.'
where id = 'kalibrr';

-- +goose Down

update job_sources
set enabled_globally = false,
    notes = replace(
        notes,
        ' | ENABLED BY OPERATOR DECISION (migration 0007) with the Kalibrr ToS language in view: written permission is required to reuse their materials and "automated extraction, scraping" is named as misuse. Coverage: Indonesia + Philippines only (SG/MY have no listings). Filters: text=, location=; country= honoured for ID/PH only.',
        ''
    )
where id = 'kalibrr';
