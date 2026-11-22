import type { ModelPair } from '@/shared/api/contract'
import { newIdempotencyKey } from '@/shared/api/idempotencyKey'
import { ApiError } from '@/shared/api/problemDetails'
import { waitForCommand, type WaitForCommandOptions } from '@/shared/commands/waitForCommand'
import {
  constraintsPayload,
  type ConstraintDraft,
  type Project,
  type ProjectsApi,
} from '../api/projectsApi'

export interface NewProjectDraft {
  name: string
  businessNeed: string
  attachments: File[]
  modelPair: ModelPair | null
  artifactLanguage: string
  /** Optional technical constraints, saved as version 1 with the project (US-54 CA-1). */
  constraints?: ConstraintDraft[]
}

export type FlowStep = 'create' | 'attachments' | 'configuration' | 'start'

export type FlowOutcome =
  | { kind: 'started'; project: Project }
  /** The project exists but a later step failed: it stays `created` (ESP-05 ER-05-9). */
  | { kind: 'partial'; project: Project; step: FlowStep; error: unknown }
  /** Nothing was created; the wizard keeps every field (CA-05-5). */
  | { kind: 'not-created'; error: unknown }

/**
 * "Criar e iniciar o workflow" (ESP-05 ER-05-8): createProject, one attachFile per file,
 * modifyProjectConfiguration with the model pair and the artifact language, then startWorkflow
 * followed until the command is applied or refused. Every write carries an Idempotency-Key that
 * belongs to this flow, so a second click or a retry never creates a second project (CA-05-12).
 */
export class NewProjectFlow {
  private readonly createKey = newIdempotencyKey()
  private readonly attachmentKeys = new Map<File, string>()
  private readonly startKey = newIdempotencyKey()
  private running: Promise<FlowOutcome> | undefined

  constructor(
    private readonly api: ProjectsApi,
    private readonly onStep: (step: FlowStep) => void = () => undefined,
    private readonly commandOptions: WaitForCommandOptions = {},
  ) {}

  run(draft: NewProjectDraft): Promise<FlowOutcome> {
    this.running ??= this.execute(draft).finally(() => {
      this.running = undefined
    })
    return this.running
  }

  private attachmentKey(file: File): string {
    let key = this.attachmentKeys.get(file)
    if (!key) {
      key = newIdempotencyKey()
      this.attachmentKeys.set(file, key)
    }
    return key
  }

  private async execute(draft: NewProjectDraft): Promise<FlowOutcome> {
    let project: Project
    this.onStep('create')
    try {
      project = await this.api.createProject(
        {
          name: draft.name.trim(),
          business_need: draft.businessNeed,
          provider_submission_acknowledgment: true,
          ...(draft.constraints?.length
            ? { constraints: constraintsPayload(draft.constraints) }
            : {}),
        },
        this.createKey,
      )
    } catch (error) {
      return { kind: 'not-created', error }
    }

    let step: FlowStep = 'attachments'
    try {
      this.onStep(step)
      for (const file of draft.attachments) {
        await this.api.attachFile(project.id, file, this.attachmentKey(file))
      }

      step = 'configuration'
      this.onStep(step)
      const { etag } = await this.api.getConfiguration(project.id)
      await this.api.modifyConfiguration(
        project.id,
        {
          ...(draft.modelPair ? { model_pair: draft.modelPair } : {}),
          artifact_language: draft.artifactLanguage,
        },
        etag,
      )

      step = 'start'
      this.onStep(step)
      const accepted = await this.api.startWorkflow(project.id, this.startKey)
      const finished = await waitForCommand(
        accepted,
        (commandId) => this.api.getCommand(project.id, commandId),
        this.commandOptions,
      )
      if (finished.state === 'refused') {
        return {
          kind: 'partial',
          project,
          step,
          error: new ApiError(finished.refusal?.status ?? 409, finished.refusal),
        }
      }
      return { kind: 'started', project }
    } catch (error) {
      return { kind: 'partial', project, step, error }
    }
  }
}
