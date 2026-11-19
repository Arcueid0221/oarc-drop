<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  describeSiloFolder,
  formatBytes,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import { useBodyScrollLock } from '../composables/useMobileViewport'
import UiIcon from './UiIcon.vue'

export interface VaultTransferSource {
  storage: 'silo' | 'gdrive' | 'local'
  key: string
  name: string
  folder?: string
  size?: number
  protected?: boolean
}

const props = withDefaults(
  defineProps<{
    items: VaultTransferSource[]
    initialMode?: 'copy' | 'move'
  }>(),
  {
    initialMode: 'copy',
  },
)

const emit = defineEmits<{
  (e: 'close'): void
  (
    e: 'completed',
    payload: {
      mode: 'copy' | 'move'
      succeeded: number
      dstStorage: 'silo' | 'gdrive' | 'local'
      dstFolder: string
    },
  ): void
}>()

const store = useDropStore()
useBodyScrollLock(ref(true))

const mode = ref<'copy' | 'move'>(props.initialMode)
const dstStorage = ref<'silo' | 'gdrive' | 'local'>('silo')
const dstSiloFolder = ref<string>('images')
const dstDriveFolder = ref<string>('/')

const newFolderDraft = ref('')
const creatingFolder = ref(false)

const running = ref(false)
const doneCount = ref(0)
const errorText = ref('')

// Smart initial destination: pick a different pool from source when possible so 1-click cross-pool feels natural
function initDefaults() {
  mode.value = props.initialMode
  const first = props.items[0]
  const siloOk = Boolean(store.status.value?.siloEnabled)
  const gdOk = Boolean(store.status.value?.gdriveEnabled)

  if (first?.storage === 'silo') {
    dstStorage.value = gdOk ? 'gdrive' : 'silo'
  } else if (first?.storage === 'gdrive') {
    dstStorage.value = siloOk ? 'silo' : 'local'
  } else {
    dstStorage.value = siloOk ? 'silo' : gdOk ? 'gdrive' : 'local'
  }

  if (siloOk && store.siloFolders.value.length === 0) {
    void store.loadSiloVault(false)
  }
  if (gdOk && store.gdriveFolders.value.length <= 1) {
    void store.loadGDriveFolders()
  }
}

watch(
  () => props.items,
  () => initDefaults(),
  { immediate: true },
)

const totalBytes = computed(() =>
  props.items.reduce((acc, it) => acc + (it.size || 0), 0),
)

const hasProtectedMove = computed(
  () => mode.value === 'move' && props.items.some((it) => Boolean(it.protected)),
)

const poolOptions = computed(() => [
  {
    key: 'silo' as const,
    label: 'Silo 存储桶',
    enabled: Boolean(store.status.value?.siloEnabled),
  },
  {
    key: 'gdrive' as const,
    label: 'Google Drive',
    enabled: Boolean(store.status.value?.gdriveEnabled),
  },
  {
    key: 'local' as const,
    label: '私密互传',
    enabled: true,
  },
])

const siloFolderChoices = computed(() => {
  const presets = ['images', 'drop', 'videos']
  const merged = new Set<string>(presets)
  for (const f of store.siloFolders.value) {
    const clean = (f || '').replace(/^\/+|\/+$/g, '')
    if (!clean || clean === '/' || clean.endsWith('/thumbs') || clean === 'thumbs') continue
    merged.add(clean)
  }
  const rest = Array.from(merged)
    .filter((f) => !presets.includes(f))
    .sort((a, b) => a.localeCompare(b))
  return [...presets, ...rest]
})

const driveFolderChoices = computed(() => {
  const set = new Set<string>(['/'])
  for (const f of store.gdriveFolders.value) set.add(f || '/')
  return ['/', ...Array.from(set).filter((f) => f !== '/').sort((a, b) => a.localeCompare(b))]
})

const activeDstFolder = computed(() => {
  if (dstStorage.value === 'silo') return dstSiloFolder.value
  if (dstStorage.value === 'gdrive') return dstDriveFolder.value
  return ''
})

function storageLabel(s: 'silo' | 'gdrive' | 'local'): string {
  if (s === 'silo') return 'Silo 存储桶'
  if (s === 'gdrive') return 'Google Drive'
  return '私密互传'
}

