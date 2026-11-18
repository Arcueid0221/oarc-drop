<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  copyImagePixels,
  copyText,
  formatBytes,
  formatHtmlImage,
  formatMarkdownImage,
  formatTimeShort,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import type { GDriveFile, VaultObject } from '../types'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

type PoolKey = 'silo' | 'gdrive' | 'local'
const activePool = ref<PoolKey>('silo')
const viewMode = ref<'grid' | 'list'>('grid')
const activeFolder = ref('')
const search = ref('')
const loading = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)

// Lightbox & Rename dialog
const previewObj = ref<{
  name: string
  streamUrl: string
  publicUrl?: string
  size: number
  kind: string
  spec: string
} | null>(null)

const renameTarget = ref<VaultObject | null>(null)
const renameDraft = ref('')

const pools = computed(() => [
  { key: 'silo' as const, label: 'Silo 媒体/图床桶', enabled: Boolean(store.status.value?.siloEnabled) },
  { key: 'gdrive' as const, label: 'Google Drive', enabled: Boolean(store.status.value?.gdriveEnabled) },
  { key: 'local' as const, label: '本机暂存', enabled: true },
])

const activePoolIdx = computed(() => Math.max(0, pools.value.findIndex((p) => p.key === activePool.value)))

async function switchPool(key: PoolKey, enabled: boolean) {
  if (!enabled) {
    showToast(key === 'gdrive' ? '尚未配置 Google Drive' : '尚未连接 Silo')
    return
  }
  activePool.value = key
  activeFolder.value = ''
  if (key === 'silo' && store.siloObjects.value.length === 0) {
    await refreshCurrent()
  } else if (key === 'gdrive' && store.gdriveFiles.value.length === 0) {
    viewMode.value = 'list'
    await refreshCurrent()
  } else if (key === 'local') {
    viewMode.value = 'list'
  }
}

async function refreshCurrent() {
  loading.value = true
  try {
    if (activePool.value === 'silo') {
      await store.loadSiloVault(false)
    } else if (activePool.value === 'gdrive') {
      await store.loadGDriveFiles()
    } else {
      await store.loadInitial()
    }
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void refreshCurrent()
})

const filteredSilo = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.siloObjects.value.filter((it) => {
    if (activeFolder.value && it.folder !== activeFolder.value) return false
    if (q && !it.key.toLowerCase().includes(q)) return false
    return true
  })
})

const filteredDrive = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.gdriveFiles.value.filter((it) => {
    if (q && !it.path.toLowerCase().includes(q)) return false
    return true
  })
})

const localFileClips = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.clips.value.filter((c) => {
    if (c.kind === 'text') return false
    if (c.storage && c.storage !== 'local') return false
    if (q && !(c.fileName || '').toLowerCase().includes(q)) return false
    return true
  })
})

function onPickedVaultUpload(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return
  const prefix =
    activePool.value === 'silo'
      ? activeFolder.value && activeFolder.value !== '/'
        ? `${activeFolder.value.replace(/\/+$/, '')}/`
        : 'images/'
      : 'drop/'
  for (const f of Array.from(input.files)) {
    void store.uploadChunkedFile(f, {
      target: activePool.value,
      siloPrefix: prefix,
      ttlHours: 0,
      maxDownloads: 0,
      passcode: '',
      addToClip: true,
      skipShare: true,
      autoCopyFormat: activePool.value === 'silo' && f.type.startsWith('image/') ? 'md' : 'none',
    })
  }
  input.value = ''
}

async function shareVaultItem(storage: 'silo' | 'gdrive', key: string, fileName: string, size: number) {
  const res = await fetch('/api/shares/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ storage, key, fileName, size, ttlHours: 0 }),
  })
  if (res.ok) {
    const sh = await res.json()
    const url = shareFullUrl(sh.code)
    await copyText(url, '公开短链已生成并复制')
    store.openQrModal(fileName, url, '外部访客免登录扫码取件')
  }
}

