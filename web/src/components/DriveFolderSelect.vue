<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useDropStore } from '../composables/useDropStore'
import { useBodyScrollLock } from '../composables/useMobileViewport'
import UiIcon from './UiIcon.vue'

const props = withDefaults(
  defineProps<{
    /** For per-file card mode: current gdrivePath if already saved, e.g. "2026-11/a.zip" or "a.zip" */
    savedPath?: string
    /** For composer preset mode: '' means private-only (not direct to Drive), '/' or 'sub' means direct to Drive */
    presetValue?: string
    mode?: 'file' | 'preset'
    busy?: boolean
  }>(),
  {
    savedPath: '',
    presetValue: '',
    mode: 'file',
    busy: false,
  },
)

const emit = defineEmits<{
  (e: 'select', folder: string): void
}>()

const store = useDropStore()
const triggerEl = ref<HTMLElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)
const isOpen = ref(false)
const isMobileOverlay = ref(false)
const creating = ref(false)
const newFolderDraft = ref('')
const menuStyle = ref<Record<string, string>>({})
useBodyScrollLock(computed(() => isOpen.value && isMobileOverlay.value))

const savedFolder = computed(() => {
  if (!props.savedPath) return ''
  const idx = props.savedPath.lastIndexOf('/')
  return idx > 0 ? props.savedPath.slice(0, idx) : '/'
})

const triggerLabel = computed(() => {
  if (props.busy) return '正在存入 Drive…'
  if (props.mode === 'preset') {
    if (!props.presetValue) return '直传 Drive：关'
    return props.presetValue === '/' ? '直传 Drive: /' : `直传 Drive: ${props.presetValue}`
  }
  if (props.savedPath) {
    return savedFolder.value === '/' ? '已存 Drive (/)' : `Drive: ${savedFolder.value}`
  }
  return '存 Drive'
})

const isHighlighted = computed(() =>
  props.mode === 'preset' ? Boolean(props.presetValue) : Boolean(props.savedPath),
)

function updatePosition() {
  const btn = triggerEl.value
  if (!btn) return
  const compact =
    window.innerWidth <= 720 ||
    (window.innerHeight <= 520 && window.matchMedia('(pointer: coarse)').matches)
  isMobileOverlay.value = compact
  if (compact) {
    menuStyle.value = {
      position: 'fixed',
      left: 'max(14px, calc(env(safe-area-inset-left, 0px) + 14px))',
      right: 'max(14px, calc(env(safe-area-inset-right, 0px) + 14px))',
      top: 'max(66px, calc(env(safe-area-inset-top, 0px) + 18px))',
    }
    return
  }
  const r = btn.getBoundingClientRect()
  const menuW = 228
  let left = r.right - menuW
  if (left < 10) left = 10
  if (left + menuW > window.innerWidth - 10) {
    left = window.innerWidth - menuW - 10
  }
  const spaceBelow = window.innerHeight - r.bottom
  if (spaceBelow < 240 && r.top > 240) {
    menuStyle.value = {
      position: 'fixed',
      left: `${left}px`,
      bottom: `${window.innerHeight - r.top + 6}px`,
      width: `${menuW}px`,
    }
  } else {
    menuStyle.value = {
      position: 'fixed',
      left: `${left}px`,
      top: `${r.bottom + 6}px`,
      width: `${menuW}px`,
    }
  }
}

function onWindowScroll(e: Event) {
  const t = e.target as Node | null
  if (t && menuEl.value?.contains(t)) return
  if (document.activeElement && menuEl.value?.contains(document.activeElement)) return
  closeMenu()
}

async function toggleOpen() {
  if (props.busy) return
  if (isOpen.value) {
    closeMenu()
    return
  }
  if (store.gdriveFolders.value.length <= 1) {
    void store.loadGDriveFolders()
  }
  isOpen.value = true
  await nextTick()
  updatePosition()
  window.addEventListener('pointerdown', onOutsidePointer, true)
  window.addEventListener('scroll', onWindowScroll, true)
  window.addEventListener('resize', updatePosition)
}

function closeMenu() {
  isOpen.value = false
  newFolderDraft.value = ''
  window.removeEventListener('pointerdown', onOutsidePointer, true)
  window.removeEventListener('scroll', onWindowScroll, true)
  window.removeEventListener('resize', updatePosition)
}

function onOutsidePointer(e: PointerEvent) {
  const t = e.target as Node | null
  if (!t) return
  if (triggerEl.value?.contains(t) || menuEl.value?.contains(t)) return
  closeMenu()
}

function pickFolder(folder: string) {
  closeMenu()
  emit('select', folder)
}

async function submitNewFolder() {
  const raw = newFolderDraft.value.trim()
  if (!raw || creating.value) return
  creating.value = true
  try {
    const created = await store.createGDriveFolder(raw)
    if (created) {
      closeMenu()
      emit('select', created)
    }
  } finally {
    creating.value = false
  }
}

onBeforeUnmount(() => {
  closeMenu()
})
</script>

