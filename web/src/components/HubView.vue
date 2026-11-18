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

// Public share options (OPT-IN by default -> private transfer first!)
const autoPublicShare = ref(false)
const ttlHours = ref<number>(168)
const passcode = ref('')
const openShareOpts = ref(false)
const optsWrap = ref<HTMLElement | null>(null)

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
  { key: 'local' as const, label: '私密收发', enabled: true },
  { key: 'silo' as const, label: 'Silo 图床', enabled: Boolean(store.status.value?.siloEnabled) },
  { key: 'gdrive' as const, label: 'Google Drive', enabled: Boolean(store.status.value?.gdriveEnabled) },
])

const activeModeIdx = computed(() =>
  Math.max(0, modeOptions.value.findIndex((o) => o.key === mode.value)),
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
    const created = await store.sendTextClip(txt)
    draftText.value = ''
    sendState.value = 'success'
    if (autoPublicShare.value && created) {
      await shareClip(created, false)
    }
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
    // Fast direct paste route for < 20 MiB private or Silo image-host without public /s/ share
    if (
      !autoPublicShare.value &&
      f.size <= 20 * 1024 * 1024 &&
      (mode.value === 'local' || mode.value === 'silo')
    ) {
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
      ttlHours: autoPublicShare.value ? ttlHours.value : 0,
      maxDownloads: 0,
      passcode: autoPublicShare.value ? passcode.value : '',
      addToClip: true,
      skipShare: !autoPublicShare.value,
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

// 1-Tap Mobile & Desktop System Clipboard Reader!
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
    showToast('请允许剪贴板读取权限，或直接在下方输入框长按粘贴')
  }
}