function startRename(item: VaultObject) {
  renameTarget.value = item
  renameDraft.value = item.name
}

async function confirmRename() {
  const target = renameTarget.value
  if (!target) return
  const newName = renameDraft.value.trim()
  if (!newName || newName === target.name) {
    renameTarget.value = null
    return
  }
  let force = false
  if (target.protected) {
    force = window.confirm(
      `「${target.key}」位于博客生产目录，改名可能导致已发布文章/歌单引用失效，确定仍要强制改名吗？`,
    )
    if (!force) return
  }
  const res = await fetch('/api/silo/rename', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key: target.key, newName, force }),
  })
  if (res.ok) {
    renameTarget.value = null
    showToast('重命名成功')
    await store.loadSiloVault(false)
  } else {
    const err = await res.json().catch(() => ({ error: '重命名失败' }))
    showToast(err.error || '重命名失败')
  }
}

async function deleteSilo(item: VaultObject) {
  let force = false
  if (item.protected) {
    force = window.confirm(
      `「${item.key}」属于博客生产目录（非 images/ 或 drop/），删除会导致线上死链！确定强制永久删除吗？`,
    )
    if (!force) return
  } else if (!window.confirm(`确定从 Silo 删除「${item.name}」吗？`)) {
    return
  }
  const res = await fetch(
    `/api/silo/object?key=${encodeURIComponent(item.key)}&force=${force ? '1' : '0'}`,
    { method: 'DELETE' },
  )
  if (res.ok) {
    store.siloObjects.value = store.siloObjects.value.filter((x) => x.key !== item.key)
    showToast(`已删除：${item.name}`)
  } else {
    const err = await res.json().catch(() => ({ error: '删除失败' }))
    showToast(err.error || '删除失败')
  }
}

async function deleteDrive(item: GDriveFile) {
  if (!window.confirm(`确定从 Google Drive 删除「${item.name}」吗？`)) return
  const res = await fetch(`/api/gdrive/file?path=${encodeURIComponent(item.path)}`, {
    method: 'DELETE',
  })
  if (res.ok) {
    store.gdriveFiles.value = store.gdriveFiles.value.filter((x) => x.path !== item.path)
    showToast(`已从 Drive 删除：${item.name}`)
  } else {
    const err = await res.json().catch(() => ({ error: '删除失败' }))
    showToast(err.error || '删除失败')
  }
}

function openPreviewSilo(item: VaultObject) {
  previewObj.value = {
    name: item.name,
    streamUrl: item.streamUrl,
    publicUrl: item.publicUrl,
    size: item.size,
    kind: item.kind,
    spec: `${item.key} · ${formatBytes(item.size)} · ${formatTimeShort(item.lastModified)}`,
  }
}
</script>

