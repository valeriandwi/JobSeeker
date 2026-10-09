-- +goose Up

-- Sources in the catalog that this build has no adapter for are switched off by
-- default. They stay visible (and documented) so the UI can explain what is not
-- covered and why, but a run must not report them as broken sources.

update job_sources
set enabled_globally = false,
    notes = notes || ' | No adapter in this build: requires an operator-supplied API key and an explicit decision to enable (see docs/sources.md).'
where id in ('adzuna', 'usajobs', 'jooble');

-- +goose Down

update job_sources
set enabled_globally = true
where id in ('adzuna', 'usajobs', 'jooble');
