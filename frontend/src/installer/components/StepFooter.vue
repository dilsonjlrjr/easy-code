<script setup lang="ts">
import { mdiArrowLeft, mdiArrowRight, mdiCheckCircle, mdiFlaskOutline } from '@mdi/js'
import { computed } from 'vue'
import { useInstallerStore } from '../installerStore'
import { useReason } from '../useReason'

/** Test, back and continue of a step, with the result announced (ESP-31 section 8). */
const props = defineProps<{
  /** The test of the step; omitted when the step has none. */
  test?: () => Promise<boolean>
  /** Offers "Skip" (optional steps). */
  skippable?: boolean
  nextLabel?: string
}>()
const emit = defineEmits<{ skip: []; next: [] }>()

const store = useInstallerStore()
const { errorText } = useReason()
const passed = computed(() => store.passed(store.current))
const stale = computed(() => store.stale(store.current))

function goNext(): void {
  emit('next')
  store.next()
}
</script>

<template>
  <div class="mt-6">
    <div role="status" aria-live="polite" class="mb-3">
      <p v-if="store.busy" class="text-body-2">{{ $t('installer.testing') }}</p>
      <p v-else-if="props.test && passed" class="text-body-2 text-success d-flex align-center ga-2">
        <v-icon :icon="mdiCheckCircle" size="small" />
        {{ $t('installer.testPassed') }}
      </p>
      <p v-else-if="stale" class="text-body-2 text-warning">{{ $t('installer.testAgain') }}</p>
    </div>
    <v-alert
      v-if="store.generalProblem"
      type="error"
      variant="tonal"
      role="alert"
      class="mb-4"
      :text="errorText(store.generalProblem)"
    />
    <div class="d-flex flex-wrap ga-3">
      <v-btn
        v-if="store.stepIndex > 0"
        variant="text"
        :prepend-icon="mdiArrowLeft"
        :disabled="store.busy"
        @click="store.back()"
      >
        {{ $t('installer.actions.back') }}
      </v-btn>
      <v-spacer />
      <v-btn v-if="skippable" variant="text" :disabled="store.busy" @click="emit('skip')">
        {{ $t('installer.actions.skip') }}
      </v-btn>
      <v-btn
        v-if="test"
        variant="outlined"
        color="primary"
        :prepend-icon="mdiFlaskOutline"
        :loading="store.busy"
        @click="test()"
      >
        {{ $t('installer.actions.test') }}
      </v-btn>
      <v-btn
        color="primary"
        variant="flat"
        :append-icon="mdiArrowRight"
        :disabled="!passed || store.busy"
        data-testid="step-next"
        @click="goNext"
      >
        {{ nextLabel ?? $t('installer.actions.next') }}
      </v-btn>
    </div>
  </div>
</template>