async function copyLatestItem() {
  const first = store.clips.value[0]
  if (!first) {
    showToast('收发台暂无内容')
    return
  }
  if (first.kind === 'text' && first.text) {
    await copyText(first.text, '已复制最新文本')
  } else if (first.kind === 'image') {
    if (first.publicUrl && mode.value === 'silo') {
      await copyText(formatMarkdownImage(first.fileName || 'image', first.publicUrl), '已复制最新图床 Markdown')
    } else {
      await copyImagePixels(`/api/clips/${first.id}/raw`)
    }
  } else if (first.publicUrl) {
    await copyText(first.publicUrl, '已复制最新文件直链')
  } else {
    window.location.href = `/api/clips/${first.id}/raw?download=1`
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

async function shareClip(item: ClipItem, showQr = true) {
  if (item.shareCode) {
    const url = shareFullUrl(item.shareCode)
    await copyText(url, '已复制公开分享短链')
    if (showQr) store.openQrModal(item.fileName || '文本片段', url, '外部访客免登录扫码直取')
    return
  }
  const res = await fetch(`/api/clips/${item.id}/share`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      ttlHours: ttlHours.value,
      passcode: passcode.value,
    }),
  })
  if (res.ok) {
    const sh = await res.json()
    item.shareCode = sh.code
    if (!store.shares.value.some((s) => s.code === sh.code)) {
      store.shares.value = [sh, ...store.shares.value]
    }
    const url = shareFullUrl(sh.code)
    await copyText(url, '公开短链已生成并复制')
    if (showQr) {
      store.openQrModal(
        item.fileName || '文本片段',
        url,
        sh.hasPasscode ? '已设提取码保护 · 外部扫码取件' : '免登录公开短链 · 手机相机扫码直取',
      )
    }
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
  if (st === 'silo') return 'Silo 图床/桶'
  if (st === 'gdrive') return 'Google Drive'
  return '本机私密'
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

function onDocClick(e: MouseEvent) {
  if (optsWrap.value && !optsWrap.value.contains(e.target as Node)) {
    openShareOpts.value = false
  }
}

// Listen to global ⌘V from App.vue respecting current Hub mode (e.g. Silo 图床模式!)
function onHubGlobalPaste(e: Event) {
  const files = (e as CustomEvent<File[]>).detail
  if (files?.length) dispatchFiles(files)
}

onMounted(() => {
  document.addEventListener('mousedown', onDocClick)
  window.addEventListener('oarc-hub-files', onHubGlobalPaste)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocClick)
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
        <p class="pane-subtitle">
          <template v-if="mode === 'silo'">
            图床直传模式 · 粘贴或拖入图片即存 `{{ siloPrefix }}` 并自动复制 {{ autoCopyFmt === 'md' ? 'Markdown' : 'URL' }}
          </template>
          <template v-else-if="mode === 'gdrive'">
            大文件切片直入 Google Drive (`gdrive:OArc-Drop`) · 另一台设备点「直下」即可取回
          </template>
          <template v-else>
            跨端私密收发 · 默认不生成公开外链，另一台设备打开即看即下
          </template>
        </p>
      </div>

      <div class="pane-actions mode-bar">
        <div class="tab-pill-group" :style="{ gridTemplateColumns: 'repeat(3, 1fr)' }">
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
      </div>
    </header>

    <div class="pane-body--narrow hub-stack">
      <!-- Silo Image-Host Strip (only when Silo mode selected) -->
      <Transition name="tray-expand">
        <div v-if="mode === 'silo'" class="glass-box imghost-bar">
          <div class="ih-group">
            <span class="ih-lbl">写入目录</span>
            <button
              v-for="p in ['images/', 'drop/', 'articles/', 'photos/']"
              :key="p"
              type="button"
              class="silo-chip mono"
              :class="{ 'is-on': siloPrefix === p }"
              @click="setPrefix(p)"
            >
              {{ p }}
            </button>
            <input v-model="siloPrefix" class="silo-custom mono" placeholder="自定义/" />
          </div>

          <div class="ih-group">
            <span class="ih-lbl">自动复制</span>
            <button
              type="button"
              class="silo-chip"
              :class="{ 'is-on': autoCopyFmt === 'md' }"
              @click="setAutoCopyFmt('md')"
            >
              Markdown
            </button>
            <button
              type="button"
              class="silo-chip"
              :class="{ 'is-on': autoCopyFmt === 'url' }"
              @click="setAutoCopyFmt('url')"
            >
              纯直链
            </button>
          </div>
        </div>
      </Transition>

      <!-- Unified Composer + Drag-Drop Pod -->
      <section class="glass-box composer-pod" :class="{ 'is-drag': isDragOver }">
        <div class="composer-main">
          <textarea
            v-model="draftText"
            rows="2"
            class="composer-textarea"
            :placeholder="
              mode === 'silo'
                ? '在此按 ⌘V 粘贴截图直传图床，或拖入任意文件 / 输入文本笔记…'
                : '输入文本/代码（⌘+Enter 发送），或直接粘贴截图、拖入任意体积大文件…'
            "
            @keydown="handleComposerKey"
            @paste="handleComposerPaste"
          />
        </div>

        <footer class="composer-bar">
          <div class="cbar-left">
            <button
              type="button"
              class="pill-btn pill-btn--accent"
              title="一键读取手机或电脑当前剪贴板并发送"
              @click="pasteFromSystemClipboard"
            >
              <UiIcon name="clipboard" :size="14" />
              <span>粘贴剪贴板</span>
            </button>

            <button type="button" class="pill-btn" @click="fileInput?.click()">
              <UiIcon name="plus" :size="14" />
              <span>选文件 / 原图</span>
            </button>
            <input ref="fileInput" type="file" multiple hidden @change="onPickedFiles" />

            <button
              v-if="store.clips.value.length > 0"
              type="button"
              class="pill-btn hide-xs"
              title="复制收发台最新一条记录"
              @click="copyLatestItem"
            >
              <UiIcon name="copy" :size="13.5" />
              <span>复制最新</span>
            </button>

            <!-- Optional Public Share Toggle -->
            <div ref="optsWrap" class="pop-anchor">
              <button
                type="button"
                class="pill-btn"
                :class="{ 'is-highlighted': autoPublicShare }"
                @click="openShareOpts = !openShareOpts"
              >
                <UiIcon :name="autoPublicShare ? 'link' : 'lock'" :size="13.5" />
                <span>{{ autoPublicShare ? '同步出外链' : '仅自己可见' }}</span>
              </button>

              <Transition name="spring-menu">
                <div v-if="openShareOpts" class="liquid-chrome share-pop">
                  <label class="pop-check">
                    <input v-model="autoPublicShare" type="checkbox" />
                    <span>上传时自动生成 `/s/短码` 公开取件链接</span>
                  </label>
                  <template v-if="autoPublicShare">
                    <div class="pop-field">
                      <span>有效期</span>
                      <select v-model.number="ttlHours" class="pop-select">
                        <option :value="24">24 小时</option>
                        <option :value="168">7 天</option>
                        <option :value="720">30 天</option>
                        <option :value="0">长期有效</option>
                      </select>
                    </div>
                    <div class="pop-field">
                      <span>提取码</span>
                      <input v-model="passcode" class="pop-input mono" placeholder="留空免密" maxlength="20" />
                    </div>
                  </template>
                </div>
              </Transition>
            </div>
          </div>

          <button
            type="button"
            class="submit-circle"
            :class="`state-${sendState}`"
            :disabled="!draftText.trim() && sendState === 'idle'"
            title="发送文本 (⌘+Enter)"
            @click="submitText"
          >
            <span v-if="sendState === 'loading'" class="mini-spinner" />
            <UiIcon v-else-if="sendState === 'success'" name="check" :size="16" />
            <UiIcon v-else name="arrow-up" :size="16.5" />
          </button>
        </footer>
      </section>

      <!-- Live Chunked Upload Tasks -->
      <div v-if="activeUploads.length > 0" class="upload-queue">
        <div v-for="u in activeUploads" :key="u.localId" class="glass-box up-row">
          <div class="up-top">
            <span class="up-name">{{ u.fileName }}</span>
            <span class="up-stat mono">
              <template v-if="u.status === 'uploading'">
                {{ formatBytes(u.uploadedBytes) }} / {{ formatBytes(u.size) }} · {{ formatBytes(u.speedBps) }}/s
              </template>
              <template v-else-if="u.status === 'assembling'">正在服务端合并校验…</template>
              <template v-else class="err-txt">{{ u.error }}</template>
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

      <!-- Stream Filter & Search -->
      <div class="stream-header">
        <div class="filter-chips">
          <button
            v-for="f in [
              { k: 'all' as const, l: '全部' },
              { k: 'text' as const, l: '文本' },
              { k: 'image' as const, l: '图片' },
              { k: 'file' as const, l: '文件' },
            ]"
            :key="f.k"
            type="button"
            class="silo-chip"
            :class="{ 'is-on': streamFilter === f.k }"
            @click="streamFilter = f.k"
          >
            {{ f.l }}
          </button>
        </div>

        <div class="search-pill mini-search">
          <UiIcon name="search" :size="14" />
          <input v-model="searchQuery" placeholder="筛选收发记录…" />
        </div>
      </div>

      <!-- Empty State -->
      <div v-if="filteredClips.length === 0" class="glass-box empty-card">
        暂无匹配的收发记录 · 直接拖入文件、按 <kbd class="mono">⌘V</kbd> 或点「粘贴剪贴板」即可跨设备秒传
      </div>

      <!-- Unified Real-Time Feed -->
      <TransitionGroup name="stack-slide" tag="div" class="clip-stream">
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
              <span v-if="item.kind !== 'text'" class="storage-tag mono">
                {{ storageBadge(item.storage) }}
              </span>
              <span v-if="item.pinned" class="pin-pill">置顶</span>
              <span class="clip-meta-txt mono">
                {{ formatBytes(item.size) }} · {{ formatTimeShort(item.createdAt) }}
              </span>
            </div>

            <div class="clip-toolbar">
              <!-- Text Actions -->
              <template v-if="item.kind === 'text'">
                <CopyPill :text="item.text || ''" label="复制" copied-label="已复制" compact />
                <button
                  v-if="(item.text || '').length <= 240"
                  type="button"
                  class="mini-icon-act"
                  title="手机扫码读取文本/链接"
                  @click="store.openQrModal('扫码读取文本', item.text || '')"
                >
                  <UiIcon name="qr" :size="14" />
                </button>
              </template>

              <!-- Image Actions: Copy Image Pixels + Image-Host Markdown/URL + Download -->
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
                  class="pill-btn pill-btn--mini"
                  title="直接复制原图像素到系统剪贴板（微信/文档直接粘贴）"
                  @click="copyImagePixels(`/api/clips/${item.id}/raw`)"
                >
                  <UiIcon name="copy" :size="13" />
                  <span>复制原图</span>
                </button>
                <a class="mini-icon-act" :href="`/api/clips/${item.id}/raw?download=1`" title="下载原图文件">
                  <UiIcon name="download" :size="14" />
                </a>
              </template>

              <!-- General File Actions: Preview + Direct Authenticated Download! -->
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
                  class="pill-btn pill-btn--mini"
                  @click="inlinePreviewIds[item.id] = !inlinePreviewIds[item.id]"
                >
                  <UiIcon name="eye" :size="13" />
                  <span>{{ inlinePreviewIds[item.id] ? '收起' : '预览' }}</span>
                </button>
                <a class="pill-btn pill-btn--mini pill-btn--accent" :href="`/api/clips/${item.id}/raw?download=1`">
                  <UiIcon name="download" :size="13.5" />
                  <span>直下</span>
                </a>
              </template>

              <!-- On-demand Public Share + QR -->
              <button
                type="button"
                class="mini-icon-act"
                :class="{ 'is-shared': Boolean(item.shareCode) }"
                :title="item.shareCode ? `已生成外链 /s/${item.shareCode}（点击复制并看二维码）` : '生成对外公开分享短链 + 二维码'"
                @click="shareClip(item, true)"
              >
                <UiIcon :name="item.shareCode ? 'qr' : 'share'" :size="14" />
              </button>

              <button
                type="button"
                class="mini-icon-act"
                :class="{ 'is-pinned-btn': item.pinned }"
                title="置顶"
                @click="togglePin(item)"
              >
                <UiIcon name="pin" :size="14" />
              </button>
              <button
                type="button"
                class="mini-icon-act is-danger"
                title="删除"
                @click="removeClip(item)"
              >
                <UiIcon name="trash" :size="14" />
              </button>
            </div>
          </div>

          <!-- Card Body -->
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

          <!-- Inline Media / PDF Preview -->
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
      </TransitionGroup>
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
                <a class="pill-btn" :href="`/api/clips/${lightboxClip.id}/raw?download=1`">
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
  gap: 14px;
}

