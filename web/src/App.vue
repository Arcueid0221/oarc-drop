<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import AudioEngine from './components/AudioEngine.vue'
import ClipboardView from './components/ClipboardView.vue'
import DropView from './components/DropView.vue'
import GalleryView from './components/GalleryView.vue'
import MusicView from './components/MusicView.vue'
import PublicSharePortal from './components/PublicSharePortal.vue'
import SharesView from './components/SharesView.vue'
import UiIcon from './components/UiIcon.vue'
import { formatBytes, useDropStore } from './composables/useDropStore'

const store = useDropStore()

type NavTab = 'drop' | 'clip' | 'gallery' | 'music' | 'shares'

const navItems = computed<
  Array<{
    key: NavTab
    label: string
    shortLabel: string
    icon: 'layers' | 'copy' | 'image' | 'disc' | 'link'
    badge?: string | number
  }>
>(() => [
  { key: 'drop', label: '快传投递', shortLabel: '快传', icon: 'layers' },
  {
    key: 'clip',
    label: '无损剪贴板',
    shortLabel: '剪贴板',
    icon: 'copy',
    badge: store.clips.value.length || undefined,
  },
  {
    key: 'gallery',
    label: 'Silo 画廊',
    shortLabel: '画廊',
    icon: 'image',
    badge: store.imageCount.value || undefined,
  },
  {
    key: 'music',
    label: 'Silo 音乐',
    shortLabel: '音乐',
    icon: 'disc',
    badge: store.audioCount.value || undefined,
  },
  {
    key: 'shares',
    label: '分享管理',
    shortLabel: '分享',
    icon: 'link',
    badge: store.shares.value.length || undefined,
  },
])

const activeTab = ref<NavTab>('drop')
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

