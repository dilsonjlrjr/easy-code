import { describe, expect, it } from 'vitest'
import {
  contentPrefix,
  contentRoot,
  displayPath,
  pagePath,
  relativePoint,
  screenMap,
  servedStories,
  withState,
} from '../composables/prototypeModel'

const header = {
  id: 'TELAS',
  prototipo: 'docs/design/prototipo/',
  fluxo: 'docs/design/fluxo.md',
  estilo: 'docs/design/estilo.md',
  telas: [
    {
      id: 'TL-1',
      nome: 'Login',
      arquivo: 'docs/design/prototipo/login.html',
      us: ['US-01'],
      criterios: ['US-01/CA-1', 'US-01/CA-2'],
      estados: ['erro', 'sem_permissao'],
    },
    {
      id: 'TL-2',
      nome: 'Guias',
      arquivo: 'docs/design/prototipo/sub/guias.html',
      us: ['US-02', 'US-04'],
      criterios: ['US-04/CA-1'],
      estados: ['vazio'],
      operacoes: ['listGuias'],
    },
    { nome: 'sem id' },
    'lixo',
  ],
  acessibilidade: { nivel: 'wcag22_aa', verificado: false, observacoes: 'TL-2 sem contraste' },
  lacunas_contrato: ['ACH-03'],
}

describe('screenMap (ER-11-5: the screens come from the TELAS header)', () => {
  it('reads the screens in order and ignores entries without an id', () => {
    const map = screenMap(header)
    expect(map.screens.map((screen) => screen.id)).toEqual(['TL-1', 'TL-2'])
    expect(map.screens[1]).toMatchObject({
      name: 'Guias',
      us: ['US-02', 'US-04'],
      states: ['vazio'],
      operations: ['listGuias'],
    })
    expect(map.prototype).toBe('docs/design/prototipo/')
    expect(map.accessibility).toEqual({
      level: 'wcag22_aa',
      verified: false,
      notes: 'TL-2 sem contraste',
    })
    expect(map.contractGaps).toEqual(['ACH-03'])
  })

  it('gives an empty map without a header', () => {
    expect(screenMap(undefined).screens).toEqual([])
    expect(screenMap({ telas: 'x' }).screens).toEqual([])
  })
})

describe('content paths (getArtifactContentUrl)', () => {
  it('splits the prototype folder as the api signs it', () => {
    expect(contentRoot('docs/design/prototipo/')).toBe('docs/design/')
    expect(contentPrefix('docs/design/prototipo/')).toBe('prototipo/')
    expect(contentRoot('prototipo')).toBe('')
    expect(contentPrefix('prototipo')).toBe('prototipo/')
  })

  it('asks only for files inside the prototype folder', () => {
    const prototype = 'docs/design/prototipo/'
    expect(pagePath(prototype, 'docs/design/prototipo/login.html')).toBe('prototipo/login.html')
    expect(pagePath(prototype, 'docs/design/prototipo/sub/guias.html')).toBe(
      'prototipo/sub/guias.html',
    )
    expect(pagePath(prototype, 'docs/design/estilo.md')).toBeUndefined()
    expect(pagePath(prototype, 'docs/design/prototipo/../TELAS.md')).toBeUndefined()
    expect(pagePath(prototype, '')).toBeUndefined()
    expect(pagePath('', 'docs/design/prototipo/login.html')).toBeUndefined()
  })

  it('shows the path relative to the prototype folder', () => {
    expect(displayPath('docs/design/prototipo/', 'docs/design/prototipo/login.html')).toBe(
      'login.html',
    )
    expect(displayPath('docs/design/prototipo', 'docs/design/prototipo/a/b.html')).toBe('a/b.html')
    expect(displayPath('docs/design/prototipo/', 'outro/x.html')).toBe('outro/x.html')
  })
})

describe('withState (ER-11-11, CA-11-14)', () => {
  it('adds ?estado= to the signed URL and keeps it otherwise', () => {
    const url = 'https://content.easycode.test/c/abc.def/prototipo/login.html'
    expect(withState(url, 'erro')).toBe(`${url}?estado=erro`)
    expect(withState(url, undefined)).toBe(url)
  })
})

describe('servedStories (CA-11-6)', () => {
  it('lists each story with the criteria the screen shows', () => {
    const [login, guias] = screenMap(header).screens
    if (!login || !guias) throw new Error('screens not read')
    expect(servedStories(login)).toEqual([{ us: 'US-01', criteria: ['CA-1', 'CA-2'] }])
    expect(servedStories(guias)).toEqual([
      { us: 'US-02', criteria: [] },
      { us: 'US-04', criteria: ['CA-1'] },
    ])
  })
})

describe('relativePoint (US-57 CA-1)', () => {
  it('keeps the point relative to the frame and inside it', () => {
    const box = { left: 100, top: 50, width: 400, height: 200 }
    expect(relativePoint(200, 100, box)).toEqual({ x: 0.25, y: 0.25 })
    expect(relativePoint(0, 1000, box)).toEqual({ x: 0, y: 1 })
    expect(relativePoint(150, 60, { left: 0, top: 0, width: 0, height: 0 })).toEqual({ x: 0, y: 0 })
  })
})