async function handleCreateSubfolder() {
  const raw = newFolderDraft.value.trim().replace(/^\/+|\/+$/g, '')
  if (!raw || creatingFolder.value) return
  creatingFolder.value = true
  try {
    if (dstStorage.value === 'silo') {
      const created = await store.createSiloFolder(raw)
      if (created) {
        dstSiloFolder.value = created
        newFolderDraft.value = ''
      }
    } else if (dstStorage.value === 'gdrive') {
      const created = await store.createGDriveFolder(raw)
      if (created) {
        dstDriveFolder.value = created
        newFolderDraft.value = ''
      }
    }
  } finally {
    creatingFolder.value = false
  }
}

async function executeTransfer() {
  if (running.value || props.items.length === 0) return
  running.value = true
  doneCount.value = 0
  errorText.value = ''

  const touchedPools = new Set<'silo' | 'gdrive' | 'local'>([dstStorage.value])
  let succeeded = 0
  let firstErr = ''

  try {
    for (const item of props.items) {
      if (mode.value === 'move') touchedPools.add(item.storage)
      const res = await store.transferVaultItem({
        srcStorage: item.storage,
        srcKey: item.key,
        dstStorage: dstStorage.value,
        dstFolder: activeDstFolder.value,
        move: mode.value === 'move',
        force: Boolean(item.protected),
        silent: true,
      })
      if (res.ok) {
        succeeded++
      } else if (!firstErr && res.error) {
        firstErr = res.error
      }
      doneCount.value++
    }

    // Refresh all affected storage pools in parallel
    const jobs: Array<Promise<unknown>> = []
    if (touchedPools.has('silo')) {
      jobs.push(store.loadSiloVault(false), store.loadMedia(true))
    }
    if (touchedPools.has('gdrive')) {
      jobs.push(store.loadGDriveFiles(), store.loadGDriveFolders())
    }
    if (touchedPools.has('local') || mode.value === 'move') {
      jobs.push(store.loadInitial())
    }
    await Promise.all(jobs)

    if (succeeded === props.items.length) {
      const actionLabel = mode.value === 'move' ? '已移动' : '已复制'
      const targetDesc =
        dstStorage.value === 'local'
          ? '私密互传'
          : `${storageLabel(dstStorage.value)} · ${activeDstFolder.value}`
      showToast(
        props.items.length === 1
          ? `${actionLabel}至 ${targetDesc}`
          : `${actionLabel} ${succeeded} 个文件至 ${targetDesc}`,
      )
      emit('completed', {
        mode: mode.value,
        succeeded,
        dstStorage: dstStorage.value,
        dstFolder: activeDstFolder.value,
      })
    } else {
      errorText.value = `成功 ${succeeded}/${props.items.length} 项${firstErr ? `：${firstErr}` : ''}`
      if (succeeded > 0) {
        emit('completed', {
          mode: mode.value,
          succeeded,
          dstStorage: dstStorage.value,
          dstFolder: activeDstFolder.value,
        })
      }
    }
  } finally {
    running.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && !running.value) {
    emit('close')
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="vc-fade">
      <div class="vc-backdrop" @click.self="!running && emit('close')">
        <div class="liquid-chrome vc-dialog" @click.stop>
          <div class="sheet-grabber" aria-hidden="true" />
          <header class="vc-head">
            <div class="vc-head-left">
              <div class="vc-title">
                {{ mode === 'move' ? '移动文件到目标目录' : '复制文件到其他容器目录' }}
              </div>
              <div class="vc-sub">
                <template v-if="items.length === 1">
                  {{ items[0].name }} · 来源：{{ storageLabel(items[0].storage) }}
                </template>
                <template v-else>
                  已选 {{ items.length }} 个文件 · 合计 {{ formatBytes(totalBytes) }}
                </template>
              </div>
            </div>
            <button
              type="button"
              class="icon-circle-btn"
              :disabled="running"
              @click="emit('close')"
            >
              <UiIcon name="close" :size="15" />
            </button>
          </header>

          <!-- Mode Segmented Switch -->
          <div class="vc-mode-switch" role="group" aria-label="操作方式">
            <button
              type="button"
              class="vc-mode-btn"
              :class="{ 'is-on': mode === 'copy' }"
              :disabled="running"
              @click="mode = 'copy'"
            >
              <UiIcon name="copy" :size="13.5" />
              <span>复制<span class="vc-mode-note">（保留原件）</span></span>
            </button>
            <button
              type="button"
              class="vc-mode-btn"
              :class="{ 'is-on': mode === 'move' }"
              :disabled="running"
              @click="mode = 'move'"
            >
              <UiIcon name="move" :size="14" />
              <span>移动<span class="vc-mode-note">（清理原件）</span></span>
            </button>
          </div>

          <!-- Destination Storage Pool Selector -->
          <div class="vc-section">
            <div class="vc-label">1. 选择目标存储容器</div>
            <div class="vc-pools">
              <button
                v-for="p in poolOptions"
                :key="p.key"
                type="button"
                class="vc-pool-btn"
                :class="{ 'is-on': dstStorage === p.key, 'is-off': !p.enabled }"
                :disabled="!p.enabled || running"
                @click="dstStorage = p.key"
              >
                <UiIcon
                  :name="p.key === 'silo' ? 'layers' : p.key === 'gdrive' ? 'cloud' : 'folder'"
                  :size="14"
                />
                <span>{{ p.label }}</span>
              </button>
            </div>
          </div>

          <!-- Destination Specific Folder Selector -->
          <div class="vc-section">
            <div class="vc-label">
              {{ dstStorage === 'local' ? '2. 目标位置说明' : '2. 选择具体目标文件夹' }}
            </div>

            <!-- Silo Folders -->
            <template v-if="dstStorage === 'silo'">
              <div class="vc-folder-list">
                <button
                  v-for="f in siloFolderChoices"
                  :key="f"
                  type="button"
                  class="vc-folder-row"
                  :class="{ 'is-cur': dstSiloFolder === f }"
                  :disabled="running"
                  @click="dstSiloFolder = f"
                >
                  <div class="vcf-left">
                    <UiIcon name="folder" :size="14" />
                    <span class="vcf-desc">{{ describeSiloFolder(f) }}</span>
                    <span class="vcf-path mono">{{ f }}/</span>
                  </div>
                  <UiIcon v-if="dstSiloFolder === f" name="check" :size="14" />
                </button>
              </div>

              <form class="vc-new-dir" @submit.prevent="handleCreateSubfolder">
                <input
                  v-model="newFolderDraft"
                  class="vc-dir-input mono"
                  placeholder="+ 新建 Silo 目录（如 images/memes 或 drop/归档）"
                  :disabled="creatingFolder || running"
                />
                <button
                  v-if="newFolderDraft.trim() || creatingFolder"
                  type="submit"
                  class="pill-btn pill-btn--solid vc-mk-btn"
                  :disabled="creatingFolder || running"
                >
                  <UiIcon v-if="creatingFolder" name="spinner" :size="13" class="spin-anim" />
                  <span>{{ creatingFolder ? '创建中' : '新建并选中' }}</span>
                </button>
              </form>
            </template>

            <!-- Google Drive Folders -->
            <template v-else-if="dstStorage === 'gdrive'">
              <div class="vc-folder-list">
                <button
                  v-for="df in driveFolderChoices"
                  :key="df"
                  type="button"
                  class="vc-folder-row"
                  :class="{ 'is-cur': dstDriveFolder === df }"
                  :disabled="running"
                  @click="dstDriveFolder = df"
                >
                  <div class="vcf-left">
                    <UiIcon name="folder" :size="14" />
                    <span class="vcf-desc">{{ df === '/' ? 'OArc-Drop 根目录' : df }}</span>
                    <span class="vcf-path mono">
                      {{ df === '/' ? 'OArc-Drop/' : `OArc-Drop/${df}/` }}
                    </span>
                  </div>
                  <UiIcon v-if="dstDriveFolder === df" name="check" :size="14" />
                </button>
              </div>

              <form class="vc-new-dir" @submit.prevent="handleCreateSubfolder">
                <input
                  v-model="newFolderDraft"
                  class="vc-dir-input"
                  placeholder="+ 新建 Drive 子文件夹（如 2026-备份）"
                  :disabled="creatingFolder || running"
                />
                <button
                  v-if="newFolderDraft.trim() || creatingFolder"
                  type="submit"
                  class="pill-btn pill-btn--solid vc-mk-btn"
                  :disabled="creatingFolder || running"
                >
                  <UiIcon v-if="creatingFolder" name="spinner" :size="13" class="spin-anim" />
                  <span>{{ creatingFolder ? '创建中' : '新建并选中' }}</span>
                </button>
              </form>
            </template>

            <!-- Local Private Pool -->
            <div v-else class="vc-local-note">
              将直接由服务端流式写入本机「私密互传」池，同步出现在收发台中（仅通过 Cloudflare Access 验证的本人设备可见，无公网暴露）。
            </div>
          </div>

          <div v-if="hasProtectedMove" class="vc-warn">
            注意：选中项包含博客或音乐生产目录加锁文件，确认移动将移除原目录文件（已有公开短链会自动跟随新位置）。
          </div>

          <div v-if="errorText" class="vc-warn">{{ errorText }}</div>

          <!-- Progress Bar during Batch Transfer -->
          <div v-if="running" class="vc-progress-wrap">
            <div class="vc-progress-bar">
              <div
                class="vc-progress-fill"
                :style="{ width: `${Math.round((doneCount / Math.max(1, items.length)) * 100)}%` }"
              />
            </div>
            <span class="vc-progress-txt mono">
              {{ mode === 'move' ? '移动中' : '复制中' }} {{ doneCount }}/{{ items.length }}
            </span>
          </div>

          <footer class="vc-foot">
            <button type="button" class="pill-btn" :disabled="running" @click="emit('close')">
              取消
            </button>
            <button
              type="button"
              class="pill-btn pill-btn--solid"
              :disabled="running || items.length === 0"
              @click="executeTransfer"
            >
              <UiIcon v-if="running" name="spinner" :size="14" class="spin-anim" />
              <UiIcon v-else :name="mode === 'move' ? 'move' : 'copy'" :size="14" />
              <span>
                {{
                  running
                    ? `${mode === 'move' ? '正在移动' : '正在复制'}…`
                    : mode === 'move'
                      ? hasProtectedMove
                        ? '强制移动到此目录'
                        : '确认移动到此目录'
                      : '确认复制到此目录'
                }}
              </span>
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.vc-backdrop {
  position: fixed;
  inset: 0;
  z-index: 265;
  background: rgba(15, 23, 42, 0.48);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  overscroll-behavior: contain;
  padding:
    calc(16px + env(safe-area-inset-top, 0px))
    calc(16px + env(safe-area-inset-right, 0px))
    calc(16px + env(safe-area-inset-bottom, 0px))
    calc(16px + env(safe-area-inset-left, 0px));
}

.vc-dialog {
  width: min(468px, 100%);
  max-height: calc(100dvh - 32px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px));
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  border-radius: 22px;
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 13px;
}

.vc-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.vc-head-left {
  min-width: 0;
}

.vc-title {
  font-size: 0.98rem;
  font-weight: 660;
  color: var(--ink-1);
}

.vc-sub {
  margin-top: 3px;
  font-size: 0.74rem;
  color: var(--ink-3);
  word-break: break-word;
}

.vc-mode-switch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 5px;
  padding: 3px;
  border-radius: 11px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
}

