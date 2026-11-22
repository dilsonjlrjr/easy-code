<script setup lang="ts">
import { mdiDeleteOutline } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CONSTRAINT_CATEGORIES,
  CONSTRAINT_DESCRIPTION_MAX,
  CONSTRAINT_DESCRIPTION_MIN,
  type ConstraintDraft,
} from '../api/projectsApi'

/**
 * Editable list of technical constraints (US-54, PL-13): one category and one description per item.
 * Items keep their `RESTR-NN` id; new items get one from the api.
 */
const items = defineModel<ConstraintDraft[]>({ required: true })
defineProps<{ disabled?: boolean }>()

const { t } = useI18n()

const categories = computed(() =>
  CONSTRAINT_CATEGORIES.map((value) => ({
    value,
    title: t(`projects.constraints.categories.${value}`),
  })),
)

function add(): void {
  items.value = [...items.value, { category: 'other', description: '' }]
}

function remove(index: number): void {
  items.value = items.value.filter((_, position) => position !== index)
}

function descriptionRule(value: string): true | string {
  const length = value.trim().length
  if (length < CONSTRAINT_DESCRIPTION_MIN)
    return t('projects.constraints.descriptionTooShort', { min: CONSTRAINT_DESCRIPTION_MIN })
  if (length > CONSTRAINT_DESCRIPTION_MAX)
    return t('projects.constraints.descriptionTooLong', { max: CONSTRAINT_DESCRIPTION_MAX })
  return true
}
</script>

<template>
  <div>
    <ol v-if="items.length > 0" class="constraint-items">
      <li v-for="(item, index) in items" :key="item.id ?? `new-${index}`" class="mb-2">
        <fieldset class="constraint-item pa-3">
          <legend class="text-body-2 font-weight-medium px-1">
            {{ item.id ?? $t('projects.constraints.newItem', { n: index + 1 }) }}
          </legend>
          <v-select
            v-model="item.category"
            :items="categories"
            item-title="title"
            item-value="value"
            :label="$t('projects.constraints.category')"
            :disabled="disabled"
            density="compact"
          />
          <v-textarea
            v-model="item.description"
            :label="$t('projects.constraints.description')"
            :rules="[descriptionRule]"
            :counter="CONSTRAINT_DESCRIPTION_MAX"
            :disabled="disabled"
            rows="2"
            auto-grow
          />
          <v-btn
            :prepend-icon="mdiDeleteOutline"
            variant="text"
            size="small"
            :disabled="disabled"
            @click="remove(index)"
          >
            {{ $t('projects.constraints.removeItem', { item: item.id ?? index + 1 }) }}
          </v-btn>
        </fieldset>
      </li>
    </ol>
    <v-btn variant="tonal" :disabled="disabled" @click="add">
      {{ $t('projects.constraints.add') }}
    </v-btn>
  </div>
</template>

<style scoped>
.constraint-items {
  list-style: none;
  padding: 0;
}

.constraint-item {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
}
</style>
