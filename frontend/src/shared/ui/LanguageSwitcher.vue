<script setup lang="ts">
import { mdiTranslate } from '@mdi/js'
import { useInterfaceLanguage } from '@/shared/composables/useInterfaceLanguage'

const { availableLocales, currentLocale, changeLocale } = useInterfaceLanguage()
</script>

<template>
  <v-menu location="bottom end">
    <template #activator="{ props: activatorProps }">
      <v-btn
        v-bind="activatorProps"
        :icon="mdiTranslate"
        :aria-label="
          $t('language.switcherLabel', { current: $t(`language.names.${currentLocale}`) })
        "
        data-testid="language-switcher"
      />
    </template>
    <v-list :aria-label="$t('language.menuLabel')" role="menu" density="comfortable">
      <v-list-item
        v-for="locale in availableLocales"
        :key="locale"
        role="menuitemradio"
        :aria-checked="locale === currentLocale"
        :active="locale === currentLocale"
        :lang="locale"
        :title="$t(`language.names.${locale}`)"
        @click="changeLocale(locale)"
      />
    </v-list>
  </v-menu>
</template>
