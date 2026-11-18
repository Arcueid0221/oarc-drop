<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AudioEngine from './components/AudioEngine.vue'
import HubView from './components/HubView.vue'
import MusicView from './components/MusicView.vue'
import PublicSharePortal from './components/PublicSharePortal.vue'
import QrDialog from './components/QrDialog.vue'
import SharesView from './components/SharesView.vue'
import UiIcon from './components/UiIcon.vue'
import VaultView from './components/VaultView.vue'
import { formatBytes, useDropStore } from './composables/useDropStore'

const store = useDropStore()

type NavTab = 'hub' | 'vault' | 'music' | 'shares'

const navItems = computed<
  Array<{
    key: NavTab
    label: string
    shortLabel: string
    icon: 'layers' | 'folder' | 'disc' | 'link'
    badge?: string | number
  }>
>(() => [
  {
    key: 'hub',
    label: '收发台',
    shortLabel: '收发',
    icon: 'layers',
    badge: store.clips.value.length || undefined,
  },
  {
    key: 'vault',
    label: '文件柜',
    shortLabel: '文件柜',
    icon: 'folder',
    badge: store.siloObjects.value.length || store.imageCount.value || undefined,
  },
  {
    key: 'music',
    label: '音乐厅',
    shortLabel: '音乐',
    icon: 'disc',
    badge: store.audioCount.value || undefined,
  },
  {
    key: 'shares',
    label: '外链管理',
    shortLabel: '外链',
    icon: 'link',
    badge: store.shares.value.length || undefined,
  },
])

const activeTab = ref<NavTab>('hub')
const activeIdx = computed(() => Math.max(0, navItems.value.findIndex((i) => i.key === activeTab.value)))

const theme = ref<'light' | 'dark'>(
  (localStorage.getItem('oarc_drop_theme') as 'light' | 'dark') || 'light',
)

function applyTheme(val: 'light' | 'dark') {
  theme.value = val
  document.documentElement.setAttribute('data-theme', val)
  localStorage.setItem('oarc_drop_theme', val)
}

function toggleTheme() {
  applyTheme(theme.value === 'light' ? 'dark' : 'light')
}

const shareCode = computed(() => {
  const m = window.location.pathname.match(/^\/s\/([A-Za-z0-9_-]+)\/?$/)
  return m ? m[1] : ''
})

const activeUpload = computed(() =>
  store.uploads.value.find((u) => u.status === 'uploading' || u.status === 'assembling'),
)

const uploadPct = computed(() => {
  const u = activeUpload.value
  if (!u || u.size <= 0) return 0
  return Math.min(100, Math.round((u.uploadedBytes / u.size) * 100))
})

const RULER_TICKS = 20
const activeTicks = computed(() => {
  const tot = store.status.value?.diskTotal || 0
  const free = store.status.value?.diskFree || 0
  if (!tot) return 3
  const usedRatio = Math.min(1, Math.max(0.05, (tot - free) / tot))
  return Math.max(1, Math.round(usedRatio * RULER_TICKS))
})

function toggleAudioPlay() {
  window.dispatchEvent(new CustomEvent('oarc-audio-toggle'))
}

function nextAudioTrack() {
  window.dispatchEvent(new CustomEvent('oarc-audio-step', { detail: 1 }))
}

let stopSSE: (() => void) | null = null

async function onGlobalPaste(e: ClipboardEvent) {
  if (shareCode.value) return
  const el = e.target as HTMLElement | null
  if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) {
    return
  }
  const files = e.clipboardData?.files
  if (files && files.length > 0) {
    e.preventDefault()
    if (activeTab.value === 'hub') {
      window.dispatchEvent(new CustomEvent('oarc-hub-files', { detail: Array.from(files) }))
      return
    }
    for (const f of Array.from(files)) {
      await store.sendQuickPasteFile(f, { target: 'local', autoCopyFormat: 'none' })
    }
    return
  }
  const txt = e.clipboardData?.getData('text/plain')
  if (txt && txt.trim()) {
    e.preventDefault()
    await store.sendTextClip(txt)
  }
}

