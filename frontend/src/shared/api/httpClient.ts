import { ApiError, type ProblemDetails } from './problemDetails'

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export interface HttpClientOptions {
  baseUrl: string
  /** Current interface language, sent as `Accept-Language` so API messages follow it. */
  currentLanguage: () => string
  fetchImplementation?: typeof fetch
  /** Called when the API answers 401 (no session, session expired), before the error is thrown. */
  onUnauthorized?: (problem: ProblemDetails | undefined) => void
}

export interface RequestOptions {
  /** JSON-serializable body, or `FormData` for multipart uploads. */
  body?: unknown
  headers?: Record<string, string>
  /** Content type of a JSON body (`application/merge-patch+json` for configuration changes). */
  contentType?: string
  /** Query parameters; arrays repeat the key, `undefined` values are skipped. */
  query?: Record<string, string | number | boolean | Array<string | number> | undefined>
  signal?: AbortSignal
}

/** Response body plus the headers some operations need (`ETag`, `Location`). */
export interface HttpResponse<TResponse> {
  data: TResponse
  status: number
  headers: Headers
}

export interface HttpClient {
  /** Base URL of the versioned API, for clients that open their own connection (event stream). */
  readonly baseUrl: string
  send<TResponse>(
    method: HttpMethod,
    path: string,
    options?: RequestOptions,
  ): Promise<HttpResponse<TResponse>>
  request<TResponse>(method: HttpMethod, path: string, options?: RequestOptions): Promise<TResponse>
  get<TResponse>(path: string, options?: RequestOptions): Promise<TResponse>
  post<TResponse>(path: string, body?: unknown, options?: RequestOptions): Promise<TResponse>
  put<TResponse>(path: string, body?: unknown, options?: RequestOptions): Promise<TResponse>
  delete<TResponse>(path: string, options?: RequestOptions): Promise<TResponse>
}

const NO_CONTENT = 204
const UNAUTHORIZED = 401

export function createHttpClient(options: HttpClientOptions): HttpClient {
  const fetchImplementation = options.fetchImplementation ?? globalThis.fetch.bind(globalThis)

  async function send<TResponse>(
    method: HttpMethod,
    path: string,
    requestOptions: RequestOptions = {},
  ): Promise<HttpResponse<TResponse>> {
    const url = withQuery(joinUrl(options.baseUrl, path), requestOptions.query)
    const response = await fetchImplementation(url, {
      method,
      headers: buildHeaders(options.currentLanguage(), requestOptions),
      body: serializeBody(requestOptions.body),
      credentials: 'include',
      signal: requestOptions.signal,
    })

    if (!response.ok) {
      const problem = await readProblemDetails(response)
      if (response.status === UNAUTHORIZED) options.onUnauthorized?.(problem)
      throw new ApiError(response.status, problem)
    }
    const data =
      response.status === NO_CONTENT ? undefined : ((await readBody(response)) as TResponse)
    return { data: data as TResponse, status: response.status, headers: response.headers }
  }

  async function request<TResponse>(
    method: HttpMethod,
    path: string,
    requestOptions?: RequestOptions,
  ): Promise<TResponse> {
    return (await send<TResponse>(method, path, requestOptions)).data
  }

  return {
    baseUrl: options.baseUrl,
    send,
    request,
    get: (path, requestOptions) => request('GET', path, requestOptions),
    post: (path, body, requestOptions) => request('POST', path, { ...requestOptions, body }),
    put: (path, body, requestOptions) => request('PUT', path, { ...requestOptions, body }),
    delete: (path, requestOptions) => request('DELETE', path, requestOptions),
  }
}

function serializeBody(body: unknown): BodyInit | undefined {
  if (body === undefined) return undefined
  if (body instanceof FormData) return body
  return JSON.stringify(body)
}

function buildHeaders(language: string, requestOptions: RequestOptions): Headers {
  const headers = new Headers({
    Accept: 'application/json, application/problem+json',
    'Accept-Language': language,
    ...requestOptions.headers,
  })
  // FormData sets its own multipart boundary.
  if (requestOptions.body !== undefined && !(requestOptions.body instanceof FormData)) {
    headers.set('Content-Type', requestOptions.contentType ?? 'application/json')
  }
  return headers
}

export function joinUrl(baseUrl: string, path: string): string {
  const normalizedBase = baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${normalizedBase}${normalizedPath}`
}

export function withQuery(url: string, query: RequestOptions['query']): string {
  if (!query) return url
  const parameters = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) continue
    const values = Array.isArray(value) ? value : [value]
    for (const item of values) parameters.append(key, String(item))
  }
  const serialized = parameters.toString()
  if (serialized === '') return url
  return `${url}${url.includes('?') ? '&' : '?'}${serialized}`
}

async function readBody(response: Response): Promise<unknown> {
  const contentType = response.headers.get('Content-Type') ?? ''
  if (contentType.includes('json')) return response.json()
  return response.text()
}

async function readProblemDetails(response: Response): Promise<ProblemDetails | undefined> {
  const contentType = response.headers.get('Content-Type') ?? ''
  if (!contentType.includes('json')) {
    return undefined
  }
  try {
    return (await response.json()) as ProblemDetails
  } catch {
    return undefined
  }
}
