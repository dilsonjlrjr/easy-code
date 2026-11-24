<script setup lang="ts">
import { mdiAccountSearch } from '@mdi/js'
import { computed, onMounted, ref } from 'vue'
import { useInstallerStore, type PersonMode } from '../installerStore'
import type { DirectoryUser } from '../types'
import { useFieldErrors } from '../useFieldErrors'
import PasswordField from '../components/PasswordField.vue'
import StepFooter from '../components/StepFooter.vue'

/** Steps 5 (administrator) and 6 (regular user, optional). */
const props = defineProps<{ field: 'administrator' | 'user' }>()
const store = useInstallerStore()
const errors = useFieldErrors()

const person = computed(() => (props.field === 'administrator' ? store.administrator : store.user))
const mode = computed<PersonMode>({
  get: () => (props.field === 'administrator' ? store.administratorMode : store.userMode),
  set: (value) => {
    if (props.field === 'administrator') store.administratorMode = value
    else store.userMode = value
  },
})
const chosen = computed<DirectoryUser | null>({
  get: () => (props.field === 'administrator' ? store.administratorChosen : store.userChosen),
  set: (value) => {
    if (props.field === 'administrator') store.administratorChosen = value
    else store.userChosen = value
  },
})
const existingRealm = computed(() => store.sso.mode === 'existing_realm')
const query = ref('')
const results = ref<DirectoryUser[] | null>(null)
const f = (name: string) => `${props.field}.${name}`

onMounted(() => {
  if (props.field === 'user') store.userSkipped = false
})

async function search(): Promise<void> {
  results.value = await store.searchUsers(query.value)
}

function skip(): void {
  store.skipUser()
}
</script>

<template>
  <section>
    <p class="text-body-1 mb-4">
      {{
        field === 'administrator'
          ? $t('installer.person.introAdministrator')
          : $t('installer.person.introUser')
      }}
    </p>
    <v-radio-group v-if="existingRealm" v-model="mode" inline class="mb-2">
      <v-radio value="new" :label="$t('installer.person.choiceNew')" />
      <v-radio value="existing" :label="$t('installer.person.choiceExisting')" />
    </v-radio-group>

    <template v-if="mode === 'new'">
      <v-row dense>
        <v-col cols="12" sm="6">
          <v-text-field
            v-model="person.first_name"
            :label="$t('installer.person.firstName')"
            :error-messages="errors(f('first_name'))"
          />
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field
            v-model="person.last_name"
            :label="$t('installer.person.lastName')"
            :error-messages="errors(f('last_name'))"
          />
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field
            v-model="person.username"
            :label="$t('installer.person.username')"
            autocomplete="off"
            spellcheck="false"
            :error-messages="errors(f('username'))"
          />
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field
            v-model="person.email"
            :label="$t('installer.person.email')"
            type="email"
            autocomplete="off"
            :error-messages="errors(f('email'))"
          />
        </v-col>
        <v-col cols="12">
          <PasswordField
            v-model="person.password"
            :label="$t('installer.person.password')"
            :hint="$t('installer.person.passwordHint')"
            autocomplete="new-password"
            :error-messages="errors(f('password'))"
          />
        </v-col>
      </v-row>
    </template>

    <template v-else>
      <form class="d-flex ga-2 align-start" @submit.prevent="search">
        <v-text-field v-model="query" :label="$t('installer.person.search')" class="flex-grow-1" />
        <v-btn
          type="submit"
          color="primary"
          variant="outlined"
          :prepend-icon="mdiAccountSearch"
          min-height="56"
          :loading="store.busy"
        >
          {{ $t('installer.actions.search') }}
        </v-btn>
      </form>
      <p v-if="results && results.length === 0" class="text-body-2" role="status">
        {{ $t('installer.person.noResults') }}
      </p>
      <v-list
        v-if="results && results.length > 0"
        density="comfortable"
        :aria-label="$t('installer.person.search')"
      >
        <v-list-item
          v-for="item in results"
          :key="item.id"
          :title="item.name"
          :subtitle="`${item.username}${item.email ? ' · ' + item.email : ''}`"
          :active="chosen?.id === item.id"
          :disabled="!item.enabled"
          @click="chosen = item"
        />
      </v-list>
      <p v-if="chosen" class="text-body-2 mt-2" role="status">
        {{ $t('installer.person.chosen', { name: `${chosen.name} (${chosen.username})` }) }}
      </p>
      <p
        v-for="message in errors(f('existing_id'))"
        :key="message"
        class="text-error text-body-2"
        role="alert"
      >
        {{ message }}
      </p>
    </template>

    <StepFooter :test="() => store.testPerson(field)" :skippable="field === 'user'" @skip="skip" />
  </section>
</template>
