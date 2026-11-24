<script setup lang="ts">
import { mdiEye, mdiEyeOff } from '@mdi/js'
import { ref } from 'vue'

defineProps<{
  label: string
  hint?: string
  errorMessages?: string[]
  autocomplete?: string
}>()
const model = defineModel<string>({ required: true })
const visible = ref(false)
</script>

<template>
  <v-text-field
    v-model="model"
    :label="label"
    :hint="hint"
    :persistent-hint="Boolean(hint)"
    :error-messages="errorMessages"
    :type="visible ? 'text' : 'password'"
    :autocomplete="autocomplete ?? 'off'"
    spellcheck="false"
  >
    <template #append-inner>
      <v-btn
        :icon="visible ? mdiEyeOff : mdiEye"
        :aria-label="
          visible ? $t('installer.actions.hidePassword') : $t('installer.actions.showPassword')
        "
        :aria-pressed="visible"
        variant="text"
        density="comfortable"
        size="small"
        @click="visible = !visible"
      />
    </template>
  </v-text-field>
</template>
