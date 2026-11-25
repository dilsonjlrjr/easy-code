import { describe, expect, it } from 'vitest'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import type { SearchResults } from '../api/searchApi'
import {
  isSearchShortcut,
  missingCharacters,
  moveActive,
  resultCount,
  searchSections,
  seeAllRoute,
} from '../searchResults'

const P1 = { id: 'p1', name: 'Reserva de salas', state: 'in_progress' as const }
const P2 = { id: 'p2', name: 'Portal RH', state: 'paused' as const }

function results(overrides: Partial<SearchResults> = {}): SearchResults {
  return {
    projects: { items: [P1], total: 1, project_count: 1 },
    artifacts: {
      items: [{ id: 'US-12', type: 'US', title: 'Reservar sala', state: 'approved', project: P1 }],
      total: 14,
      project_count: 2,
    },
    findings: { items: [], total: 0, project_count: 0 },
    ...overrides,
  }
}

describe('header search (US-56)', () => {
  it('CA-3: counts the characters still missing, ignoring the spaces of the ends', () => {
    expect(missingCharacters('')).toBe(2)
    expect(missingCharacters(' a ')).toBe(1)
    expect(missingCharacters('ç')).toBe(1)
    expect(missingCharacters('US')).toBe(0)
  })

  it('CA-2, RN-3: groups in order, skips empty ones and ends each with "see all"', () => {
    const sections = searchSections(results(), 'hs')
    expect(sections.map((section) => section.kind)).toEqual(['projects', 'artifacts'])
    expect(sections[0]?.options.map((option) => option.to)).toEqual([
      { name: ROUTE_NAMES.project, params: { projectId: 'p1' } },
      { name: ROUTE_NAMES.projects },
    ])
    expect(sections[1]?.options[0]).toMatchObject({
      id: 'hs-artifacts-0',
      to: { name: ROUTE_NAMES.artifact, params: { projectId: 'p1', artifactId: 'US-12' } },
    })
    expect(sections[1]?.options[1]).toMatchObject({ seeAll: true, id: 'hs-artifacts-all' })
    expect(resultCount(results())).toBe(15)
  })

  it('RN-3: "see all" opens the list of the one project, or the project list for several', () => {
    const finding = {
      id: 'ACH-07',
      title: 'Falha',
      status: 'open' as const,
      severity: 'high' as const,
    }
    expect(
      seeAllRoute('findings', { items: [{ ...finding, project: P2 }], total: 1, project_count: 1 }),
    ).toEqual({ name: ROUTE_NAMES.projectFindings, params: { projectId: 'p2' } })
    expect(
      seeAllRoute('findings', { items: [{ ...finding, project: P2 }], total: 3, project_count: 2 }),
    ).toEqual({ name: ROUTE_NAMES.projects })
    expect(
      searchSections(results({ projects: { items: [], total: 0, project_count: 0 } }), 'x')[0]
        ?.options[0]?.to,
    ).toEqual({
      name: ROUTE_NAMES.artifact,
      params: { projectId: 'p1', artifactId: 'US-12' },
    })
  })

  it('CA-5: the arrows wrap around the options', () => {
    expect(moveActive(-1, 3, 1)).toBe(0)
    expect(moveActive(-1, 3, -1)).toBe(2)
    expect(moveActive(2, 3, 1)).toBe(0)
    expect(moveActive(0, 3, -1)).toBe(2)
    expect(moveActive(0, 0, 1)).toBe(-1)
  })

  it('CA-5: "/" focuses the field, except while typing in another field', () => {
    const key = (target: Partial<HTMLElement>, extra: Partial<KeyboardEvent> = {}) =>
      ({
        key: '/',
        ctrlKey: false,
        metaKey: false,
        altKey: false,
        target,
        ...extra,
      }) as KeyboardEvent
    expect(isSearchShortcut(key({ tagName: 'BODY', isContentEditable: false }))).toBe(true)
    expect(isSearchShortcut(key({ tagName: 'TEXTAREA', isContentEditable: false }))).toBe(false)
    expect(isSearchShortcut(key({ tagName: 'DIV', isContentEditable: true }))).toBe(false)
    expect(isSearchShortcut(key({ tagName: 'BODY' }, { ctrlKey: true }))).toBe(false)
  })
})
