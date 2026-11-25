<script setup lang="ts">
import { useFormat } from '@/shared/format/useFormat'
import CanvasIcon from '@/shared/ui/CanvasIcon.vue'
import type { SkillTreeNode } from '../skillTree'

/**
 * The files of a skill package as a nested list (folders, then their files). Paths are data from the
 * package: shown as text, never rendered (RN-1).
 */
defineProps<{ nodes: SkillTreeNode[] }>()

const format = useFormat()
</script>

<template>
  <ul class="file-tree">
    <li
      v-for="node in nodes"
      :key="node.path"
      class="file-node"
      :data-testid="`skill-file-${node.path}`"
    >
      <span class="file-row">
        <CanvasIcon
          :name="node.kind === 'folder' ? 'folder' : 'document'"
          :size="16"
          class="file-icon"
        />
        <span class="mono file-name">{{
          node.kind === 'folder' ? `${node.name}/` : node.name
        }}</span>
        <span class="t2 file-tokens">
          {{ $t('skillLibrary.tree.tokens', { n: format.integer(node.estimatedTokens) }) }}
        </span>
      </span>
      <SkillFileTree v-if="node.kind === 'folder'" :nodes="node.children" />
    </li>
  </ul>
</template>

<style scoped>
.file-tree {
  list-style: none;
  margin: 0;
  padding: 0 0 0 22px;
  border-left: 1px solid rgba(167, 139, 250, 0.25);
}

.file-node {
  margin: 0;
}

.file-row {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 28px;
  font-size: 13px;
}

.file-icon {
  flex: none;
  color: #a78bfa;
}

.file-name {
  min-width: 0;
  overflow-wrap: anywhere;
}

.file-tokens {
  margin-left: auto;
  white-space: nowrap;
  font-size: 12px;
}
</style>
