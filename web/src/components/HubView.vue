<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
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
import type { ClipItem } from '../types'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

type HubMode = 'local' | 'silo' | 'gdrive'
const mode = ref<HubMode>(
  (localStorage.getItem('oarc_hub_mode') as HubMode) || 'local',
)
const siloPrefix = ref(localStorage.getItem('oarc_imghost_prefix') || 'images/')
const autoCopyFmt = ref<'md' | 'url'>(
  (localStorage.getItem('oarc_imghost_fmt') as 'md' | 'url') || 'md',
)

const draftText = ref('')
const sendState = ref<'idle' | 'loading' | 'success'>('idle')
const isDragOver = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

// Stream filters & preview states
const streamFilter = ref<'all' | 'text' | 'image' | 'file'>('all')
const searchQuery = ref('')
const expandedIds = ref<Record<string, boolean>>({})
const inlinePreviewIds = ref<Record<string, boolean>>({})
const lightboxClip = ref<ClipItem | null>(null)

const modeOptions = computed(() => [
  {
    key: 'local' as const,
    label: '私密互传',
    hint: '仅你自己的电脑与手机可见，打开网页点「直下 / 复制」即取，外人不可见。',
    enabled: true,
  },
  {
    key: 'silo' as const,
    label: '存图床拿外链',
    hint: `公开存入 Silo (${siloPrefix.value})，上传完自动把 ${autoCopyFmt.value === 'md' ? 'Markdown 图片链接' : '公网 URL'} 复制进剪贴板。`,
    enabled: Boolean(store.status.value?.siloEnabled),
  },
  {
    key: 'gdrive' as const,
    label: '存 Google Drive',
    hint: '20 MB 自动切片直传你的网盘隔离目录 (gdrive:OArc-Drop)，不占服务器硬盘。',
    enabled: Boolean(store.status.value?.gdriveEnabled),
  },
])

const activeModeIdx = computed(() =>
  Math.max(0, modeOptions.value.findIndex((o) => o.key === mode.value)),
)

const currentModeHint = computed(
  () => modeOptions.value.find((o) => o.key === mode.value)?.hint || '',
)

function selectMode(m: HubMode, enabled: boolean) {
  if (!enabled) {
    showToast(m === 'gdrive' ? 'Google Drive 未启用' : 'Silo 未连接')
    return
  }
  mode.value = m
  localStorage.setItem('oarc_hub_mode', m)
}

function setPrefix(p: string) {
  siloPrefix.value = p
  localStorage.setItem('oarc_imghost_prefix', p)
}

