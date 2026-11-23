import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import ptBR from '@/i18n/locales/pt-BR'

function keys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) return [prefix]
  return Object.entries(value).flatMap(([key, child]) =>
    keys(child, prefix ? `${prefix}.${key}` : key),
  )
}

function leaves(value: unknown): unknown[] {
  if (typeof value !== 'object' || value === null) return [value]
  return Object.values(value).flatMap(leaves)
}

describe('message catalogs (RNF-21)', () => {
  it('have the same keys in pt-BR and en', () => {
    expect(keys(en).sort()).toEqual(keys(ptBR).sort())
  })

  it('have no empty message', () => {
    for (const leaf of [...leaves(ptBR), ...leaves(en)]) {
      expect(typeof leaf === 'string' && leaf.trim().length > 0).toBe(true)
    }
  })
})
