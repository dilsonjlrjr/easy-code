<script setup lang="ts">
import { ref, watch } from 'vue'
import { useFormat } from '@/shared/format/useFormat'
import LoadingState from '@/shared/ui/LoadingState.vue'
import MarkdownContent from '@/shared/ui/MarkdownContent.vue'
import ProblemAlert from '@/shared/ui/ProblemAlert.vue'
import { artifactsApi, type ArtifactAttachment } from '../api/artifactsApi'
import { attachmentKind } from '../composables/artifactLinks'

/**
 * ESP-21 3 "Anexos" (PL-24, ER-21-14, CA-21-16): the other files of the version with name, type and
 * size. Markdown opens sanitized, other text as plain text; HTML, scripts, styles and images of the
 * prototype only open through the content origin (ER-21-5), never here.
 */
const props = defineProps<{
  projectId: string
  artifactId: string
  version: number
  attachments: ArtifactAttachment[]
}>()

const format = useFormat()
const open = ref<string>()
const content = ref<string>()
const loading = ref(false)
const failure = ref<unknown>()

watch(
  () => [props.artifactId, props.version],
  () => {
    open.value = undefined
    content.value = undefined
  },
)

async function toggle(attachment: ArtifactAttachment): Promise<void> {
  if (open.value === attachment.path) {
    open.value = undefined
    return
  }
  open.value = attachment.path
  content.value = undefined
  failure.value = undefined
  loading.value = true
  try {
    content.value = await artifactsApi.getAttachment(
      props.projectId,
      props.artifactId,
      props.version,
      attachment.path,
    )
  } catch (error) {
    failure.value = error
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <section
    id="attachments"
    class="card att-card"
    aria-labelledby="artifact-attachments-heading"
    data-testid="artifact-attachments"
  >
    <h2 id="artifact-attachments-heading">{{ $t('artifactLinks.attachments') }}</h2>
    <p v-if="attachments.length === 0" class="t2 att-text">
      {{ $t('artifactLinks.noAttachments') }}
    </p>
    <ul v-else class="att-list">
      <li v-for="attachment in attachments" :key="attachment.path" class="att">
        <span class="att-line">
          <span class="mono att-name">{{ attachment.path }}</span>
          <span class="t2"
            >{{ attachment.media_type }} · {{ format.bytes(attachment.size_bytes) }}</span
          >
          <button
            v-if="attachmentKind(attachment.media_type) !== 'content-origin'"
            type="button"
            class="ib"
            :aria-expanded="open === attachment.path ? 'true' : 'false'"
            :aria-controls="`attachment-${attachment.sha256}`"
            @click="toggle(attachment)"
          >
            {{
              open === attachment.path
                ? $t('artifactLinks.closeAttachment')
                : $t('artifactLinks.openAttachment')
            }}
          </button>
          <span v-else class="t2">{{ $t('artifactLinks.contentOrigin') }}</span>
        </span>
        <div
          v-if="open === attachment.path"
          :id="`attachment-${attachment.sha256}`"
          class="att-content"
        >
          <LoadingState v-if="loading" type="paragraph" />
          <ProblemAlert v-else-if="failure" :error="failure" />
          <template v-else-if="content !== undefined">
            <MarkdownContent
              v-if="attachmentKind(attachment.media_type) === 'markdown'"
              :source="content"
            />
            <pre v-else class="mono att-pre">{{ content }}</pre>
            <p class="t2 att-text">{{ $t('artifacts.sanitizedNotice') }}</p>
          </template>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.att-card {
  padding: 18px 22px;
}

.att-card h2 {
  margin-bottom: 4px;
}

.att-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.att {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.att-line {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  font-size: 13px;
}

.att-name {
  overflow-wrap: anywhere;
}

.att-content {
  padding: 12px 14px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.04);
  max-height: 480px;
  overflow: auto;
}

.att-pre {
  margin: 0;
  white-space: pre-wrap;
  font-size: 12px;
}

.att-text {
  margin: 0;
  font-size: 12px;
}
</style>
