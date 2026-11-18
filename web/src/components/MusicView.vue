<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  copyText,
  formatBytes,
  parseTrackDisplayName,
  shareFullUrl,
  useDropStore,
} from '../composables/useDropStore'
import type { MediaItem } from '../types'
import UiIcon from './UiIcon.vue'

const store = useDropStore()
const search = ref('')
const selectedPlaylistId = ref<number | 'all'>('all')
const lyricsContainer = ref<HTMLElement | null>(null)
const trackScrollEl = ref<HTMLElement | null>(null)

const scrubEl = ref<HTMLElement | null>(null)
const isScrubbing = ref(false)
const scrubPreviewSec = ref(0)
const jellyStretch = ref(0)
const sharingTrackKey = ref('')
const refreshingMusic = ref(false)

// Spring rebound state when switching between lyric & instrumental tracks
const reboundState = ref<'none' | 'collapse' | 'expand'>('none')
let reboundTimer: number | undefined

onMounted(() => {
  if (store.mediaItems.value.length === 0) {
    void store.loadMedia(false)
  }
  if (store.playlists.value.length === 0) {
    void store.loadMusicCatalog(false)
  }
})

async function refreshMusic() {
  if (refreshingMusic.value) return
  refreshingMusic.value = true
  try {
    await Promise.all([store.loadMedia(true), store.loadMusicCatalog(true)])
  } finally {
    refreshingMusic.value = false
  }
}

const filteredTracks = computed(() => {
  const q = search.value.trim().toLowerCase()
  let base = store.audioTracks.value
  if (selectedPlaylistId.value !== 'all') {
    const pl = store.playlists.value.find((p) => p.id === selectedPlaylistId.value)
    if (pl) {
      const orderMap = new Map<string, number>()
      pl.trackKeys.forEach((k, idx) => orderMap.set(k, idx))
      base = base
        .filter((t) => orderMap.has(t.key))
        .slice()
        .sort((a, b) => (orderMap.get(a.key) ?? 0) - (orderMap.get(b.key) ?? 0))
    }
  }
  if (!q) return base
  return base.filter((t) => {
    const info = parseTrackDisplayName(t, store.catalogTracks.value)
    const hay = `${t.name} ${info.title} ${info.artist} ${info.album}`.toLowerCase()
    return hay.includes(q)
  })
})

// Synchronously knows whether the active track carries lyrics (zero fetch flicker between two lyric songs!)
const hasLyricStage = computed(() => {
  if (!store.currentTrack.value) return false
  return Boolean(store.currentTrack.value.lyricsKey) || store.currentLyrics.value.length > 0
})

watch(hasLyricStage, (nowHas, prevHas) => {
  if (prevHas === undefined || nowHas === prevHas) return
  if (reboundTimer) window.clearTimeout(reboundTimer)
  reboundState.value = nowHas ? 'expand' : 'collapse'
  reboundTimer = window.setTimeout(() => {
    reboundState.value = 'none'
  }, 560)
})

const displayedProgressSec = computed(() =>
  isScrubbing.value ? scrubPreviewSec.value : store.audioProgress.value,
)

const progressRatio = computed(() => {
  const dur = store.audioDuration.value
  if (!dur || dur <= 0) return 0
  return Math.min(1, Math.max(0, displayedProgressSec.value / dur))
})

const activeLyricIndex = computed(() => {
  const lines = store.currentLyrics.value
  if (!lines.length) return -1
  const t = displayedProgressSec.value
  let idx = -1
  for (let i = 0; i < lines.length; i++) {
    if (t >= lines[i].time) idx = i
    else break
  }
  return idx
})

function centerElementInScrollBox(
  box: HTMLElement | null,
  el: HTMLElement | null,
  smooth = true,
) {
  if (!box || !el) return
  const boxRect = box.getBoundingClientRect()
  const elRect = el.getBoundingClientRect()
  const offsetInside = elRect.top - boxRect.top + box.scrollTop
  const targetTop = offsetInside - box.clientHeight / 2 + elRect.height / 2
  box.scrollTo({
    top: Math.max(0, targetTop),
    behavior: smooth ? 'smooth' : 'auto',
  })
}

