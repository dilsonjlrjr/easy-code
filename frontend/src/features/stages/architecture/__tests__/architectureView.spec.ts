import { describe, expect, it } from 'vitest'
import { apiOperations, c4Model, chosenOption, openApiAttachment } from '../architectureView'

describe('architectureView (ESP-08)', () => {
  it('marks the option the decision cites, the longest name when names overlap', () => {
    const header = {
      opcoes: [
        { nome: 'Go', pros: ['rápido'], contras: [] },
        { nome: 'Go com fasthttp', pros: [], contras: [] },
        { nome: 'Java', pros: [], contras: [] },
      ],
      decisao: 'Go com fasthttp, porque o time conhece.',
    }
    expect(chosenOption(header)).toBe(1)
    expect(chosenOption({ ...header, decisao: 'Nenhuma.' })).toBe(-1)
    expect(chosenOption({ natureza: 'imposta', opcoes: [{ nome: 'SigNoz' }], decisao: 'x' })).toBe(
      0,
    )
  })

  it('reads the operations of the OpenAPI paths', () => {
    const source = [
      'openapi: 3.1.0',
      'info:',
      '  title: Reserva',
      'paths:',
      '  /rooms:',
      '    get:',
      '      summary: Lista as salas',
      '      responses: {}',
      '    post:',
      "      summary: 'Cria sala'",
      "  '/rooms/{id}':",
      '    parameters: []',
      '    delete: { summary: Remove sala }',
      'components:',
      '  schemas: {}',
    ].join('\n')
    expect(apiOperations(source)).toEqual([
      { method: 'GET', path: '/rooms', summary: 'Lista as salas' },
      { method: 'POST', path: '/rooms', summary: 'Cria sala' },
      { method: 'DELETE', path: '/rooms/{id}', summary: 'Remove sala' },
    ])
  })

  it('turns the Mermaid C4 into elements and relations, external systems by text', () => {
    const model = c4Model(
      [
        'C4Container',
        'Person(user, "Pessoa", "usa o navegador")',
        'Container(api, "API", "Go", "reservas")',
        'System_Ext(sso, "Keycloak", "SSO")',
        'Rel(user, api, "Usa", "HTTPS")',
      ].join('\n'),
    )
    expect(model.elements.map((e) => [e.id, e.label, e.external])).toEqual([
      ['user', 'Pessoa', false],
      ['api', 'API', false],
      ['sso', 'Keycloak', true],
    ])
    expect(model.relations).toEqual([{ from: 'user', to: 'api', label: 'Usa · HTTPS' }])
  })

  it('finds the OpenAPI attachment of the ARQ version', () => {
    expect(openApiAttachment([{ path: 'estrutura.md' }, { path: 'api/openapi.yaml' }])).toBe(
      'api/openapi.yaml',
    )
    expect(openApiAttachment([])).toBeUndefined()
  })
})
