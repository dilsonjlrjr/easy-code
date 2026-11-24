<script setup lang="ts">
import { computed, reactive, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
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
const baseId = useId()
/** Items whose description the person already typed in or left: only these show the rule. */
const touched = reactive(new Set<ConstraintDraft>())

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
  const item = items.value[index]
  if (item) touched.delete(item)
  items.value = items.value.filter((_, position) => position !== index)
}

function descriptionError(item: ConstraintDraft): string | undefined {
  if (!touched.has(item)) return undefined
  const result = descriptionRule(item.description)
  return result === true ? undefined : result
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
  <!-- Look: canvas "Novo projeto", step 2 fields (`.fl` label over `.fld`) inside a bordered block. -->
  <div class="constraints">
    <ol v-if="items.length > 0" class="constraint-items">
      <li v-for="(item, index) in items" :key="item.id ?? `new-${index}`">
        <fieldset class="constraint-item">
          <legend class="constraint-legend">
            {{ item.id ?? $t('projects.constraints.newItem', { n: index + 1 }) }}
          </legend>
          <div class="fl">
            <label :for="`${baseId}-${index}-category`" class="fl-control">
              <span :id="`${baseId}-${index}-category-label`" class="fl-label">{{
                $t('projects.constraints.category')
              }}</span>
              <select
                :id="`${baseId}-${index}-category`"
                v-model="item.category"
                :aria-labelledby="`${baseId}-${index}-category-label`"
                class="fld"
                :disabled="disabled"
              >
                <option v-for="option in categories" :key="option.value" :value="option.value">
                  {{ option.title }}
                </option>
              </select>
            </label>
          </div>
          <div class="fl">
            <label :for="`${baseId}-${index}-description`" class="fl-control">
              <span class="fl-label">{{ $t('projects.constraints.description') }}</span>
              <textarea
                :id="`${baseId}-${index}-description`"
                v-model="item.description"
                class="fld"
                rows="2"
                :disabled="disabled"
                :aria-invalid="!!descriptionError(item)"
                :aria-describedby="
                  descriptionError(item)
                    ? `${baseId}-${index}-error ${baseId}-${index}-counter`
                    : `${baseId}-${index}-counter`
                "
                @input="touched.add(item)"
                @blur="touched.add(item)"
              />
            </label>
            <span class="fld-foot">
              <span
                v-if="descriptionError(item)"
                :id="`${baseId}-${index}-error`"
                class="fld-error"
                role="alert"
                >{{ descriptionError(item) }}</span
              >
              <span :id="`${baseId}-${index}-counter`" class="t2 fld-counter"
                >{{ item.description.length }} / {{ CONSTRAINT_DESCRIPTION_MAX }}</span
              >
            </span>
          </div>
          <div>
            <button
              type="button"
              class="btn btn-d constraint-remove"
              :disabled="disabled"
              @click="remove(index)"
            >
              {{ $t('projects.constraints.removeItem', { item: item.id ?? index + 1 }) }}
            </button>
          </div>
        </fieldset>
      </li>
    </ol>
    <div>
      <button type="button" class="btn constraint-add" :disabled="disabled" @click="add">
        <CanvasIcon name="plus" :size="16" :stroke-width="2.2" />
        {{ $t('projects.constraints.add') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.constraints {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.constraint-items {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.constraint-item {
  margin: 0;
  min-width: 0;
  padding: 12px 16px 16px;
  border: 1px solid rgba(140, 150, 255, 0.16);
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.constraint-legend {
  padding: 0 6px;
  font-weight: 600;
  font-size: 13px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.fl {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.fl-control {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.fl-label {
  font-weight: 600;
  font-size: 13px;
}

.fld[aria-invalid='true'] {
  border-color: #fca5a5;
}

.fld-foot {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.fld-error {
  color: #fca5a5;
  font-size: 12px;
}

.fld-counter {
  margin-left: auto;
  font-size: 12px;
  white-space: nowrap;
}

.constraint-remove,
.constraint-add {
  min-height: 40px;
}
</style>
