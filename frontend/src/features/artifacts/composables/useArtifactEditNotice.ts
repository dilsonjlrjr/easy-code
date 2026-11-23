import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { artifactsApi, type ArtifactEditStatus } from '../api/artifactsApi'
import { followArtifactEdit } from './artifactEditing'

/**
 * US-53 after the dialog: the edit waits for the stage agent's `edit_review` wave (RN-1). The page
 * says so, links the wave, follows the edit until applied (the new version) or refused (the agent
 * did not validate it: the previous version still counts, CA-2), then reloads its data.
 */
export function useArtifactEditNotice(projectId: () => string, reload: () => Promise<void>) {
  const { t } = useI18n()
  const { message, announce } = useAnnouncer()
  const edit = ref<ArtifactEditStatus>()
  const notice = ref<string>()

  function describe(current: ArtifactEditStatus): string {
    if (current.state === 'applied')
      return t('artifactEdit.applied', { version: current.version ?? '' })
    if (current.state === 'refused') return t('artifactEdit.notValidated')
    return t('artifactEdit.sent')
  }

  async function track(first: ArtifactEditStatus): Promise<void> {
    edit.value = first
    notice.value = describe(first)
    announce(notice.value)
    await reload()
    const settled = await followArtifactEdit(
      first,
      () => artifactsApi.getArtifactEdit(projectId(), first.id),
      (state) => state === 'pending' || state === 'reviewing',
      { intervalMs: 2000, timeoutMs: 120_000 },
    ).catch(() => first)
    if (settled.state === first.state) return
    edit.value = settled
    notice.value = describe(settled)
    announce(notice.value)
    await reload()
  }

  function dismiss(): void {
    notice.value = undefined
  }

  return { edit, notice, message, track, dismiss }
}