<template>
  <div class="vault-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">文件柜</h1>
        <p class="pane-subtitle">
          Silo 全格式图床与资源桶 · Google Drive 隔离目录 · 本机暂存一站式浏览与删改
        </p>
      </div>

      <div class="pane-actions vault-top-acts">
        <div class="tab-pill-group" :style="{ gridTemplateColumns: 'repeat(3, 1fr)' }">
          <div
            class="tab-pill-indicator"
            :style="{
              width: 'calc((100% - 7px) / 3)',
              transform: `translateX(${activePoolIdx * 100}%)`,
            }"
          />
          <button
            v-for="p in pools"
            :key="p.key"
            type="button"
            class="tab-pill-btn"
            :class="{ 'is-active': activePool === p.key, 'is-disabled': !p.enabled }"
            @click="switchPool(p.key, p.enabled)"
          >
            {{ p.label }}
          </button>
        </div>
      </div>
    </header>

    <div class="pane-body vault-body">
      <!-- Toolbar: Search + Upload to Current Folder + View Toggle -->
      <div class="vault-toolbar">
        <div class="search-pill">
          <UiIcon name="search" :size="14.5" />
          <input v-model="search" placeholder="搜索文件名或目录…" />
        </div>

        <div class="vt-right">
          <button type="button" class="pill-btn pill-btn--accent" @click="uploadInput?.click()">
            <UiIcon name="plus" :size="14" />
            <span>上传到此库</span>
          </button>
          <input ref="uploadInput" type="file" multiple hidden @change="onPickedVaultUpload" />

          <div v-if="activePool === 'silo'" class="view-switch">
            <button
              type="button"
              class="icon-circle-btn"
              :class="{ 'is-on': viewMode === 'grid' }"
              title="画廊网格视图"
              @click="viewMode = 'grid'"
            >
              <UiIcon name="grid" :size="14.5" />
            </button>
            <button
              type="button"
              class="icon-circle-btn"
              :class="{ 'is-on': viewMode === 'list' }"
              title="全格式列表视图"
              @click="viewMode = 'list'"
            >
              <UiIcon name="list" :size="14.5" />
            </button>
          </div>

          <button type="button" class="icon-circle-btn" title="刷新列表" @click="refreshCurrent">
            <UiIcon name="refresh" :size="14.5" :class="{ 'is-spinning': loading }" />
          </button>
        </div>
      </div>

      <!-- Silo Folder Filter Chips -->
      <div v-if="activePool === 'silo' && store.siloFolders.value.length > 0" class="folder-pills">
        <button
          type="button"
          class="folder-pill"
          :class="{ 'is-on': activeFolder === '' }"
          @click="activeFolder = ''"
        >
          全部 ({{ store.siloObjects.value.length }})
        </button>
        <button
          v-for="f in store.siloFolders.value"
          :key="f"
          type="button"
          class="folder-pill mono"
          :class="{ 'is-on': activeFolder === f }"
          @click="activeFolder = f"
        >
          {{ f }}
        </button>
      </div>

      <!-- POOL 1: SILO -->
      <template v-if="activePool === 'silo'">
        <div v-if="filteredSilo.length === 0" class="glass-box empty-state">
          当前筛选下暂无对象
        </div>

        <!-- Grid Mode (Image-Host Gallery + Visual Cards for non-images!) -->
        <div v-else-if="viewMode === 'grid'" class="lens-grid">
          <article
            v-for="obj in filteredSilo"
            :key="obj.key"
            class="lens-tile"
            @click="openPreviewSilo(obj)"
          >
            <img
              v-if="obj.kind === 'image'"
              :src="obj.thumbUrl || obj.streamUrl"
              :alt="obj.name"
              loading="lazy"
            />
            <div v-else class="non-img-tile">
              <UiIcon :name="obj.kind === 'audio' ? 'disc' : 'file'" :size="30" />
              <span class="ext-tag mono">{{ obj.ext || 'BIN' }}</span>
            </div>

            <div class="tile-scrim" @click.stop>
              <div class="tile-info" @click="openPreviewSilo(obj)">
                <div class="tile-name">
                  <UiIcon v-if="obj.protected" name="lock" :size="11" class="inline-lock" />
                  <span>{{ obj.name }}</span>
                </div>
                <div class="tile-meta mono">{{ obj.folder }} · {{ formatBytes(obj.size) }}</div>
              </div>

              <div class="tile-btns">
                <CopyPill
                  v-if="obj.publicUrl && obj.kind === 'image'"
                  :text="formatMarkdownImage(obj.name, obj.publicUrl)"
                  label="MD"
                  copied-label="已复制"
                  compact
                />
                <CopyPill
                  v-if="obj.publicUrl"
                  :text="obj.publicUrl"
                  label="URL"
                  copied-label="已复制"
                  compact
                />
                <a class="scrim-circle" :href="`${obj.streamUrl}&download=1`" title="下载">
                  <UiIcon name="download" :size="13" />
                </a>
                <button
                  type="button"
                  class="scrim-circle"
                  title="重命名"
                  @click="startRename(obj)"
                >
                  <UiIcon name="edit" :size="12.5" />
                </button>
                <button
                  type="button"
                  class="scrim-circle danger"
                  title="删除"
                  @click="deleteSilo(obj)"
                >
                  <UiIcon name="trash" :size="12.5" />
                </button>
              </div>
            </div>
          </article>
        </div>

        <!-- List Mode (Crisp Finder-style rows for all 597+ files!) -->
        <div v-else class="file-list">
          <div v-for="obj in filteredSilo" :key="obj.key" class="glass-box file-row">
            <div class="fr-main" @click="openPreviewSilo(obj)">
              <img
                v-if="obj.kind === 'image'"
                :src="obj.thumbUrl || obj.streamUrl"
                class="fr-thumb"
                alt=""
                loading="lazy"
              />
              <div v-else class="fr-icon">
                <UiIcon :name="obj.kind === 'audio' ? 'disc' : 'file'" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">
                  <UiIcon v-if="obj.protected" name="lock" :size="12" class="lock-muted" />
                  <span>{{ obj.name }}</span>
                </div>
                <div class="fr-sub mono">
                  {{ obj.folder }} · {{ formatBytes(obj.size) }} · {{ formatTimeShort(obj.lastModified) }}
                </div>
              </div>
            </div>

            <div class="fr-acts">
              <CopyPill
                v-if="obj.publicUrl && obj.kind === 'image'"
                :text="formatMarkdownImage(obj.name, obj.publicUrl)"
                label="MD"
                copied-label="已复制"
                compact
              />
              <CopyPill
                v-if="obj.publicUrl"
                :text="obj.publicUrl"
                label="直链"
                copied-label="已复制"
                compact
              />
              <a class="mini-act" :href="`${obj.streamUrl}&download=1`" title="下载">
                <UiIcon name="download" :size="14" />
              </a>
              <button
                type="button"
                class="mini-act"
                title="生成公开取件短链 + 二维码"
                @click="shareVaultItem('silo', obj.key, obj.name, obj.size)"
              >
                <UiIcon name="qr" :size="14" />
              </button>
              <button type="button" class="mini-act" title="重命名" @click="startRename(obj)">
                <UiIcon name="edit" :size="14" />
              </button>
              <button type="button" class="mini-act is-danger" title="删除" @click="deleteSilo(obj)">
                <UiIcon name="trash" :size="14" />
              </button>
            </div>
          </div>
        </div>
      </template>

      <!-- POOL 2: GOOGLE DRIVE -->
      <template v-else-if="activePool === 'gdrive'">
        <div v-if="filteredDrive.length === 0" class="glass-box empty-state">
          `gdrive:OArc-Drop` 目录暂无文件 · 点右上角「上传到此库」即可通过 20 MB 切片直传 Google Drive
        </div>
        <div v-else class="file-list">
          <div v-for="gd in filteredDrive" :key="gd.path" class="glass-box file-row">
            <div class="fr-main">
              <div class="fr-icon">
                <UiIcon name="cloud" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">{{ gd.name }}</div>
                <div class="fr-sub mono">
                  {{ gd.path }} · {{ formatBytes(gd.size) }} · {{ formatTimeShort(gd.lastModified) }}
                </div>
              </div>
            </div>
            <div class="fr-acts">
              <a
                class="pill-btn pill-btn--mini"
                :href="`${gd.streamUrl}&size=${gd.size}&download=1`"
              >
                <UiIcon name="download" :size="13.5" />
                <span>下载回本机</span>
              </a>
              <button
                type="button"
                class="mini-act"
                title="生成公开分享短链 + 二维码"
                @click="shareVaultItem('gdrive', gd.path, gd.name, gd.size)"
              >
                <UiIcon name="qr" :size="14" />
              </button>
              <button type="button" class="mini-act is-danger" title="从 Drive 删除" @click="deleteDrive(gd)">
                <UiIcon name="trash" :size="14" />
              </button>
            </div>
          </div>
        </div>
      </template>

      <!-- POOL 3: LOCAL PRIVATE FILES -->
      <template v-else>
        <div v-if="localFileClips.length === 0" class="glass-box empty-state">
          本机暂存目录暂无文件
        </div>
        <div v-else class="file-list">
          <div v-for="lc in localFileClips" :key="lc.id" class="glass-box file-row">
            <div class="fr-main">
              <div class="fr-icon">
                <UiIcon :name="lc.kind === 'image' ? 'image' : 'file'" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">{{ lc.fileName }}</div>
                <div class="fr-sub mono">
                  本机私密暂存 · {{ formatBytes(lc.size) }} · {{ formatTimeShort(lc.createdAt) }}
                </div>
              </div>
            </div>
            <div class="fr-acts">
              <a class="pill-btn pill-btn--mini" :href="`/api/clips/${lc.id}/raw?download=1`">
                <UiIcon name="download" :size="13.5" />
                <span>直下</span>
              </a>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Rename Modal -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="renameTarget" class="modal-backdrop" @click.self="renameTarget = null">
          <div class="liquid-chrome rename-box">
            <div class="rn-title">重命名对象</div>
            <div class="rn-sub mono">{{ renameTarget.folder }}</div>
            <input
              v-model="renameDraft"
              class="rn-input mono"
              @keydown.enter="confirmRename"
            />
            <div class="rn-btns">
              <button type="button" class="pill-btn" @click="renameTarget = null">取消</button>
              <button type="button" class="pill-btn pill-btn--accent" @click="confirmRename">
                确认改名
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Preview Lightbox (Image / Video / Audio / PDF) -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="previewObj" class="modal-backdrop" @click.self="previewObj = null">
          <div class="liquid-chrome preview-dialog">
            <button type="button" class="icon-circle-btn pd-close" @click="previewObj = null">
              <UiIcon name="close" :size="16" />
            </button>
            <div class="pd-stage">
              <img v-if="previewObj.kind === 'image'" :src="previewObj.streamUrl" :alt="previewObj.name" />
              <video v-else-if="previewObj.kind === 'video'" controls :src="previewObj.streamUrl" />
              <audio v-else-if="previewObj.kind === 'audio'" controls :src="previewObj.streamUrl" />
              <iframe
                v-else-if="previewObj.name.toLowerCase().endsWith('.pdf')"
                :src="previewObj.streamUrl"
                class="pd-pdf"
                title="Preview"
              />
              <div v-else class="pd-fallback">
                <UiIcon name="file" :size="40" />
                <div>{{ previewObj.name }}</div>
              </div>
            </div>
            <footer class="pd-bar">
              <span class="mono">{{ previewObj.spec }}</span>
              <div class="pd-acts">
                <button
                  v-if="previewObj.kind === 'image'"
                  type="button"
                  class="pill-btn"
                  @click="copyImagePixels(previewObj.streamUrl)"
                >
                  <UiIcon name="copy" :size="14" />
                  <span>复制原图</span>
                </button>
                <CopyPill
                  v-if="previewObj.publicUrl && previewObj.kind === 'image'"
                  :text="formatMarkdownImage(previewObj.name, previewObj.publicUrl)"
                  label="Markdown"
                  copied-label="已复制"
                />
                <CopyPill
                  v-if="previewObj.publicUrl && previewObj.kind === 'image'"
                  :text="formatHtmlImage(previewObj.name, previewObj.publicUrl)"
                  label="HTML"
                  copied-label="已复制"
                />
                <CopyPill
                  v-if="previewObj.publicUrl"
                  :text="previewObj.publicUrl"
                  label="复制直链"
                  copied-label="已复制"
                />
                <a class="pill-btn" :href="`${previewObj.streamUrl}&download=1`">
                  <UiIcon name="download" :size="14" />
                  <span>下载</span>
                </a>
              </div>
            </footer>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.vault-top-acts .tab-pill-group {
  min-width: 288px;
}

