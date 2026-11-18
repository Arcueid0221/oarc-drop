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
const showLyricsSheet = ref(true)
const lyricsContainer = ref<HTMLElement | null>(null)
const trackScrollEl = ref<HTMLElement | null>(null)

const scrubEl = ref<HTMLElement | null>(null)
const isScrubbing = ref(false)
const jellyStretch = ref(0)

onMounted(() => {
  if (store.mediaItems.value.length === 0) {
    void store.loadMedia(false)
  }
  if (store.playlists.value.length === 0) {
    void store.loadMusicCatalog(false)
  }
})

async function refreshMusic() {
  await Promise.all([store.loadMedia(true), store.loadMusicCatalog(true)])
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

const progressRatio = computed(() => {
  const dur = store.audioDuration.value
  if (!dur || dur <= 0) return 0
  return Math.min(1, Math.max(0, store.audioProgress.value / dur))
})

const activeLyricIndex = computed(() => {
  const lines = store.currentLyrics.value
  if (!lines.length) return -1
  const t = store.audioProgress.value
  let idx = 0
  for (let i = 0; i < lines.length; i++) {
    if (t >= lines[i].time) idx = i
    else break
  }
  return idx
})

const activeLyricText = computed(() => {
  const idx = activeLyricIndex.value
  return idx >= 0 ? store.currentLyrics.value[idx]?.text || '' : ''
})

watch(activeLyricIndex, async (idx) => {
  if (idx < 0 || !showLyricsSheet.value) return
  await nextTick()
  const box = lyricsContainer.value
  if (!box) return
  const lineEl = box.querySelector<HTMLElement>(`[data-lrc="${idx}"]`)
  if (lineEl) {
    const targetTop = lineEl.offsetTop - box.clientHeight / 2 + lineEl.clientHeight / 2
    box.scrollTo({ top: Math.max(0, targetTop), behavior: 'smooth' })
  }
})

function scrollToPlayingTrack() {
  const curKey = store.currentTrack.value?.key
  const box = trackScrollEl.value
  if (!curKey || !box) return
  const rowEl = box.querySelector<HTMLElement>('[data-playing="1"]')
  if (rowEl) {
    const targetTop = rowEl.offsetTop - box.clientHeight / 2 + rowEl.clientHeight / 2
    box.scrollTo({ top: Math.max(0, targetTop), behavior: 'smooth' })
  }
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

function seekFromClientX(clientX: number) {
  const el = scrubEl.value
  const dur = store.audioDuration.value
  if (!el || !dur) return
  const r = el.getBoundingClientRect()
  const raw = (clientX - r.left) / r.width
  if (raw < 0) jellyStretch.value = Math.max(-8, raw * 22)
  else if (raw > 1) jellyStretch.value = Math.min(8, (raw - 1) * 22)
  else jellyStretch.value = 0

  const clamped = Math.min(1, Math.max(0, raw))
  seekToTime(clamped * dur)
}

function onScrubDown(e: PointerEvent) {
  isScrubbing.value = true
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  seekFromClientX(e.clientX)
}

function onScrubMove(e: PointerEvent) {
  if (!isScrubbing.value) return
  seekFromClientX(e.clientX)
}

function onScrubUp() {
  isScrubbing.value = false
  jellyStretch.value = 0
}

onBeforeUnmount(() => {
  isScrubbing.value = false
})

async function shareTrack(track: MediaItem) {
  const res = await fetch('/api/shares/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      storage: 'silo',
      key: track.key,
      fileName: track.name,
      size: track.size,
      ttlHours: 0,
    }),
  })
  if (res.ok) {
    const sh = await res.json()
    const url = shareFullUrl(sh.code)
    await copyText(url, '歌曲公开短链已复制')
    store.openQrModal(track.name, url, '外部访客扫码免登录试听或下载真名音频')
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
  return '顺序'
})
</script>

