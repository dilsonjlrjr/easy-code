/** RFC 9457 problem details, as the API returns them (`application/problem+json`). */
export interface ProblemDetails {
  type?: string
  title?: string
  status?: number
  detail?: string
  instance?: string
  /** OpenTelemetry trace of the request (RNF-14), shown so support can find the logs. */
  trace_id?: string
  errors?: Array<{ field: string; message: string }>
  current_state?: string
  available_actions?: string[]
  /** What is missing to start the workflow (`incomplete-configuration`, US-05 CA-2). */
  missing?: string[]
  /** Capability the chosen model lacks (`model-without-capability`, US-16 CA-2). */
  missing_capacity?: string
  limit_bytes?: number
  /** `artifact-invalid` (US-29 CA-1): schema errors of the artifact, JSON pointer and message. */
  schema_errors?: Array<{ path: string; message: string }>
  /** `invalid-key` (US-15 CA-3): the provider's error, summarized and without secrets. */
  provider_message?: string
  [extension: string]: unknown
}

const PROBLEM_TYPE_PREFIX = 'urn:easycode:problem:'

export class ApiError extends Error {
  readonly status: number
  readonly problem: ProblemDetails | undefined

  constructor(status: number, problem: ProblemDetails | undefined) {
    super(problem?.title ?? `HTTP ${String(status)}`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
  }

  /** Stable slug of the problem type (`urn:easycode:problem:<slug>`); the UI decides by it, never by text. */
  get slug(): string | undefined {
    return problemSlug(this.problem)
  }
}

export function problemSlug(problem: ProblemDetails | undefined): string | undefined {
  const type = problem?.type
  if (!type?.startsWith(PROBLEM_TYPE_PREFIX)) return undefined
  return type.slice(PROBLEM_TYPE_PREFIX.length)
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

/** True when the error is the API's answer of the given problem slug. */
export function hasProblemSlug(error: unknown, slug: string): error is ApiError {
  return isApiError(error) && error.slug === slug
}