<template>
  <button
    ref="triggerEl"
    type="button"
    class="pill-btn drive-sel-btn"
    :class="{ 'is-done': isHighlighted, 'is-open': isOpen }"
    :disabled="busy"
    @click.stop="toggleOpen"
  >
    <UiIcon
      :name="busy ? 'spinner' : savedPath ? 'check' : 'cloud'"
      :size="13.5"
      :class="{ 'spin-anim': busy }"
    />
    <span>{{ triggerLabel }}</span>
    <UiIcon v-if="!busy" name="chevron-down" :size="12" class="chev" :class="{ 'is-up': isOpen }" />
  </button>

  <Teleport to="body">
    <Transition name="pop-drop">
      <div v-if="isOpen" class="dp-layer">
        <div
          v-if="isMobileOverlay"
          class="dp-backdrop"
          @click.stop="closeMenu"
        />
        <div ref="menuEl" class="liquid-chrome drive-pop" :style="menuStyle" @click.stop>
          <div class="dp-head-row">
            <div class="dp-head mono">GOOGLE DRIVE · OArc-Drop</div>
            <button
              v-if="isMobileOverlay"
              type="button"
              class="dp-close-btn"
              aria-label="关闭目录选择"
              @click="closeMenu"
            >
              <UiIcon name="close" :size="13.5" />
            </button>
          </div>

          <div class="dp-list">
            <button
              v-if="mode === 'preset'"
              type="button"
              class="dp-item"
              :class="{ 'is-cur': !presetValue }"
              @click="pickFolder('')"
            >
              <span>不直传（默认私密互传）</span>
              <UiIcon v-if="!presetValue" name="check" :size="13" />
            </button>

            <button
              v-for="f in store.gdriveFolders.value"
              :key="f"
              type="button"
              class="dp-item"
              :class="{
                'is-cur': mode === 'preset' ? presetValue === f : savedFolder === f && Boolean(savedPath),
              }"
              @click="pickFolder(f)"
            >
              <span class="dp-item-l">
                <UiIcon name="folder" :size="13.5" />
                <span class="mono">{{ f === '/' ? '/ (根目录)' : f }}</span>
              </span>
              <UiIcon
                v-if="mode === 'preset' ? presetValue === f : savedFolder === f && Boolean(savedPath)"
                name="check"
                :size="13"
              />
            </button>
          </div>

          <form class="dp-create" @submit.prevent="submitNewFolder">
            <input
              v-model="newFolderDraft"
              placeholder="+ 新建子文件夹…"
              class="dp-input"
              enterkeyhint="done"
              :disabled="creating"
            />
            <button
              v-if="newFolderDraft.trim() || creating"
              type="submit"
              class="dp-add-btn"
              :disabled="creating"
            >
              <UiIcon v-if="creating" name="spinner" :size="12.5" class="spin-anim" />
              <span>{{ creating ? '创建中' : '新建' }}</span>
            </button>
          </form>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.drive-sel-btn {
  gap: 5px;
  padding: 0 10px;
  height: 31px;
  font-size: 0.76rem;
}

.drive-sel-btn.is-done {
  background: var(--emerald-bg);
  color: var(--emerald-fg);
  border-color: var(--emerald-border);
}

.chev {
  opacity: 0.65;
  transition: transform 0.2s var(--ease-spring);
}
.chev.is-up {
  transform: rotate(180deg);
}

.dp-backdrop {
  position: fixed;
  inset: 0;
  z-index: 9994;
  background: rgba(11, 13, 17, 0.36);
  -webkit-backdrop-filter: blur(6px);
  backdrop-filter: blur(6px);
}

.drive-pop {
  z-index: 9995;
  border-radius: 13px;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overscroll-behavior: contain;
}

.dp-head-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dp-close-btn {
  width: 28px;
  height: 28px;
  border-radius: 8px;
  border: 0;
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: inline-grid;
  place-items: center;
  cursor: pointer;
}

.dp-head {
  font-size: 0.64rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--ink-3);
  padding: 4px 8px 2px;
}

.dp-list {
  max-height: 188px;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.dp-item {
  border: 0;
  background: transparent;
  color: var(--ink-1);
  border-radius: 8px;
  padding: 7px 9px;
  font-size: 0.78rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  cursor: pointer;
  text-align: left;
}

.dp-item:hover {
  background: var(--surface-recessed-hover);
}

.dp-item.is-cur {
  background: var(--accent-soft);
  font-weight: 620;
}

.dp-item-l {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dp-create {
  border-top: 1px solid var(--border-hairline);
  padding-top: 5px;
  display: flex;
  align-items: center;
  gap: 4px;
}

.dp-input {
  flex: 1;
  min-width: 0;
  height: 29px;
  padding: 0 8px;
  border-radius: 7px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.76rem;
  outline: none;
}

.dp-input:focus {
  background: var(--surface-pill);
  border-color: var(--border-strong);
}

.dp-add-btn {
  height: 29px;
  padding: 0 9px;
  border-radius: 7px;
  border: 0;
  background: var(--cta-bg);
  color: var(--cta-fg);
  font-size: 0.73rem;
  font-weight: 620;
  cursor: pointer;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

@media (max-width: 720px), (max-height: 520px) and (pointer: coarse) {
  .drive-sel-btn {
    height: 36px;
    padding: 0 11px;
    font-size: 0.79rem;
  }
  .drive-pop {
    padding: 10px;
    border-radius: 16px;
    gap: 6px;
  }
  .dp-list {
    max-height: min(220px, calc(var(--vv-height, 100dvh) - 180px));
  }
  .dp-item {
    min-height: 40px;
    padding: 9px 11px;
    font-size: 0.84rem;
  }
  .dp-input {
    height: 38px;
    font-size: 16px;
  }
  .dp-add-btn {
    height: 38px;
    padding: 0 13px;
    font-size: 0.82rem;
  }
}

.pop-drop-enter-active,
.pop-drop-leave-active {
  transition:
    transform 0.2s var(--ease-spring),
    opacity 0.15s ease;
}
.pop-drop-enter-from,
.pop-drop-leave-to {
  opacity: 0;
  transform: translateY(-5px) scale(0.97);
}
</style>
