<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  copyImagePixels,
  copyText,
  formatBytes,
  formatMarkdownImage,
  formatTimeShort,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import type { ClipItem } from '../types'
import CopyPill from './CopyPill.vue'
import DriveFolderSelect from './DriveFolderSelect.vue'
import MorphDeleteBtn from './MorphDeleteBtn.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

const draftText = ref('')
const sendState = ref<'idle' | 'loading' | 'success'>('idle')
const isDragOver = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const directDriveFolder = ref('')

// Per-file async busy states (keyed by clip.id)
const promotingSilo = ref<Record<string, boolean>>({})
const archivingDrive = ref<Record<string, boolean>>({})

// Stream filter & inline preview states
const streamFilter = ref<'all' | 'file' | 'image' | 'text'>('all')
const expandedIds = ref<Record<string, boolean>>({})
const inlinePreviewIds = ref<Record<string, boolean>>({})
const lightboxClip = ref<ClipItem | null>(null)

async function submitText() {
  const txt = draftText.value
  if (!txt.trim() || sendState.value !== 'idle') return
  sendState.value = 'loading'
  try {
    await store.sendTextClip(txt)
    draftText.value = ''
    sendState.value = 'success'
    window.setTimeout(() => {
      sendState.value = 'idle'
    }, 900)
  } catch {
    sendState.value = 'idle'
  }
}

function handleComposerKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
    e.preventDefault()
    void submitText()
  }
}

function dispatchFiles(files: File[]) {
  const presetDrive = directDriveFolder.value
  for (const f of files) {
    if (presetDrive) {
      void store.uploadChunkedFile(f, {
        target: 'gdrive',
        siloPrefix: 'drop/',
        gdriveFolder: presetDrive,
        ttlHours: 0,
        maxDownloads: 0,
        passcode: '',
        addToClip: true,
        skipShare: true,
      })
      continue
    }
    if (f.size <= 20 * 1024 * 1024) {
      void store.sendQuickPasteFile(f, {
        target: 'local',
        autoCopyFormat: 'none',
      })
      continue
    }
    void store.uploadChunkedFile(f, {
      target: 'local',
      siloPrefix: 'drop/',
      ttlHours: 0,
      maxDownloads: 0,
      passcode: '',
      addToClip: true,
      skipShare: true,
    })
  }
}

function onPickedFiles(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return
  dispatchFiles(Array.from(input.files))
  input.value = ''
}

function onDropFiles(e: DragEvent) {
  isDragOver.value = false
  if (!e.dataTransfer?.files?.length) return
  dispatchFiles(Array.from(e.dataTransfer.files))
}

function handleComposerPaste(e: ClipboardEvent) {
  const files = e.clipboardData?.files
  if (files && files.length > 0) {
    e.preventDefault()
    dispatchFiles(Array.from(files))
  }
}

async function pasteFromSystemClipboard() {
  if (!navigator.clipboard) {
    showToast('当前环境不支持读取剪贴板，请直接按 ⌘V 粘贴')
    return
  }
  try {
    if (navigator.clipboard.read) {
      const items = await navigator.clipboard.read()
      for (const item of items) {
        const imgType = item.types.find((t) => t.startsWith('image/'))
        if (imgType) {
          const blob = await item.getType(imgType)
          const ext = imgType.split('/')[1] || 'png'
          const file = new File([blob], `paste-${Date.now().toString().slice(-5)}.${ext}`, {
            type: imgType,
          })
          dispatchFiles([file])
          return
        }
      }
    }
    const txt = await navigator.clipboard.readText()
    if (txt && txt.trim()) {
      await store.sendTextClip(txt)
      return
    }
    showToast('剪贴板为空')
  } catch {
    showToast('请允许剪贴板读取权限，或在粘贴框内长按粘贴')
  }
}

async function onPromoteToSilo(item: ClipItem) {
  if (promotingSilo.value[item.id]) return
  promotingSilo.value[item.id] = true
  try {
    await store.promoteClipToSilo(item, 'md')
  } finally {
    promotingSilo.value[item.id] = false
  }
}

async function onArchiveToDrive(item: ClipItem, folder: string) {
  if (!folder || archivingDrive.value[item.id]) return
  archivingDrive.value[item.id] = true
  try {
    await store.archiveClipToGDrive(item, folder)
  } finally {
    archivingDrive.value[item.id] = false
  }
}

