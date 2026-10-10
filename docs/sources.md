# Job source feasibility and obligations

This document records **what was verified against the live services**, what each
source permits, and what the product owes in return. Everything in the first
three tables was probed directly (HTTP status + payload inspected) rather than
taken from marketing pages or blog posts.

Verified on a development machine on the date the project was set up. Endpoints
and terms change: re-probe before relying on any row.

## 1. Available, keyless — shipped in this build

| Source | Endpoint | Observed | Fields actually published | Obligations |
|---|---|---|---|---|
| **Arbeitnow** | `GET https://www.arbeitnow.com/api/job-board-api?page=N` | 200, 325 jobs/page, paginated, hourly refresh | title, company_name, description, tags, location, remote, job_types, created_at, url | `meta.terms`: free public API, "please do not abuse", link back appreciated |
| **Remotive** | `GET https://remotive.com/api/remote-jobs?limit=N` | 200 | title, company, category, job_type, salary, candidate_required_location, publication_date, tags, url | `0-legal-notice`: attribution + link back; **must not be redistributed to third-party job boards**; data delayed 24h |
| **Remote OK** | `GET https://remoteok.com/api` | 200, 100-job window, element 0 carries the licence | position, company, tags, location, salary_min/max, url, date, apply_url | **dofollow backlink required** — the API suspends access otherwise. Rendered as a normal follow link in the UI (`backlink_required = true`) |
| **Jobicy** | `GET https://jobicy.com/api/v2/remote-jobs?count=N` | 200 | jobTitle, companyName, jobGeo, jobIndustry, jobLevel, jobType, salaryMin/Max/Currency/Period, pubDate, url | free collaborative remote feed |
| **Himalayas** | `GET https://himalayas.app/jobs/api?limit=N` | 200 | title, company, seniority, employmentType, expiryDate, locationRestrictions, min/maxSalary, currency, applicationLink, pubDate | free public feed |
| **Greenhouse** (company boards) | `GET https://boards-api.greenhouse.io/v1/boards/{slug}/jobs?content=true` | 200 | title, location, offices, content, absolute_url, first_published, updated_at, application_deadline | public board API; requires a per-user company watchlist |
| **Lever** (company boards) | `GET https://api.lever.co/v0/postings/{slug}?mode=json` | 200 on live boards | text, hostedUrl, applyUrl, categories.{location,team,commitment}, descriptionPlain, createdAt | public board API; requires a watchlist |
| **Ashby** (company boards) | `GET https://api.ashbyhq.com/posting-api/job-board/{slug}?includeCompensation=true` | 200 | title, location, department, employmentType, isRemote, **workplaceType**, compensation, publishedAt, jobUrl | public board API; requires a watchlist |
| **SmartRecruiters** (company boards) | `GET https://api.smartrecruiters.com/v1/companies/{id}/postings?limit=&offset=` | 200 (`sgs` → 4,639 postings) | id, name, company, releasedDate, location{city,region,country(ISO-2),fullLocation,remote,hybrid}, typeOfEmployment, experienceLevel, department/function/industry | public postings API; requires a watchlist. **Publishes no description** on the list endpoint, so `description` is deliberately left empty rather than fetched per posting |
| **Recruitee** (company boards) | `GET https://{sub}.recruitee.com/api/offers/` | 200 (`channable` → 10 offers) | id, title, company_name, description/requirements, city/state_name/country_code, remote/hybrid/on_site, employment_type_code, experience_code, salary{min,max,period,currency}, published_at, close_at, careers_url | public offers API; requires a watchlist. Draft/archived offers are skipped (only `status=published`) |
| **Personio** (company boards) | `GET https://{sub}.jobs.personio.de/xml` | 200 (thermondo → 197 positions) | id, subcompany, office, department, name, jobDescriptions, employmentType, seniority, schedule, yearsOfExperience, createdAt | public XML feed; requires a watchlist. Feed carries **no URL and no country** — the job URL is built from the board's own `/job/{id}` path (verified 200) and country stays empty |
| **Kalibrr** | `GET https://www.kalibrr.com/kjs/job_board/search?limit=&offset=&text=&location=&country=` | 200, keyless JSON, 1,514 listings | id, name, company{code,name}, description + qualifications, google_location.address_components{city,region,country}, tenure, is_work_from_home, is_hybrid, salary block, activation_date, application_end_date, job_sds_skills | **ENABLED BY OPERATOR DECISION (migration 0007) with the ToS language in view.** Coverage: Indonesia + Philippines only. `text=` and `location=` are honoured; **`country=` only for `Indonesia`/`Philippines`** — any other value returns the *entire* marketplace, and so do unknown place names (`location=Singapore` → 1,514 Indonesian listings), so the adapter forwards only a verified allow-list of 18 cities and **skips** unserved markets instead of filing Indonesian vacancies under a Singapore label |

