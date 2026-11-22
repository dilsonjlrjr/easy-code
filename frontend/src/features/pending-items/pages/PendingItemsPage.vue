<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { pendingItemsApi, waveOfQuestion, type PendingItem } from '../api/pendingItemsApi'

const format = useFormat()
const items = ref<PendingItem[]>([])
const total = ref(0)

const loader = useLoader(async () => {
  const page = await pendingItemsApi.list()
  items.value = page.items
  total.value = page.total
})

onMounted(() => {
  void loader.run()
})
</script>

<template>
  <v-container max-width="1200">
    <h1 class="text-h4 mb-2">{{ $t('pages.pendingItems.title') }}</h1>
    <p class="text-body-1 text-medium-emphasis mb-4">{{ $t('pages.pendingItems.description') }}</p>
    <LoadingState v-if="loader.loading.value" type="list-item-two-line@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="items.length === 0">{{ $t('pendingItems.empty') }}</p>
    <template v-else>
      <p class="text-body-2 mb-2">{{ $t('pendingItems.total', { count: total }) }}</p>
      <!-- US-10 CA-2: the question shows project, stage, agent and how long it has waited. -->
      <ul class="plain-list">
        <li v-for="item in items" :key="`${item.project.id}-${item.target.id}`" class="mb-3">
          <v-card variant="outlined" class="pa-4">
            <h2 class="text-subtitle-1 font-weight-medium">
              {{ $t(`pendingItems.types.${item.target.type}`) }} · {{ item.project.name }}
            </h2>
            <p class="text-body-2">
              <template v-if="item.stage">{{ $t(`stages.${item.stage}`) }} · </template>
              <template v-if="item.agent">{{ $t(`agents.${item.agent}`) }} · </template>
              <span :title="format.dateTime(item.opened_at)">
                {{ $t('pendingItems.waiting', { age: format.age(item.opened_at) }) }}
              </span>
            </p>
            <p v-if="item.summary?.questions" class="text-body-2">
              {{ $t('pendingItems.questionCount', { count: item.summary.questions }) }}
            </p>
            <router-link
              v-if="item.target.type === 'question' && waveOfQuestion(item.target.id)"
              :to="{
                name: ROUTE_NAMES.wave,
                params: { projectId: item.project.id, waveId: waveOfQuestion(item.target.id) },
              }"
            >
              {{ $t('pendingItems.answer') }}
            </router-link>
            <router-link
              v-else
              :to="{ name: ROUTE_NAMES.project, params: { projectId: item.project.id } }"
            >
              {{ $t('pendingItems.openProject') }}
            </router-link>
          </v-card>
        </li>
      </ul>
    </template>
  </v-container>
</template>
