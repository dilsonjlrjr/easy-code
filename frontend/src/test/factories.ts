import type { Command } from '@/shared/api/contract'
import type { Project } from '@/features/projects/api/projectsApi'

export function aProject(overrides: Partial<Project> = {}): Project {
  return {
    id: '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14',
    name: 'Reserva de salas',
    state: 'created',
    workflow: 'workflow-padrao',
    created_at: '2026-11-22T10:00:00Z',
    created_by: { id: 'dev', name: 'Dev' },
    my_roles: ['lead', 'approver'],
    available_actions: ['start', 'cancel'],
    last_sequence: 0,
    ...overrides,
  }
}

export function aCommand(overrides: Partial<Command> = {}): Command {
  return {
    id: '0193a5c0-44aa-7e01-b2c9-7f3d1e6a9b30',
    type: 'start',
    target: { type: 'project', id: '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14' },
    state: 'pending',
    requested_by: { id: 'dev', name: 'Dev' },
    origin: 'web',
    requested_at: '2026-11-22T10:00:00Z',
    ...overrides,
  }
}