.vault-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.vault-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.vt-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.view-switch {
  display: flex;
  gap: 4px;
}

.icon-circle-btn.is-on {
  background: var(--accent-soft);
  color: var(--accent);
}

.pill-btn--accent {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: rgba(0, 113, 227, 0.25);
  font-weight: 610;
}

.pill-btn--mini {
  padding: 4px 10px;
  font-size: 0.75rem;
}

.folder-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.folder-pill {
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.75rem;
  cursor: pointer;
}

.folder-pill.is-on {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: rgba(0, 113, 227, 0.28);
  font-weight: 620;
}

.empty-state {
  padding: 48px 20px;
  border-radius: 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

.lens-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  gap: 14px;
}

.lens-tile {
  position: relative;
  aspect-ratio: 4 / 3;
  border-radius: 18px;
  overflow: hidden;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  cursor: pointer;
}

.lens-tile img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.non-img-tile {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--ink-3);
}

.ext-tag {
  font-size: 0.72rem;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--surface-card);
}

.tile-scrim {
  position: absolute;
  inset: auto 0 0 0;
  padding: 24px 11px 10px;
  background: linear-gradient(to top, rgba(15, 23, 42, 0.82), rgba(15, 23, 42, 0.35) 68%, transparent);
  color: #fff;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tile-name {
  font-size: 0.78rem;
  font-weight: 620;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: flex;
  align-items: center;
  gap: 4px;
}

