#!/usr/bin/env bash
# End-to-end smoke test for the JobSeeker API.
#
# Drives the real HTTP surface against a running API and a real database:
# register -> upload a real CV -> confirm the profile -> configure search ->
# run a search that calls the live job sources -> read back scored results.
#
# Usage:  scripts/smoke.sh [base_url]
# Requires: the API running (make run-api), a migrated database, python3.
set -euo pipefail

BASE="${1:-http://127.0.0.1:8080}"
JAR="$(mktemp -t jobseeker-cookies.XXXXXX)"
PDF="internal/resume/testdata/sample_cv.pdf"
EMAIL="smoke-$(date +%s)-$$@example.com"
PASSWORD="correct-horse-battery-staple"

pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }
step() { printf '\n\033[36m== %s\033[0m\n' "$1"; }

api() { # method path [json-body]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$BASE$path" -b "$JAR" -c "$JAR" \
      -H 'Content-Type: application/json' -d "$body" -w '\n%{http_code}'
  else
    curl -sS -X "$method" "$BASE$path" -b "$JAR" -c "$JAR" -w '\n%{http_code}'
  fi
}

# split_response extracts the body and status code from api() output.
split_response() { python3 -c '
import sys
raw = sys.stdin.read()
body, _, code = raw.rpartition("\n")
print(code.strip())
print(body)
'; }

step "waiting for the API at $BASE"
for _ in $(seq 1 40); do
  if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.5
done
curl -fsS "$BASE/healthz" >/dev/null || fail "API is not answering /healthz"
pass "API is up"

step "unauthenticated access is rejected"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/v1/jobs")
[ "$code" = "401" ] || fail "expected 401 without a session, got $code"
pass "GET /api/v1/jobs without a session -> 401"

step "register"
out=$(api POST /api/v1/auth/register "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"display_name\":\"Smoke Test\"}")
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "201" ] || fail "register returned $code: $out"
pass "registration created a session (201)"

