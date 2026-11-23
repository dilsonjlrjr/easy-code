import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/shared/api/problemDetails'
import { aCommand, aProject } from '@/test/factories'
import type { ProjectsApi } from '../api/projectsApi'
import { NewProjectFlow, type NewProjectDraft } from '../stores/newProjectFlow'

function fakeApi(overrides: Partial<ProjectsApi> = {}): ProjectsApi {
  return {
    listProjects: vi.fn(),
    createProject: vi.fn().mockResolvedValue(aProject()),
    getProject: vi.fn(),
    attachFile: vi.fn().mockResolvedValue({}),
    getConfiguration: vi.fn().mockResolvedValue({ configuration: {}, etag: '"v1"' }),
    modifyConfiguration: vi.fn().mockResolvedValue({ configuration: {}, etag: '"v2"' }),
    startWorkflow: vi.fn().mockResolvedValue(aCommand()),
    getCommand: vi.fn().mockResolvedValue(aCommand({ state: 'applied' })),
    getWorkflow: vi.fn(),
    getStage: vi.fn(),
    listWaves: vi.fn(),
    pauseProject: vi.fn(),
    resumeProject: vi.fn(),
    cancelProject: vi.fn(),
    completeProject: vi.fn(),
    getCosts: vi.fn(),
    deleteProject: vi.fn(),
    ...overrides,
  }
}

const pair = {
  reasoning: { provider: 'anthropic' as const, model: 'reasoning-model' },
  execution: { provider: 'anthropic' as const, model: 'execution-model' },
}

function draft(overrides: Partial<NewProjectDraft> = {}): NewProjectDraft {
  return {
    name: '  Reserva de salas ',
    businessNeed: 'Reservar salas sem conflito.',
    attachments: [new File(['ata'], 'ata.txt', { type: 'text/plain' })],
    modelPair: pair,
    artifactLanguage: 'pt-BR',
    ...overrides,
  }
}

const noWait = { sleep: () => Promise.resolve() }

describe('NewProjectFlow (ESP-05 ER-05-8)', () => {
  it('US-03 CA-1/CA-3, US-05 CA-1: creates with the acknowledgment, attaches, configures and starts', async () => {
    const api = fakeApi()
    const steps: string[] = []
    const flow = new NewProjectFlow(api, (step) => steps.push(step), noWait)

    const outcome = await flow.run(draft())

    expect(outcome.kind).toBe('started')
    expect(steps).toEqual(['create', 'attachments', 'configuration', 'start'])
    expect(api.createProject).toHaveBeenCalledWith(
      {
        name: 'Reserva de salas',
        business_need: 'Reservar salas sem conflito.',
        provider_submission_acknowledgment: true,
      },
      expect.any(String),
    )
    expect(api.attachFile).toHaveBeenCalledOnce()
    expect(api.modifyConfiguration).toHaveBeenCalledWith(
      aProject().id,
      { model_pair: pair, artifact_language: 'pt-BR' },
      '"v1"',
    )
    expect(api.startWorkflow).toHaveBeenCalledOnce()
  })

  it('CA-05-12: a retry of the same intention reuses the Idempotency-Key, so only one project exists', async () => {
    const api = fakeApi({
      createProject: vi
        .fn()
        .mockRejectedValueOnce(new TypeError('network'))
        .mockResolvedValue(aProject()),
    })
    const flow = new NewProjectFlow(api, undefined, noWait)

    expect((await flow.run(draft())).kind).toBe('not-created')
    await flow.run(draft())

    const keys = vi.mocked(api.createProject).mock.calls.map(([, key]) => key)
    expect(keys[0]).toBe(keys[1])
  })

  it('CA-05-12: a double click while running does not send a second creation', async () => {
    const api = fakeApi()
    const flow = new NewProjectFlow(api, undefined, noWait)
    await Promise.all([flow.run(draft()), flow.run(draft())])
    expect(api.createProject).toHaveBeenCalledOnce()
  })

  it('CA-05-10 / ER-05-9: a refused start keeps the project created and returns the reason', async () => {
    const refusal = {
      type: 'urn:easycode:problem:incomplete-configuration',
      title: 'Configuração incompleta',
      status: 409,
      missing: ['provider_key'],
    }
    const api = fakeApi({
      getCommand: vi.fn().mockResolvedValue(aCommand({ state: 'refused', refusal })),
    })
    const outcome = await new NewProjectFlow(api, undefined, noWait).run(draft())

    expect(outcome.kind).toBe('partial')
    if (outcome.kind !== 'partial') return
    expect(outcome.step).toBe('start')
    expect(outcome.error).toBeInstanceOf(ApiError)
    expect((outcome.error as ApiError).problem?.missing).toEqual(['provider_key'])
  })

  it('CA-05-5: a duplicate name creates nothing and lets the wizard point at the name', async () => {
    const duplicate = new ApiError(409, { type: 'urn:easycode:problem:duplicate-name' })
    const api = fakeApi({ createProject: vi.fn().mockRejectedValue(duplicate) })
    const outcome = await new NewProjectFlow(api, undefined, noWait).run(draft())
    expect(outcome).toEqual({ kind: 'not-created', error: duplicate })
    expect(api.attachFile).not.toHaveBeenCalled()
  })
})