async function togglePin(item: ClipItem) {
  const res = await fetch(`/api/clips/${item.id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinned: !item.pinned }),
  })
  if (res.ok) item.pinned = !item.pinned
}

async function shareClip(item: ClipItem) {
  if (item.shareCode) {
    const url = shareFullUrl(item.shareCode)
    await copyText(url, '已复制公开分享短链')
    store.openQrModal(item.fileName || '文本片段', url, '免登录扫码取件')
    return
  }
  const res = await fetch(`/api/clips/${item.id}/share`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ttlHours: 168, passcode: '' }),
  })
  if (res.ok) {
    const sh = await res.json()
    item.shareCode = sh.code
    if (!store.shares.value.some((s) => s.code === sh.code)) {
      store.shares.value = [sh, ...store.shares.value]
    }
    const url = shareFullUrl(sh.code)
    await copyText(url, '公开短链已生成并复制')
    store.openQrModal(item.fileName || '文本片段', url, '7 天有效 · 可在外链管理随时作废')
  }
}

async function removeClip(item: ClipItem) {
  await fetch(`/api/clips/${item.id}`, { method: 'DELETE' })
  store.clips.value = store.clips.value.filter((c) => c.id !== item.id)
}

function isLongText(txt?: string) {
  if (!txt) return false
  return txt.length > 340 || txt.split('\n').length > 8
}

function canInlinePreview(item: ClipItem) {
  const m = (item.mimeType || '').toLowerCase()
  const n = (item.fileName || '').toLowerCase()
  return (
    m.startsWith('video/') ||
    m.startsWith('audio/') ||
    m === 'application/pdf' ||
    n.endsWith('.mp4') ||
    n.endsWith('.webm') ||
    n.endsWith('.mp3') ||
    n.endsWith('.flac') ||
    n.endsWith('.wav') ||
    n.endsWith('.pdf')
  )
}

const filteredClips = computed(() =>
  store.clips.value.filter((c) => {
    if (streamFilter.value === 'all') return true
    if (streamFilter.value === 'file') return c.kind === 'file'
    return c.kind === streamFilter.value
  }),
)

const activeUploads = computed(() =>
  store.uploads.value.filter(
    (u) => u.status === 'uploading' || u.status === 'assembling' || u.status === 'error',
  ),
)

function onHubGlobalPaste(e: Event) {
  const files = (e as CustomEvent<File[]>).detail
  if (files?.length) dispatchFiles(files)
}

onMounted(() => {
  window.addEventListener('oarc-hub-files', onHubGlobalPaste)
})
onBeforeUnmount(() => {
  window.removeEventListener('oarc-hub-files', onHubGlobalPaste)
})
</script>

<template>
  <div
    class="hub-page"
    @dragover.prevent="isDragOver = true"
    @dragleave.self="isDragOver = false"
    @drop.prevent="onDropFiles"
  >
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">收发台</h1>
        <span class="pane-count">{{ store.clips.value.length }}</span>
      </div>

      <div class="pane-actions">
        <div class="filter-strip">
          <button
            v-for="f in [
              { k: 'all' as const, l: '全部' },
              { k: 'file' as const, l: '文件' },
              { k: 'image' as const, l: '图片' },
              { k: 'text' as const, l: '文本' },
            ]"
            :key="f.k"
            type="button"
            class="flt-chip"
            :class="{ 'is-on': streamFilter === f.k }"
            @click="streamFilter = f.k"
          >
            {{ f.l }}
          </button>
        </div>
      </div>
    </header>

    <div class="pane-body--narrow hub-stack">
      <!-- SINGLE FOCAL PASTE & FILE DROP PAD -->
      <section class="glass-box paste-dock" :class="{ 'is-drag': isDragOver }">
        <div class="paste-well">
          <textarea
            v-model="draftText"
            rows="3"
            class="paste-input"
            placeholder="按 ⌘V 粘贴文件、原图或文字，或直接将文件拖入此处（默认私密互传）…"
            @keydown="handleComposerKey"
            @paste="handleComposerPaste"
          />

          <div v-if="isDragOver" class="drag-overlay">
            <UiIcon name="arrow-up" :size="22" />
            <span>松手立即私密互传</span>
          </div>
        </div>

        <footer class="paste-bar">
          <div class="pb-left">
            <button type="button" class="pill-btn pill-btn--solid" @click="fileInput?.click()">
              <UiIcon name="plus" :size="14" />
              <span>选择文件</span>
            </button>
            <input ref="fileInput" type="file" multiple hidden @change="onPickedFiles" />

            <button
              type="button"
              class="pill-btn"
              title="一键读取手机或电脑系统剪贴板"
              @click="pasteFromSystemClipboard"
            >
              <UiIcon name="clipboard" :size="13.5" />
              <span>粘贴剪贴板</span>
            </button>

            <DriveFolderSelect
              v-if="store.status.value?.gdriveEnabled"
              mode="preset"
              :preset-value="directDriveFolder"
              @select="directDriveFolder = $event"
            />
          </div>

          <Transition name="pop-btn">
            <button
              v-if="draftText.trim() || sendState !== 'idle'"
              type="button"
              class="pill-btn pill-btn--solid send-text-btn"
              :class="`state-${sendState}`"
              @click="submitText"
            >
              <UiIcon v-if="sendState === 'success'" name="check" :size="14" />
              <UiIcon v-else name="arrow-up" :size="14" />
              <span>{{ sendState === 'success' ? '已发送' : '发送文字' }}</span>
            </button>
          </Transition>
        </footer>
      </section>

      <!-- Active Chunked Upload Progress -->
      <div v-if="activeUploads.length > 0" class="upload-queue">
        <div v-for="u in activeUploads" :key="u.localId" class="glass-box up-row">
          <div class="up-top">
            <span class="up-name">{{ u.fileName }}</span>
            <span class="up-stat mono">
              <template v-if="u.status === 'uploading'">
                {{ formatBytes(u.uploadedBytes) }} / {{ formatBytes(u.size) }} · {{ formatBytes(u.speedBps) }}/s
              </template>
              <template v-else-if="u.status === 'assembling'">合并校验中…</template>
              <template v-else>{{ u.error }}</template>
            </span>
            <button
              v-if="u.status === 'uploading'"
              type="button"
              class="icon-circle-btn up-cancel"
              title="取消上传"
              @click="u.abortCtrl.abort()"
            >
              <UiIcon name="close" :size="12.5" />
            </button>
          </div>
          <div class="up-bar">
            <div
              class="up-fill"
              :style="{ width: `${u.size ? Math.min(100, (u.uploadedBytes / u.size) * 100) : 0}%` }"
            />
          </div>
        </div>
      </div>

      <!-- Empty State -->
      <div v-if="filteredClips.length === 0" class="glass-box empty-state">
        暂无互传内容 · 在上方按 ⌘V 或选择文件即可跨设备同步
      </div>

      <!-- FILE-CENTRIC PRIVATE STREAM -->
      <TransitionGroup v-else name="stream-card" tag="div" class="clip-stream">
        <article
          v-for="item in filteredClips"
          :key="item.id"
          class="glass-box clip-card"
          :class="{ 'is-pinned': item.pinned }"
        >
          <!-- TEXT ITEM -->
          <template v-if="item.kind === 'text'">
            <header class="cc-head">
              <div class="cc-tags">
                <span v-if="item.pinned" class="tag-pill is-pin">置顶</span>
                <span class="tag-pill mono">TEXT</span>
                <span class="cc-meta mono">
                  {{ formatBytes(item.size) }} · {{ formatTimeShort(item.createdAt) }}
                </span>
              </div>

              <div class="cc-tools">
                <CopyPill :text="item.text || ''" label="复制" copied-label="已复制" />
                <button
                  type="button"
                  class="icon-circle-btn"
                  title="生成公开分享码"
                  @click="shareClip(item)"
                >
                  <UiIcon name="qr" :size="13.5" />
                </button>
                <button
                  type="button"
                  class="icon-circle-btn"
                  :class="{ 'is-pinned-btn': item.pinned }"
                  title="置顶"
                  @click="togglePin(item)"
                >
                  <UiIcon name="pin" :size="13.5" />
                </button>
                <MorphDeleteBtn title="删除此条文本" @confirm="removeClip(item)" />
              </div>
            </header>

            <pre
              class="cc-text mono"
              :class="{ 'is-collapsed': isLongText(item.text) && !expandedIds[item.id] }"
            >{{ item.text }}</pre>
            <button
              v-if="isLongText(item.text)"
              type="button"
              class="expand-btn"
              @click="expandedIds[item.id] = !expandedIds[item.id]"
            >
              {{ expandedIds[item.id] ? '收起' : '展开全文' }}
            </button>
          </template>

          <!-- FILE / IMAGE ITEM (Core Private Transfer + Per-File Silo & Google Drive Controls) -->
          <template v-else>
            <div class="file-main-row">
              <!-- Thumbnail / Icon -->
              <div
                v-if="item.kind === 'image'"
                class="file-thumb-box"
                title="点击查看原图"
                @click="lightboxClip = item"
              >
                <img :src="`/api/clips/${item.id}/raw`" :alt="item.fileName" loading="lazy" />
              </div>
              <div v-else class="file-ext-box mono">
                {{ (item.fileName?.split('.').pop() || 'FILE').slice(0, 4).toUpperCase() }}
              </div>

              <!-- File Metadata -->
              <div class="file-info">
                <div class="file-title-line">
                  <span v-if="item.pinned" class="tag-pill is-pin">置顶</span>
                  <span class="file-name">{{ item.fileName }}</span>
                </div>
                <div class="file-meta mono">
                  <span>私密互传</span>
                  <span>· {{ formatBytes(item.size) }}</span>
                  <span>· {{ formatTimeShort(item.createdAt) }}</span>
                </div>
              </div>

              <!-- Quick Download / Copy Image + Pin + Share + Morph Delete -->
              <div class="file-quick-tools">
                <button
                  v-if="item.kind === 'image'"
                  type="button"
                  class="pill-btn"
                  title="复制无损原图到剪贴板"
                  @click="copyImagePixels(`/api/clips/${item.id}/raw`)"
                >
                  <UiIcon name="copy" :size="13.5" />
                  <span class="hide-xs">复制原图</span>
                </button>

                <button
                  v-if="canInlinePreview(item)"
                  type="button"
                  class="icon-circle-btn"
                  :title="inlinePreviewIds[item.id] ? '关闭预览' : '内联预览'"
                  @click="inlinePreviewIds[item.id] = !inlinePreviewIds[item.id]"
                >
                  <UiIcon name="eye" :size="14" />
                </button>

                <a
                  class="pill-btn pill-btn--solid"
                  :href="`/api/clips/${item.id}/raw?download=1`"
                  :title="`下载 ${item.fileName}`"
                >
                  <UiIcon name="download" :size="13.5" />
                  <span>下载</span>
                </a>

                <button
                  type="button"
                  class="icon-circle-btn"
                  title="生成公开分享短链与二维码"
                  @click="shareClip(item)"
                >
                  <UiIcon name="qr" :size="13.5" />
                </button>

                <button
                  type="button"
                  class="icon-circle-btn"
                  :class="{ 'is-pinned-btn': item.pinned }"
                  title="置顶"
                  @click="togglePin(item)"
                >
                  <UiIcon name="pin" :size="13.5" />
                </button>

                <MorphDeleteBtn title="删除此文件" @confirm="removeClip(item)" />
              </div>
            </div>

            <!-- Inline Audio / Video / PDF Preview -->
            <div v-if="inlinePreviewIds[item.id]" class="inline-player-box">
              <video
                v-if="(item.mimeType || '').startsWith('video/') || (item.fileName || '').endsWith('.mp4')"
                controls
                preload="metadata"
                :src="`/api/clips/${item.id}/raw`"
                class="inline-video"
              />
              <audio
                v-else-if="(item.mimeType || '').startsWith('audio/')"
                controls
                preload="metadata"
                :src="`/api/clips/${item.id}/raw`"
                class="inline-audio"
              />
              <iframe
                v-else
                :src="`/api/clips/${item.id}/raw`"
                class="inline-pdf"
                title="PDF Preview"
              />
            </div>

            <!-- PER-FILE DESTINATION ROUTING BAR: Silo Image-Host Link & Google Drive Folder Select -->
            <footer class="file-dest-bar">
              <!-- Silo Image Host Switch / Copy Links -->
              <div v-if="store.status.value?.siloEnabled" class="dest-silo-group">
                <template v-if="item.publicUrl">
                  <span class="silo-ready-badge">
                    <UiIcon name="check" :size="12" />
                    <span>已存图床</span>
                  </span>
                  <CopyPill
                    v-if="item.kind === 'image'"
                    :text="formatMarkdownImage(item.fileName || 'image', item.publicUrl)"
                    label="复制 MD"
                    copied-label="已复制"
                  />
                  <CopyPill :text="item.publicUrl" label="复制外链" copied-label="已复制" />
                </template>
                <button
                  v-else
                  type="button"
                  class="pill-btn silo-promote-btn"
                  :disabled="promotingSilo[item.id]"
                  @click="onPromoteToSilo(item)"
                >
                  <UiIcon name="link" :size="13.5" />
                  <span>{{ promotingSilo[item.id] ? '正在转存图床…' : '存图床拿外链' }}</span>
                </button>
              </div>

              <!-- Per-File Google Drive Folder Dropdown -->
              <DriveFolderSelect
                v-if="store.status.value?.gdriveEnabled"
                mode="file"
                :saved-path="item.gdrivePath || (item.storage === 'gdrive' ? item.storageKey : '')"
                :busy="Boolean(archivingDrive[item.id])"
                @select="onArchiveToDrive(item, $event)"
              />
            </footer>
          </template>
        </article>
      </TransitionGroup>
    </div>

    <!-- Image Lightbox -->
    <Teleport to="body">
      <Transition name="lb-fade">
        <div v-if="lightboxClip" class="lb-backdrop" @click.self="lightboxClip = null">
          <div class="liquid-chrome lb-panel">
            <img :src="`/api/clips/${lightboxClip.id}/raw`" :alt="lightboxClip.fileName" />
            <div class="lb-bar">
              <span class="lb-name">{{ lightboxClip.fileName }}</span>
              <div class="lb-acts">
                <button
                  type="button"
                  class="pill-btn"
                  @click="copyImagePixels(`/api/clips/${lightboxClip.id}/raw`)"
                >
                  复制原图
                </button>
                <a
                  class="pill-btn pill-btn--solid"
                  :href="`/api/clips/${lightboxClip.id}/raw?download=1`"
                >
                  下载原图
                </a>
                <button type="button" class="icon-circle-btn" @click="lightboxClip = null">
                  <UiIcon name="close" :size="14" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.hub-stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.filter-strip {
  display: inline-flex;
  padding: 2.5px;
  border-radius: 9px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  gap: 2px;
}

.flt-chip {
  border: 0;
  background: transparent;
  color: var(--ink-2);
  font-size: 0.76rem;
  font-weight: 560;
  padding: 4px 10px;
  border-radius: 6.5px;
  cursor: pointer;
  transition: all 0.16s ease;
}

.flt-chip.is-on {
  background: var(--surface-pill);
  color: var(--ink-1);
  font-weight: 640;
  box-shadow: var(--shadow-pill);
}

/* Focal Paste & Drop Dock */
.paste-dock {
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition: border-color 0.2s ease, transform 0.2s var(--ease-spring);
}

.paste-dock.is-drag {
  border-color: var(--ink-1);
  transform: scale(1.006);
}

.paste-well {
  position: relative;
  border-radius: 12px;
  background: var(--surface-recessed);
  border: 1px dashed var(--border-strong);
  transition: background-color 0.18s ease, border-color 0.18s ease;
}

.paste-well:focus-within {
  background: var(--surface-pill);
  border-style: solid;
  border-color: var(--border-strong);
}

.paste-input {
  width: 100%;
  border: 0;
  background: transparent;
  color: var(--ink-1);
  padding: 13px 15px;
  font-size: 0.88rem;
  line-height: 1.55;
  resize: vertical;
  min-height: 80px;
  outline: none;
  display: block;
}

.paste-input::placeholder {
  color: var(--ink-3);
}

.drag-overlay {
  position: absolute;
  inset: 0;
  border-radius: 12px;
  background: var(--surface-popover);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-size: 0.88rem;
  font-weight: 640;
  color: var(--ink-1);
}

.paste-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.pb-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.send-text-btn.state-success {
  background: var(--emerald-fg);
}

/* Upload Progress */
.upload-queue {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.up-row {
  padding: 11px 14px;
  border-radius: 13px;
}

.up-top {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 7px;
}

.up-name {
  font-size: 0.82rem;
  font-weight: 620;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.up-stat {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.up-cancel {
  width: 25px;
  height: 25px;
}

.up-bar {
  height: 4px;
  border-radius: 999px;
  background: var(--surface-recessed-hover);
  overflow: hidden;
}

.up-fill {
  height: 100%;
  background: var(--cta-bg);
  transition: width 0.2s linear;
}

.empty-state {
  padding: 46px 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

/* Stream Cards */
.clip-stream {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.clip-card {
  padding: 13px 15px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.clip-card.is-pinned {
  border-color: var(--border-strong);
}

.cc-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.cc-tags {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}

.tag-pill {
  font-size: 0.65rem;
  font-weight: 630;
  padding: 2px 6px;
  border-radius: 5px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.tag-pill.is-pin {
  background: rgba(234, 88, 12, 0.12);
  color: var(--led-orange);
}

.cc-meta {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.cc-tools {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.is-pinned-btn {
  color: var(--led-orange);
  border-color: rgba(234, 88, 12, 0.3);
}

.cc-text {
  margin: 0;
  padding: 11px 13px;
  border-radius: 10px;
  background: var(--surface-recessed);
  font-size: 0.81rem;
  line-height: 1.56;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
}

.cc-text.is-collapsed {
  max-height: 168px;
  overflow: hidden;
}

.expand-btn {
  align-self: flex-start;
  border: 0;
  background: transparent;
  color: var(--ink-2);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 0;
  cursor: pointer;
}

/* File / Image Card Layout */
.file-main-row {
  display: flex;
  align-items: center;
  gap: 13px;
}

.file-thumb-box {
  width: 58px;
  height: 58px;
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  flex-shrink: 0;
  cursor: zoom-in;
}

.file-thumb-box img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.file-ext-box {
  width: 52px;
  height: 52px;
  border-radius: 10px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  display: grid;
  place-items: center;
  font-size: 0.7rem;
  font-weight: 600;
  color: var(--ink-2);
  flex-shrink: 0;
}

.file-info {
  flex: 1;
  min-width: 0;
}

.file-title-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.file-name {
  font-size: 0.9rem;
  font-weight: 640;
  color: var(--ink-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-meta {
  margin-top: 3px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 0.72rem;
  color: var(--ink-3);
}

.file-quick-tools {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.file-dest-bar {
  padding-top: 9px;
  border-top: 1px solid var(--border-hairline);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.dest-silo-group {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.silo-promote-btn {
  height: 31px;
  padding: 0 10px;
  font-size: 0.76rem;
}

.silo-ready-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 29px;
  padding: 0 9px;
  border-radius: 8px;
  font-size: 0.74rem;
  font-weight: 610;
  background: var(--emerald-bg);
  color: var(--emerald-fg);
  border: 1px solid var(--emerald-border);
}

.inline-player-box {
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface-recessed);
  padding: 6px;
}

.inline-video {
  width: 100%;
  max-height: 340px;
  border-radius: 7px;
  display: block;
}

.inline-audio {
  width: 100%;
}

.inline-pdf {
  width: 100%;
  height: 380px;
  border: 0;
  border-radius: 7px;
}

@media (max-width: 680px) {
  .file-main-row {
    flex-wrap: wrap;
  }
  .file-quick-tools {
    width: 100%;
    justify-content: flex-end;
  }
  .hide-xs {
    display: none;
  }
}

/* Lightbox */
.lb-backdrop {
  position: fixed;
  inset: 0;
  z-index: 9980;
  background: rgba(8, 9, 12, 0.68);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  display: grid;
  place-items: center;
  padding: 18px;
}

.lb-panel {
  max-width: min(92vw, 980px);
  max-height: 88dvh;
  border-radius: 16px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.lb-panel img {
  max-width: 100%;
  max-height: calc(88dvh - 70px);
  object-fit: contain;
  border-radius: 10px;
}

.lb-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 4px;
}

.lb-name {
  font-size: 0.82rem;
  font-weight: 620;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lb-acts {
  display: flex;
  align-items: center;
  gap: 7px;
}

.pop-btn-enter-active,
.pop-btn-leave-active {
  transition: transform 0.22s var(--ease-bounce), opacity 0.15s ease;
}
.pop-btn-enter-from,
.pop-btn-leave-to {
  opacity: 0;
  transform: scale(0.88);
}

.stream-card-enter-active,
.stream-card-leave-active {
  transition: all 0.26s var(--ease-spring);
}
.stream-card-enter-from,
.stream-card-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

.lb-fade-enter-active,
.lb-fade-leave-active {
  transition: opacity 0.2s ease;
}
.lb-fade-enter-from,
.lb-fade-leave-to {
  opacity: 0;
}
</style>
