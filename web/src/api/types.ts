// Hand-written types matching the frozen REST contract. No code generation.

export type ErrorCode =
  | 'validation_error'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'payload_too_large'
  | 'unsupported_media_type'
  | 'rate_limited'
  | 'internal_error'
  | 'service_unavailable'
  | 'bad_request'

export interface FieldError {
  field: string
  message: string
}

export interface ErrorBody {
  code: ErrorCode
  message: string
  details?: FieldError[] | null
  request_id?: string
}

export interface ErrorEnvelope {
  error: ErrorBody
}

export interface User {
  id: string
  email: string
  display_name: string
  created_at: string
}

export type Provenance = 'inferred' | 'confirmed'

export interface DraftField {
  value: unknown
  provenance: Provenance
  confidence?: number
}

export type ResumeDraft = Record<string, DraftField>

export interface Resume {
  id: string
  file_name?: string
  original_filename?: string
  size_bytes?: number
  content_type?: string
  uploaded_at?: string
  status?: string
  created_at?: string
  [key: string]: unknown
}

export interface ResumeUploadResult {
  resume: Resume
  draft: ResumeDraft
}

export interface EducationEntry {
  institution?: string | null
  degree?: string | null
  field?: string | null
  start_year?: number | null
  end_year?: number | null
}

export interface CertificationEntry {
  name?: string | null
  issuer?: string | null
  year?: number | null
}

export interface ProjectEntry {
  name?: string | null
  description?: string | null
  url?: string | null
}

export interface Profile {
  full_name?: string | null
  headline?: string | null
  current_title?: string | null
  years_experience?: number | null
  seniority?: string | null
  skills: string[]
  industries: string[]
  education: EducationEntry[]
  certifications: CertificationEntry[]
  languages: string[]
  projects: ProjectEntry[]
  /** Server-owned, but returned so the profile keeps its link to the CV. */
  source_resume_id?: string | null
  version: number
  confirmed_field_keys: string[]
}

export type WorkArrangementValue = 'remote' | 'onsite' | 'hybrid'
export type EmploymentType = 'permanent' | 'full_time' | 'contract' | 'part_time' | 'internship'
export type SalaryPeriod = 'year' | 'month'
export type SalaryCurrency = 'USD'

export interface PreferenceLocation {
  country: string
  city?: string | null
  region?: string | null
  mandatory: boolean
}

export interface PreferenceArrangement {
  value: WorkArrangementValue
  mandatory: boolean
}

export interface Preferences {
  job_titles: string[]
  keywords: string[]
  excluded_keywords: string[]
  locations: PreferenceLocation[]
  work_arrangements: PreferenceArrangement[]
  employment_types: EmploymentType[]
  seniority_levels: string[]
  min_salary: number | null
  salary_currency: SalaryCurrency | null
  salary_period: SalaryPeriod | null
  min_score: number
}

export interface Schedule {
  cron_spec: string
  timezone: string
  enabled: boolean
  last_run_at: string | null
  next_run_at: string | null
}

export interface Source {
  id: string
  display_name: string
  kind: string
  requires_key: boolean
  enabled: boolean
  attribution_text: string
  attribution_url: string
  backlink_required: boolean
  notes: string
  configured: boolean
}

export interface WatchlistItem {
  id?: string
  board_slug: string
  company: string
}

export type RunStatus = 'running' | 'succeeded' | 'partial' | 'failed' | 'aborted'

export interface RunItem {
  source_id: string
  status: string
  fetched: number
  accepted: number
  rejected: number
  duplicated: number
  new_jobs: number
  updated_jobs: number
  duration_ms: number
  error: string | null
}

export interface Run {
  id: string
  trigger: string
  status: RunStatus
  started_at: string
  finished_at: string | null
  error_summary: string | null
  stats?: Record<string, number>
  items?: RunItem[]
}

export interface RunsPage {
  runs: Run[]
  total: number
  page: number
  per_page: number
}

export type JobStatus = string

export interface MatchExplanationPart {
  component: string
  weight: number
  contribution: number
  reason: string
}

export interface JobSource {
  source_id: string
  display_name: string
  url: string
  apply_url?: string | null
  attribution_text: string
  attribution_url: string
  backlink_required: boolean
  last_status?: string
}

export interface Job {
  id: string
  title: string
  company: string
  location_city: string | null
  location_country: string | null
  work_arrangement: string | null
  employment_type: string | null
  seniority: string | null
  salary_min: number | null
  salary_max: number | null
  salary_currency: string | null
  salary_period: string | null
  published_at: string | null
  expires_at: string | null
  status: JobStatus | null
  first_discovered_at: string | null
  last_verified_at: string | null
  match_score: number | null
  match_explanation: MatchExplanationPart[]
  sources: JobSource[]
  attributes_verified: string[]
}

export interface JobDetail extends Job {
  description_summary?: string | null
}

export interface JobsPage {
  jobs: Job[]
  total: number
  page: number
  per_page: number
}

export type ApplicationStatus =
  | 'saved'
  | 'applied'
  | 'interviewing'
  | 'rejected'
  | 'accepted'
  | 'withdrawn'

export interface Application {
  job_id: string
  status: ApplicationStatus
  applied_at: string | null
  notes: string | null
  follow_up_at: string | null
  job?: Job
}

export interface DashboardSummary {
  total_jobs: number
  new_since_last_run: number
  high_match_count: number
  last_run: Run | null
  next_run_at: string | null
  recent_runs: Run[]
}

export interface JobFilters {
  q?: string
  country?: string
  city?: string
  arrangement?: string
  employment_type?: string
  seniority?: string
  source?: string
  min_score?: string
  status?: string
  sort?: string
  page?: string
  per_page?: string
}
