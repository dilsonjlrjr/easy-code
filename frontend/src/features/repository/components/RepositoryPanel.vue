<script setup lang="ts">
import { computed, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useAnnouncer } from '@/shared/composables/useAnnouncer'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { repositoryApi, type Repository, type RepositoryApi } from '../api/repositoryApi'

/**
 * US-42, ESP-14 "Repositório", ESP-06 (endereço de clone), PT-37: the platform's clone address
 * (copyable), how to authenticate the clone, the main branch, the last commit and the version
 * marks. `status: not_created` (no wave concluded yet) reads "not available yet"; a 404 means no
 * role in the project (RNF-04, US-42 CA-3) and shows as not found, like the rest of the project.
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
const unavailable = computed(() => repository.value?.status === 'not_created')
const headingTag = computed(() => `h${String(props.headingLevel)}`)
/** On the project page it is a canvas card of its own; inside a stage it sits in the stage's layout. */
const standalone = computed(() => props.headingLevel === 2)
const cloneUrlId = useId()

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
  <!-- Layout: canvas primitives of Projeto.dc.html (side card, `.fld`, `.btn`, `.kv`); no own artboard. -->
  <section
    aria-labelledby="repository-heading"
    class="repository"
    :class="{ card: standalone, 'repository-card': standalone }"
    data-testid="repository-panel"
  >
    <component :is="headingTag" id="repository-heading" class="repository-heading">
      {{ $t('repository.title') }}
    </component>
    <LoadingState v-if="loader.loading.value && !repository" />
    <ProblemAlert
      v-else-if="loader.error.value && !repository"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="unavailable" class="t2 repository-text" data-testid="repository-unavailable">
      {{ $t('repository.unavailable') }}
    </p>
    <template v-else-if="repository">
      <div class="clone-row">
        <label :for="cloneUrlId" class="clone-field">
          <span :id="`${cloneUrlId}-label`" class="lb">{{ $t('repository.cloneUrl') }}</span>
          <input
            :id="cloneUrlId"
            :value="repository.clone_url"
            :aria-labelledby="`${cloneUrlId}-label`"
            class="fld mono"
            type="text"
            readonly
            data-testid="clone-url"
          />
        </label>
        <button type="button" class="btn" data-testid="copy-clone-url" @click="copyCloneUrl">
          {{ $t('repository.copy') }}
        </button>
      </div>
      <div role="status" class="sr-only">{{ message }}</div>
      <p class="t2 repository-text">{{ $t('repository.credential') }}</p>
      <div class="repository-rows">
        <div class="kv">
          <span class="t2">{{ $t('repository.mainBranch') }}</span>
          <span class="kv-value mono">{{ repository.main_branch }}</span>
        </div>
        <div class="kv">
          <span class="t2">{{ $t('repository.lastCommit') }}</span>
          <span class="kv-value" data-testid="repository-last-commit">
            <template v-if="repository.last_commit?.sha">
              <span class="mono" :title="repository.last_commit.sha">{{
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
          </span>
        </div>
        <div class="kv">
          <span class="t2">{{ $t('repository.marks') }}</span>
          <span class="kv-value">
            <ul v-if="repository.marks?.length" class="marks">
              <li v-for="mark in repository.marks" :key="mark.name">
                <span class="mono">{{ mark.name }}</span> ·
                <span class="mono">{{ shortSha(mark.commit) }}</span>
                <template v-if="mark.when"> · {{ format.dateTime(mark.when) }}</template>
              </li>
            </ul>
            <template v-else>{{ $t('repository.noMarks') }}</template>
          </span>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.repository {
  display: flex;
  flex-direction: column;
}

.repository-card {
  padding: 18px 22px;
}

.repository-heading {
  margin: 0 0 10px;
  font-size: 18px;
  line-height: 24px;
  font-weight: 700;
}

.clone-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-end;
}

.clone-field {
  flex: 1 1 220px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.repository-text {
  margin: 10px 0 0;
  font-size: 13px;
}

.repository-rows {
  margin-top: 10px;
}

.kv-value {
  font-weight: 600;
  text-align: right;
  min-width: 0;
  overflow-wrap: anywhere;
}

.marks {
  list-style: none;
  margin: 0;
  padding: 0;
}
</style>