watch(
  [activeLyricIndex, () => store.currentLyrics.value.length],
  async ([idx]) => {
    if (idx < 0) return
    await nextTick()
    const box = lyricsContainer.value
    if (!box) return
    const lineEl = box.querySelector<HTMLElement>(`[data-lrc="${idx}"]`)
    centerElementInScrollBox(box, lineEl, true)
  },
)

function scrollToPlayingTrack() {
  const box = trackScrollEl.value
  if (!store.currentTrack.value || !box) return
  const rowEl = box.querySelector<HTMLElement>('[data-playing="1"]')
  centerElementInScrollBox(box, rowEl, true)
}

function seekToTime(sec: number) {
  window.dispatchEvent(new CustomEvent('oarc-audio-seek', { detail: sec }))
}

function onRowClick(track: MediaItem) {
  if (store.currentTrack.value?.key === track.key) {
    window.dispatchEvent(new CustomEvent('oarc-audio-toggle'))
    return
  }
  void store.playTrack(track, filteredTracks.value)
}

function togglePlay() {
  if (!store.currentTrack.value && filteredTracks.value.length > 0) {
    void store.playTrack(filteredTracks.value[0], filteredTracks.value)
    return
  }
  window.dispatchEvent(new CustomEvent('oarc-audio-toggle'))
}

function stepTrack(delta: number) {
  if (!store.currentTrack.value && filteredTracks.value.length > 0) {
    void store.playTrack(filteredTracks.value[0], filteredTracks.value)
    return
  }
  window.dispatchEvent(new CustomEvent('oarc-audio-step', { detail: delta }))
}

function updateScrubPreviewFromX(clientX: number) {
  const el = scrubEl.value
  const dur = store.audioDuration.value
  if (!el || !dur) return
  const r = el.getBoundingClientRect()
  const raw = (clientX - r.left) / r.width
  if (raw < 0) jellyStretch.value = Math.max(-7, raw * 20)
  else if (raw > 1) jellyStretch.value = Math.min(7, (raw - 1) * 20)
  else jellyStretch.value = 0

  const clamped = Math.min(1, Math.max(0, raw))
  scrubPreviewSec.value = clamped * dur
}

function onScrubDown(e: PointerEvent) {
  if (!store.audioDuration.value) return
  isScrubbing.value = true
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  updateScrubPreviewFromX(e.clientX)
}

function onScrubMove(e: PointerEvent) {
  if (!isScrubbing.value) return
  updateScrubPreviewFromX(e.clientX)
}

function onScrubUp() {
  if (!isScrubbing.value) return
  const targetSec = scrubPreviewSec.value
  isScrubbing.value = false
  jellyStretch.value = 0
  seekToTime(targetSec)
}

function onScrubCancel() {
  isScrubbing.value = false
  jellyStretch.value = 0
}

onBeforeUnmount(() => {
  isScrubbing.value = false
  if (reboundTimer) window.clearTimeout(reboundTimer)
})

async function shareTrack(track: MediaItem) {
  if (sharingTrackKey.value === track.key) return
  sharingTrackKey.value = track.key
  try {
    const res = await fetch('/api/shares/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        storage: 'silo',
        key: track.key,
        fileName: track.name,
        size: track.size,
        ttlHours: 168,
      }),
    })
    if (res.ok) {
      const sh = await res.json()
      if (!store.shares.value.some((s) => s.code === sh.code)) {
        store.shares.value = [sh, ...store.shares.value]
      }
      const url = shareFullUrl(sh.code)
      await copyText(url, '歌曲公开短链已复制')
      store.openQrModal(track.name, url, '免登录扫码试听或下载 · 可设提取码', sh.code)
    }
  } finally {
    sharingTrackKey.value = ''
  }
}