.inline-lock {
  opacity: 0.8;
}

.tile-meta {
  font-size: 0.68rem;
  opacity: 0.78;
}

.tile-btns {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px;
}

.scrim-circle {
  width: 25px;
  height: 25px;
  border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.25);
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
}

.scrim-circle.danger:hover {
  background: rgba(225, 29, 72, 0.75);
}

.file-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.file-row {
  padding: 10px 14px;
  border-radius: 16px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.fr-main {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
  flex: 1;
  cursor: pointer;
}

.fr-thumb {
  width: 38px;
  height: 38px;
  border-radius: 9px;
  object-fit: cover;
  flex-shrink: 0;
}

.fr-icon {
  width: 38px;
  height: 38px;
  border-radius: 9px;
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  flex-shrink: 0;
}

.fr-text {
  min-width: 0;
}

.fr-name {
  font-size: 0.85rem;
  font-weight: 620;
  display: flex;
  align-items: center;
  gap: 5px;
  word-break: break-all;
}

.lock-muted {
  color: var(--ink-3);
}

.fr-sub {
  margin-top: 2px;
  font-size: 0.72rem;
  color: var(--ink-3);
}

.fr-acts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.mini-act {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
}

.mini-act.is-danger:hover {
  color: var(--danger-fg);
}

.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 250;
  background: rgba(15, 23, 42, 0.45);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  padding: 20px;
}