function setAutoCopyFmt(f: 'md' | 'url') {
  autoCopyFmt.value = f
  localStorage.setItem('oarc_imghost_fmt', f)
}

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
    }, 1000)
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
  for (const f of files) {
    if (f.size <= 20 * 1024 * 1024 && (mode.value === 'local' || mode.value === 'silo')) {
      void store.sendQuickPasteFile(f, {
        target: mode.value,
        prefix: mode.value === 'silo' ? siloPrefix.value : '',
        autoCopyFormat: mode.value === 'silo' ? autoCopyFmt.value : 'none',
      })
      continue
    }
    void store.uploadChunkedFile(f, {
      target: mode.value,
      siloPrefix: mode.value === 'silo' ? siloPrefix.value : 'drop/',
      ttlHours: 0,
      maxDownloads: 0,
      passcode: '',
      addToClip: true,
      skipShare: true,
      autoCopyFormat: mode.value === 'silo' ? autoCopyFmt.value : 'none',
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
    showToast('浏览器不支持直接读取剪贴板，请在输入框粘贴')
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
          const file = new File([blob], `image.${ext}`, { type: imgType })
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
    showToast('当前系统剪贴板为空')
  } catch {
    showToast('请允许剪贴板权限，或直接在输入框长按粘贴')
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
    store.openQrModal(item.fileName || '文本片段', url, '外部访客免登录扫码直取')
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
    store.openQrModal(item.fileName || '文本片段', url, '免登录公开短链（7 天有效，可在「外链管理」随时作废）')
  }
}

async function removeClip(item: ClipItem) {
  await fetch(`/api/clips/${item.id}`, { method: 'DELETE' })
  store.clips.value = store.clips.value.filter((c) => c.id !== item.id)
}

function isLongText(txt?: string) {
  if (!txt) return false
  return txt.length > 360 || txt.split('\n').length > 8
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

function storageBadge(st?: string) {
  if (st === 'silo') return 'Silo 图床'
  if (st === 'gdrive') return 'Google Drive'
  return '私密暂存'
}

const filteredClips = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return store.clips.value.filter((c) => {
    if (streamFilter.value !== 'all' && c.kind !== streamFilter.value) return false
    if (q) {
      const hay = `${c.fileName || ''} ${c.text || ''} ${c.storageKey || ''}`.toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })
})

const activeUploads = computed(() =>
  store.uploads.value.filter((u) => u.status === 'uploading' || u.status === 'assembling' || u.status === 'error'),
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
        <p class="pane-subtitle">电脑与手机互传文字、原图与大文件 · 按 ⌘V 或拖入即传</p>
      </div>
    </header>

    <div class="pane-body--narrow hub-stack">
      <!-- Single Self-Explanatory Send & Drop Card (Zero overlapping floating popovers!) -->
      <section class="glass-box composer-pod" :class="{ 'is-drag': isDragOver }">
        <!-- Step 1: Clear Destination Switch -->
        <div class="dest-row">
          <div class="tab-pill-group dest-pills" :style="{ gridTemplateColumns: 'repeat(3, 1fr)' }">
            <div
              class="tab-pill-indicator"
              :style="{
                width: 'calc((100% - 7px) / 3)',
                transform: `translateX(${activeModeIdx * 100}%)`,
              }"
            />
            <button
              v-for="opt in modeOptions"
              :key="opt.key"
              type="button"
              class="tab-pill-btn"
              :class="{ 'is-active': mode === opt.key, 'is-disabled': !opt.enabled }"
              @click="selectMode(opt.key, opt.enabled)"
            >
              {{ opt.label }}
            </button>
          </div>
          <div class="dest-hint">{{ currentModeHint }}</div>
        </div>

        <!-- Inline Image-Host Sub-options (only shown in Silo mode, clean flat flex row!) -->
        <div v-if="mode === 'silo'" class="imghost-inline">
          <div class="ih-pair">
            <span class="ih-lbl">目录：</span>
            <button
              v-for="p in ['images/', 'drop/']"
              :key="p"
              type="button"
              class="chip-btn mono"
              :class="{ 'is-on': siloPrefix === p }"
              @click="setPrefix(p)"
            >
              {{ p }}
            </button>
          </div>
          <div class="ih-pair">
            <span class="ih-lbl">自动复制：</span>
            <button
              type="button"
              class="chip-btn"
              :class="{ 'is-on': autoCopyFmt === 'md' }"
              @click="setAutoCopyFmt('md')"
            >
              Markdown
            </button>
            <button
              type="button"
              class="chip-btn"
              :class="{ 'is-on': autoCopyFmt === 'url' }"
              @click="setAutoCopyFmt('url')"
            >
              纯 URL
            </button>
          </div>
        </div>

        <!-- Step 2: Text / Paste / Drag Area -->
        <div class="composer-main">
          <textarea
            v-model="draftText"
            rows="3"
            class="composer-textarea"
            :placeholder="
              mode === 'silo'
                ? '在此按 ⌘V 粘贴截图，或把图片拖进来，自动上传图床并复制链接…'
                : '输入文字/代码（⌘+Enter 发送），或按 ⌘V 贴原图、把任意大小文件直接拖进这里…'
            "
            @keydown="handleComposerKey"
            @paste="handleComposerPaste"
          />
        </div>

        <!-- Step 3: Clean Action Bar -->
        <footer class="composer-bar">
          <div class="cbar-left">
            <button
              type="button"
              class="pill-btn"
              title="一键把手机或电脑剪贴板里的文字/图片发上来"
              @click="pasteFromSystemClipboard"
            >
              <UiIcon name="clipboard" :size="14" />
              <span>粘贴剪贴板</span>
            </button>

            <button type="button" class="pill-btn" @click="fileInput?.click()">
              <UiIcon name="plus" :size="14" />
              <span>选择文件 / 原图</span>
            </button>
            <input ref="fileInput" type="file" multiple hidden @change="onPickedFiles" />
          </div>

          <button
            type="button"
            class="pill-btn pill-btn--solid send-btn"
            :class="`state-${sendState}`"
            :disabled="!draftText.trim() && sendState === 'idle'"
            @click="submitText"
          >
            <UiIcon v-if="sendState === 'success'" name="check" :size="14.5" />
            <UiIcon v-else name="arrow-up" :size="14.5" />
            <span>{{ sendState === 'success' ? '已发送' : '发送文字' }}</span>
          </button>
        </footer>
      </section>

      <!-- Live Chunked Upload Progress -->
      <div v-if="activeUploads.length > 0" class="upload-queue">
        <div v-for="u in activeUploads" :key="u.localId" class="glass-box up-row">
          <div class="up-top">
            <span class="up-name">{{ u.fileName }}</span>
            <span class="up-stat mono">
              <template v-if="u.status === 'uploading'">
                {{ formatBytes(u.uploadedBytes) }} / {{ formatBytes(u.size) }} · {{ formatBytes(u.speedBps) }}/s
              </template>
              <template v-else-if="u.status === 'assembling'">正在服务端合并校验…</template>
              <template v-else>{{ u.error }}</template>
            </span>
            <button
              v-if="u.status === 'uploading'"
              type="button"
              class="mini-icon-act is-danger"
              title="取消上传"
              @click="u.abortCtrl.abort()"
            >
              <UiIcon name="close" :size="13" />
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

      <!-- History Feed Header -->
      <div class="stream-header">
        <div class="filter-chips">
          <button
            v-for="f in [
              { k: 'all' as const, l: '全部记录' },
              { k: 'text' as const, l: '文本' },
              { k: 'image' as const, l: '图片' },
              { k: 'file' as const, l: '文件' },
            ]"
            :key="f.k"
            type="button"
            class="chip-btn"
            :class="{ 'is-on': streamFilter === f.k }"
            @click="streamFilter = f.k"
          >
            {{ f.l }}
          </button>
        </div>

        <div class="search-pill mini-search">
          <UiIcon name="search" :size="14" />
          <input v-model="searchQuery" placeholder="搜索记录…" />
        </div>
      </div>

      <div v-if="filteredClips.length === 0" class="glass-box empty-card">
        暂无记录 · 在上方打字、粘贴截图或拖入文件，你的其他设备打开就能立刻取回
      </div>

      <!-- Unified Stream -->
      <div v-else class="clip-stream">
        <article
          v-for="item in filteredClips"
          :key="item.id"
          class="glass-box clip-card"
          :class="{ 'is-pinned': item.pinned }"
        >
          <div class="clip-top">
            <div class="clip-tags">
              <span class="kind-dot" :class="`kind-${item.kind}`" />
              <span class="kind-name">
                {{ item.kind === 'text' ? '文本片段' : item.fileName || '未命名文件' }}
              </span>
              <span v-if="item.kind !== 'text'" class="storage-tag">
                {{ storageBadge(item.storage) }}
              </span>
              <span v-if="item.pinned" class="pin-pill">置顶</span>
              <span class="clip-meta-txt mono">
                {{ formatBytes(item.size) }} · {{ formatTimeShort(item.createdAt) }}
              </span>
            </div>

            <div class="clip-toolbar">
              <template v-if="item.kind === 'text'">
                <CopyPill :text="item.text || ''" label="复制文本" copied-label="已复制" compact />
                <button
                  v-if="(item.text || '').length <= 240"
                  type="button"
                  class="mini-icon-act"
                  title="手机扫码读取"
                  @click="store.openQrModal('扫码读取文本', item.text || '')"
                >
                  <UiIcon name="qr" :size="14" />
                </button>
              </template>

              <template v-else-if="item.kind === 'image'">
                <template v-if="item.publicUrl">
                  <CopyPill
                    :text="formatMarkdownImage(item.fileName || 'image', item.publicUrl)"
                    label="MD"
                    copied-label="已复制"
                    compact
                  />
                  <CopyPill :text="item.publicUrl" label="URL" copied-label="已复制" compact />
                  <CopyPill
                    :text="formatHtmlImage(item.fileName || 'image', item.publicUrl)"
                    label="HTML"
                    copied-label="已复制"
                    compact
                  />
                </template>
                <button
                  type="button"
                  class="mini-pill-act"
                  title="把图片直接写进系统剪贴板，去微信/文档直接粘贴"
                  @click="copyImagePixels(`/api/clips/${item.id}/raw`)"
                >
                  <UiIcon name="copy" :size="12.5" />
                  <span>复制原图</span>
                </button>
                <a class="mini-icon-act" :href="`/api/clips/${item.id}/raw?download=1`" title="下载文件">
                  <UiIcon name="download" :size="14" />
                </a>
              </template>

              <template v-else>
                <CopyPill
                  v-if="item.publicUrl"
                  :text="item.publicUrl"
                  label="直链"
                  copied-label="已复制"
                  compact
                />
                <button
                  v-if="canInlinePreview(item)"
                  type="button"
                  class="mini-pill-act"
                  @click="inlinePreviewIds[item.id] = !inlinePreviewIds[item.id]"
                >
                  <UiIcon name="eye" :size="12.5" />
                  <span>{{ inlinePreviewIds[item.id] ? '收起' : '预览' }}</span>
                </button>
                <a class="mini-pill-act mini-pill-act--solid" :href="`/api/clips/${item.id}/raw?download=1`">
                  <UiIcon name="download" :size="12.5" />
                  <span>直下</span>
                </a>
              </template>

              <button
                type="button"
                class="mini-pill-act"
                :class="{ 'is-shared': Boolean(item.shareCode) }"
                :title="item.shareCode ? '查看公开分享链接与二维码' : '生成发给别人的免登下载链接与二维码'"
                @click="shareClip(item)"
              >
                <UiIcon :name="item.shareCode ? 'qr' : 'share'" :size="12.5" />
                <span>{{ item.shareCode ? '已分享' : '外发分享' }}</span>
              </button>

              <button
                type="button"
                class="mini-icon-act"
                :class="{ 'is-pinned-btn': item.pinned }"
                title="置顶"
                @click="togglePin(item)"
              >
                <UiIcon name="pin" :size="13.5" />
              </button>
              <button
                type="button"
                class="mini-icon-act is-danger"
                title="删除"
                @click="removeClip(item)"
              >
                <UiIcon name="trash" :size="13.5" />
              </button>
            </div>
          </div>

          <div v-if="item.kind === 'text'" class="clip-text-wrap">
            <pre
              class="clip-pre mono"
              :class="{ 'is-collapsed': isLongText(item.text) && !expandedIds[item.id] }"
            >{{ item.text }}</pre>
            <button
              v-if="isLongText(item.text)"
              type="button"
              class="expand-link"
              @click="expandedIds[item.id] = !expandedIds[item.id]"
            >
              {{ expandedIds[item.id] ? '收起长文本' : '展开全部内容' }}
            </button>
          </div>

          <div v-else-if="item.kind === 'image'" class="clip-img-box" @click="lightboxClip = item">
            <img :src="`/api/clips/${item.id}/raw`" :alt="item.fileName" loading="lazy" />
          </div>

          <div v-else-if="inlinePreviewIds[item.id]" class="inline-preview-box">
            <video
              v-if="(item.mimeType || '').startsWith('video/') || (item.fileName || '').match(/\.(mp4|webm)$/i)"
              controls
              preload="metadata"
              :src="`/api/clips/${item.id}/raw`"
              class="preview-video"
            />
            <audio
              v-else-if="(item.mimeType || '').startsWith('audio/') || (item.fileName || '').match(/\.(mp3|flac|wav)$/i)"
              controls
              :src="`/api/clips/${item.id}/raw`"
              class="preview-audio"
            />
            <iframe
              v-else
              :src="`/api/clips/${item.id}/raw`"
              class="preview-pdf"
              title="PDF Preview"
            />
          </div>
        </article>
      </div>
    </div>

    <!-- Image Lightbox -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="lightboxClip" class="lb-backdrop" @click.self="lightboxClip = null">
          <div class="liquid-chrome lb-dialog">
            <button type="button" class="icon-circle-btn lb-close" @click="lightboxClip = null">
              <UiIcon name="close" :size="16" />
            </button>
            <div class="lb-stage">
              <img :src="`/api/clips/${lightboxClip.id}/raw`" :alt="lightboxClip.fileName" />
            </div>
            <footer class="lb-bar">
              <span class="mono">{{ lightboxClip.fileName }} · {{ formatBytes(lightboxClip.size) }}</span>
              <div class="lb-acts">
                <button
                  type="button"
                  class="pill-btn"
                  @click="copyImagePixels(`/api/clips/${lightboxClip.id}/raw`)"
                >
                  <UiIcon name="copy" :size="14" />
                  <span>复制原图</span>
                </button>
                <CopyPill
                  v-if="lightboxClip.publicUrl"
                  :text="formatMarkdownImage(lightboxClip.fileName || 'image', lightboxClip.publicUrl)"
                  label="复制 Markdown"
                  copied-label="已复制"
                />
                <a class="pill-btn pill-btn--solid" :href="`/api/clips/${lightboxClip.id}/raw?download=1`">
                  <UiIcon name="download" :size="14" />
                  <span>下载原文件</span>
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
.hub-page {
  min-height: 100%;
}

