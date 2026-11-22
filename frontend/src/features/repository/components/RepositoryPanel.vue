<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { isApiError } from '@/shared/api/problemDetails'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { repositoryApi, type Repository, type RepositoryApi } from '../api/repositoryApi'

/**
 * US-42, ESP-14 "Repositório", ESP-06 (endereço de clone), PT-37: the platform's clone address
 * (copyable), how to authenticate the clone, the main branch, the last commit and the version
 * marks. The api answers 404 to anyone without a role (RNF-04); the project page itself is then
 * already "not found", so a 404 here only means the repository does not exist yet.
 */
const props = withDefaults(
  defineProps<{ projectId: string; headingLevel?: 2 | 3; api?: RepositoryApi }>(),
  { headingLevel: 2, api: () => repositoryApi },
)

const { t } = useI18n()
const format = useFormat()
const { message, announce } = useAnnouncer()
const repository = ref<Repository>()
const loader = useLoader(async () => {
  repository.value = await props.api.getRepository(props.projectId)
})
const unavailable = computed(
  () => isApiError(loader.error.value) && loader.error.value.status === 404,
)
const headingTag = computed(() => `h${String(props.headingLevel)}`)

onMounted(() => void loader.run())

async function copyCloneUrl(): Promise<void> {
  if (!repository.value) return
  try {
    await navigator.clipboard.writeText(repository.value.clone_url)
    announce(t('repository.copied'))
  } catch {
    announce(t('repository.copyFailed'))
  }
}

function shortSha(sha: string | undefined): string {
  return sha ? sha.slice(0, 12) : ''
}
</script>

<template>
  <section aria-labelledby="repository-heading" class="mb-6" data-testid="repository-panel">
    <component :is="headingTag" id="repository-heading" class="text-h6 mb-2">
      {{ $t('repository.title') }}
    </component>
    <LoadingState v-if="loader.loading.value && !repository" />
    <p v-else-if="unavailable" class="text-body-2" data-testid="repository-unavailable">
      {{ $t('repository.unavailable') }}
    </p>
    <ProblemAlert
      v-else-if="loader.error.value && !repository"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else-if="repository">
      <div class="d-flex flex-wrap align-center ga-2">
        <v-text-field
          :model-value="repository.clone_url"
          :label="$t('repository.cloneUrl')"
          readonly
          density="compact"
          hide-details
          class="clone-url monospace"
          data-testid="clone-url"
        />
        <v-btn variant="outlined" data-testid="copy-clone-url" @click="copyCloneUrl">
          {{ $t('repository.copy') }}
        </v-btn>
      </div>
      <div role="status" class="sr-only">{{ message }}</div>
      <p class="text-body-2 mt-2">{{ $t('repository.credential') }}</p>
      <dl class="summary-list mt-2">
        <dt>{{ $t('repository.mainBranch') }}</dt>
        <dd class="monospace">{{ repository.main_branch }}</dd>
        <dt>{{ $t('repository.lastCommit') }}</dt>
        <dd data-testid="repository-last-commit">
          <template v-if="repository.last_commit?.sha">
            <span class="monospace" :title="repository.last_commit.sha">{{
              shortSha(repository.last_commit.sha)
            }}</span>
            <template v-if="repository.last_commit.when">
              ·
              {{
                $t('repository.lastCommitWhen', {
                  when: format.dateTime(repository.last_commit.when),
                })
              }}
            </template>
            <template v-if="repository.last_commit.wave">
              ·
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId, waveId: repository.last_commit.wave },
                }"
              >
                {{ $t('repository.lastCommitWave', { wave: repository.last_commit.wave }) }}
              </router-link>
            </template>
          </template>
          <template v-else>{{ $t('repository.noCommit') }}</template>
        </dd>
        <dt>{{ $t('repository.marks') }}</dt>
        <dd>
          <ul v-if="repository.marks?.length" class="plain-list">
            <li v-for="mark in repository.marks" :key="mark.name">
              <span class="monospace">{{ mark.name }}</span> ·
              <span class="monospace">{{ shortSha(mark.commit) }}</span>
              <template v-if="mark.when"> · {{ format.dateTime(mark.when) }}</template>
            </li>
          </ul>
          <template v-else>{{ $t('repository.noMarks') }}</template>
        </dd>
      </dl>
    </template>
  </section>
</template>

<style scoped>
.clone-url {
  flex: 1 1 22rem;
  min-width: 0;
}
</style>