step "weak password is rejected"
out=$(curl -sS -X POST "$BASE/api/v1/auth/register" -H 'Content-Type: application/json' \
  -d '{"email":"weak@example.com","password":"short"}' -w '\n%{http_code}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "400" ] || fail "weak password should be 400, got $code"
pass "short password rejected (400)"

step "upload a real CV (PDF)"
[ -f "$PDF" ] || fail "fixture $PDF is missing"
out=$(curl -sS -X POST "$BASE/api/v1/resumes" -b "$JAR" -c "$JAR" -F "file=@$PDF" -w '\n%{http_code}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "201" ] || fail "upload returned $code: $out"
printf '%s' "$out" | python3 -c '
import json, sys
raw = sys.stdin.read()
raw = raw.rsplit("\n", 1)[0]
d = json.loads(raw)
draft = d.get("draft") or {}
fields = draft.get("fields") or {}
assert d["resume"]["status"] == "parsed", d["resume"]
for key in ("skills", "current_title", "years_experience"):
    assert key in fields, f"missing extracted field {key}: {sorted(fields)}"
skills = fields["skills"]["value"]
assert "React" in skills and "Go" in skills, skills
print("extracted skills:", ", ".join(skills[:8]), "...")
print("years_experience:", fields["years_experience"]["value"])
print("seniority:", fields.get("seniority", {}).get("value"))
'
pass "CV parsed and a draft profile was extracted"

step "a non-document upload is rejected"
out=$(curl -sS -X POST "$BASE/api/v1/resumes" -b "$JAR" -c "$JAR" -F "file=@README.md;type=text/markdown" -w '\n%{http_code}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "400" ] || fail "a markdown upload should be rejected, got $code"
pass "unsupported format rejected (400)"

step "confirm the candidate profile"
out=$(api PUT /api/v1/profile '{"full_name":"Valerian Dwipurnomo","headline":"Senior Full Stack Engineer","current_title":"Senior Full Stack Engineer","years_experience":10.2,"seniority":"senior","skills":["React","TypeScript","Go","PostgreSQL","Docker","Kubernetes","AWS","gRPC","Kafka","Prometheus"],"industries":["Fintech"],"languages":["English","Indonesian"]}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "200" ] || fail "profile save returned $code: $out"
pass "profile confirmed"

step "configure search preferences"
out=$(api PUT /api/v1/preferences '{"job_titles":["Frontend Engineer","Full Stack Engineer"],"keywords":["react","typescript","go"],"excluded_keywords":[],"locations":[{"country":"ID","mandatory":false},{"country":"MY","mandatory":false}],"work_arrangements":[{"value":"remote","mandatory":false},{"value":"hybrid","mandatory":false}],"employment_types":["permanent","full_time","contract"],"seniority_levels":["senior","mid"],"min_salary":null,"salary_currency":"","salary_period":"","min_score":0}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "200" ] || fail "preferences save returned $code: $out"
pass "preferences saved"

step "schedule"
out=$(api PUT /api/v1/schedule '{"cron_spec":"0 8 * * *","timezone":"Asia/Jakarta","enabled":true}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "200" ] || fail "schedule save returned $code: $out"
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
print("next run (UTC):", d["next_run_at"])
'
pass "daily schedule configured"

step "invalid cron is rejected"
out=$(api PUT /api/v1/schedule '{"cron_spec":"every morning","timezone":"Asia/Jakarta"}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "400" ] || fail "an invalid cron spec should be 400, got $code"
pass "invalid cron rejected (400)"

step "sources catalog"
out=$(api GET /api/v1/sources)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
srcs = d["sources"]
enabled = [s["id"] for s in srcs if s["enabled"]]
print("catalog:", len(srcs), "sources; enabled:", ", ".join(enabled))
remoteok = next((s for s in srcs if s["id"] == "remoteok"), None)
assert remoteok and remoteok["backlink_required"], "RemoteOK must declare its backlink requirement"
assert remoteok["attribution_text"], "attribution text must be present"
print("remoteok attribution:", remoteok["attribution_text"], "| backlink required:", remoteok["backlink_required"])
'
pass "source catalog exposes attribution obligations"

step "run a search against the live sources (this calls the real APIs)"
out=$(api POST /api/v1/searches/run '{}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "202" ] || fail "manual run returned $code: $out"
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
run = d["run"]
print("run status:", run["status"])
for item in sorted(run.get("items") or [], key=lambda i: i["source_id"]):
    print("  %-11s %-8s fetched=%-4d new=%-4d dup=%-4d %s" % (
        item["source_id"], item["status"], item["fetched"], item["new_jobs"], item["duplicated"], item["error"][:70]))
'
pass "search run completed"

step "job list with scores and explanations"
out=$(api GET '/api/v1/jobs?per_page=5&sort=score')
printf '%s' "$out" | python3 -c '
import json, sys
raw = sys.stdin.read().rsplit("\n",1)[0]
d = json.loads(raw)
jobs = d["jobs"]
print("total jobs:", d["total"], "| showing", len(jobs))
assert d["total"] > 0, "a live run over the permitted sources should have produced jobs"
for j in jobs[:5]:
    print("")
    print("  %s/100  %s @ %s" % (j["match_score"], j["title"], j["company"]))
    print("    arrangement=%s type=%s sen=%s verified=%s" % (
        j["work_arrangement"], j["employment_type"], j["seniority"], j["attributes_verified"]))
    for c in j["match_explanation"][:3]:
        print("    - %s: %s (%s/%s) %s" % (c["component"], c["status"], c["contribution"], c["weight"], c["reason"][:90]))
    for s in j["sources"]:
        print("    source: %s -> %s" % (s["source_id"], s["url"]))
'
pass "jobs returned with scores, explanations and source links"

step "duplicate handling across two runs"
before=$(api GET '/api/v1/jobs?per_page=1' | sed -n '$p' >/dev/null; api GET '/api/v1/jobs?per_page=1' | python3 -c 'import json,sys; print(json.loads(sys.stdin.read().rsplit("\n",1)[0])["total"])')
out=$(api POST /api/v1/searches/run '{}')
after=$(api GET '/api/v1/jobs?per_page=1' | python3 -c 'import json,sys; print(json.loads(sys.stdin.read().rsplit("\n",1)[0])["total"])')
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
run = d["run"]
dup = sum(i["duplicated"] for i in run.get("items") or [])
new = sum(i["new_jobs"] for i in run.get("items") or [])
print("second run: duplicated=%d new=%d (jobs before this run: %s)" % (dup, new, "'"$before"'"))
assert dup > 0 or new < '"$before"', "the second run must not re-create the same vacancies"
'
pass "second run deduplicated against the first (jobs: $before -> $after)"

step "search history"
out=$(api GET '/api/v1/searches/runs?per_page=3')
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
print("runs recorded:", d["total"])
for r in d["runs"]:
    items = r.get("items") or []
    ok = [i for i in items if i["status"] == "ok"]
    print("  %-9s %-10s sources_ok=%d/%d" % (r["trigger"], r["status"], len(ok), len(items)))
'
pass "run history is observable"

step "dashboard summary"
out=$(api GET /api/v1/dashboard/summary)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
print("total:", d["total_jobs"], "| new since last run:", d["new_since_last_run"],
      "| high match:", d["high_match_count"], "| next run:", d["next_run_at"])
'
pass "dashboard summary rendered"

step "job detail and tracking"
jobid=$(api GET '/api/v1/jobs?per_page=1' | python3 -c 'import json,sys; print(json.loads(sys.stdin.read().rsplit("\n",1)[0])["jobs"][0]["id"])')
out=$(api GET "/api/v1/jobs/$jobid")
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "200" ] || fail "job detail returned $code"
out=$(api POST "/api/v1/jobs/$jobid/save")
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "204" ] || fail "save returned $code"
out=$(api PUT "/api/v1/applications/$jobid" '{"status":"applied","applied_at":"2026-10-09T08:00:00Z","notes":"Applied through the original link."}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "200" ] || fail "application upsert returned $code: $out"
out=$(api GET /api/v1/applications)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.loads(sys.stdin.read().rsplit("\n",1)[0])
assert d["applications"], "the application should be listed"
print("application:", d["applications"][0]["status"], "-", d["applications"][0]["title"])
'
pass "save, apply and list tracked the vacancy"

step "invalid application status is rejected"
out=$(api PUT "/api/v1/applications/$jobid" '{"status":"hired"}')
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "400" ] || fail "an invalid status should be 400, got $code"
pass "invalid status rejected (400)"

step "logout revokes the session"
out=$(api POST /api/v1/auth/logout)
code=$(printf '%s' "$out" | sed -n '$p')
[ "$code" = "204" ] || fail "logout returned $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" "$BASE/api/v1/jobs")
[ "$code" = "401" ] || fail "after logout the session must be rejected, got $code"
pass "session revoked"

rm -f "$JAR"
printf '\n\033[32mAll smoke checks passed.\033[0m\n'
