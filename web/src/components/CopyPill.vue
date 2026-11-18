<script setup lang="ts">
import { ref } from 'vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(
  defineProps<{
    text: string
    label?: string
    copiedLabel?: string
    compact?: boolean
  }>(),
  {
    label: '复制',
    copiedLabel: '已复制',
    compact: false,
  },
)

const copied = ref(false)
let timer: number | undefined

async function handleCopy() {
  if (!props.text) return
  try {
    await navigator.clipboard.writeText(props.text)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = props.text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  copied.value = true
  if (timer) window.clearTimeout(timer)
  timer = window.setTimeout(() => {
    copied.value = false
  }, 1750)
}
</script>

<template>
  <button
    type="button"
    class="copy-pill"
    :class="{ 'is-copied': copied, 'is-compact': compact }"
    @click.stop="handleCopy"
  >
    <Transition name="glyph-morph" mode="out-in">
      <span v-if="copied" key="ok" class="copy-pill__inner">
        <UiIcon name="check" :size="14" />
        <span>{{ copiedLabel }}</span>
      </span>
      <span v-else key="idle" class="copy-pill__inner">
        <UiIcon name="copy" :size="13.5" />
        <span>{{ label }}</span>
      </span>
    </Transition>
  </button>
</template>

<style scoped>
.copy-pill {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 32px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border-subtle);
  background: var(--surface-card);
  color: var(--ink-1);
  font-size: 0.79rem;
  font-weight: 570;
  cursor: pointer;
  box-shadow: var(--specular-inset);
  transition:
    background-color 0.26s var(--ease-island),
    border-color 0.26s var(--ease-island),
    color 0.26s var(--ease-island),
    transform 0.22s var(--ease-spring);
}

.copy-pill.is-compact {
  height: 28px;
  padding: 0 10px;
  font-size: 0.75rem;
}

.copy-pill:hover {
  border-color: var(--border-strong);
  background: var(--surface-pill);
}

.copy-pill:active {
  transform: scale(0.94);
}

.copy-pill.is-copied {
  background: var(--emerald-bg);
  border-color: var(--emerald-border);
  color: var(--emerald-fg);
}

.copy-pill__inner {
  display: inline-flex;
  align-items: center;
  gap: 5.5px;
  white-space: nowrap;
}

.glyph-morph-enter-active,
.glyph-morph-leave-active {
  transition:
    opacity 0.16s ease,
    transform 0.22s var(--ease-spring),
    filter 0.16s ease;
}

.glyph-morph-enter-from {
  opacity: 0;
  transform: scale(0.78);
  filter: blur(4px);
}

.glyph-morph-leave-to {
  opacity: 0;
  transform: scale(0.84);
  filter: blur(3px);
}
</style>