.vc-mode-btn {
  height: 33px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--ink-2);
  font-size: 0.77rem;
  font-weight: 580;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  transition: all 0.18s ease;
}

.vc-mode-btn.is-on {
  background: var(--surface-pill);
  color: var(--ink-1);
  font-weight: 650;
  box-shadow: var(--shadow-pill);
}

.vc-section {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.vc-label {
  font-size: 0.73rem;
  font-weight: 630;
  color: var(--ink-2);
}

.vc-pools {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
}

.vc-pool-btn {
  height: 36px;
  padding: 0 8px;
  border-radius: 10px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.76rem;
  font-weight: 580;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.16s ease;
}

.vc-pool-btn.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.vc-pool-btn.is-off {
  opacity: 0.38;
  cursor: not-allowed;
}

.vc-folder-list {
  max-height: 204px;
  overflow-y: auto;
  border-radius: 12px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.vc-folder-row {
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--ink-1);
  padding: 8px 10px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  cursor: pointer;
  text-align: left;
}

.vc-folder-row:hover {
  background: var(--surface-recessed-hover);
}

.vc-folder-row.is-cur {
  background: var(--surface-pill);
  box-shadow: var(--shadow-pill);
  font-weight: 630;
}

.vcf-left {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  overflow: hidden;
}

.vcf-desc {
  font-size: 0.79rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.vcf-path {
  font-size: 0.68rem;
  color: var(--ink-3);
  white-space: nowrap;
}

.vc-new-dir {
  display: flex;
  align-items: center;
  gap: 6px;
}

.vc-dir-input {
  flex: 1;
  min-width: 0;
  height: 34px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.78rem;
  outline: none;
}

.vc-dir-input:focus {
  background: var(--surface-pill);
  border-color: var(--border-strong);
}

.vc-mk-btn {
  height: 34px;
  font-size: 0.75rem;
  padding: 0 11px;
  flex-shrink: 0;
}

.vc-local-note {
  padding: 11px 12px;
  border-radius: 11px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  font-size: 0.77rem;
  line-height: 1.48;
  color: var(--ink-2);
}

.vc-warn {
  padding: 8px 11px;
  border-radius: 10px;
  background: var(--danger-bg);
  color: var(--danger-fg);
  font-size: 0.75rem;
  line-height: 1.45;
}

.vc-progress-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
}

.vc-progress-bar {
  flex: 1;
  height: 6px;
  border-radius: 999px;
  background: var(--surface-recessed);
  overflow: hidden;
}

.vc-progress-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--cta-bg);
  transition: width 0.2s ease;
}