<template>
  <div class="music-page">
    <!-- TOP FIXED REGION: Header + Vinyl Turntable + Controls + Lyrics + Playlist Filter (NEVER scrolls away!) -->
    <div class="music-top-fixed">
      <header class="pane-header music-header">
        <div class="pane-title-wrap">
          <h1 class="pane-title">音乐厅</h1>
          <p class="pane-subtitle">
            {{ store.audioCount.value }} 首真名曲库 · 上方唱机固定，下方歌单独立滑动
          </p>
        </div>

        <div class="pane-actions search-bar-wrap">
          <div class="search-pill">
            <UiIcon name="search" :size="14.5" />
            <input v-model="search" placeholder="搜歌手、歌名或专辑…" />
          </div>
          <button
            v-if="store.currentTrack.value"
            type="button"
            class="pill-btn"
            title="在下方列表定位到正在播放的歌曲"
            @click="scrollToPlayingTrack"
          >
            <UiIcon name="disc" :size="14" />
            <span class="hide-xs">定位当前</span>
          </button>
          <button type="button" class="icon-circle-btn" title="刷新曲库" @click="refreshMusic">
            <UiIcon name="refresh" :size="14.5" />
          </button>
        </div>
      </header>

      <div class="music-fixed-inner">
        <section class="glass-box deck-wrapper">
          <div class="turntable-deck">
            <div class="platter-box" @click="togglePlay" title="点击唱盘播放 / 暂停">
              <div
                class="vinyl-platter"
                :class="{ 'is-spinning': store.isPlaying.value && Boolean(store.currentTrack.value) }"
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
                :class="{ 'is-engaged': store.isPlaying.value && Boolean(store.currentTrack.value) }"
              >
                <div class="arm-pivot" />
                <div class="arm-rod" />
              </div>
            </div>

            <div class="deck-console">
              <div class="deck-status-line">
                <div class="now-pill">
                  <div class="eq-bars" :class="{ 'is-paused': !store.isPlaying.value }">
                    <span /><span /><span /><span />
                  </div>
                  <span class="mono">{{ store.isPlaying.value ? 'PLAYING' : 'PAUSED' }}</span>
                </div>

                <button
                  v-if="store.currentLyrics.value.length > 0"
                  type="button"
                  class="lrc-toggle-pill"
                  :class="{ 'is-on': showLyricsSheet }"
                  @click="showLyricsSheet = !showLyricsSheet"
                >
                  {{ showLyricsSheet ? '收起歌词' : '展开歌词' }}
                </button>
              </div>

              <div class="deck-title">
                {{ store.currentMeta.value?.title || '未播放 · 点击下方任意歌曲开始播放' }}
              </div>
              <div class="deck-sub">
                <template v-if="store.currentMeta.value?.artist">
                  {{ store.currentMeta.value.artist }}
                  <template v-if="store.currentMeta.value?.album">
                    · {{ store.currentMeta.value.album }}
                  </template>
                </template>
                <template v-else-if="activeLyricText">{{ activeLyricText }}</template>
                <template v-else>点按播放键或下方歌曲即可试听</template>
              </div>

              <!-- Scrubber -->
              <div
                ref="scrubEl"
                class="jelly-scrubber"
                :style="{
                  transform: `translateX(${jellyStretch}px) scaleX(${1 + Math.abs(jellyStretch) * 0.004})`,
                }"
                @pointerdown="onScrubDown"
                @pointermove="onScrubMove"
                @pointerup="onScrubUp"
                @pointercancel="onScrubUp"
              >
                <div class="scrub-track">
                  <div class="scrub-fill" :style="{ width: `${progressRatio * 100}%` }" />
                </div>
                <div class="scrub-times mono">
                  <span>{{ fmtSec(store.audioProgress.value) }}</span>
                  <span>{{ fmtSec(store.audioDuration.value) }}</span>
                </div>
              </div>

              <!-- Unmistakable High-Contrast Transport Controls -->
              <div class="deck-transport">
                <button
                  type="button"
                  class="pill-btn mode-pill"
                  :class="{ 'mode-active': store.playMode.value !== 'sequence' }"
                  title="切换顺序 / 随机 / 单曲循环"
                  @click="store.cyclePlayMode"
                >
                  <UiIcon :name="playModeIcon" :size="14" />
                  <span>{{ playModeLabel }}</span>
                </button>

                <button type="button" class="icon-circle-btn" title="上一首" @click="stepTrack(-1)">
                  <UiIcon name="prev" :size="15" />
                </button>

                <button
                  type="button"
                  class="play-main-btn"
                  :title="store.isPlaying.value ? '暂停' : '播放'"
                  @click="togglePlay"
                >
                  <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="18" />
                  <span>{{ store.isPlaying.value ? '暂停' : '播放' }}</span>
                </button>

                <button type="button" class="icon-circle-btn" title="下一首" @click="stepTrack(1)">
                  <UiIcon name="next" :size="15" />
                </button>

                <div class="vol-wrap hide-xs" title="调节音量">
                  <UiIcon name="volume" :size="14" />
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

          <!-- Compact Scrolling Lyrics Drawer -->
          <div
            v-if="showLyricsSheet && store.currentLyrics.value.length > 0"
            ref="lyricsContainer"
            class="lyrics-drawer"
          >
            <button
              v-for="(ln, idx) in store.currentLyrics.value"
              :key="idx"
              type="button"
              :data-lrc="idx"
              class="lrc-row"
              :class="{ 'is-current': idx === activeLyricIndex }"
              @click="seekToTime(ln.time)"
            >
              <span class="lrc-time mono">{{ fmtSec(ln.time) }}</span>
              <span class="lrc-txt">{{ ln.text }}</span>
            </button>
          </div>
        </section>

        <!-- Playlist Filter Bar (stays pinned above the scrollable track list!) -->
        <div class="playlist-strip">
          <button
            type="button"
            class="pl-chip"
            :class="{ 'is-on': selectedPlaylistId === 'all' }"
            @click="selectedPlaylistId = 'all'"
          >
            <span>全部 ({{ store.audioTracks.value.length }})</span>
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
            <span>{{ pl.name }} ({{ pl.trackCount }})</span>
          </button>
        </div>
      </div>
    </div>

    <!-- BOTTOM INDEPENDENTLY SCROLLABLE TRACK LIST -->
    <div ref="trackScrollEl" class="track-scroll-area">
      <div class="track-scroll-inner">
        <div v-if="filteredTracks.length === 0" class="glass-box empty-box">
          没有匹配的歌曲
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
              :title="store.currentTrack.value?.key === tr.key && store.isPlaying.value ? '暂停' : '播放此曲'"
            >
              <UiIcon
                v-if="store.currentTrack.value?.key === tr.key"
                :name="store.isPlaying.value ? 'pause' : 'play'"
                :size="13"
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
              <UiIcon name="disc" :size="16" />
            </div>

            <div class="tr-meta">
              <div class="tr-title">
                <span>{{ parseTrackDisplayName(tr, store.catalogTracks.value).title }}</span>
                <span v-if="tr.lyricsKey" class="lrc-badge mono">词</span>
              </div>
              <div class="tr-sub">
                {{ parseTrackDisplayName(tr, store.catalogTracks.value).artist }}
                <template v-if="parseTrackDisplayName(tr, store.catalogTracks.value).album">
                  · {{ parseTrackDisplayName(tr, store.catalogTracks.value).album }}
                </template>
                <span class="mono"> · {{ formatBytes(tr.size) }}</span>
              </div>
            </div>

            <div class="tr-acts" @click.stop>
              <a
                class="mini-circle"
                :href="`${tr.streamUrl}&download=1`"
                :title="`下载 ${tr.name}`"
              >
                <UiIcon name="download" :size="13.5" />
              </a>
              <button
                type="button"
                class="mini-circle"
                title="生成分享短链 + 二维码"
                @click="shareTrack(tr)"
              >
                <UiIcon name="qr" :size="13.5" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Split-pane architecture: top fixed, bottom track list scrolls independently! */