onMounted(() => {
  applyTheme(theme.value)
  if (!shareCode.value) {
    void store.loadInitial()
    void store.loadMedia(false)
    void store.loadMusicCatalog(false)
    void store.loadGDriveFolders()
    stopSSE = store.connectSSE()
    window.addEventListener('paste', onGlobalPaste)
  }
})

onBeforeUnmount(() => {
  if (stopSSE) stopSSE()
  window.removeEventListener('paste', onGlobalPaste)
})
</script>

<template>
  <div class="frost-backdrop" aria-hidden="true" />
  <div
    class="frost-cover-aura"
    :class="{ 'is-active': Boolean(store.currentTrack.value && store.isPlaying.value) }"
    aria-hidden="true"
  />

  <Transition name="toast-slide">
    <div v-if="store.toastText.value" class="toast-anchor">
      <div class="liquid-chrome toast-pill">
        <span class="toast-ok">
          <UiIcon name="check" :size="11.5" />
        </span>
        <span>{{ store.toastText.value }}</span>
      </div>
    </div>
  </Transition>

  <PublicSharePortal v-if="shareCode" :code="shareCode" />

  <div v-else class="desktop-frame" :class="{ 'is-music-locked': activeTab === 'music' }">
    <div class="macos-window">
      <!-- Precision Studio Sidebar -->
      <aside class="macos-sidebar">
        <div class="sb-top">
          <div class="brand-lockup" @click="activeTab = 'hub'">
            <svg class="brand-emblem" viewBox="0 0 30 30" fill="none" aria-hidden="true">
              <rect width="30" height="30" rx="7.5" class="emblem-plate" />
              <rect x="1" y="1" width="28" height="28" rx="6.5" class="emblem-bevel" />
              <circle cx="15" cy="15" r="9.2" class="emblem-ring-outer" />
              <circle cx="15" cy="15" r="5.6" class="emblem-ring-inner" />
              <path
                d="M15 7.4v7.6m0 0 2.9-2.9M15 15l-2.9-2.9"
                class="emblem-arrow"
              />
              <circle
                cx="22.8"
                cy="7.2"
                r="1.8"
                class="emblem-led"
                :class="{ 'is-live': Boolean(activeUpload || store.isPlaying.value) }"
              />
            </svg>

            <div class="brand-wordmark">
              <span class="brand-serif">O.Arc</span>
              <span class="brand-code mono">DROP</span>
            </div>
          </div>

          <button
            type="button"
            class="icon-circle-btn sb-theme-btn"
            :title="theme === 'light' ? '切换曜石深色' : '切换冰晶浅色'"
            @click="toggleTheme"
          >
            <UiIcon :name="theme === 'light' ? 'sun' : 'moon'" :size="14" />
          </button>
        </div>

        <!-- Tactile Vertical Switchboard -->
        <nav class="sb-nav">
          <div
            class="sb-nav-indicator"
            :style="{ transform: `translateY(${activeIdx * 42}px)` }"
          />
          <button
            v-for="item in navItems"
            :key="item.key"
            type="button"
            class="sb-nav-btn"
            :class="{ 'is-active': activeTab === item.key }"
            @click="activeTab = item.key"
          >
            <UiIcon :name="item.icon" :size="15.5" />
            <span class="sb-nav-label">{{ item.label }}</span>

            <span v-if="item.key === 'hub' && activeUpload" class="sb-live-pill mono">
              {{ uploadPct }}%
            </span>
            <span
              v-else-if="item.key === 'music' && store.currentTrack.value && store.isPlaying.value"
              class="sb-eq"
            >
              <i /><i /><i />
            </span>
            <span v-else-if="item.badge !== undefined" class="sb-count mono">
              {{ item.badge }}
            </span>
          </button>
        </nav>

        <!-- Bottom Hardware Dock -->
        <div class="sb-bottom">
          <Transition name="fade-up">
            <div
              v-if="store.currentTrack.value"
              class="glass-box sb-player"
              @click="activeTab = 'music'"
            >
              <div class="sbp-disc" :class="{ 'is-spinning': store.isPlaying.value }">
                <img v-if="store.currentCoverUrl.value" :src="store.currentCoverUrl.value" alt="" />
                <span v-else class="sbp-core" />
              </div>
              <div class="sbp-meta">
                <div class="sbp-title">
                  {{ store.currentMeta.value?.title || store.currentTrack.value.name }}
                </div>
                <div class="sbp-sub">
                  {{ store.currentMeta.value?.artist || store.currentMeta.value?.format }}
                </div>
              </div>
              <div class="sbp-btns" @click.stop>
                <button type="button" class="sbp-btn" @click="toggleAudioPlay">
                  <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="12.5" />
                </button>
                <button type="button" class="sbp-btn" @click="nextAudioTrack">
                  <UiIcon name="next" :size="12.5" />
                </button>
              </div>
            </div>
          </Transition>

          <!-- Precision 20-Segment Storage Tick Ruler -->
          <div v-if="store.status.value?.diskTotal" class="sb-meter">
            <div class="meter-row mono">
              <span>STORAGE</span>
              <span>FREE {{ formatBytes(store.status.value.diskFree) }}</span>
            </div>
            <div class="tick-ruler" aria-hidden="true">
              <i
                v-for="n in RULER_TICKS"
                :key="n"
                :class="{ 'is-lit': n <= activeTicks }"
              />
            </div>
          </div>

          <div v-if="store.status.value?.headerLinks?.length" class="sb-links">
            <a
              v-for="lk in store.status.value.headerLinks"
              :key="lk.url"
              :href="lk.url"
              target="_blank"
              rel="noopener"
              class="sb-ext-link"
            >
              <span>{{ lk.label }}</span>
              <UiIcon name="external" :size="11" />
            </a>
          </div>
        </div>
      </aside>

      <!-- Mobile Top Safe-Area Bar -->
      <header class="mobile-topbar">
        <div class="brand-lockup" @click="activeTab = 'hub'">
          <svg class="brand-emblem brand-emblem--sm" viewBox="0 0 30 30" fill="none" aria-hidden="true">
            <rect width="30" height="30" rx="7.5" class="emblem-plate" />
            <rect x="1" y="1" width="28" height="28" rx="6.5" class="emblem-bevel" />
            <circle cx="15" cy="15" r="9.2" class="emblem-ring-outer" />
            <circle cx="15" cy="15" r="5.6" class="emblem-ring-inner" />
            <path d="M15 7.4v7.6m0 0 2.9-2.9M15 15l-2.9-2.9" class="emblem-arrow" />
            <circle cx="22.8" cy="7.2" r="1.8" class="emblem-led" />
          </svg>
          <div class="brand-wordmark">
            <span class="brand-serif">O.Arc</span>
            <span class="brand-code mono">DROP</span>
          </div>
          <span v-if="activeUpload" class="m-up-pill mono">{{ uploadPct }}%</span>
        </div>

        <div class="m-right">
          <a
            v-for="lk in (store.status.value?.headerLinks || []).slice(0, 2)"
            :key="lk.url"
            :href="lk.url"
            target="_blank"
            rel="noopener"
            class="m-ext"
          >
            {{ lk.label }}
          </a>
          <button type="button" class="icon-circle-btn" @click="toggleTheme">
            <UiIcon :name="theme === 'light' ? 'sun' : 'moon'" :size="14.5" />
          </button>
        </div>
      </header>

      <!-- Main Right Canvas -->
      <main class="macos-canvas" :class="{ 'is-locked-pane': activeTab === 'music' }">
        <Transition name="pane-fade" mode="out-in">
          <HubView v-if="activeTab === 'hub'" key="hub" />
          <VaultView v-else-if="activeTab === 'vault'" key="vault" />
          <MusicView v-else-if="activeTab === 'music'" key="music" />
          <SharesView v-else key="shares" />
        </Transition>
      </main>
    </div>

    <!-- Mobile Mini Player + Bottom Tab Bar -->
    <div class="mobile-bottom-stack">
      <Transition name="fade-up">
        <div
          v-if="store.currentTrack.value && activeTab !== 'music'"
          class="liquid-chrome mobile-mini-player"
          @click="activeTab = 'music'"
        >
          <div class="sbp-disc" :class="{ 'is-spinning': store.isPlaying.value }">
            <img v-if="store.currentCoverUrl.value" :src="store.currentCoverUrl.value" alt="" />
            <span v-else class="sbp-core" />
          </div>
          <div class="mmp-title">
            {{ store.currentMeta.value?.title || store.currentTrack.value.name }}
          </div>
          <div class="sbp-btns" @click.stop>
            <button type="button" class="sbp-btn" @click="toggleAudioPlay">
              <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="13.5" />
            </button>
            <button type="button" class="sbp-btn" @click="nextAudioTrack">
              <UiIcon name="next" :size="13.5" />
            </button>
          </div>
        </div>
      </Transition>

      <nav class="liquid-chrome mobile-tabbar">
        <div
          class="m-tab-indicator"
          :style="{
            width: 'calc((100% - 10px) / 4)',
            transform: `translateX(${activeIdx * 100}%)`,
          }"
        />
        <button
          v-for="item in navItems"
          :key="item.key"
          type="button"
          class="m-tab-btn"
          :class="{ 'is-active': activeTab === item.key }"
          @click="activeTab = item.key"
        >
          <UiIcon :name="item.icon" :size="16.5" />
          <span>{{ item.shortLabel }}</span>
        </button>
      </nav>
    </div>

    <AudioEngine />
    <QrDialog />
  </div>