.vc-progress-txt {
  font-size: 0.71rem;
  color: var(--ink-2);
}

.vc-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 2px;
}

@media (max-width: 720px), (max-height: 520px) and (pointer: coarse) {
  .vc-backdrop {
    display: flex;
    align-items: flex-end;
    justify-content: center;
    padding: calc(16px + env(safe-area-inset-top, 0px)) 0 0 0;
    -webkit-backdrop-filter: blur(6px);
    backdrop-filter: blur(6px);
  }

  .vc-dialog {
    width: 100%;
    max-height: calc(var(--vv-height, 100dvh) - 18px - env(safe-area-inset-top, 0px));
    border-radius: 22px 22px 0 0;
    border-bottom: 0;
    padding:
      12px
      calc(16px + env(safe-area-inset-right, 0px))
      calc(18px + env(safe-area-inset-bottom, 0px) + var(--kb-inset, 0px))
      calc(16px + env(safe-area-inset-left, 0px));
  }

  .vc-mode-btn {
    height: 38px;
    font-size: 0.8rem;
  }

  .vc-pools {
    grid-template-columns: repeat(3, 1fr);
    gap: 6px;
  }

  .vc-pool-btn {
    height: 40px;
    padding: 0 6px;
    font-size: 0.74rem;
  }

  .vc-folder-row {
    min-height: 42px;
    padding: 9px 11px;
  }

  .vc-dir-input {
    font-size: 16px;
    height: 40px;
  }

  .vc-mk-btn {
    height: 40px;
    padding: 0 12px;
  }

  .vc-foot {
    display: grid;
    grid-template-columns: 0.9fr 1.6fr;
    gap: 8px;
  }

  .vc-foot > .pill-btn {
    width: 100%;
    height: 42px;
  }
}

@media (max-width: 400px) {
  .vc-mode-note {
    display: none;
  }
  .vc-pools {
    grid-template-columns: 1fr;
  }
  .vc-pool-btn {
    justify-content: flex-start;
    padding: 0 12px;
    font-size: 0.8rem;
  }
}

.vc-fade-enter-active,
.vc-fade-leave-active {
  transition: opacity 0.18s ease, transform 0.24s var(--ease-spring);
}
.vc-fade-enter-from,
.vc-fade-leave-to {
  opacity: 0;
  transform: scale(0.96);
}
</style>