const diskUsedPct = computed(() => {
  const tot = store.status.value?.diskTotal || 0
  const free = store.status.value?.diskFree || 0
  if (!tot) return 15
  return Math.min(100, Math.max(4, Math.round(((tot - free) / tot) * 100)))
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
    for (const f of Array.from(files)) {
      await store.sendQuickPasteFile(f)
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

  <!-- Subtle Floating Status Toast -->
  <Transition name="toast-slide">
    <div v-if="store.toastText.value" class="toast-anchor">
      <div class="liquid-chrome toast-pill">
        <span class="toast-ok">
          <UiIcon name="check" :size="12" />
        </span>
        <span>{{ store.toastText.value }}</span>
      </div>
    </div>
  </Transition>

  <!-- Public Recipient View (/s/:code) -->
  <PublicSharePortal v-if="shareCode" :code="shareCode" />

  <!-- Layout 2: macOS Tahoe Unified App Window (Desktop) + iOS 26 Fullscreen Shell (Mobile) -->
  <div v-else class="desktop-frame">
    <div class="macos-window">
      <!-- Left Sidebar (Desktop) -->
      <aside class="macos-sidebar">
        <div class="sb-top">
          <div class="sb-brand">
            <span class="brand-orb" />
            <span class="brand-title">O.Arc Drop</span>
          </div>
          <button
            type="button"
            class="icon-circle-btn sb-theme-btn"
            :title="theme === 'light' ? '切换深色玻璃' : '切换浅色冰晶'"
            @click="toggleTheme"
          >
            <UiIcon :name="theme === 'light' ? 'sun' : 'moon'" :size="14.5" />
          </button>
        </div>

        <!-- Vertical Sliding Pill Navigation -->
        <nav class="sb-nav">
          <div
            class="sb-nav-indicator"
            :style="{ transform: `translateY(${activeIdx * 44}px)` }"
          />
          <button
            v-for="item in navItems"
            :key="item.key"
            type="button"
            class="sb-nav-btn"
            :class="{ 'is-active': activeTab === item.key }"
            @click="activeTab = item.key"
          >
            <UiIcon :name="item.icon" :size="16.5" />
            <span class="sb-nav-label">{{ item.label }}</span>

            <!-- Live Upload % on Drop tab -->
            <span v-if="item.key === 'drop' && activeUpload" class="sb-live-pill mono">
              {{ uploadPct }}%
            </span>
            <!-- Dancing Equalizer on Music tab when playing -->
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

        <!-- Sidebar Bottom Dock: Mini Vinyl Card + Storage Meter + External Portals -->
        <div class="sb-bottom">
          <!-- Mini Now-Playing Card -->
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
                  <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="13" />
                </button>
                <button type="button" class="sbp-btn" @click="nextAudioTrack">
                  <UiIcon name="next" :size="13" />
                </button>
              </div>
            </div>
          </Transition>

          <!-- Disk Meter -->
          <div v-if="store.status.value?.diskTotal" class="sb-meter">
            <div class="meter-row mono">
              <span>HostDzire 存储</span>
              <span>余 {{ formatBytes(store.status.value.diskFree) }}</span>
            </div>
            <div class="meter-track">
              <div class="meter-fill" :style="{ width: `${diskUsedPct}%` }" />
            </div>
          </div>

          <!-- Ecosystem Links -->
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
        <div class="m-brand">
          <span class="brand-orb" />
          <span class="brand-title">O.Arc Drop</span>
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
            <UiIcon :name="theme === 'light' ? 'sun' : 'moon'" :size="15" />
          </button>
        </div>
      </header>

      <!-- Right Equal-Height Scrollable Main Canvas -->
      <main class="macos-canvas">
        <Transition name="pane-fade" mode="out-in">
          <DropView v-if="activeTab === 'drop'" key="drop" />
          <ClipboardView v-else-if="activeTab === 'clip'" key="clip" />
          <GalleryView v-else-if="activeTab === 'gallery'" key="gallery" />
          <MusicView v-else-if="activeTab === 'music'" key="music" />
          <SharesView v-else key="shares" />
        </Transition>
      </main>
    </div>

    <!-- Mobile iOS 26 Mini Player + Bottom Capsule Tab Bar -->
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
              <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="14" />
            </button>
            <button type="button" class="sbp-btn" @click="nextAudioTrack">
              <UiIcon name="next" :size="14" />
            </button>
          </div>
        </div>
      </Transition>

      <nav class="liquid-chrome mobile-tabbar">
        <div
          class="m-tab-indicator"
          :style="{
            width: 'calc((100% - 10px) / 5)',
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
          <UiIcon :name="item.icon" :size="17" />
          <span>{{ item.shortLabel }}</span>
        </button>
      </nav>
    </div>

    <AudioEngine />
  </div>
</template>

<style scoped>
/* Desktop App Frame (Locked Viewport Height, Zero Jagged Edges) */
.desktop-frame {
  position: relative;
  z-index: 1;
  height: 100dvh;
  padding: 16px;
  display: flex;
  justify-content: center;
  align-items: stretch;
}

.macos-window {
  width: min(1320px, 100%);
  height: 100%;
  border-radius: 28px;
  background: var(--bg-window);
  -webkit-backdrop-filter: blur(36px) saturate(190%);
  backdrop-filter: blur(36px) saturate(190%);
  border: 1px solid var(--border-window);
  box-shadow: var(--shadow-window);
  display: grid;
  grid-template-columns: 236px minmax(0, 1fr);
  overflow: hidden;
}

/* Left Translucent Sidebar */
.macos-sidebar {
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-hairline);
  padding: 18px 14px 16px;
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
  padding: 2px 6px 8px;
}

.sb-brand,
.m-brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
}

.brand-orb {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: linear-gradient(135deg, #38bdf8, #0284c7);
  box-shadow: 0 0 10px rgba(14, 165, 233, 0.45);
}

.brand-title {
  font-size: 0.96rem;
  font-weight: 700;
  letter-spacing: -0.015em;
}

.sb-theme-btn {
  width: 30px;
  height: 30px;
}

/* Sidebar Vertical Sliding Pill Nav */
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
  height: 40px;
  border-radius: 13px;
  background: var(--surface-pill);
  box-shadow: var(--shadow-pill);
  transition: transform 0.38s var(--ease-spring);
  pointer-events: none;
}

.sb-nav-btn {
  position: relative;
  z-index: 1;
  height: 40px;
  padding: 0 12px;
  border: 0;
  border-radius: 13px;
  background: transparent;
  color: var(--ink-2);
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 0.86rem;
  font-weight: 550;
  cursor: pointer;
  transition: color 0.18s ease;
}

.sb-nav-btn:hover {
  color: var(--ink-1);
}

.sb-nav-btn.is-active {
  color: var(--ink-1);
  font-weight: 640;
}

.sb-nav-label {
  flex: 1;
  text-align: left;
}

.sb-count {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.sb-live-pill {
  font-size: 0.69rem;
  padding: 1px 6px;
  border-radius: 999px;
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
  border-radius: 2px;
  background: var(--amber-eq);
  animation: sb-bounce 0.75s ease-in-out infinite alternate;
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
  gap: 12px;
}

.sb-player {
  padding: 9px 10px;
  border-radius: 15px;
  display: flex;
  align-items: center;
  gap: 9px;
  cursor: pointer;
}

.sbp-disc {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #141519;
  overflow: hidden;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.sbp-disc.is-spinning {
  animation: disc360 7s linear infinite;
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
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--amber-eq);
}

.sbp-meta {
  min-width: 0;
  flex: 1;
}

.sbp-title {
  font-size: 0.77rem;
  font-weight: 630;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sbp-sub {
  font-size: 0.68rem;
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
  width: 26px;
  height: 26px;
  border-radius: 50%;
  border: 0;
  background: var(--surface-recessed);
  color: var(--ink-1);
  display: grid;
  place-items: center;
  cursor: pointer;
}

.sb-meter {
  padding: 0 4px;
}

.meter-row {
  display: flex;
  justify-content: space-between;
  font-size: 0.7rem;
  color: var(--ink-3);
  margin-bottom: 5px;
}

.meter-track {
  height: 4px;
  border-radius: 999px;
  background: var(--surface-recessed-hover);
  overflow: hidden;
}

.meter-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--ink-2);
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
  font-size: 0.73rem;
  font-weight: 560;
  color: var(--ink-3);
  text-decoration: none;
  padding: 4px 6px;
  border-radius: 8px;
  transition: color 0.18s ease, background-color 0.18s ease;
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

/* Hide mobile chrome on Desktop */
.mobile-topbar,
.mobile-bottom-stack {
  display: none;
}

/* Mobile Adaptation (iPhone Air 420x912pt & phones/small tablets <= 860px) */
@media (max-width: 860px) {
  .desktop-frame {
    height: auto;
    min-height: 100dvh;
    padding: 0;
    display: block;
  }

  .macos-window {
    width: 100%;
    height: auto;
    min-height: 100dvh;
    border-radius: 0;
    border: 0;
    box-shadow: none;
    display: block;
    padding-bottom: calc(96px + env(safe-area-inset-bottom, 0px));
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
  }

  .m-up-pill {
    font-size: 0.69rem;
    padding: 2px 7px;
    border-radius: 999px;
    background: var(--emerald-bg);
    color: var(--emerald-fg);
  }

  .m-right {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .m-ext {
    font-size: 0.76rem;
    font-weight: 560;
    color: var(--ink-2);
    text-decoration: none;
  }

  .macos-canvas {
    background: transparent;
    height: auto;
    overflow: visible;
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
    height: 48px;
    padding: 0 10px;
    border-radius: 999px;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .mmp-title {
    flex: 1;
    min-width: 0;
    font-size: 0.82rem;
    font-weight: 620;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mobile-tabbar {
    pointer-events: auto;
    position: relative;
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    padding: 5px;
    border-radius: 999px;
  }

  .m-tab-indicator {
    position: absolute;
    top: 5px;
    bottom: 5px;
    left: 5px;
    border-radius: 999px;
    background: var(--surface-pill);
    box-shadow: var(--shadow-pill);
    transition: transform 0.38s var(--ease-spring);
    pointer-events: none;
  }

  .m-tab-btn {
    position: relative;
    z-index: 1;
    border: 0;
    background: transparent;
    color: var(--ink-2);
    padding: 6px 2px;
    border-radius: 999px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    font-size: 0.67rem;
    font-weight: 560;
    cursor: pointer;
  }

  .m-tab-btn.is-active {
    color: var(--ink-1);
    font-weight: 660;
  }
}

/* Toast */
.toast-anchor {
  position: fixed;
  top: 24px;
  right: 28px;
  z-index: 110;
  pointer-events: none;
}

@media (max-width: 860px) {
  .toast-anchor {
    top: calc(58px + env(safe-area-inset-top, 0px));
    left: 16px;
    right: 16px;
    display: flex;
    justify-content: center;
  }
}

.toast-pill {
  padding: 8px 15px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.81rem;
  font-weight: 590;
}

.toast-ok {
  width: 19px;
  height: 19px;
  border-radius: 50%;
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
  transition: opacity 0.16s ease, transform 0.24s var(--ease-spring);
}
.pane-fade-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.pane-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.fade-up-enter-active,
.fade-up-leave-active {
  transition: opacity 0.22s ease, transform 0.3s var(--ease-spring);
}
.fade-up-enter-from,
.fade-up-leave-to {
  opacity: 0;
  transform: translateY(10px);
}
</style>
