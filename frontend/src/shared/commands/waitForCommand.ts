import type { Command } from '@/shared/api/contract'

export interface WaitForCommandOptions {
  intervalMs?: number
  timeoutMs?: number
  sleep?: (milliseconds: number) => Promise<void>
}

const INTERVAL_MS = 500
const TIMEOUT_MS = 30_000

const defaultSleep = (milliseconds: number) =>
  new Promise<void>((resolve) => {
    setTimeout(resolve, milliseconds)
  })

/**
 * Follows an asynchronous command (202) until it is `applied` or `refused` (ESP-00 ER-00-10). The
 * screen shows "pending" meanwhile; after the timeout the last known state is returned (still pending).
 */
export async function waitForCommand(
  command: Command,
  read: (commandId: string) => Promise<Command>,
  options: WaitForCommandOptions = {},
): Promise<Command> {
  const intervalMs = options.intervalMs ?? INTERVAL_MS
  const timeoutMs = options.timeoutMs ?? TIMEOUT_MS
  const sleep = options.sleep ?? defaultSleep
  let current = command
  let waited = 0
  while (current.state === 'pending' && waited < timeoutMs) {
    await sleep(intervalMs)
    waited += intervalMs
    current = await read(current.id)
  }
  return current
}
