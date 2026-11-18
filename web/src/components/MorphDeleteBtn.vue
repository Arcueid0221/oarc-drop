<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import UiIcon from './UiIcon.vue'

const props = withDefaults(
  defineProps<{
    title?: string
    confirmLabel?: string
    protectedItem?: boolean
    protectedTitle?: string
    protectedMessage?: string
    compact?: boolean
  }>(),
  {
    title: '删除',
    confirmLabel: '确认删除',
    protectedItem: false,
    protectedTitle: '受保护的博客资源',
    protectedMessage: '该文件位于博客生产目录，删除可能导致线上已发布的文章或歌单出现死链。确定仍要强制永久删除吗？',
    compact: false,
  },
)

const emit = defineEmits<{
  (e: 'confirm', force: boolean): void
}>()

const armed = ref(false)
const showLockSheet = ref(false)
let resetTimer: number | undefined

function disarm() {
  armed.value = false
  if (resetTimer) {
    window.clearTimeout(resetTimer)
    resetTimer = undefined
  }
}

function onTriggerClick() {
  if (props.protectedItem) {
    showLockSheet.value = true
    return
  }
  armed.value = true
  if (resetTimer) window.clearTimeout(resetTimer)
  resetTimer = window.setTimeout(() => {
    armed.value = false
  }, 3200)
}

function onConfirmInline() {
  disarm()
  emit('confirm', false)
}

function onConfirmForce() {
  showLockSheet.value = false
  emit('confirm', true)
}

onBeforeUnmount(() => {
  if (resetTimer) window.clearTimeout(resetTimer)
})
</script>

<template>
  <div class="morph-del-wrap" @click.stop>
    <Transition name="morph-pop" mode="out-in">
      <div v-if="armed" key="armed" class="morph-armed">
        <button type="button" class="morph-yes" @click="onConfirmInline">
          {{ confirmLabel }}
        </button>
        <button type="button" class="morph-no" title="取消" @click="disarm">
          <UiIcon name="close" :size="12" />
        </button>
      </div>

      <button
        v-else
        key="idle"
        type="button"
        class="morph-trigger"
        :class="{ 'is-compact': compact }"
        :title="title"
        @click="onTriggerClick"
      >
        <UiIcon name="trash" :size="compact ? 13 : 14" />
      </button>
    </Transition>

    <Teleport to="body">
      <Transition name="sheet-fade">
        <div v-if="showLockSheet" class="lock-backdrop" @click.self="showLockSheet = false">
          <div class="liquid-chrome lock-dialog" role="alertdialog" aria-modal="true">
            <div class="lock-head">
              <span class="lock-badge">
                <UiIcon name="lock" :size="13" />
                <span>PROTECTED</span>
              </span>
              <span class="lock-title">{{ protectedTitle }}</span>
            </div>
            <p class="lock-desc">{{ protectedMessage }}</p>
            <div class="lock-foot">
              <button type="button" class="pill-btn" @click="showLockSheet = false">取消</button>
              <button type="button" class="pill-btn lock-danger-btn" @click="onConfirmForce">
                强制删除
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.morph-del-wrap {
  display: inline-flex;
  align-items: center;
}

.morph-trigger {
  width: 33px;
  height: 33px;
  padding: 0;
  border-radius: 9px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  display: inline-grid;
  place-items: center;
  cursor: pointer;
  box-shadow: var(--specular-inset);
  transition:
    transform 0.2s var(--ease-spring),
    color 0.16s ease,
    border-color 0.16s ease,
    background-color 0.16s ease;
}

.morph-trigger.is-compact {
  width: 28px;
  height: 28px;
  border-radius: 7px;
}

.morph-trigger:hover {
  color: var(--danger-fg);
  border-color: rgba(220, 38, 38, 0.32);
  background: var(--danger-bg);
}

.morph-trigger:active {
  transform: scale(0.92);
}

.morph-armed {
  display: inline-flex;
  align-items: center;
  height: 31px;
  padding: 2px;
  border-radius: 9px;
  background: var(--danger-solid);
  box-shadow: 0 6px 16px -4px rgba(220, 38, 38, 0.42);
  gap: 2px;
}

.morph-yes {
  height: 100%;
  border: 0;
  border-radius: 7px;
  padding: 0 9px;
  background: transparent;
  color: #ffffff;
  font-size: 0.75rem;
  font-weight: 640;
  cursor: pointer;
  white-space: nowrap;
}

.morph-yes:hover {
  background: rgba(255, 255, 255, 0.14);
}

.morph-no {
  width: 25px;
  height: 25px;
  border: 0;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.18);
  color: #ffffff;
  display: inline-grid;
  place-items: center;
  cursor: pointer;
}

.morph-no:hover {
  background: rgba(0, 0, 0, 0.3);
}

.morph-pop-enter-active,
.morph-pop-leave-active {
  transition:
    transform 0.24s var(--ease-bounce),
    opacity 0.16s ease;
}
.morph-pop-enter-from,
.morph-pop-leave-to {
  opacity: 0;
  transform: scale(0.86);
}

.lock-backdrop {
  position: fixed;
  inset: 0;
  z-index: 9990;
  background: rgba(8, 9, 12, 0.42);
  -webkit-backdrop-filter: blur(8px);
  backdrop-filter: blur(8px);
  display: grid;
  place-items: center;
  padding: 18px;
}

.lock-dialog {
  width: min(390px, 100%);
  border-radius: 18px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.lock-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lock-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-family: var(--font-mono);
  font-size: 0.66rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  padding: 2px 7px;
  border-radius: 6px;
  background: var(--danger-bg);
  color: var(--danger-fg);
}

.lock-title {
  font-size: 0.94rem;
  font-weight: 660;
  color: var(--ink-1);
}

.lock-desc {
  margin: 0;
  font-size: 0.82rem;
  line-height: 1.55;
  color: var(--ink-2);
}

.lock-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}

.lock-danger-btn {
  background: var(--danger-solid);
  color: #ffffff;
  border-color: transparent;
}
.lock-danger-btn:hover {
  background: var(--danger-solid);
  opacity: 0.9;
  border-color: transparent;
}

.sheet-fade-enter-active,
.sheet-fade-leave-active {
  transition: opacity 0.2s ease;
}
.sheet-fade-enter-from,
.sheet-fade-leave-to {
  opacity: 0;
}
</style>