.mode-bar .tab-pill-group {
  min-width: 268px;
}

.imghost-bar {
  padding: 10px 14px;
  border-radius: 18px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.ih-group {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.ih-lbl {
  font-size: 0.74rem;
  font-weight: 620;
  color: var(--ink-3);
  margin-right: 2px;
}

.silo-chip {
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.74rem;
  cursor: pointer;
  transition: all 0.18s ease;
}

.silo-chip.is-on {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: rgba(0, 113, 227, 0.28);
  font-weight: 620;
}

.silo-custom {
  width: 96px;
  padding: 4px 9px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.74rem;
  outline: none;
}

.composer-pod {
  border-radius: 24px;
  padding: 14px 16px 12px;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.composer-pod.is-drag {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.composer-textarea {
  width: 100%;
  border: none;
  background: transparent;
  color: var(--ink-1);
  font: inherit;
  font-size: 0.91rem;
  line-height: 1.55;
  resize: vertical;
  min-height: 52px;
  max-height: 220px;
  outline: none;
}

.composer-textarea::placeholder {
  color: var(--ink-3);
}

.composer-bar {
  margin-top: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--border-hairline);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.cbar-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.pill-btn--accent {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: rgba(0, 113, 227, 0.24);
  font-weight: 610;
}

.pill-btn--mini {
  padding: 4px 9px;
  font-size: 0.74rem;
}

.pop-anchor {
  position: relative;
}

.share-pop {
  position: absolute;
  top: calc(100% + 8px);
  left: 0;
  z-index: 60;
  width: 268px;
  padding: 13px 15px;
  border-radius: 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.pop-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.78rem;
  font-weight: 560;
  cursor: pointer;
}

.pop-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 0.76rem;
  color: var(--ink-2);
}

.pop-select,
.pop-input {
  width: 148px;
  padding: 5px 9px;
  border-radius: 10px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.76rem;
  outline: none;
}

.submit-circle {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: none;
  background: var(--accent);
  color: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
  flex-shrink: 0;
  transition: transform 0.18s ease, opacity 0.18s ease;
}

.submit-circle:disabled {
  opacity: 0.35;
  cursor: default;
}

.submit-circle.state-success {
  background: #10b981;
}

.upload-queue {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.up-row {
  padding: 11px 15px;
  border-radius: 16px;
}

.up-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 0.8rem;
}

.up-name {
  font-weight: 610;
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
  align-items: center;
  gap: 6px;
}

.mini-search {
  min-width: 180px;
  padding: 5px 11px;
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
  gap: 11px;
}

.clip-card {
  padding: 13px 16px;
  border-radius: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.clip-card.is-pinned {
  border-color: rgba(0, 113, 227, 0.3);
}

.clip-top {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
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
  font-weight: 630;
  word-break: break-all;
}

.storage-tag,
.pin-pill {
  font-size: 0.68rem;
  padding: 2px 7px;
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
  gap: 5px;
}

.mini-icon-act {
  width: 28px;
  height: 28px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
  transition: all 0.16s ease;
}

.mini-icon-act:hover {
  color: var(--ink-1);
  background: var(--surface-hover);
}

.mini-icon-act.is-shared,
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

@media (max-width: 720px) {
  .hide-xs {
    display: none;
  }
  .mode-bar .tab-pill-group {
    min-width: 0;
    width: 100%;
  }
}
</style>
