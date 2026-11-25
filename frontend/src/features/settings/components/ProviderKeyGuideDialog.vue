<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProviderId } from '@/shared/api/contract'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import type { Provider } from '../api/providersApi'
import anthropicKeys from '../assets/key-guide/anthropic-1.svg'
import anthropicCreate from '../assets/key-guide/anthropic-2.svg'
import anthropicCopy from '../assets/key-guide/anthropic-3.svg'
import openaiKeys from '../assets/key-guide/openai-1.svg'
import openaiCreate from '../assets/key-guide/openai-2.svg'
import openaiCopy from '../assets/key-guide/openai-3.svg'

/**
 * How to get an API key of a provider, step by step with pictures of the provider console. The
 * pictures are drawings of the screen (no capture of a real account) and the links open the
 * console in a new tab; the key itself is pasted only in the key dialog.
 */
interface GuideStep {
  text: string
  image?: string
  link?: { href: string; label: string }
}

interface Guide {
  consoleUrl: string
  billingUrl: string
  keyPrefix: string
  images: [string, string, string]
}

const GUIDES: Record<ProviderId, Guide> = {
  anthropic: {
    consoleUrl: 'https://platform.claude.com/settings/keys',
    billingUrl: 'https://platform.claude.com/settings/billing',
    keyPrefix: 'sk-ant-',
    images: [anthropicKeys, anthropicCreate, anthropicCopy],
  },
  openai: {
    consoleUrl: 'https://platform.openai.com/api-keys',
    billingUrl: 'https://platform.openai.com/settings/organization/billing/overview',
    keyPrefix: 'sk-',
    images: [openaiKeys, openaiCreate, openaiCopy],
  },
}

const props = defineProps<{ provider: Provider | undefined }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ addKey: [provider: Provider] }>()

const { t } = useI18n()
const guide = computed(() => (props.provider ? GUIDES[props.provider.id] : undefined))
const providerName = computed(() => props.provider?.name ?? '')

const steps = computed<GuideStep[]>(() => {
  const current = guide.value
  if (!current || !props.provider) return []
  const prefix = `settings.keyGuide.${props.provider.id}`
  return [
    {
      text: t(`${prefix}.open`),
      image: current.images[0],
      link: { href: current.consoleUrl, label: t('settings.keyGuide.openConsole') },
    },
    { text: t(`${prefix}.create`), image: current.images[1] },
    {
      text: t('settings.keyGuide.copy', { prefix: current.keyPrefix }),
      image: current.images[2],
    },
    {
      text: t('settings.keyGuide.billing'),
      link: { href: current.billingUrl, label: t('settings.keyGuide.openBilling') },
    },
    { text: t('settings.keyGuide.paste') },
  ]
})

function addKey(): void {
  if (!props.provider) return
  open.value = false
  emit('addKey', props.provider)
}
</script>

<template>
  <v-dialog v-model="open" max-width="680" scrollable aria-labelledby="key-guide-title">
    <v-card data-testid="key-guide">
      <div class="guide-head">
        <h2 id="key-guide-title">
          {{ $t('settings.keyGuide.title', { provider: providerName }) }}
        </h2>
        <button
          type="button"
          class="icon-btn"
          :aria-label="$t('common.close')"
          @click="open = false"
        >
          <CanvasIcon name="close" :size="18" />
        </button>
      </div>
      <div class="guide-body">
        <p class="t2 intro">{{ $t('settings.keyGuide.intro') }}</p>
        <ol class="steps">
          <li
            v-for="(step, index) in steps"
            :key="index"
            class="step"
            :data-testid="`key-guide-step-${index + 1}`"
          >
            <span class="step-number" aria-hidden="true">{{ index + 1 }}</span>
            <div class="step-content">
              <p class="step-text">{{ step.text }}</p>
              <a
                v-if="step.link"
                :href="step.link.href"
                target="_blank"
                rel="noopener noreferrer"
                class="step-link"
              >
                {{ step.link.label }}
                <span class="sr-only">{{ $t('settings.keyGuide.newTab') }}</span>
              </a>
              <img
                v-if="step.image"
                :src="step.image"
                :alt="$t('settings.keyGuide.imageAlt', { step: index + 1, provider: providerName })"
                class="step-image"
                width="560"
                height="300"
                loading="lazy"
              />
            </div>
          </li>
        </ol>
        <p class="note security">
          <CanvasIcon name="alert" :size="18" />
          <span>{{ $t('settings.keyGuide.security') }}</span>
        </p>
      </div>
      <div class="dialog-actions">
        <button type="button" class="btn" @click="open = false">{{ $t('common.close') }}</button>
        <button type="button" class="btn btn-p" @click="addKey">
          {{ $t('settings.keyGuide.addNow') }}
        </button>
      </div>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.guide-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 24px 4px;
}

.guide-head h2 {
  margin: 0;
}

.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  cursor: pointer;
}

.icon-btn:focus-visible {
  outline: 2px solid #c4b5fd;
}

.guide-body {
  padding: 4px 24px 8px;
}

.intro {
  margin: 0 0 12px;
  font-size: 13px;
}

.steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.step {
  display: flex;
  gap: 12px;
}

.step-number {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: rgba(139, 92, 246, 0.25);
  color: #ffffff;
  font-weight: 700;
  font-size: 13px;
}

.step-content {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.step-text {
  margin: 4px 0 0;
}

.step-link {
  align-self: flex-start;
  color: #c4b5fd;
  font-weight: 600;
}

.step-image {
  width: 100%;
  height: auto;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.security {
  margin: 18px 0 0;
}

.security svg {
  flex: none;
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 24px 22px;
}
</style>
