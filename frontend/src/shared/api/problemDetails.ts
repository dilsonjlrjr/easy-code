/** RFC 9457 problem details, as the API returns them (`application/problem+json`). */
export interface ProblemDetails {
  type?: string
  title?: string
  status?: number
  detail?: string
  instance?: string
  [extension: string]: unknown
}

export class ApiError extends Error {
  readonly status: number
  readonly problem: ProblemDetails | undefined

  constructor(status: number, problem: ProblemDetails | undefined) {
    super(problem?.title ?? `HTTP ${String(status)}`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
  }
}
