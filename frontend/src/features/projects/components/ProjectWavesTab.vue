<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { waveElapsedMs, waveTokenTotals } from '@/features/waves/composables/waveRow'
import { useCurrentProjectStore } from '../stores/currentProjectStore'

const format = useFormat()
const store = useCurrentProjectStore()
/** The time of an open wave grows with the clock (ESP-06 CA-06-11). */
const now = ref(new Date())
const CLOCK_TICK_MS = 30_000
let clock: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  clock = setInterval(() => {
    now.value = new Date()
  }, CLOCK_TICK_MS)
})

onBeforeUnmount(() => {
  if (clock !== undefined) clearInterval(clock)
})

function elapsed(wave: (typeof store.waves)[number]): string {
  const milliseconds = waveElapsedMs(wave, now.value)
  return milliseconds === undefined ? '—' : format.longDuration(milliseconds)
}
</script>

<template>
  <!-- Layout: "Ondas" card of canvas "Projeto" (Projeto.dc.html). -->
  <section class="card waves-card" aria-labelledby="waves-heading">
    <h2 id="waves-heading">{{ $t('pages.project.waves') }}</h2>
    <p v-if="store.waves.length === 0" class="t2 waves-empty">{{ $t('projects.waves.empty') }}</p>
    <div v-else class="table-scroll">
      <table class="tbl" data-testid="project-waves">
        <thead>
          <tr>
            <th scope="col">{{ $t('projects.waves.wave') }}</th>
            <th scope="col">{{ $t('projects.waves.stage') }}</th>
            <th scope="col">{{ $t('projects.waves.state') }}</th>
            <th scope="col">{{ $t('projects.waves.tokens') }}</th>
            <th scope="col">{{ $t('projects.waves.cost') }}</th>
            <th scope="col">{{ $t('projects.waves.time') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="wave in store.waves" :key="wave.id" :data-testid="`project-wave-${wave.id}`">
            <td class="mono">
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId: store.project?.id ?? '', waveId: wave.id },
                }"
              >
                {{ wave.id }}
              </router-link>
            </td>
            <td>
              {{ $t(`stages.${wave.stage}`) }}
              <template v-if="wave.purpose !== 'execution'">
                · {{ $t(`waves.purpose.${wave.purpose}`) }}
              </template>
            </td>
            <td><StateLabel machine="wave" :state="wave.state" /></td>
            <td data-testid="wave-tokens">
              {{ format.integer(waveTokenTotals(wave.tokens).main) }}
              <span v-if="waveTokenTotals(wave.tokens).cache > 0" class="t2 cache-tokens">
                {{
                  $t('projects.waves.cacheTokens', {
                    n: format.integer(waveTokenTotals(wave.tokens).cache),
                  })
                }}
              </span>
            </td>
            <td>{{ format.money(wave.cost_total) }}</td>
            <td data-testid="wave-time">
              {{ elapsed(wave) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.waves-card {
  padding: 18px 22px;
}

.waves-card h2 {
  margin-bottom: 8px;
}

.waves-empty {
  margin: 0;
}

.tbl a {
  text-decoration: none;
}

.cache-tokens {
  display: block;
  font-size: 12px;
}
</style>