</template>

<style scoped>
.desktop-frame {
  position: relative;
  z-index: 1;
  height: 100dvh;
  padding: 14px;
  display: flex;
  justify-content: center;
  align-items: stretch;
}

.macos-window {
  width: min(1340px, 100%);
  height: 100%;
  border-radius: 22px;
  background: var(--bg-window);
  -webkit-backdrop-filter: blur(34px) saturate(185%);
  backdrop-filter: blur(34px) saturate(185%);
  border: 1px solid var(--border-window);
  box-shadow: var(--shadow-window);
  display: grid;
  grid-template-columns: 228px minmax(0, 1fr);
  overflow: hidden;
}

.macos-sidebar {
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-hairline);
  padding: 16px 13px 15px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 16px;
  user-select: none;
}

.sb-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 2px 3px 6px;
}

/* Precision Studio Brand Mark (Editorial Serif Italic + Engraved Mono) */
.brand-lockup {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  cursor: pointer;
}

.brand-emblem {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  filter: drop-shadow(0 2px 5px rgba(11, 13, 17, 0.16));
  transition: transform 0.28s var(--ease-bounce);
}

.brand-lockup:hover .brand-emblem {
  transform: rotate(-6deg) scale(1.04);
}

.brand-emblem--sm {
  width: 26px;
  height: 26px;
}