.music-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.music-top-fixed {
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-hairline);
  background: rgba(255, 255, 255, 0.28);
}

[data-theme="dark"] .music-top-fixed {
  background: rgba(17, 18, 22, 0.35);
}

.music-header {
  position: static;
  border-bottom: none;
  padding-bottom: 8px;
}

.music-fixed-inner {
  max-width: 820px;
  width: 100%;
  margin: 0 auto;
  padding: 0 24px 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.deck-wrapper {
  border-radius: 22px;
  padding: 14px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.turntable-deck {
  display: grid;
  grid-template-columns: 128px 1fr;
  align-items: center;
  gap: 20px;
}

.platter-box {
  position: relative;
  width: 122px;
  height: 122px;
  display: grid;
  place-items: center;
  cursor: pointer;
}

.vinyl-platter {
  width: 116px;
  height: 116px;
  border-radius: 50%;
  background: radial-gradient(circle, #1e293b 0%, #0f172a 68%, #020617 100%);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.26);
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
  inset: 8px;
  border-radius: 50%;
  border: 1px dashed rgba(255, 255, 255, 0.09);
}

.vinyl-label {
  width: 58px;
  height: 58px;
  border-radius: 50%;
  overflow: hidden;
  position: relative;
  display: grid;
  place-items: center;
  background: linear-gradient(135deg, #3b82f6, #6366f1);
}

.vinyl-label img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.spindle-hole {
  position: absolute;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #0f172a;
  border: 2px solid rgba(255, 255, 255, 0.75);
}

.tonearm {
  position: absolute;
  top: 4px;
  right: -4px;
  width: 28px;
  height: 76px;
  transform-origin: 18px 10px;
  transform: rotate(-18deg);
  transition: transform 0.45s cubic-bezier(0.22, 1, 0.36, 1);
  pointer-events: none;
}

.tonearm.is-engaged {
  transform: rotate(12deg);
}

.arm-pivot {
  position: absolute;
  top: 3px;
  right: 3px;
  width: 13px;
  height: 13px;
  border-radius: 50%;
  background: linear-gradient(135deg, #e2e8f0, #94a3b8);
}

.arm-rod {
  position: absolute;
  top: 12px;
  right: 8px;
  width: 2.5px;
  height: 54px;
  background: linear-gradient(to bottom, #cbd5e1, #64748b);
  border-radius: 2px;
}

.deck-console {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.deck-status-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.now-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 0.68rem;
  font-weight: 680;
  color: var(--accent);
}

.eq-bars {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 11px;
}

.eq-bars span {
  width: 2.5px;
  height: 100%;
  background: var(--accent);
  border-radius: 2px;
  animation: eq 0.8s ease-in-out infinite alternate;
}
.eq-bars span:nth-child(2) {
  animation-delay: 0.2s;
}
.eq-bars span:nth-child(3) {
  animation-delay: 0.35s;
}
.eq-bars.is-paused span {
  animation-play-state: paused;
  height: 35%;
}

@keyframes eq {
  from {
    height: 25%;
  }
  to {
    height: 100%;
  }
}

.lrc-toggle-pill {
  padding: 3px 10px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.72rem;
  cursor: pointer;
}

.lrc-toggle-pill.is-on {
  background: var(--accent-soft);
  color: var(--accent);
}

.deck-title {
  font-size: 1.04rem;
  font-weight: 700;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.deck-sub {
  font-size: 0.79rem;
  color: var(--ink-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.jelly-scrubber {
  margin-top: 2px;
  cursor: pointer;
  user-select: none;
  touch-action: none;
}

.scrub-track {
  height: 6px;
  border-radius: 999px;
  background: var(--surface-recessed);
  overflow: hidden;
}

.scrub-fill {
  height: 100%;
  background: var(--cta-bg);
  border-radius: 999px;
}

.scrub-times {
  margin-top: 3px;
  display: flex;
  justify-content: space-between;
  font-size: 0.69rem;
  color: var(--ink-3);
}

.deck-transport {
  margin-top: 2px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.mode-pill {
  height: 34px;
  padding: 0 11px;
  font-size: 0.76rem;
}

.mode-active {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent-border);
}

/* Bold, High-Contrast Play / Pause Capsule Button (Impossible to miss!) */
.play-main-btn {
  height: 38px;
  padding: 0 18px;
  border-radius: 999px;
  border: none;
  background: var(--cta-bg);
  color: var(--cta-fg);
  font-size: 0.84rem;
  font-weight: 660;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  cursor: pointer;
  box-shadow: 0 6px 18px -4px rgba(15, 23, 42, 0.32);
  transition: transform 0.18s var(--ease-spring), opacity 0.18s ease;
}

.play-main-btn:hover {
  opacity: 0.92;
}

.play-main-btn:active {
  transform: scale(0.95);
}

.vol-wrap {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--ink-2);
}

.vol-slider {
  width: 78px;
  accent-color: var(--cta-bg);
  cursor: pointer;
}

.lyrics-drawer {
  max-height: 108px;
  overflow-y: auto;
  padding: 6px 6px;
  border-top: 1px solid var(--border-hairline);
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.lrc-row {
  border: none;
  background: transparent;
  padding: 4px 10px;
  border-radius: 9px;
  display: flex;
  align-items: baseline;
  gap: 10px;
  text-align: left;
  color: var(--ink-3);
  font-size: 0.8rem;
  cursor: pointer;
}

.lrc-row:hover {
  background: var(--surface-recessed);
  color: var(--ink-1);
}

.lrc-row.is-current {
  color: var(--accent);
  font-weight: 660;
  font-size: 0.87rem;
  background: var(--accent-soft);
}

.lrc-time {
  font-size: 0.68rem;
  opacity: 0.65;
  flex-shrink: 0;
}

.playlist-strip {
  display: flex;
  overflow-x: auto;
  gap: 6px;
  padding-bottom: 2px;
}

.pl-chip {
  flex-shrink: 0;
  padding: 4px 11px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.75rem;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}

.pl-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
  font-weight: 620;
}

.pl-ava {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  object-fit: cover;
}

/* INDEPENDENT SCROLL AREA FOR SONG LIST */
.track-scroll-area {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.track-scroll-inner {
  max-width: 820px;
  width: 100%;
  margin: 0 auto;
  padding: 12px 24px 28px;
}

.track-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.track-row {
  padding: 8px 13px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  gap: 11px;
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.track-row:hover {
  border-color: var(--border-strong);
}

.track-row.is-playing {
  border-color: var(--accent-border);
  background: var(--accent-soft);
}

.tr-play-ind {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  border: none;
  background: transparent;
  color: var(--ink-3);
  font-size: 0.73rem;
  display: grid;
  place-items: center;
  cursor: pointer;
  flex-shrink: 0;
}

.track-row.is-playing .tr-play-ind {
  background: var(--cta-bg);
  color: var(--cta-fg);
}

.tr-cover {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  object-fit: cover;
  flex-shrink: 0;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.12);
}

.tr-cover--empty {
  background: var(--surface-recessed);
  color: var(--ink-3);
  display: grid;
  place-items: center;
}

.tr-meta {
  flex: 1;
  min-width: 0;
}

.tr-title {
  font-size: 0.87rem;
  font-weight: 640;
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.lrc-badge {
  font-size: 0.62rem;
  padding: 1px 5px;
  border-radius: 5px;
  background: var(--surface-recessed);
  color: var(--ink-2);
  flex-shrink: 0;
}

.tr-sub {
  margin-top: 2px;
  font-size: 0.73rem;
  color: var(--ink-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tr-acts {
  display: flex;
  align-items: center;
  gap: 6px;
}

.mini-circle {
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

.empty-box {
  padding: 40px 20px;
  text-align: center;
  border-radius: 18px;
  color: var(--ink-3);
  font-size: 0.84rem;
}

@media (max-width: 860px) {
  .music-page {
    height: calc(100dvh - 54px - 86px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px));
  }
  .music-fixed-inner {
    padding: 0 14px 8px;
  }
  .track-scroll-inner {
    padding: 8px 14px 20px;
  }
  .turntable-deck {
    grid-template-columns: 88px 1fr;
    gap: 12px;
  }
  .platter-box {
    width: 86px;
    height: 86px;
  }
  .vinyl-platter {
    width: 82px;
    height: 82px;
  }
  .vinyl-label {
    width: 42px;
    height: 42px;
  }
  .tonearm {
    display: none;
  }
  .lyrics-drawer {
    max-height: 76px;
  }
  .hide-xs {
    display: none;
  }
}
</style>
