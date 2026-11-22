<script setup lang="ts">
import { mdiBrightnessAuto, mdiWeatherNight, mdiWhiteBalanceSunny } from '@mdi/js'
import { computed } from 'vue'
import { useThemePreference } from '@/shared/composables/useThemePreference'
import type { ThemePreference } from '@/shared/preferences/preferencesStore'

const { availablePreferences, themePreference, changeThemePreference } = useThemePreference()

const iconByPreference: Record<ThemePreference, string> = {
  system: mdiBrightnessAuto,
  light: mdiWhiteBalanceSunny,
  dark: mdiWeatherNight,
}

const currentIcon = computed(() => iconByPreference[themePreference.value])
</script>

<template>
  <v-menu location="bottom end">
    <template #activator="{ props: activatorProps }">
      <v-btn
        v-bind="activatorProps"
        :icon="currentIcon"
        :aria-label="$t('theme.switcherLabel', { current: $t(`theme.names.${themePreference}`) })"
        data-testid="theme-switcher"
      />
    </template>
    <v-list :aria-label="$t('theme.menuLabel')" role="menu" density="comfortable">
      <v-list-item
        v-for="preference in availablePreferences"
        :key="preference"
        role="menuitemradio"
        :aria-checked="preference === themePreference"
        :active="preference === themePreference"
        :prepend-icon="iconByPreference[preference]"
        :title="$t(`theme.names.${preference}`)"
        @click="changeThemePreference(preference)"
      />
    </v-list>
  </v-menu>
</template>