.emblem-plate {
  fill: #0b0d11;
}

[data-theme="dark"] .emblem-plate {
  fill: #171920;
}

.emblem-bevel {
  stroke: rgba(255, 255, 255, 0.16);
  stroke-width: 1;
}

.emblem-ring-outer {
  stroke: rgba(255, 255, 255, 0.26);
  stroke-width: 1.1;
}

.emblem-ring-inner {
  stroke: #f4f5f8;
  stroke-width: 1.35;
  stroke-dasharray: 22 12;
}

.emblem-arrow {
  stroke: #ffffff;
  stroke-width: 1.65;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.emblem-led {
  fill: var(--led-orange);
}

.emblem-led.is-live {
  filter: drop-shadow(0 0 4px var(--led-orange));
}

.brand-wordmark {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
}

.brand-serif {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 400;
  font-size: 1.28rem;
  line-height: 1;
  letter-spacing: -0.015em;
  color: var(--ink-1);
}

.brand-code {
  font-size: 0.62rem;
  font-weight: 600;
  letter-spacing: 0.12em;
  padding: 2px 5px;
  border-radius: 5px;
  border: 1px solid var(--border-strong);
  background: var(--surface-recessed);
  color: var(--ink-2);
  transform: translateY(-1.5px);
}

.sb-theme-btn {
  width: 29px;
  height: 29px;
  border-radius: 8px;
}

/* Sidebar Vertical Switchboard */
.sb-nav {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: auto;
}

.sb-nav-indicator {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 38px;
  border-radius: 10px;
  background: var(--surface-pill);
  border: 1px solid var(--border-hairline);
  box-shadow: var(--shadow-pill);
  transition: transform 0.36s var(--ease-spring);
  pointer-events: none;
}

.sb-nav-btn {
  position: relative;
  z-index: 1;
  height: 38px;
  padding: 0 11px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  color: var(--ink-2);
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 0.84rem;
  font-weight: 550;
  cursor: pointer;
  transition: color 0.16s ease;
}

.sb-nav-btn:hover {
  color: var(--ink-1);
}

.sb-nav-btn.is-active {
  color: var(--ink-1);
  font-weight: 650;
}

.sb-nav-label {
  flex: 1;
  text-align: left;
}

.sb-count {
  font-size: 0.7rem;
  color: var(--ink-3);
}

.sb-live-pill {
  font-size: 0.67rem;
  padding: 1px 6px;
  border-radius: 6px;
  background: var(--emerald-bg);
  color: var(--emerald-fg);
}

.sb-eq {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 11px;
}
.sb-eq i {
  width: 2px;
  border-radius: 1px;
  background: var(--led-orange);
  animation: sb-bounce 0.72s ease-in-out infinite alternate;
}
.sb-eq i:nth-child(2) { animation-delay: -0.22s; }
.sb-eq i:nth-child(3) { animation-delay: -0.42s; }
@keyframes sb-bounce {
  from { height: 3px; }
  to { height: 11px; }
}

/* Sidebar Bottom */
.sb-bottom {
  display: flex;
  flex-direction: column;
  gap: 11px;
}

.sb-player {
  padding: 8px 9px;
  border-radius: 13px;
  display: flex;
  align-items: center;
  gap: 9px;
  cursor: pointer;
}

.sbp-disc {
  width: 31px;
  height: 31px;
  border-radius: 50%;
  background: #141519;
  overflow: hidden;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.sbp-disc.is-spinning {
  animation: disc360 8s linear infinite;
}
@keyframes disc360 {
  to { transform: rotate(360deg); }
}
.sbp-disc img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.sbp-core {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--led-orange);
}

.sbp-meta {
  min-width: 0;
  flex: 1;
}

.sbp-title {
  font-size: 0.76rem;
  font-weight: 630;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sbp-sub {
  font-size: 0.67rem;
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sbp-btns {
  display: flex;
  align-items: center;
  gap: 3px;
}

.sbp-btn {
  width: 25px;
  height: 25px;
  border-radius: 7px;
  border: 0;
  background: var(--surface-recessed);
  color: var(--ink-1);
  display: grid;
  place-items: center;
  cursor: pointer;
}

.sbp-btn:hover {
  background: var(--surface-recessed-hover);
}

/* Hardware 20-Tick Storage Gauge */
.sb-meter {
  padding: 2px 3px 0;
}

.meter-row {
  display: flex;
  justify-content: space-between;
  font-size: 0.65rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: var(--ink-3);
  margin-bottom: 5px;
}

.tick-ruler {
  display: grid;
  grid-template-columns: repeat(20, 1fr);
  gap: 2.5px;
  height: 6px;
}

.tick-ruler i {
  border-radius: 1.5px;
  background: var(--surface-recessed-hover);
  transition: background-color 0.25s ease;
}

.tick-ruler i.is-lit {
  background: var(--ink-1);
}

.sb-links {
  display: flex;
  justify-content: space-between;
  padding-top: 8px;
  border-top: 1px solid var(--border-hairline);
}

.sb-ext-link {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 0.72rem;
  font-weight: 560;
  color: var(--ink-3);
  text-decoration: none;
  padding: 3px 6px;
  border-radius: 6px;
  transition: color 0.16s ease, background-color 0.16s ease;
}

.sb-ext-link:hover {
  color: var(--ink-1);
  background: var(--surface-recessed);
}

/* Main Right Canvas */
.macos-canvas {
  background: var(--bg-canvas-pane);
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  -webkit-overflow-scrolling: touch;
}

.macos-canvas.is-locked-pane {
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.mobile-topbar,
.mobile-bottom-stack {
  display: none;
}

@media (max-width: 860px) {
  .desktop-frame {
    height: auto;
    min-height: 100dvh;
    padding: 0;
    display: block;
  }

  .desktop-frame.is-music-locked {
    height: 100dvh;
    overflow: hidden;
  }

  .macos-window {
    width: 100%;
    height: auto;
    min-height: 100dvh;
    border-radius: 0;
    border: 0;
    box-shadow: none;
    display: block;
    padding-bottom: calc(92px + env(safe-area-inset-bottom, 0px));
  }

  .desktop-frame.is-music-locked .macos-window {
    height: 100dvh;
    display: flex;
    flex-direction: column;
    padding-bottom: calc(74px + env(safe-area-inset-bottom, 0px));
  }

  .macos-sidebar {
    display: none;
  }

  .mobile-topbar {
    position: sticky;
    top: 0;
    z-index: 35;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: calc(10px + env(safe-area-inset-top, 0px)) 16px 10px;
    background: var(--bg-window);
    -webkit-backdrop-filter: blur(24px) saturate(185%);
    backdrop-filter: blur(24px) saturate(185%);
    border-bottom: 1px solid var(--border-hairline);
    flex-shrink: 0;
  }

  .m-up-pill {
    font-size: 0.67rem;
    padding: 2px 6px;
    border-radius: 6px;
    background: var(--emerald-bg);
    color: var(--emerald-fg);
  }

  .m-right {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .m-ext {
    font-size: 0.75rem;
    font-weight: 560;
    color: var(--ink-2);
    text-decoration: none;
  }

  .macos-canvas {
    background: transparent;
    height: auto;
    overflow: visible;
  }

  .macos-canvas.is-locked-pane {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .mobile-bottom-stack {
    position: fixed;
    left: 12px;
    right: 12px;
    bottom: calc(10px + env(safe-area-inset-bottom, 0px));
    z-index: 55;
    display: flex;
    flex-direction: column;
    gap: 8px;
    pointer-events: none;
  }

  .mobile-mini-player {
    pointer-events: auto;
    height: 46px;
    padding: 0 10px;
    border-radius: 14px;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .mmp-title {
    flex: 1;
    min-width: 0;
    font-size: 0.81rem;
    font-weight: 620;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mobile-tabbar {
    pointer-events: auto;
    position: relative;
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    padding: 4px;
    border-radius: 16px;
  }

  .m-tab-indicator {
    position: absolute;
    top: 4px;
    bottom: 4px;
    left: 4px;
    border-radius: 12px;
    background: var(--surface-pill);
    box-shadow: var(--shadow-pill);
    transition: transform 0.36s var(--ease-spring);
    pointer-events: none;
  }

  .m-tab-btn {
    position: relative;
    z-index: 1;
    border: 0;
    background: transparent;
    color: var(--ink-2);
    padding: 6px 2px;
    border-radius: 12px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    font-size: 0.66rem;
    font-weight: 560;
    cursor: pointer;
  }

  .m-tab-btn.is-active {
    color: var(--ink-1);
    font-weight: 660;
  }
}

.toast-anchor {
  position: fixed;
  top: 22px;
  right: 26px;
  z-index: 110;
  pointer-events: none;
}

@media (max-width: 860px) {
  .toast-anchor {
    top: calc(56px + env(safe-area-inset-top, 0px));
    left: 16px;
    right: 16px;
    display: flex;
    justify-content: center;
  }
}

.toast-pill {
  padding: 7px 14px;
  border-radius: 11px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
  font-weight: 590;
}

.toast-ok {
  width: 18px;
  height: 18px;
  border-radius: 6px;
  background: var(--emerald-bg);
  color: var(--emerald-fg);
  display: grid;
  place-items: center;
}

.toast-slide-enter-active,
.toast-slide-leave-active {
  transition: opacity 0.2s ease, transform 0.3s var(--ease-spring);
}
.toast-slide-enter-from,
.toast-slide-leave-to {
  opacity: 0;
  transform: translateY(-10px) scale(0.95);
}

.pane-fade-enter-active,
.pane-fade-leave-active {
  transition: opacity 0.15s ease, transform 0.22s var(--ease-spring);
}
.pane-fade-enter-from {
  opacity: 0;
  transform: translateY(5px);
}
.pane-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.fade-up-enter-active,
.fade-up-leave-active {
  transition: opacity 0.2s ease, transform 0.28s var(--ease-spring);
}
.fade-up-enter-from,
.fade-up-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>
