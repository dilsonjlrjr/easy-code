<script setup lang="ts">
import { ref } from 'vue'
import { ROUTE_NAMES } from '@/app/router/routeNames'
import { useLoader } from '@/shared/composables/useLoader'
import AppBreadcrumbs from '@/shared/ui/AppBreadcrumbs.vue'
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
  <!-- Layout: canvas "Agentes e skills" (Agentes.dc.html), before a project is chosen. -->
  <AppBreadcrumbs :items="[{ title: $t('pages.agents.title') }]" />
  <div>
    <h1>{{ $t('pages.agents.title') }}</h1>
    <p class="t2 page-description">{{ $t('agentsPage.chooseProject') }}</p>
  </div>
  <LoadingState v-if="loader.loading.value && !loader.loaded.value" type="list-item@3" />
  <ProblemAlert
    v-else-if="loader.error.value"
    :error="loader.error.value"
    retry
    @retry="loader.run"
  />
  <section v-else class="card panel" :aria-label="$t('pages.projects.title')">
    <p v-if="projects.length === 0" class="empty">{{ $t('agentsPage.noProjects') }}</p>
    <ul v-else class="project-list" data-testid="agents-projects">
      <li v-for="project in projects" :key="project.id">
        <router-link
          class="li"
          :to="{ name: ROUTE_NAMES.projectAgents, params: { projectId: project.id } }"
        >
          {{ project.name }}
        </router-link>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.page-description {
  margin: 4px 0 0;
}

.panel {
  padding: 18px 22px;
}

.empty {
  margin: 0;
}

.project-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.project-list li:first-child .li {
  border-top: 0;
}

.li {
  min-height: 44px;
  font-weight: 600;
}
</style>