.rename-box {
  width: min(380px, 100%);
  padding: 20px;
  border-radius: 24px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rn-title {
  font-size: 0.95rem;
  font-weight: 650;
}

.rn-sub {
  font-size: 0.73rem;
  color: var(--ink-3);
}

.rn-input {
  width: 100%;
  padding: 8px 11px;
  border-radius: 12px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.84rem;
  outline: none;
}

.rn-btns {
  margin-top: 4px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.preview-dialog {
  width: min(920px, 100%);
  max-height: 90vh;
  padding: 16px;
  border-radius: 26px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  position: relative;
}

.pd-close {
  position: absolute;
  top: 14px;
  right: 14px;
  z-index: 2;
}

.pd-stage {
  flex: 1;
  min-height: 0;
  display: grid;
  place-items: center;
  overflow: hidden;
}

.pd-stage img,
.pd-stage video {
  max-width: 100%;
  max-height: 72vh;
  border-radius: 14px;
  object-fit: contain;
}

.pd-pdf {
  width: 100%;
  height: 72vh;
  border: none;
  border-radius: 12px;
}

.pd-fallback {
  padding: 48px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  color: var(--ink-2);
}

.pd-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 0.76rem;
  color: var(--ink-2);
}

.pd-acts {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

@media (max-width: 720px) {
  .vault-top-acts .tab-pill-group {
    min-width: 0;
    width: 100%;
  }
}
</style>
