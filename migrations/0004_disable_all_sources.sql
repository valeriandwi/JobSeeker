-- +goose Up

-- Every source is switched off in the search configuration, pending a decision
-- about the source set the product should search.
--
-- Catalog-only and fully reversible: no adapter, no catalog row, no vacancy and
-- no user setting is deleted. Adapters stay compiled in and callable — a run
-- simply selects no sources while the flag is off, and reports that clearly
-- ("no sources were runnable: enable at least one source with a configured
-- adapter") instead of pretending to have searched.
--
-- A user's own user_source_settings row cannot resurrect a source while this
-- flag is off: source selection requires enabled_globally (see
-- internal/search/repository.go, EnabledSourceIDs).

update job_sources
set enabled_globally = false,
    notes = notes || ' | Switched off pending a source-set decision (migration 0004). Adapter and catalog row intact; restore with migration 0004 Down.'
where enabled_globally;

-- +goose Down

-- Restore exactly the sources that this migration switched off (the two
-- key-gated aggregators were already disabled by migration 0002 and stay off).
update job_sources
set enabled_globally = true,
    notes = replace(
        notes,
        ' | Switched off pending a source-set decision (migration 0004). Adapter and catalog row intact; restore with migration 0004 Down.',
        ''
    )
where id in (
    'arbeitnow', 'remotive', 'remoteok', 'jobicy', 'himalayas',
    'greenhouse', 'lever', 'ashby',
    'smartrecruiters', 'recruitee', 'personio'
);