function fmtSec(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '0:00'
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

const playModeIcon = computed(() => {
  if (store.playMode.value === 'shuffle') return 'shuffle'
  if (store.playMode.value === 'one') return 'repeat-one'
  return 'repeat'
})

const playModeLabel = computed(() => {
  if (store.playMode.value === 'shuffle') return '随机'
  if (store.playMode.value === 'one') return '单曲'
  return '列表'
})
</script>

<template>
  <div class="music-page">
    <!-- TOP FIXED STUDIO DECK (Turntable + Transport + Integrated Centered Lyrics + Playlist Strip) -->
    <div class="music-top-fixed">
      <header class="pane-header music-header">
        <div class="pane-title-wrap">
          <h1 class="pane-title">音乐厅</h1>
          <span class="pane-count">{{ store.audioCount.value }}</span>
        </div>

        <div class="pane-actions search-bar-wrap">
          <div class="search-pill">
            <UiIcon name="search" :size="14" />
            <input v-model="search" placeholder="搜索歌手、歌名…" />
          </div>
          <button
            v-if="store.currentTrack.value"
            type="button"
            class="pill-btn"
            title="在下方列表居中定位正在播放的歌曲"
            @click="scrollToPlayingTrack"
          >
            <UiIcon name="disc" :size="13.5" />
            <span class="hide-xs">定位当前</span>
          </button>
          <button type="button" class="icon-circle-btn" title="刷新曲库" @click="refreshMusic">
            <UiIcon name="refresh" :size="14" :class="{ 'spin-anim': refreshingMusic }" />
          </button>
        </div>
      </header>

      <div class="music-fixed-inner">
        <!-- Unified Constant-Height Studio Deck (Lyrics fused inside, spring rebound on lyric <-> instrumental!) -->
        <section
          class="glass-box studio-deck"
          :class="[
            hasLyricStage ? 'has-lyrics' : 'no-lyrics',
            reboundState !== 'none' ? `rebound-${reboundState}` : '',
          ]"
        >
          <!-- Left: Platter + Track Info + Tick Scrubber + Tactile Transport -->
          <div class="deck-left">
            <div class="turntable-unit">
              <div class="platter-box" title="点击唱盘播放 / 暂停" @click="togglePlay">
                <div
                  class="vinyl-platter"
                  :class="{
                    'is-spinning': store.isPlaying.value && Boolean(store.currentTrack.value),
                  }"
                >
                  <div class="vinyl-grooves" />
                  <div class="vinyl-label">
                    <img
                      v-if="store.currentCoverUrl.value"
                      :src="store.currentCoverUrl.value"
                      alt=""
                    />
                    <div v-else class="label-fallback" />
                    <span class="spindle-hole" />
                  </div>
                </div>

                <div
                  class="tonearm"
                  :class="{
                    'is-engaged': store.isPlaying.value && Boolean(store.currentTrack.value),
                  }"
                >
                  <div class="arm-pivot" />
                  <div class="arm-rod" />
                </div>
              </div>

              <div class="deck-console">
                <div class="deck-status-row">
                  <span class="led-status mono">
                    <i class="led-dot" :class="{ 'is-on': store.isPlaying.value }" />
                    <span>{{ store.isPlaying.value ? 'ON AIR' : 'STANDBY' }}</span>
                  </span>

                  <span
                    v-if="store.currentTrack.value && !hasLyricStage"
                    class="inst-chip mono"
                  >
                    INSTRUMENTAL · 纯音乐
                  </span>
                </div>

                <div class="deck-title">
                  {{ store.currentMeta.value?.title || '点选下方曲目开始播放' }}
                </div>
                <div class="deck-sub">
                  <template v-if="store.currentMeta.value?.artist">
                    {{ store.currentMeta.value.artist }}
                    <template v-if="store.currentMeta.value?.album">
                      · {{ store.currentMeta.value.album }}
                    </template>
                  </template>
                  <template v-else>—</template>
                </div>

                <!-- Precision Jelly Scrubber with Hardware Micro-Ticks -->
                <div
                  ref="scrubEl"
                  class="jelly-scrubber"
                  :style="{
                    transform: `translateX(${jellyStretch}px) scaleX(${1 + Math.abs(jellyStretch) * 0.0035})`,
                  }"
                  @pointerdown="onScrubDown"
                  @pointermove="onScrubMove"
                  @pointerup="onScrubUp"
                  @pointercancel="onScrubCancel"
                >
                  <div class="scrub-track">
                    <div class="scrub-fill" :style="{ width: `${progressRatio * 100}%` }" />
                  </div>
                  <div class="scrub-ruler-row mono">
                    <span>{{ fmtSec(displayedProgressSec) }}</span>
                    <div class="scrub-ticks" aria-hidden="true">
                      <i v-for="n in 9" :key="n" />
                    </div>
                    <span>{{ fmtSec(store.audioDuration.value) }}</span>
                  </div>
                </div>

                <!-- Hardware Transport Keys -->
                <div class="deck-transport">
                  <button
                    type="button"
                    class="pill-btn mode-pill"
                    :class="{ 'mode-active': store.playMode.value !== 'sequence' }"
                    title="切换列表 / 随机 / 单曲循环"
                    @click="store.cyclePlayMode"
                  >
                    <UiIcon :name="playModeIcon" :size="13.5" />
                    <span>{{ playModeLabel }}</span>
                  </button>

                  <button type="button" class="icon-circle-btn" title="上一首" @click="stepTrack(-1)">
                    <UiIcon name="prev" :size="14" />
                  </button>

                  <button
                    type="button"
                    class="play-main-btn"
                    :title="store.isPlaying.value ? '暂停' : '播放'"
                    @click="togglePlay"
                  >
                    <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="15.5" />
                    <span>{{ store.isPlaying.value ? '暂停' : '播放' }}</span>
                  </button>

                  <button type="button" class="icon-circle-btn" title="下一首" @click="stepTrack(1)">
                    <UiIcon name="next" :size="14" />
                  </button>

                  <div class="vol-wrap hide-xs" title="音量">
                    <UiIcon name="volume" :size="13.5" />
                    <input
                      type="range"
                      min="0"
                      max="1"
                      step="0.02"
                      :value="store.audioVolume.value"
                      class="vol-slider"
                      @input="store.setVolume(Number(($event.target as HTMLInputElement).value))"
                    />
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Right: Built-In Centered Lyric Stage (seamlessly fused into the top deck!) -->
          <div v-if="hasLyricStage" class="deck-lyric-pane">
            <div ref="lyricsContainer" class="lyric-scroll-box">
              <div class="lyric-spacer" aria-hidden="true" />
              <div v-if="store.currentLyrics.value.length === 0" class="lrc-loading mono">
                SYNCING LYRICS…
              </div>
              <button
                v-for="(ln, idx) in store.currentLyrics.value"
                :key="idx"
                type="button"
                :data-lrc="idx"
                class="lrc-line"
                :class="{
                  'is-current': idx === activeLyricIndex,
                  'is-near': Math.abs(idx - activeLyricIndex) === 1,
                }"
                @click="seekToTime(ln.time)"
              >
                <span class="lrc-txt">{{ ln.text }}</span>
              </button>
              <div class="lyric-spacer" aria-hidden="true" />
            </div>
          </div>
        </section>

        <!-- Playlist Filter Strip -->
        <div class="playlist-strip">
          <button
            type="button"
            class="pl-chip"
            :class="{ 'is-on': selectedPlaylistId === 'all' }"
            @click="selectedPlaylistId = 'all'"
          >
            <span>全部</span>
            <span class="pl-num mono">{{ store.audioTracks.value.length }}</span>
          </button>
          <button
            v-for="pl in store.playlists.value"
            :key="pl.id"
            type="button"
            class="pl-chip"
            :class="{ 'is-on': selectedPlaylistId === pl.id }"
            @click="selectedPlaylistId = pl.id"
          >
            <img v-if="pl.coverUrl" :src="pl.coverUrl" class="pl-ava" alt="" loading="lazy" />
            <span>{{ pl.name }}</span>
            <span class="pl-num mono">{{ pl.trackCount }}</span>
          </button>
        </div>
      </div>
    </div>

    <!-- BOTTOM INDEPENDENTLY SCROLLABLE TRACK LIST -->
    <div ref="trackScrollEl" class="track-scroll-area">
      <div class="track-scroll-inner">
        <div v-if="filteredTracks.length === 0" class="glass-box empty-box">
          未找到匹配的曲目
        </div>

        <div v-else class="track-list">
          <div
            v-for="(tr, idx) in filteredTracks"
            :key="tr.key"
            class="glass-box track-row"
            :class="{ 'is-playing': store.currentTrack.value?.key === tr.key }"
            :data-playing="store.currentTrack.value?.key === tr.key ? '1' : undefined"
            @click="onRowClick(tr)"
          >
            <button
              type="button"
              class="tr-play-ind"
              :title="store.currentTrack.value?.key === tr.key && store.isPlaying.value ? '暂停' : '播放'"
            >
              <UiIcon
                v-if="store.currentTrack.value?.key === tr.key"
                :name="store.isPlaying.value ? 'pause' : 'play'"
                :size="12.5"
              />
              <span v-else class="mono">{{ String(idx + 1).padStart(2, '0') }}</span>
            </button>

            <img
              v-if="parseTrackDisplayName(tr, store.catalogTracks.value).coverUrl"
              :src="parseTrackDisplayName(tr, store.catalogTracks.value).coverUrl"
              class="tr-cover"
              alt=""
              loading="lazy"
            />
            <div v-else class="tr-cover tr-cover--empty">
              <UiIcon name="disc" :size="15" />
            </div>

            <div class="tr-meta">
              <div class="tr-title">
                <span>{{ parseTrackDisplayName(tr, store.catalogTracks.value).title }}</span>
                <span v-if="tr.lyricsKey" class="lrc-badge mono">LRC</span>
              </div>
              <div class="tr-sub">
                {{ parseTrackDisplayName(tr, store.catalogTracks.value).artist }}
                <template v-if="parseTrackDisplayName(tr, store.catalogTracks.value?.album ? store.catalogTracks.value : store.catalogTracks.value).album">
                  · {{ parseTrackDisplayName(tr, store.catalogTracks.value).album }}
                </template>
                <span class="mono"> · {{ formatBytes(tr.size) }}</span>
              </div>
            </div>

            <div class="tr-acts" @click.stop>
              <a
                class="mini-key"
                :href="`${tr.streamUrl}&download=1`"
                :title="`下载 ${tr.name}`"
              >
                <UiIcon name="download" :size="13" />
              </a>
              <button
                type="button"
                class="mini-key"
                title="生成分享短链与二维码"
                :disabled="sharingTrackKey === tr.key"
                @click="shareTrack(tr)"
              >
                <UiIcon
                  :name="sharingTrackKey === tr.key ? 'spinner' : 'qr'"
                  :size="13"
                  :class="{ 'spin-anim': sharingTrackKey === tr.key }"
                />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.music-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.music-top-fixed {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-hairline);
  background: rgba(255, 255, 255, 0.25);
}

