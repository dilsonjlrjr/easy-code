import { reactive } from 'vue'
import type { PrototypeAnnotation } from '../api/prototypeApi'

/** A draft pin while the approver works (US-57 CA-4): only in this tab until the rejection is sent. */
export interface DraftAnnotation extends PrototypeAnnotation {
  id: number
}

export const MAX_ANNOTATIONS = 50
export const MAX_COMMENT = 2000

/**
 * The drafts of each TELAS version of a project, kept in memory only: nothing is written to the artifact
 * or the api before the rejection (CA-4), and they survive moving between the screens of the viewer.
 */
const drafts = reactive(new Map<string, DraftAnnotation[]>())
let nextId = 1

function key(projectId: string, version: number): string {
  return `${projectId}:${String(version)}`
}

export function useAnnotationDrafts(projectId: () => string, version: () => number | undefined) {
  function list(): DraftAnnotation[] {
    const v = version()
    return v === undefined ? [] : (drafts.get(key(projectId(), v)) ?? [])
  }

  function add(annotation: PrototypeAnnotation): DraftAnnotation | undefined {
    const v = version()
    if (v === undefined || list().length >= MAX_ANNOTATIONS) return undefined
    const draft = { ...annotation, comment: annotation.comment.slice(0, MAX_COMMENT), id: nextId++ }
    drafts.set(key(projectId(), v), [...list(), draft])
    return draft
  }

  function update(id: number, comment: string): void {
    const v = version()
    if (v === undefined) return
    drafts.set(
      key(projectId(), v),
      list().map((item) =>
        item.id === id ? { ...item, comment: comment.slice(0, MAX_COMMENT) } : item,
      ),
    )
  }

  function remove(id: number): void {
    const v = version()
    if (v === undefined) return
    drafts.set(
      key(projectId(), v),
      list().filter((item) => item.id !== id),
    )
  }

  function clear(): void {
    const v = version()
    if (v !== undefined) drafts.delete(key(projectId(), v))
  }

  /** What goes in the rejection: the drafts with a comment, without the local id. */
  function payload(): PrototypeAnnotation[] {
    return list()
      .filter((item) => item.comment.trim() !== '')
      .map((item) => ({
        screen: item.screen,
        state: item.state,
        width: item.width,
        x: item.x,
        y: item.y,
        comment: item.comment.trim(),
      }))
  }

  return { list, add, update, remove, clear, payload }
}
