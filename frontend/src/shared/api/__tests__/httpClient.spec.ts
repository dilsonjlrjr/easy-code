import { describe, expect, it, vi } from 'vitest'
import { createHttpClient, joinUrl } from '../httpClient'
import { ApiError } from '../problemDetails'

function jsonResponse(body: unknown, status = 200, contentType = 'application/json'): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': contentType } })
}

describe('createHttpClient', () => {
  it('calls the configured base URL and sends the current language as Accept-Language', async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ id: 'p1' }))
    const client = createHttpClient({
      baseUrl: 'https://api.example.test/v1/',
      currentLanguage: () => 'en',
      fetchImplementation,
    })

    const result = await client.get<{ id: string }>('/projects')

    expect(result).toEqual({ id: 'p1' })
    const [url, init] = fetchImplementation.mock.calls[0] ?? []
    expect(url).toBe('https://api.example.test/v1/projects')
    expect(new Headers(init?.headers).get('Accept-Language')).toBe('en')
  })

  it('follows a language change between requests', async () => {
    let language = 'pt-BR'
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockImplementation(() => Promise.resolve(jsonResponse({})))
    const client = createHttpClient({
      baseUrl: '/api',
      currentLanguage: () => language,
      fetchImplementation,
    })

    await client.get('/projects')
    language = 'en'
    await client.get('/projects')

    const sentLanguages = fetchImplementation.mock.calls.map(([, init]) =>
      new Headers(init?.headers).get('Accept-Language'),
    )
    expect(sentLanguages).toEqual(['pt-BR', 'en'])
  })

  it('sends a JSON body with its content type', async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }))
    const client = createHttpClient({
      baseUrl: '/api',
      currentLanguage: () => 'pt-BR',
      fetchImplementation,
    })

    await client.post('/projects', { name: 'Reserva de salas' })

    const [, init] = fetchImplementation.mock.calls[0] ?? []
    expect(init?.body).toBe('{"name":"Reserva de salas"}')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
  })

  it('turns a problem details response into an ApiError', async () => {
    const problem = { title: 'Projeto não encontrado', status: 404 }
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse(problem, 404, 'application/problem+json'))
    const client = createHttpClient({
      baseUrl: '/api',
      currentLanguage: () => 'pt-BR',
      fetchImplementation,
    })

    const failure = client.get('/projects/unknown')

    await expect(failure).rejects.toBeInstanceOf(ApiError)
    await expect(failure).rejects.toMatchObject({ status: 404, problem })
  })
})

describe('joinUrl', () => {
  it('joins base and path with exactly one slash', () => {
    expect(joinUrl('/api/', '/projects')).toBe('/api/projects')
    expect(joinUrl('/api', 'projects')).toBe('/api/projects')
  })
})