[data-theme="dark"] .music-top-fixed {
  background: rgba(16, 17, 21, 0.35);
}

.music-header {
  position: static;
  border-bottom: 0;
  padding-bottom: 8px;
}

.music-fixed-inner {
  max-width: 960px;
  width: 100%;
  margin: 0 auto;
  padding: 0 24px 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* Constant-Height Integrated Studio Deck */
.studio-deck {
  padding: 15px 18px;
  min-height: 184px;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  gap: 18px;
  transform-origin: center center;
}

.studio-deck.has-lyrics {
  grid-template-columns: minmax(0, 1.12fr) minmax(240px, 0.88fr);
}

/* Physical Spring Rebound when switching between lyric & instrumental songs */
.studio-deck.rebound-collapse {
  animation: deck-rebound-collapse 0.52s var(--ease-bounce);
}

.studio-deck.rebound-expand {
  animation: deck-rebound-expand 0.52s var(--ease-bounce);
}

@keyframes deck-rebound-collapse {
  0% {
    transform: scaleX(0.975) scaleY(1.01);
  }
  55% {
    transform: scaleX(1.016) scaleY(0.994);
  }
  100% {
    transform: scaleX(1) scaleY(1);
  }
}

@keyframes deck-rebound-expand {
  0% {
    transform: scaleX(0.98) scaleY(0.99);
  }
  55% {
    transform: scaleX(1.014) scaleY(1.006);
  }
  100% {
    transform: scaleX(1) scaleY(1);
  }
}

.deck-left {
  min-width: 0;
  display: flex;
  align-items: center;
}

.turntable-unit {
  width: 100%;
  display: grid;
  grid-template-columns: 122px minmax(0, 1fr);
  align-items: center;
  gap: 18px;
}

.platter-box {
  position: relative;
  width: 118px;
  height: 118px;
  display: grid;
  place-items: center;
  cursor: pointer;
}

.vinyl-platter {
  width: 112px;
  height: 112px;
  border-radius: 50%;
  background: radial-gradient(circle, #22252e 0%, #111318 68%, #07080a 100%);
  box-shadow:
    0 10px 22px rgba(11, 13, 17, 0.25),
    inset 0 0 0 1px rgba(255, 255, 255, 0.08);
  position: relative;
  display: grid;
  place-items: center;
  animation: spin 16s linear infinite;
  animation-play-state: paused;
}

.vinyl-platter.is-spinning {
  animation-play-state: running;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.vinyl-grooves {
  position: absolute;
  inset: 9px;
  border-radius: 50%;
  border: 1px dashed rgba(255, 255, 255, 0.1);
}

.vinyl-label {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  overflow: hidden;
  position: relative;
  display: grid;
  place-items: center;
  background: #1c1f26;
}

.vinyl-label img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.label-fallback {
  width: 100%;
  height: 100%;
  background: #272a34;
}

.spindle-hole {
  position: absolute;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #0b0d11;
  border: 1.5px solid rgba(255, 255, 255, 0.8);
}

.tonearm {
  position: absolute;
  top: 4px;
  right: -3px;
  width: 26px;
  height: 74px;
  transform-origin: 17px 9px;
  transform: rotate(-18deg);
  transition: transform 0.44s var(--ease-bounce);
  pointer-events: none;
}

.tonearm.is-engaged {
  transform: rotate(12deg);
}

.arm-pivot {
  position: absolute;
  top: 3px;
  right: 3px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #d4d8e1;
  border: 1px solid rgba(11, 13, 17, 0.25);
}

.arm-rod {
  position: absolute;
  top: 12px;
  right: 8px;
  width: 2.2px;
  height: 52px;
  background: #9ca3b2;
  border-radius: 2px;
}

.deck-console {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.deck-status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.led-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 0.64rem;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: var(--ink-3);
}

.led-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ink-3);
  opacity: 0.45;
  transition: all 0.2s ease;
}

.led-dot.is-on {
  background: var(--led-orange);
  opacity: 1;
  box-shadow: 0 0 6px var(--led-orange);
}

.inst-chip {
  font-size: 0.63rem;
  font-weight: 600;
  letter-spacing: 0.05em;
  padding: 2px 7px;
  border-radius: 5px;
  background: var(--surface-recessed);
  color: var(--ink-2);
  border: 1px solid var(--border-hairline);
  animation: pop-chip 0.36s var(--ease-bounce);
}

@keyframes pop-chip {
  from {
    opacity: 0;
    transform: scale(0.84);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.deck-title {
  font-size: 1.02rem;
  font-weight: 680;
  letter-spacing: -0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.deck-sub {
  font-size: 0.77rem;
  color: var(--ink-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Precision Scrubber + Hardware Micro-Ruler */
.jelly-scrubber {
  margin-top: 2px;
  cursor: pointer;
  user-select: none;
  touch-action: none;
}

.scrub-track {
  height: 5px;
  border-radius: 999px;
  background: var(--surface-recessed-hover);
  overflow: hidden;
}

.scrub-fill {
  height: 100%;
  background: var(--cta-bg);
  border-radius: 999px;
}

.scrub-ruler-row {
  margin-top: 3px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.66rem;
  color: var(--ink-3);
}

.scrub-ticks {
  flex: 1;
  max-width: 160px;
  margin: 0 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.scrub-ticks i {
  width: 1px;
  height: 4px;
  background: var(--border-strong);
}

.scrub-ticks i:nth-child(5) {
  height: 6px;
}

.deck-transport {
  margin-top: 3px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.mode-pill {
  height: 32px;
  padding: 0 9px;
  font-size: 0.74rem;
}

.mode-active {
  background: var(--accent-soft);
  border-color: var(--accent-border);
}

.play-main-btn {
  height: 34px;
  padding: 0 16px;
  border-radius: 9px;
  border: 0;
  background: var(--cta-bg);
  color: var(--cta-fg);
  font-size: 0.81rem;
  font-weight: 650;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  box-shadow: 0 5px 14px -4px rgba(11, 13, 17, 0.3);
  transition: transform 0.18s var(--ease-spring), opacity 0.16s ease;
}

.play-main-btn:hover {
  opacity: 0.9;
}

.play-main-btn:active {
  transform: scale(0.95);
}

.vol-wrap {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--ink-2);
}

.vol-slider {
  width: 68px;
  accent-color: var(--cta-bg);
  cursor: pointer;
}

/* Integrated Centered Lyric Stage */
.deck-lyric-pane {
  position: relative;
  min-width: 0;
  padding-left: 16px;
  border-left: 1px solid var(--border-hairline);
  display: flex;
  align-items: stretch;
  animation: lyric-pane-in 0.38s var(--ease-bounce);
}

@keyframes lyric-pane-in {
  from {
    opacity: 0;
    transform: translateX(12px) scale(0.97);
  }
  to {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
}

.lyric-scroll-box {
  position: relative;
  width: 100%;
  height: 152px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  scrollbar-width: none;
  -webkit-mask-image: linear-gradient(
    to bottom,
    transparent 0%,
    #000 22%,
    #000 78%,
    transparent 100%
  );
  mask-image: linear-gradient(
    to bottom,
    transparent 0%,
    #000 22%,
    #000 78%,
    transparent 100%
  );
}

.lyric-scroll-box::-webkit-scrollbar {
  display: none;
}

/* 50% vertical spacers ensure Line #1 and Last Line can sit dead-center in the middle! */
.lyric-spacer {
  height: 62px;
  flex-shrink: 0;
}

.lrc-loading {
  margin: auto;
  font-size: 0.68rem;
  letter-spacing: 0.08em;
  color: var(--ink-3);
}

.lrc-line {
  border: 0;
  background: transparent;
  padding: 5px 8px;
  border-radius: 7px;
  text-align: left;
  color: var(--ink-3);
  font-size: 0.8rem;
  line-height: 1.42;
  opacity: 0.46;
  cursor: pointer;
  transform-origin: left center;
  transition:
    color 0.24s ease,
    opacity 0.24s ease,
    transform 0.28s var(--ease-spring);
}

.lrc-line:hover {
  opacity: 0.85;
  color: var(--ink-1);
}

.lrc-line.is-near {
  opacity: 0.72;
  color: var(--ink-2);
}

.lrc-line.is-current {
  opacity: 1;
  color: var(--ink-1);
  font-size: 0.92rem;
  font-weight: 680;
  transform: scale(1.02);
}

.lrc-txt {
  display: block;
  word-break: break-word;
}

/* Playlist Strip */
.playlist-strip {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow-x: auto;
  padding-bottom: 2px;
  scrollbar-width: none;
}
.playlist-strip::-webkit-scrollbar {
  display: none;
}

.pl-chip {
  height: 30px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.76rem;
  font-weight: 560;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  cursor: pointer;
  flex-shrink: 0;
  transition: all 0.16s ease;
}

.pl-chip:hover {
  color: var(--ink-1);
  border-color: var(--border-strong);
}

.pl-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.pl-num {
  font-size: 0.68rem;
  opacity: 0.7;
}

.pl-ava {
  width: 17px;
  height: 17px;
  border-radius: 4px;
  object-fit: cover;
}

/* Independently Scrollable Bottom Track List */
.track-scroll-area {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.track-scroll-inner {
  max-width: 960px;
  width: 100%;
  margin: 0 auto;
  padding: 12px 24px 28px;
}

.empty-box {
  padding: 42px 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

.track-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.track-row {
  padding: 8px 12px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 11px;
  cursor: pointer;
  transition:
    transform 0.16s var(--ease-spring),
    border-color 0.16s ease,
    background-color 0.16s ease;
}

.track-row:hover {
  border-color: var(--border-strong);
}

.track-row.is-playing {
  border-color: var(--ink-1);
  background: var(--surface-pill);
}

.tr-play-ind {
  width: 26px;
  height: 26px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--ink-3);
  font-size: 0.72rem;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  cursor: pointer;
}

.track-row.is-playing .tr-play-ind {
  background: var(--cta-bg);
  color: var(--cta-fg);
}

.tr-cover {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  object-fit: cover;
  flex-shrink: 0;
  border: 1px solid var(--border-hairline);
}

.tr-cover--empty {
  display: grid;
  place-items: center;
  background: var(--surface-recessed);
  color: var(--ink-3);
}

.tr-meta {
  flex: 1;
  min-width: 0;
}

.tr-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.86rem;
  font-weight: 630;
  color: var(--ink-1);
}

.tr-title > span:first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lrc-badge {
  font-size: 0.6rem;
  font-weight: 600;
  padding: 1px 4.5px;
  border-radius: 4px;
  background: var(--surface-recessed);
  color: var(--ink-3);
  flex-shrink: 0;
}

.tr-sub {
  margin-top: 2px;
  font-size: 0.73rem;
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tr-acts {
  display: flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
}

.mini-key {
  width: 29px;
  height: 29px;
  border-radius: 8px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  text-decoration: none;
  cursor: pointer;
  transition: all 0.16s ease;
}

.mini-key:hover {
  background: var(--surface-pill);
  color: var(--ink-1);
  border-color: var(--border-strong);
}

@media (max-width: 860px) {
  .music-fixed-inner {
    padding: 0 14px 10px;
  }

  .studio-deck {
    min-height: auto;
    padding: 12px 13px;
    gap: 10px;
  }

  .studio-deck.has-lyrics {
    grid-template-columns: minmax(0, 1fr);
  }

  .turntable-unit {
    grid-template-columns: 84px minmax(0, 1fr);
    gap: 12px;
  }

  .platter-box {
    width: 82px;
    height: 82px;
  }

  .vinyl-platter {
    width: 78px;
    height: 78px;
  }

  .vinyl-label {
    width: 38px;
    height: 38px;
  }

  .tonearm {
    display: none;
  }

  .deck-lyric-pane {
    padding-left: 0;
    padding-top: 8px;
    border-left: 0;
    border-top: 1px solid var(--border-hairline);
  }

  .lyric-scroll-box {
    height: 76px;
  }

  .lyric-spacer {
    height: 26px;
  }

  .track-scroll-inner {
    padding: 10px 14px 20px;
  }

  .hide-xs {
    display: none;
  }
}
</style>
