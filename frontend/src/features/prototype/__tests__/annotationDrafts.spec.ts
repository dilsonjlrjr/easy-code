import { describe, expect, it } from 'vitest'
import { MAX_ANNOTATIONS, MAX_COMMENT, useAnnotationDrafts } from '../composables/annotationDrafts'

const pin = {
  screen: 'TL-1',
  state: 'erro',
  width: 'wide' as const,
  x: 0.4,
  y: 0.2,
  comment: ' Valor maior ',
}

describe('useAnnotationDrafts (US-57 CA-4)', () => {
  it('keeps the drafts per project and TELAS version until cleared', () => {
    let version = 1
    const drafts = useAnnotationDrafts(
      () => 'p1',
      () => version,
    )
    const first = drafts.add(pin)
    drafts.add({ ...pin, screen: 'TL-2', x: null, y: null, comment: 'Sem ponto' })
    expect(drafts.list()).toHaveLength(2)
    version = 2
    expect(drafts.list()).toEqual([])
    version = 1
    if (first) drafts.update(first.id, 'Valor bem maior')
    expect(drafts.list()[0]?.comment).toBe('Valor bem maior')
    expect(
      useAnnotationDrafts(
        () => 'p2',
        () => 1,
      ).list(),
    ).toEqual([])
    drafts.clear()
    expect(drafts.list()).toEqual([])
  })

  it('sends only the drafts with a comment, trimmed and without the local id', () => {
    const drafts = useAnnotationDrafts(
      () => 'p3',
      () => 1,
    )
    drafts.add(pin)
    const empty = drafts.add({ ...pin, comment: '   ' })
    expect(drafts.payload()).toEqual([
      { screen: 'TL-1', state: 'erro', width: 'wide', x: 0.4, y: 0.2, comment: 'Valor maior' },
    ])
    if (empty) drafts.remove(empty.id)
    expect(drafts.list()).toHaveLength(1)
  })

  it('bounds the drafts and the comment like the api', () => {
    const drafts = useAnnotationDrafts(
      () => 'p4',
      () => 1,
    )
    for (let i = 0; i < MAX_ANNOTATIONS + 2; i++) drafts.add(pin)
    expect(drafts.list()).toHaveLength(MAX_ANNOTATIONS)
    const other = useAnnotationDrafts(
      () => 'p5',
      () => 1,
    )
    other.add({ ...pin, comment: 'a'.repeat(MAX_COMMENT + 10) })
    expect(other.list()[0]?.comment).toHaveLength(MAX_COMMENT)
    expect(
      useAnnotationDrafts(
        () => 'p6',
        () => undefined,
      ).add(pin),
    ).toBeUndefined()
  })
})