.hub-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.composer-pod {
  border-radius: 24px;
  padding: 16px 18px 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.composer-pod.is-drag {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.dest-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dest-pills {
  width: 100%;
  max-width: 390px;
}

.dest-hint {
  font-size: 0.77rem;
  color: var(--ink-2);
  line-height: 1.45;
}

.imghost-inline {
  padding: 9px 12px;
  border-radius: 14px;
  background: var(--surface-recessed);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 18px;
}

.ih-pair {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ih-lbl {
  font-size: 0.75rem;
  color: var(--ink-3);
  font-weight: 580;
}

.chip-btn {
  height: 28px;
  padding: 0 11px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.75rem;
  font-weight: 560;
  cursor: pointer;
  transition: all 0.16s ease;
}

.chip-btn.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.composer-main {
  padding: 10px 12px;
  border-radius: 16px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
}

.composer-textarea {
  width: 100%;
  border: none;
  background: transparent;
  color: var(--ink-1);
  font-size: 0.9rem;
  line-height: 1.55;
  resize: vertical;
  min-height: 62px;
  max-height: 220px;
  outline: none;
  display: block;
}

.composer-textarea::placeholder {
  color: var(--ink-3);
}

.composer-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.cbar-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.send-btn:disabled {
  opacity: 0.35;
  cursor: default;
}

.send-btn.state-success {
  background: var(--emerald-fg);
  color: #fff;
}

.upload-queue {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.up-row {
  padding: 12px 15px;
  border-radius: 16px;
}

.up-top {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
}

.up-name {
  font-weight: 620;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.up-stat {
  font-size: 0.73rem;
  color: var(--ink-3);
  margin-left: auto;
}

.up-bar {
  margin-top: 7px;
  height: 5px;
  border-radius: 999px;
  background: var(--surface-recessed);
  overflow: hidden;
}

.up-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 999px;
  transition: width 0.2s ease;
}

.stream-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.filter-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.mini-search {
  height: 32px;
}

.empty-card {
  padding: 44px 20px;
  border-radius: 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

.clip-stream {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.clip-card {
  padding: 14px 16px;
  border-radius: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.clip-card.is-pinned {
  border-color: var(--accent-border);
}

.clip-top {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
}

.clip-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.kind-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
}
.kind-dot.kind-image {
  background: #10b981;
}
.kind-dot.kind-file {
  background: #f59e0b;
}

.kind-name {
  font-size: 0.87rem;
  font-weight: 640;
  word-break: break-all;
}

.storage-tag,
.pin-pill {
  font-size: 0.69rem;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.pin-pill {
  background: var(--accent-soft);
  color: var(--accent);
}

.clip-meta-txt {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.clip-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.mini-pill-act {
  height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.74rem;
  font-weight: 580;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  text-decoration: none;
  transition: all 0.16s ease;
}

.mini-pill-act:hover {
  background: var(--surface-hover);
  border-color: var(--border-strong);
}

.mini-pill-act--solid {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.mini-pill-act.is-shared {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent-border);
}

.mini-icon-act {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: inline-grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
  transition: all 0.16s ease;
}

.mini-icon-act:hover {
  color: var(--ink-1);
  background: var(--surface-hover);
}

.mini-icon-act.is-pinned-btn {
  color: var(--accent);
  background: var(--accent-soft);
}

.mini-icon-act.is-danger:hover {
  color: var(--danger-fg);
}

.clip-pre {
  margin: 0;
  padding: 10px 12px;
  border-radius: 13px;
  background: var(--surface-recessed);
  font-size: 0.81rem;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
}

.clip-pre.is-collapsed {
  max-height: 156px;
  overflow: hidden;
  mask-image: linear-gradient(to bottom, #000 68%, transparent 100%);
}

.expand-link {
  margin-top: 4px;
  border: none;
  background: none;
  color: var(--accent);
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  padding: 2px 0;
}

.clip-img-box {
  max-height: 280px;
  border-radius: 14px;
  overflow: hidden;
  background: var(--surface-recessed);
  cursor: zoom-in;
  display: flex;
  justify-content: flex-start;
}

.clip-img-box img {
  max-height: 280px;
  max-width: 100%;
  object-fit: contain;
  display: block;
}

.inline-preview-box {
  border-radius: 14px;
  overflow: hidden;
  background: var(--surface-recessed);
  padding: 8px;
}

.preview-video {
  width: 100%;
  max-height: 360px;
  border-radius: 10px;
}

.preview-audio {
  width: 100%;
}

.preview-pdf {
  width: 100%;
  height: 420px;
  border: none;
  border-radius: 10px;
}

.lb-backdrop {
  position: fixed;
  inset: 0;
  z-index: 240;
  background: rgba(15, 23, 42, 0.5);
  backdrop-filter: blur(18px);
  display: grid;
  place-items: center;
  padding: 20px;
}

.lb-dialog {
  width: min(920px, 100%);
  max-height: 90vh;
  border-radius: 26px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  position: relative;
}

.lb-close {
  position: absolute;
  top: 14px;
  right: 14px;
  z-index: 2;
}

.lb-stage {
  flex: 1;
  min-height: 0;
  display: grid;
  place-items: center;
  overflow: hidden;
}

.lb-stage img {
  max-width: 100%;
  max-height: 72vh;
  object-fit: contain;
  border-radius: 14px;
}

.lb-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 0.78rem;
  color: var(--ink-2);
}

.lb-acts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
