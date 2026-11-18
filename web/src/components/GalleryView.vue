<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  formatBytes,
  formatTimeShort,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import type { MediaItem } from '../types'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()
const search = ref('')
const activeFolder = ref('')
const activePhoto = ref<MediaItem | null>(null)
const naturalDim = ref('')
const refreshing = ref(false)
const generatedShares = ref<Record<string, string>>({})

onMounted(() => {
  if (store.mediaItems.value.length === 0) {
    void store.loadMedia(false)
  }
})

const imageFolders = computed(() => {
  const set = new Set<string>()
  for (const it of store.imageItems.value) set.add(it.folder)
  return Array.from(set).sort()
})

const filteredImages = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.imageItems.value.filter((it) => {
    if (activeFolder.value && it.folder !== activeFolder.value) return false
    if (q && !it.key.toLowerCase().includes(q)) return false
    return true
  })
})

async function refreshGallery() {
  refreshing.value = true
  try {
    await store.loadMedia(true)
  } finally {
    refreshing.value = false
  }
}

async function createSiloShare(item: MediaItem) {
  const res = await fetch('/api/shares/from-silo', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key: item.key, size: item.size, ttlHours: 0 }),
  })
  if (res.ok) {
    const sh = await res.json()
    const url = shareFullUrl(sh.code)
    generatedShares.value[item.key] = url
    await navigator.clipboard.writeText(url).catch(() => {})
    showToast('分享短链已生成并复制')
  }
}

function onTileMove(e: MouseEvent) {
  const el = e.currentTarget as HTMLElement
  const r = el.getBoundingClientRect()
  const px = (e.clientX - r.left) / r.width
  const py = (e.clientY - r.top) / r.height
  el.style.setProperty('--rx', `${((0.5 - py) * 6).toFixed(2)}deg`)
  el.style.setProperty('--ry', `${((px - 0.5) * 6).toFixed(2)}deg`)
  el.style.setProperty('--lx', `${(px * 100).toFixed(1)}%`)
  el.style.setProperty('--ly', `${(py * 100).toFixed(1)}%`)
}

function onTileLeave(e: MouseEvent) {
  const el = e.currentTarget as HTMLElement
  el.style.setProperty('--rx', '0deg')
  el.style.setProperty('--ry', '0deg')
}

function openLightbox(item: MediaItem) {
  naturalDim.value = ''
  activePhoto.value = item
}

function onFullLoad(e: Event) {
  const img = e.target as HTMLImageElement
  if (img.naturalWidth) {
    naturalDim.value = `${img.naturalWidth} × ${img.naturalHeight}`
  }
}
</script>

<template>
  <div class="gallery-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">Silo 画廊</h1>
        <p class="pane-subtitle">共 {{ store.imageCount.value }} 张原图索引 · 已自动去重配对缩略图</p>
      </div>

      <div class="pane-actions search-bar-wrap">
        <div class="search-pill">
          <UiIcon name="search" :size="15" />
          <input v-model="search" placeholder="搜索文件名或路径…" />
        </div>
        <button type="button" class="icon-circle-btn" title="重新扫描 Silo" @click="refreshGallery">
          <UiIcon name="refresh" :size="15" :class="{ 'is-spinning': refreshing }" />
        </button>
      </div>
    </header>

    <div class="pane-body">
      <div v-if="imageFolders.length > 0" class="folder-pills">
        <button
          type="button"
          class="folder-pill"
          :class="{ 'is-on': activeFolder === '' }"
          @click="activeFolder = ''"
        >
          全部 ({{ store.imageCount.value }})
        </button>
        <button
          v-for="f in imageFolders"
          :key="f"
          type="button"
          class="folder-pill mono"
          :class="{ 'is-on': activeFolder === f }"
          @click="activeFolder = f"
        >
          {{ f }}
        </button>
      </div>

      <div v-if="filteredImages.length === 0" class="glass-box gal-empty">
        没有匹配的图像对象
      </div>

      <div v-else class="lens-grid">
        <article
          v-for="img in filteredImages"
          :key="img.key"
          class="lens-tile"
          @mousemove="onTileMove"
          @mouseleave="onTileLeave"
          @click="openLightbox(img)"
        >
          <img :src="img.thumbUrl || img.streamUrl" :alt="img.name" loading="lazy" />
          <div class="lens-glare" />

          <div class="tile-scrim" @click.stop>
            <div class="tile-info" @click="openLightbox(img)">
              <div class="tile-name">{{ img.name }}</div>
              <div class="tile-meta mono">{{ img.ext }} · {{ formatBytes(img.size) }}</div>
            </div>
            <div class="tile-btns">
              <CopyPill
                v-if="img.publicUrl"
                :text="img.publicUrl"
                label="直链"
                copied-label="已复制"
                compact
              />
              <CopyPill
                v-if="generatedShares[img.key]"
                :text="generatedShares[img.key]"
                label="短链"
                copied-label="已复制"
                compact
              />
              <button
                v-else
                type="button"
                class="scrim-circle"
                title="生成分享短链"
                @click="createSiloShare(img)"
              >
                <UiIcon name="link" :size="13.5" />
              </button>
            </div>
          </div>
        </article>
      </div>
    </div>

    <!-- Lightbox -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="activePhoto" class="lens-backdrop" @click.self="activePhoto = null">
          <div class="liquid-chrome lens-dialog">
            <button
              type="button"
              class="icon-circle-btn dialog-close"
              @click="activePhoto = null"
            >
              <UiIcon name="close" :size="16" />
            </button>

            <div class="dialog-stage">
              <img :src="activePhoto.streamUrl" :alt="activePhoto.name" @load="onFullLoad" />
            </div>

            <footer class="dialog-bar">
              <div class="dialog-meta">
                <div class="d-name">{{ activePhoto.name }}</div>
                <div class="d-spec mono">
                  {{ activePhoto.key }} · {{ formatBytes(activePhoto.size) }}
                  <template v-if="naturalDim"> · {{ naturalDim }}</template>
                  · {{ formatTimeShort(activePhoto.lastModified) }}
                </div>
              </div>
              <div class="dialog-actions">
                <CopyPill
                  v-if="activePhoto.publicUrl"
                  :text="activePhoto.publicUrl"
                  label="复制直链"
                  copied-label="已复制"
                />
                <CopyPill
                  v-if="activePhoto.publicUrl"
                  :text="`![${activePhoto.name}](${activePhoto.publicUrl})`"
                  label="Markdown"
                  copied-label="已复制"
                />
                <button type="button" class="pill-btn" @click="createSiloShare(activePhoto)">
                  <UiIcon name="link" :size="14" />
                  <span>分享短链</span>
                </button>
                <a class="pill-btn pill-btn--solid" :href="`${activePhoto.streamUrl}&download=1`">
                  <UiIcon name="download" :size="14" />
                  <span>下载原图</span>
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
@media (max-width: 860px) {
  .search-bar-wrap {
    width: 100%;
  }
}

