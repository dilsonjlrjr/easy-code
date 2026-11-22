import { watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

/** Sets `document.title` from the route's `titleKey`, in the current language (WCAG 2.4.2). */
export function useDocumentTitleSync(): void {
  const route = useRoute()
  const { t } = useI18n()

  watchEffect(() => {
    const titleKey = route.meta.titleKey
    document.title = titleKey ? t('app.documentTitle', { page: t(titleKey) }) : t('app.name')
  })
}