ATS boards return `skipped` (not an error) when the user has configured no
watchlist — the run report says exactly that.

## 2. Available with an operator-supplied key — catalogued, no adapter yet

| Source | Reality | Terms / limits |
|---|---|---|
| **Adzuna** | Requires `app_id` + `app_key` | Default limits 25/min, 250/day, 1,000/week, 2,500/month; each displayed advert must carry the label "Adzuna" at least **116×23 px, hyperlinked** to the local domain; publishing beyond the free tier may need a licence |
| **USAJOBS** | Requires an API key and a contact email in `User-Agent` | US federal roles only; documented limits: max 10,000 rows/query, 500 rows/page |
| **Jooble** | Partner REST API, **one key per country domain**, obtained by registering on `jooble.org/api/about` | Onboarding step required; not a self-serve feed |

These rows are seeded with `enabled_globally = false` (migration `0002`) and are
reported in the UI as unavailable in this build rather than being silently
"searched" and failing on every run.

## 3. Not available through any permissible automated channel

| Source | Evidence |
|---|---|
| **Indeed** | Publisher API dead: sign-ups closed 2020, endpoints returned 410 by 2024; `id.indeed.com/robots.txt` disallows `/job/`, `/api/getrecjobs`, `/advanced_search`. What remains is employer/ATS-side only |
| **Glassdoor** | `api.glassdoor.com/api/api.htm` returns **410 Gone**; the public API was retired with no self-serve replacement |
| **LinkedIn Jobs** | Microsoft Learn: "We are currently not accepting new partnerships" for the Job Posting API. Jobs T&C: "Except as expressly authorized by LinkedIn in writing, use any automated means or form of scraping or data extraction to access, modify, download, query or otherwise collect information from LinkedIn." `robots.txt` disallows `/jobs?runSearch*`, `/api/jobPostings/jobs*`, `/jsearch*` **and `/jobs-guest/`**. Re-probed: `/jobs-guest/jobs/api/seeMoreJobPostings/search` **does answer 200 with real cards** (title, company, location, posted date, job URL; 10 per page; e.g. Grab and Kredivo in Jakarta, Apple in Singapore) — so the obstacle is contractual, not technical, and no adapter is built |
| **JobStreet / SEEK** | `developer.seek.com` is an integration surface for recruitment software and hirers (posting/managing ads, ATS sync) — not a seeker-side listing feed, and it needs partner onboarding. `robots.txt` (id/my/sg.jobstreet.com, `User-agent: *`) disallows `*/job/`, `*?`, `/graphql` and — exactly the endpoint the adapter uses — `/api/jobsearch/`. The Website Terms and Conditions (SEEK Limited, covering *"SEEK, JobStreet by SEEK or JobsDB by SEEK"*, last updated 20 July 2026) say: *"You may not use data mining, robots, screen scraping, or similar automated data gathering, extraction or publication tools on our websites and apps … except with our prior written consent"*, and *"our websites and apps are designed for individual human use. Except where we have provided a specific technical interface (such as an API) … you must not use … any automated process, script, tool, bot, scraper or other technical means to … access, query, search, copy, collect, mine or harvest information"*. **The operator nonetheless instructed this source to be built and enabled; that decision is recorded in `job_sources.notes` and migration `0008`.** Written consent remains outstanding. |
| **JobStreet API (probed live 2026-10-10)** | `GET https://{id,my,sg}.jobstreet.com/api/jobsearch/v5/search?siteKey={ID,MY,SG}-Main&sourcesystem=houston&keywords=…&page=&pageSize=` answers **200 JSON**. "software engineer" → **1419 (ID), 2262 (MY), 5420 (SG)**; `where` is a real place filter (**Jakarta 804, Bandung 55**); pageSize ceiling is **100** (200 → 400); `page` is 1-based and pages do not overlap. The payload carries `teaser` (not the full ad), `salaryLabel`, `workTypes`, `workArrangements`, `locations`, `classifications` — but **no seniority and no expiry**, which therefore stay unknown. Individual job pages answer a **Cloudflare JS challenge** ("Just a moment…", 403 to non-browser clients), so the listing URL `/job/{id}` is **inferred, not verified**; the adapter deliberately does not attempt to pass the challenge. A 5-request burst saw no 429/403, but the registry budget is set to the tightest in the build (**20/min, ~one request every 3s**) because the source is WAF-protected. |
| **Google Jobs** | No public API. Only paid aggregators; vendor pricing must be confirmed before committing to cost |
| **Glints** | Re-probed: `api/v2/jobOpportunities`, `api/v1/jobOpportunities`, `/id/opportunities/jobs/explore` all answer **403 with a 1.3 MB WAF "Firewall" interstitial** (also with a browser User-Agent); `apiv3.glints.com` does not resolve. `robots.txt` disallows the search surface (`/opportunities/jobs/explore?*`). Not reachable *and* not permitted |
| **Kalibrr** | An open, keyless JSON API exists (`GET https://www.kalibrr.com/kjs/job_board/search?limit=&offset=&text=`, 1,525 jobs, `country=Indonesia` → 1,403, `country=Philippines` → 798) and `robots.txt` disallows only `/root` and `/candidate/profile`. The Terms of Use do not permit reuse: "copy, distribute … or disseminate in any manner … materials from our website or Application **without written permission from Kalibrr**", and "automated extraction, scraping" is named under Account Monitoring and Acceptable Use. **The operator nonetheless instructed this source to be built and enabled; that decision is recorded in `job_sources.notes` and migration `0007`.** Written permission remains outstanding |
| **Workable** | Public widget endpoint (`/api/v1/widget/accounts/{sub}?details=true`) answers 200 but returns **no jobs for every account probed**; the real SPI v3 API (`https://{sub}.workable.com/spi/v3/jobs`) answers **401 `invalid_token`**. Effectively key-gated, so it is not shipped |

