<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useBodyScrollLock } from '../composables/useMobileViewport'
import UiIcon from './UiIcon.vue'

const props = withDefaults(
  defineProps<{
    title?: string
    confirmLabel?: string
    busyLabel?: string
    busy?: boolean
    protectedItem?: boolean
    protectedTitle?: string
    protectedMessage?: string
    compact?: boolean
  }>(),
  {
    title: '删除',
    confirmLabel: '确认删除',
    busyLabel: '删除中…',
    busy: false,
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
useBodyScrollLock(showLockSheet)

function disarm() {
  armed.value = false
  if (resetTimer) {
    window.clearTimeout(resetTimer)
    resetTimer = undefined
  }
}

function onTriggerClick() {
  if (props.busy) return
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
  if (props.busy) return
  disarm()
  emit('confirm', false)
}

function onConfirmForce() {
  if (props.busy) return
  showLockSheet.value = false
  emit('confirm', true)
}

function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (showLockSheet.value) showLockSheet.value = false
    if (armed.value) disarm()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  if (resetTimer) window.clearTimeout(resetTimer)
  window.removeEventListener('keydown', onKeyDown)
})
</script>

<template>
  <div class="morph-del-wrap" @click.stop>
    <Transition name="morph-pop" mode="out-in">
      <div v-if="busy" key="busy" class="morph-busy" :class="{ 'is-compact': compact }">
        <UiIcon name="spinner" :size="compact ? 13 : 13.5" class="spin-anim" />
        <span v-if="!compact">{{ busyLabel }}</span>
      </div>

      <div v-else-if="armed" key="armed" class="morph-armed">
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
      <Transition name="lens-modal">
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
              <button type="button" class="pill-btn lock-danger-btn" :disabled="busy" @click="onConfirmForce">
                <UiIcon v-if="busy" name="spinner" :size="13.5" class="spin-anim" />
                <span>强制删除</span>
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

@media (hover: hover) and (pointer: fine) {
  .morph-trigger:hover {
    color: var(--danger-fg);
    border-color: rgba(220, 38, 38, 0.32);
    background: var(--danger-bg);
  }
}

.morph-trigger:active {
  transform: scale(0.92);
}

.morph-busy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 31px;
  padding: 0 10px;
  border-radius: 9px;
  background: var(--danger-bg);
  border: 1px solid rgba(220, 38, 38, 0.28);
  color: var(--danger-fg);
  font-size: 0.74rem;
  font-weight: 620;
  white-space: nowrap;
}

.morph-busy.is-compact {
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 7px;
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
    transform 0.22s var(--ease-bounce),
    opacity 0.15s ease;
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
  overscroll-behavior: contain;
  padding:
    calc(18px + env(safe-area-inset-top, 0px))
    calc(18px + env(safe-area-inset-right, 0px))
    calc(18px + env(safe-area-inset-bottom, 0px))
    calc(18px + env(safe-area-inset-left, 0px));
}

@media (max-width: 860px), (orientation: landscape) and (max-height: 520px) and (pointer: coarse) {
  .morph-trigger {
    width: 38px;
    height: 38px;
    border-radius: 10px;
  }
  .morph-trigger.is-compact {
    width: 36px;
    height: 36px;
    border-radius: 9px;
  }
  .morph-busy {
    height: 36px;
    padding: 0 11px;
  }
  .morph-busy.is-compact {
    width: 36px;
    height: 36px;
  }
  .morph-armed {
    height: 36px;
    padding: 3px;
  }
  .morph-yes {
    padding: 0 10px;
    font-size: 0.78rem;
  }
  .morph-no {
    width: 30px;
    height: 30px;
  }
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
  font-size: 0.92rem;
  font-weight: 660;
  color: var(--ink-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lock-desc {
  margin: 0;
  font-size: 0.81rem;
  line-height: 1.55;
  color: var(--ink-2);
}

.lock-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 2px;
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
</style>
