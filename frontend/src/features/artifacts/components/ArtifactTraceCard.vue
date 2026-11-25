<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import {
  traceabilityApi,
  type Traceability,
  type TraceNode,
} from '@/features/traceability/api/traceabilityApi'
import { dependents, traceChain } from '../composables/artifactLinks'

/**
 * ESP-21 3 "Rastreabilidade" (US-28): the chain from requirement to test around this artifact
 * (`getArtifactTraceability`, both directions) and "Depende deste" (direction input: the artifacts that
 * cite it, by type and count). A link to a missing id shows "não existe" (CA-21-12); the full graph
 * with direction and depth stays in the traceability tab.
 */
const props = defineProps<{ projectId: string; artifactId: string; version: number }>()

const chain = ref<Traceability>()
const incoming = ref<Traceability>()

const loader = useLoader(async () => {
  const [both, input] = await Promise.all([
    traceabilityApi.links(props.projectId, props.artifactId, 2, 'both'),
    traceabilityApi.links(props.projectId, props.artifactId, 1, 'input'),
  ])
  chain.value = both
  incoming.value = input
})

watch(
  () => [props.artifactId, props.version],
  () => void loader.run(),
  { immediate: true },
)

const nodes = computed(() => traceChain(chain.value))
const groups = computed(() => dependents(incoming.value))

function isArtifact(node: TraceNode): boolean {
  return node.exists && node.type !== 'commit' && node.type !== 'test'
}
</script>

<template>
  <section
    class="card side-card"
    aria-labelledby="artifact-trace-heading"
    data-testid="artifact-trace"
  >
    <h2 id="artifact-trace-heading">{{ $t('pages.project.traceability') }}</h2>
    <p class="t2 side-text">{{ $t('artifactLinks.chainHelp') }}</p>
    <LoadingState v-if="loader.loading.value && !chain" type="paragraph" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <template v-else>
      <p v-if="nodes.length <= 1" class="t2 side-text">{{ $t('artifactLinks.noLinks') }}</p>
      <ol v-else class="tr" :aria-label="$t('artifactLinks.chain')" data-testid="artifact-chain">
        <li
          v-for="node in nodes"
          :key="node.id"
          class="n"
          :class="{ root: node.id === artifactId, missing: !node.exists }"
        >
          <router-link
            v-if="isArtifact(node) && node.id !== artifactId"
            :to="{ name: ROUTE_NAMES.artifact, params: { projectId, artifactId: node.id } }"
            class="mono"
          >
            {{ node.id }}
          </router-link>
          <span v-else class="mono" :aria-current="node.id === artifactId ? 'true' : undefined">{{
            node.id
          }}</span>
          <span v-if="!node.exists" class="chip crit">{{ $t('artifactLinks.missing') }}</span>
        </li>
      </ol>
      <span class="lb dep-label">{{ $t('artifactLinks.dependsOnThis') }}</span>
      <p v-if="groups.length === 0" class="t2 side-text">{{ $t('artifactLinks.noDependents') }}</p>
      <ul v-else class="dep-list" data-testid="artifact-dependents">
        <li v-for="group in groups" :key="group.type" class="li dep">
          <span class="chip mute mono">{{ group.type }}</span>
          <span v-if="group.count === 1 && group.nodes[0]">
            <router-link
              v-if="group.nodes[0].exists"
              :to="{
                name: ROUTE_NAMES.artifact,
                params: { projectId, artifactId: group.nodes[0].id },
              }"
              class="mono"
            >
              {{ group.nodes[0].id }}
            </router-link>
            <span v-else class="mono">{{ group.nodes[0].id }}</span>
            <template v-if="group.nodes[0].title"> · {{ group.nodes[0].title }}</template>
          </span>
          <span v-else>{{
            $t('artifactLinks.dependentCount', { n: group.count, type: group.type })
          }}</span>
        </li>
      </ul>
      <router-link
        :to="{
          name: ROUTE_NAMES.traceability,
          params: { projectId, view: 'links' },
          query: { root: artifactId },
        }"
        class="side-link"
        data-testid="artifact-traceability"
      >
        {{ $t('artifacts.traceabilityLink') }}
      </router-link>
    </template>
  </section>
</template>

<style scoped>
.side-card {
  padding: 18px 22px;
}

.side-card h2 {
  margin-bottom: 4px;
}

.side-text {
  margin: 0 0 10px;
  font-size: 13px;
}

.tr {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  list-style: none;
  margin: 0;
  padding: 0;
}

.n {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 4px 10px;
  border: 1px solid rgba(140, 150, 255, 0.28);
  border-radius: 8px;
  font-size: 13px;
}

.n + .n::before {
  content: '→';
  margin: 0 6px 0 -12px;
  color: #a9b0d6;
}

.n.root {
  background: rgba(99, 102, 241, 0.18);
  font-weight: 600;
}

.n.missing {
  border-style: dashed;
}

.dep-label {
  display: block;
  margin-top: 14px;
}

.dep-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.dep {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.side-link {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  text-decoration: none;
  font-weight: 500;
}
</style>
