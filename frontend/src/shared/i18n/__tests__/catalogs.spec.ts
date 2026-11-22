import { describe, expect, it } from 'vitest'
import { en } from '../locales/en'
import { ptBR } from '../locales/pt-BR'

type Catalog = { [key: string]: string | Catalog }

function flattenEntries(catalog: Catalog, prefix = ''): Array<[string, string]> {
  return Object.entries(catalog).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return typeof value === 'string'
      ? [[path, value] as [string, string]]
      : flattenEntries(value, path)
  })
}

describe('message catalogs', () => {
  const ptBREntries = flattenEntries(ptBR)
  const enEntries = flattenEntries(en)

  it('have exactly the same keys in pt-BR and en', () => {
    expect(enEntries.map(([key]) => key).sort()).toEqual(ptBREntries.map(([key]) => key).sort())
  })

  it('have no empty messages', () => {
    const emptyKeys = [...ptBREntries, ...enEntries].filter(([, text]) => text.trim() === '')
    expect(emptyKeys).toEqual([])
  })
})
