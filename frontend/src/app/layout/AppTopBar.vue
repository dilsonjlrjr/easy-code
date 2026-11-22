<script setup lang="ts">
import { mdiMenu } from '@mdi/js'
import LanguageSwitcher from '@/shared/ui/LanguageSwitcher.vue'
import ThemeSwitcher from '@/shared/ui/ThemeSwitcher.vue'

defineProps<{ showMenuToggle: boolean; menuExpanded: boolean }>()
defineEmits<{ toggleMenu: [] }>()
</script>

<template>
  <v-app-bar color="surface" elevation="1" density="comfortable">
    <template v-if="showMenuToggle" #prepend>
      <v-app-bar-nav-icon
        :icon="mdiMenu"
        :aria-label="$t('navigation.toggleMenu')"
        :aria-expanded="menuExpanded"
        @click="$emit('toggleMenu')"
      />
    </template>
    <v-app-bar-title class="font-weight-medium">{{ $t('app.name') }}</v-app-bar-title>
    <template #append>
      <LanguageSwitcher />
      <ThemeSwitcher />
    </template>
  </v-app-bar>
</template>
