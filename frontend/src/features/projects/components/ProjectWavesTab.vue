<script setup lang="ts">
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useFormat } from '@/shared/format/useFormat'
import StateLabel from '@/shared/ui/StateLabel.vue'
import { useCurrentProjectStore } from '../stores/currentProjectStore'

const format = useFormat()
const store = useCurrentProjectStore()
</script>

<template>
  <section aria-labelledby="waves-heading">
    <h2 id="waves-heading" class="text-h6 mb-4">{{ $t('pages.project.waves') }}</h2>
    <p v-if="store.waves.length === 0">{{ $t('projects.waves.empty') }}</p>
    <div v-else class="table-scroll">
      <v-table density="comfortable">
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
            <td>
              <router-link
                :to="{
                  name: ROUTE_NAMES.wave,
                  params: { projectId: store.project?.id ?? '', waveId: wave.id },
                }"
                class="monospace"
              >
                {{ wave.id }}
              </router-link>
            </td>
            <td>
              {{ $t(`stages.${wave.stage}`) }}
              <span v-if="wave.purpose !== 'execution'" class="text-body-2">
                ({{ $t(`waves.purpose.${wave.purpose}`) }})
              </span>
            </td>
            <td><StateLabel machine="wave" :state="wave.state" /></td>
            <td>{{ format.money(wave.cost_total) }}</td>
            <td>{{ format.dateTime(wave.created_at) }}</td>
          </tr>
        </tbody>
      </v-table>
    </div>
  </section>
</template>
