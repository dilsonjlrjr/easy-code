<script setup lang="ts">
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useCurrentProjectStore } from '../stores/currentProjectStore'

const format = useFormat()
const store = useCurrentProjectStore()
</script>

<template>
  <!-- Layout: "Ondas" card of canvas "Projeto" (Projeto.dc.html). -->
  <section class="card waves-card" aria-labelledby="waves-heading">
    <h2 id="waves-heading">{{ $t('pages.project.waves') }}</h2>
    <p v-if="store.waves.length === 0" class="t2 waves-empty">{{ $t('projects.waves.empty') }}</p>
    <div v-else class="table-scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">{{ $t('projects.waves.wave') }}</th>
            <th scope="col">{{ $t('projects.waves.stage') }}</th>
            <th scope="col">{{ $t('projects.waves.state') }}</th>
            <th scope="col">{{ $t('projects.waves.cost') }}</th>
            <th scope="col">{{ $t('projects.waves.createdAt') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="wave in store.waves" :key="wave.id">
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
            <td>{{ format.money(wave.cost_total) }}</td>
            <td>{{ format.dateTime(wave.created_at) }}</td>
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
</style>
