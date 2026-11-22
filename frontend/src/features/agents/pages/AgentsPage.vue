<script setup lang="ts">
import { ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import LoadingState from '@/shared/ui/LoadingState.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { projectsApi, type Project } from '@/features/projects/api/projectsApi'

/**
 * ESP-22 section 2: agent configuration and skill association are always of a project (PC-13), so
 * without a chosen project the screen asks for one first.
 */
const projects = ref<Project[]>([])
const loader = useLoader(async () => {
  projects.value = (await projectsApi.listProjects({})).items
})
void loader.run()
</script>

<template>
  <v-container max-width="1200">
    <h1 class="text-h4 mb-2">{{ $t('pages.agents.title') }}</h1>
    <p class="text-body-1 mb-4">{{ $t('agentsPage.chooseProject') }}</p>
    <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@3" />
    <ProblemAlert
      v-else-if="loader.error.value"
      :error="loader.error.value"
      retry
      @retry="loader.run"
    />
    <p v-else-if="projects.length === 0" class="text-body-2">{{ $t('agentsPage.noProjects') }}</p>
    <ul v-else class="plain-list" data-testid="agents-projects">
      <li v-for="project in projects" :key="project.id" class="mb-2">
        <router-link :to="{ name: ROUTE_NAMES.projectAgents, params: { projectId: project.id } }">
          {{ project.name }}
        </router-link>
      </li>
    </ul>
  </v-container>
</template>
