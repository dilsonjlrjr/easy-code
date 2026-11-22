import { ApiError, type ProblemDetails } from './problemDetails'

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export interface HttpClientOptions {
  baseUrl: string
  /** Current interface language, sent as `Accept-Language` so API messages follow it. */
  currentLanguage: () => string
  fetchImplementation?: typeof fetch
}

export interface RequestOptions {
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
}

export interface HttpClient {
  request<TResponse>(method: HttpMethod, path: string, options?: RequestOptions): Promise<TResponse>
  get<TResponse>(path: string, options?: RequestOptions): Promise<TResponse>
  post<TResponse>(path: string, body?: unknown, options?: RequestOptions): Promise<TResponse>
}

const NO_CONTENT = 204

export function createHttpClient(options: HttpClientOptions): HttpClient {
  const fetchImplementation = options.fetchImplementation ?? globalThis.fetch.bind(globalThis)

  async function request<TResponse>(
    method: HttpMethod,
    path: string,
    requestOptions: RequestOptions = {},
  ): Promise<TResponse> {
    const response = await fetchImplementation(joinUrl(options.baseUrl, path), {
      method,
      headers: buildHeaders(options.currentLanguage(), requestOptions),
      body: requestOptions.body === undefined ? undefined : JSON.stringify(requestOptions.body),
      credentials: 'include',
      signal: requestOptions.signal,
    })

    if (!response.ok) {
      throw new ApiError(response.status, await readProblemDetails(response))
    }
    if (response.status === NO_CONTENT) {
      return undefined as TResponse
    }
    return (await response.json()) as TResponse
  }

  return {
    request,
    get: (path, requestOptions) => request('GET', path, requestOptions),
    post: (path, body, requestOptions) => request('POST', path, { ...requestOptions, body }),
  }
}

function buildHeaders(language: string, requestOptions: RequestOptions): Headers {
  const headers = new Headers({
    Accept: 'application/json, application/problem+json',
    'Accept-Language': language,
    ...requestOptions.headers,
  })
  if (requestOptions.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  return headers
}

export function joinUrl(baseUrl: string, path: string): string {
  const normalizedBase = baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${normalizedBase}${normalizedPath}`
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
