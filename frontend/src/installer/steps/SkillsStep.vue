<script setup lang="ts">
import { mdiCheckCircle, mdiClose, mdiCloseCircle, mdiUpload } from '@mdi/js'
import { ref } from 'vue'
import { useInstallerStore } from '../installerStore'
import { useReason } from '../useReason'
import StepFooter from '../components/StepFooter.vue'

const store = useInstallerStore()
const { reasonText } = useReason()
const input = ref<HTMLInputElement | null>(null)

async function onFiles(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement
  const files = Array.from(target.files ?? [])
  target.value = ''
  await store.addSkillFiles(files)
}
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">{{ $t('installer.skills.intro') }}</p>
    <v-expansion-panels class="mb-4" variant="accordion">
      <v-expansion-panel
        :title="$t('installer.skills.native', { count: store.nativeSkills.length })"
      >
        <v-expansion-panel-text>
          <ul class="pl-5">
            <li v-for="skill in store.nativeSkills" :key="skill.name" class="text-body-2 mb-1">
              <span class="monospace">{{ skill.name }}</span> — {{ skill.description }}
            </li>
          </ul>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>

    <input
      ref="input"
      type="file"
      accept=".zip,application/zip"
      multiple
      class="sr-only"
      tabindex="-1"
      aria-hidden="true"
      data-testid="skill-input"
      @change="onFiles"
    />
    <v-btn color="primary" variant="outlined" :prepend-icon="mdiUpload" @click="input?.click()">
      {{ $t('installer.skills.upload') }}
    </v-btn>

    <p v-if="store.skills.length === 0" class="text-body-2 text-medium-emphasis mt-4">
      {{ $t('installer.skills.none') }}
    </p>
    <v-list v-else class="mt-4" density="comfortable" aria-live="polite">
      <v-list-item
        v-for="(skill, index) in store.skills"
        :key="`${skill.file_name}-${index}`"
        :title="skill.file_name"
      >
        <template #prepend>
          <v-icon
            :icon="skill.problem ? mdiCloseCircle : mdiCheckCircle"
            :color="skill.problem ? 'error' : skill.summary ? 'success' : undefined"
            aria-hidden="true"
          />
        </template>
        <v-list-item-subtitle>
          <span v-if="skill.problem" class="text-error">{{ reasonText(skill.problem) }}</span>
          <span v-else-if="skill.summary">{{
            $t('installer.skills.accepted', { name: skill.summary.name })
          }}</span>
          <span v-else>{{ $t('installer.testing') }}</span>
        </v-list-item-subtitle>
        <template #append>
          <v-btn
            :icon="mdiClose"
            variant="text"
            size="small"
            :aria-label="$t('installer.actions.remove', { name: skill.file_name })"
            @click="store.removeSkill(index)"
          />
        </template>
      </v-list-item>
    </v-list>
    <p class="text-body-2 text-medium-emphasis mt-4">{{ $t('installer.skills.scanNote') }}</p>
    <StepFooter />
  </section>
</template>
