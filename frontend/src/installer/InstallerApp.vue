<script setup lang="ts">
import { mdiCheckCircle, mdiCircleOutline, mdiLockCheck } from '@mdi/js'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { useInterfaceLanguageSync } from '@/shared/composables/useInterfaceLanguage'
import { useThemeSync } from '@/shared/composables/useThemePreference'
import LanguageSwitcher from '@/shared/ui/LanguageSwitcher.vue'
import ThemeSwitcher from '@/shared/ui/ThemeSwitcher.vue'
import { STEP_IDS, useInstallerStore, type StepId } from './installerStore'
import DatabaseStep from './steps/DatabaseStep.vue'
import DoneStep from './steps/DoneStep.vue'
import InstallStep from './steps/InstallStep.vue'
import KeycloakStep from './steps/KeycloakStep.vue'
import OrganizationStep from './steps/OrganizationStep.vue'
import PersonStep from './steps/PersonStep.vue'
import ReviewStep from './steps/ReviewStep.vue'
import SkillsStep from './steps/SkillsStep.vue'
import WelcomeStep from './steps/WelcomeStep.vue'

useInterfaceLanguageSync()
useThemeSync()

const store = useInstallerStore()
const { t } = useI18n()
const { smAndDown } = useDisplay()
const heading = ref<HTMLElement | null>(null)
const optional: StepId[] = ['user', 'skills']

const currentName = computed(() => t(`installer.stepNames.${store.current}`))
const stepLabel = computed(() =>
  t('installer.stepOf', {
    current: store.stepIndex + 1,
    total: STEP_IDS.length,
    name: currentName.value,
  }),
)

watch(
  () => [currentName.value, t('installer.documentTitle')],
  () => {
    document.title = `${currentName.value} · ${t('installer.documentTitle')}`
  },
  { immediate: true },
)

// ESP-31 section 8: the focus goes to the title of the step when it changes.
watch(
  () => store.current,
  async () => {
    await nextTick()
    heading.value?.focus()
  },
)

onMounted(() => void store.loadState())
onBeforeUnmount(() => {
  store.stopPolling()
})

function done(step: StepId): boolean {
  const index = STEP_IDS.indexOf(step)
  return index < store.stepIndex && (store.passed(step) || index > STEP_IDS.indexOf('review'))
}
</script>

<template>
  <v-app>
    <v-app-bar flat border="b" density="comfortable">
      <v-app-bar-title>
        <span class="font-weight-medium">{{ $t('app.name') }}</span>
        <span class="text-medium-emphasis"> · {{ $t('installer.stepNames.install') }}</span>
      </v-app-bar-title>
      <template #append>
        <LanguageSwitcher />
        <ThemeSwitcher />
      </template>
    </v-app-bar>

    <v-main id="main-content">
      <v-container max-width="1100" class="py-8">
        <template v-if="store.installedAtStart">
          <v-card class="pa-6" max-width="640">
            <v-icon
              :icon="mdiLockCheck"
              size="40"
              color="primary"
              class="mb-2"
              aria-hidden="true"
            />
            <h1 class="text-h5 mb-2">{{ $t('installer.installed.title') }}</h1>
            <p class="text-body-1">{{ $t('installer.installed.text') }}</p>
          </v-card>
        </template>

        <template v-else>
          <h1 class="text-h4 mb-1">{{ $t('installer.title') }}</h1>
          <p class="text-body-1 text-medium-emphasis mb-6">{{ $t('installer.subtitle') }}</p>

          <v-row>
            <v-col v-if="!smAndDown" cols="12" md="4" lg="3">
              <nav :aria-label="$t('installer.stepsLabel')">
                <v-list density="comfortable" rounded="lg" class="py-2" bg-color="surface" border>
                  <v-list-item
                    v-for="(step, index) in STEP_IDS"
                    :key="step"
                    :active="step === store.current"
                    :aria-current="step === store.current ? 'step' : undefined"
                    :disabled="!store.reachable(step)"
                    color="primary"
                    @click="store.goTo(step)"
                  >
                    <template #prepend>
                      <v-icon
                        :icon="done(step) ? mdiCheckCircle : mdiCircleOutline"
                        :color="done(step) ? 'success' : undefined"
                        size="small"
                        aria-hidden="true"
                      />
                    </template>
                    <v-list-item-title>
                      <span class="text-medium-emphasis mr-1">{{ index + 1 }}.</span>
                      {{ $t(`installer.stepNames.${step}`) }}
                    </v-list-item-title>
                    <v-list-item-subtitle v-if="optional.includes(step)">{{
                      $t('installer.optional')
                    }}</v-list-item-subtitle>
                  </v-list-item>
                </v-list>
              </nav>
            </v-col>

            <v-col cols="12" md="8" lg="9">
              <v-card class="pa-6" rounded="lg" border flat>
                <template v-if="smAndDown">
                  <v-progress-linear
                    :model-value="((store.stepIndex + 1) / STEP_IDS.length) * 100"
                    color="primary"
                    rounded
                    class="mb-3"
                    aria-hidden="true"
                  />
                </template>
                <p class="text-overline text-medium-emphasis mb-0">{{ stepLabel }}</p>
                <h2 ref="heading" class="text-h5 mb-4" tabindex="-1" data-testid="step-title">
                  {{ currentName }}
                </h2>
                <WelcomeStep v-if="store.current === 'welcome'" />
                <DatabaseStep v-else-if="store.current === 'database'" />
                <OrganizationStep v-else-if="store.current === 'organization'" />
                <KeycloakStep v-else-if="store.current === 'keycloak'" />
                <PersonStep
                  v-else-if="store.current === 'administrator'"
                  key="administrator"
                  field="administrator"
                />
                <PersonStep v-else-if="store.current === 'user'" key="user" field="user" />
                <SkillsStep v-else-if="store.current === 'skills'" />
                <ReviewStep v-else-if="store.current === 'review'" />
                <InstallStep v-else-if="store.current === 'install'" />
                <DoneStep v-else />
              </v-card>
            </v-col>
          </v-row>
        </template>
      </v-container>
    </v-main>
  </v-app>
</template>