.is-spinning {
  animation: spin360 0.8s linear infinite;
}
@keyframes spin360 {
  to { transform: rotate(360deg); }
}

.folder-pills {
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 4px;
  margin-bottom: 18px;
}

.folder-pill {
  height: 30px;
  padding: 0 13px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.76rem;
  white-space: nowrap;
  cursor: pointer;
  transition: all 0.2s ease;
}

.folder-pill.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.gal-empty {
  padding: 56px 20px;
  text-align: center;
  font-size: 0.85rem;
  color: var(--ink-3);
}

.lens-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(228px, 1fr));
  gap: 16px;
  perspective: 900px;
}

@media (max-width: 600px) {
  .lens-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }
}

.lens-tile {
  --rx: 0deg;
  --ry: 0deg;
  --lx: 50%;
  --ly: 20%;
  position: relative;
  aspect-ratio: 4 / 3;
  border-radius: 18px;
  overflow: hidden;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  box-shadow: var(--shadow-card);
  cursor: pointer;
  transform: rotateX(var(--rx)) rotateY(var(--ry));
  transition: transform 0.22s ease-out, box-shadow 0.28s ease;
}

.lens-tile img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  transition: transform 0.5s var(--ease-out-quart);
}

.lens-tile:hover img {
  transform: scale(1.04);
}

.lens-glare {
  position: absolute;
  inset: 0;
  pointer-events: none;
  opacity: 0;
  background: radial-gradient(200px circle at var(--lx) var(--ly), rgba(255, 255, 255, 0.34), transparent 70%);
  transition: opacity 0.25s ease;
}

.lens-tile:hover .lens-glare {
  opacity: 1;
}

.tile-scrim {
  position: absolute;
  inset: auto 8px 8px 8px;
  padding: 8px 10px;
  border-radius: 13px;
  background: rgba(255, 255, 255, 0.85);
  -webkit-backdrop-filter: blur(18px) saturate(180%);
  backdrop-filter: blur(18px) saturate(180%);
  border: 1px solid rgba(255, 255, 255, 0.75);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  opacity: 0;
  transform: translateY(6px);
  transition: opacity 0.22s ease, transform 0.28s var(--ease-spring);
}

[data-theme="dark"] .tile-scrim {
  background: rgba(18, 20, 26, 0.86);
  border-color: rgba(255, 255, 255, 0.12);
}

.lens-tile:hover .tile-scrim {
  opacity: 1;
  transform: translateY(0);
}

@media (max-width: 600px) {
  .tile-scrim {
    inset: auto 6px 6px 6px;
    padding: 6px 8px;
    opacity: 1;
    transform: none;
  }
  .tile-btns {
    display: none !important;
  }
}

.tile-info {
  min-width: 0;
}

.tile-name {
  font-size: 0.76rem;
  font-weight: 620;
  color: var(--ink-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tile-meta {
  font-size: 0.67rem;
  color: var(--ink-2);
}

.tile-btns {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.scrim-circle {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-1);
  display: grid;
  place-items: center;
  cursor: pointer;
}

.lens-backdrop {
  position: fixed;
  inset: 0;
  z-index: 100;
  background: rgba(10, 12, 18, 0.5);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  padding: 16px;
}

.lens-dialog {
  position: relative;
  width: min(980px, 100%);
  max-height: 90dvh;
  border-radius: 26px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.dialog-close {
  position: absolute;
  top: 14px;
  right: 14px;
  z-index: 2;
}

.dialog-stage {
  border-radius: 16px;
  overflow: hidden;
  background: var(--surface-recessed);
  display: grid;
  place-items: center;
  max-height: 66dvh;
}

.dialog-stage img {
  max-width: 100%;
  max-height: 66dvh;
  object-fit: contain;
}

.dialog-bar {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.d-name {
  font-size: 0.92rem;
  font-weight: 650;
  word-break: break-all;
}

.d-spec {
  margin-top: 2px;
  font-size: 0.73rem;
  color: var(--ink-2);
}

.dialog-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.lens-modal-enter-active,
.lens-modal-leave-active {
  transition: opacity 0.22s ease, transform 0.3s var(--ease-spring);
}
.lens-modal-enter-from,
.lens-modal-leave-to {
  opacity: 0;
  transform: scale(0.95);
}
</style>
