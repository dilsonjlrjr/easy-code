import { describe, expect, it } from 'vitest'
import { artifactFile, toYaml } from '../composables/artifactFile'

describe('artifactFile (US-53 edits from structured fields)', () => {
  it('writes block YAML with quoted strings, lists of objects and empty values', () => {
    const yaml = toYaml({
      id: 'US-01',
      versao: 2,
      tem_interface: true,
      criterios_aceite: [{ id: 'CA-1', dado: 'a: b "c"', quando: 'x', entao: 'y' }],
      vinculos: [],
      origem: { tipo: 'cliente', detalhe: 'ata.md' },
      rnf: ['RNF-01', 'RNF-02'],
    })
    expect(yaml).toBe(
      [
        'id: "US-01"',
        'versao: 2',
        'tem_interface: true',
        'criterios_aceite:',
        '  - id: "CA-1"',
        '    dado: "a: b \\"c\\""',
        '    quando: "x"',
        '    entao: "y"',
        'vinculos: []',
        'origem:',
        '  tipo: "cliente"',
        '  detalhe: "ata.md"',
        'rnf:',
        '  - "RNF-01"',
        '  - "RNF-02"',
        '',
      ].join('\n'),
    )
  })

  it('keeps the body after the front matter', () => {
    expect(artifactFile({ tipo: 'US' }, '# Título\n')).toBe('---\ntipo: "US"\n---\n# Título\n')
    expect(artifactFile({ tipo: 'US' }, '\n# Título')).toBe('---\ntipo: "US"\n---\n# Título')
  })
})