## 4. What this means for the product

- A "search LinkedIn + Indeed + JobStreet + Glassdoor" product is **not buildable
  legally today**. This application does not attempt it — with one recorded
  exception: **JobStreet and Kalibrr are enabled by explicit operator decision**
  against their terms (migrations `0007`, `0008`), with the clause in the row
  notes and the listing URL for JobStreet flagged as inferred.
- Coverage comes from permitted feeds plus an **ATS company watchlist**, which is
  where the most complete structured data actually lives (compensation,
  workplace type, deadlines).
- Attribution is a product feature, not an afterthought: obligations are stored
  per source in `job_sources` and returned with every listing so a badge cannot
  be forgotten when the UI changes.
- Original URLs are always preserved and shown; the canonical URL hash used for
  deduplication never replaces the link the user clicks.

## 5. Adding a source

1. Verify access: HTTP status, payload shape, terms, robots, rate limits.
2. Add a row to `job_sources` (migration) including attribution text/URL and
   whether a backlink is required.
3. Implement `sources.Adapter` in `internal/sources/<name>.go` — map fields, set
   `AttributesVerified` **only** for values the payload actually contained, and
   never invent salary, arrangement or employment type.
4. Capture a real fixture into `internal/sources/testdata/<name>.json` and add
   table-driven tests.
5. Register the adapter in `internal/app.buildRegistry` and set a rate limit.
