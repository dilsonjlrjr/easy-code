import type { Component } from 'vue'
import type { Stage } from '@/features/projects/api/projectsApi'

/** Props every stage-specific view receives from StageDetail. */
export interface StageViewProps {
  projectId: string
  stage: Stage
  /** Bumps when a live event changes the project, so the view reloads what it shows. */
  revision: number
}

/**
 * Stage-specific content of the Etapa artboards (ESP-07 to ESP-16), by stage id. StageDetail renders the
 * view of the current stage, when there is one, between its header and the common waves, artifacts and
 * findings cards. Each view is lazy, so a stage page only loads its own code.
 */
export const STAGE_VIEWS: Partial<Record<string, () => Promise<Component>>> = {}
