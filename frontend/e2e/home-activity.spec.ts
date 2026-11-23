import { expect, test } from '@playwright/test'
import { expectNoSeriousA11yViolations } from './support/a11y'
import { domainEvent, FakeApi } from './support/fakeApi'

const PROJECT_ID = '3f6c1e2a-8b4d-4c9e-a1f7-5d2b9e0c7a14'

test('ESP-03 ER-03-11: Atividade recente replays the last 24 h with since=0, newest first', async ({
  page,
}) => {
  const api = new FakeApi()
  api.signedIn = true
  api.addProject({
    project: {
      id: PROJECT_ID,
      name: 'Reserva de salas',
      state: 'in_progress',
      workflow: 'workflow-padrao',
      created_at: '2026-11-22T10:00:00-03:00',
      created_by: { id: 'dev', name: 'Dev' },
      my_roles: ['lead'],
      last_sequence: 2,
    },
  })
  const older = { ...domainEvent(PROJECT_ID, 1, 'stage.started', { stage: 'requirements' }) }
  older.time = '2026-11-22T09:00:00-03:00'
  const newer = domainEvent(PROJECT_ID, 2, 'gate.approved', { stage: 'requirements' })
  const frame = (id: number, event: Record<string, unknown>) =>
    `id: ${String(id)}\nevent: ${String(event.type)}\ndata: ${JSON.stringify(event)}\n\n`
  api.noticeStreams.push({ body: `retry: 60000\n\n${frame(10, older)}${frame(11, newer)}` })
  await api.install(page)
  await page.goto('/')

  const activity = page.getByTestId('home-activity')
  const items = activity.locator('li')
  await expect(items).toHaveCount(2)
  await expect(items.first()).toContainText('Gate da etapa')
  await expect(items.first()).toContainText('Reserva de salas')
  await expect(items.nth(1)).toContainText('Etapa')
  const stream = api.requests.find((request) => request.path.startsWith('/me/events'))
  expect(stream?.path).toContain('since=0')
  await expectNoSeriousA11yViolations(page)
})
