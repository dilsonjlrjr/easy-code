import { useI18n } from 'vue-i18n'
import type { InstallerError } from './api'

/**
 * Turns the stable reason codes of the installer API into text. A code may carry a detail after a
 * colon ("client_conflict:easycode-api"). Skill package reasons reuse the messages of the import
 * (US-44, skillLibrary.packageReasons).
 */
export function useReason() {
  const i18n = useI18n()
  const t = (key: string, values: Record<string, unknown> = {}) => i18n.t(key, values)
  const te = (key: string) => i18n.te(key)

  function reasonText(reason: string): string {
    const [code = '', ...rest] = reason.split(':')
    const detail = rest.join(':')
    const skillKey = `skillLibrary.packageReasons.${code}`
    const key = `installer.reasons.${code}`
    if (te(key)) return t(key, { detail })
    if (te(skillKey)) return t(skillKey)
    return t('installer.reasons.unknown', { detail: reason })
  }

  function errorText(error: InstallerError): string {
    if (error.code === 'failed' && error.detail) {
      return t('installer.reasons.unknown', { detail: error.detail })
    }
    return reasonText(error.code)
  }

  /** "create_client:easycode-api" → "Criar o client easycode-api". */
  function actionText(action: string): string {
    const [code = '', name = ''] = action.split(':')
    const key = `installer.actionsInRealm.${code}`
    return te(key) ? t(key, { name }) : action
  }

  return { reasonText, errorText, actionText }
}
