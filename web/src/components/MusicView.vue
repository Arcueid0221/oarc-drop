<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  formatBytes,
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
const trackShares = ref<Record<string, string>>({})

const scrubEl = ref<HTMLElement | null>(null)
const isScrubbing = ref(false)
const jellyStretch = ref(0)

onMounted(() => {
  if (store.mediaItems.value.length === 0) {
    void store.loadMedia(false)
  }
})

const audioFolders = computed(() => {
  const s = new Set<string>()
  for (const t of store.audioTracks.value) s.add(t.folder)
  return Array.from(s).sort()
})

const filteredTracks = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.audioTracks.value.filter((t) => {
    if (activeFolder.value && t.folder !== activeFolder.value) return false
    if (q && !t.key.toLowerCase().includes(q)) return false
    return true
  })
})

const progressRatio = computed(() => {
  const dur = store.audioDuration.value
  if (!dur || dur <= 0) return 0
  return Math.min(1, Math.max(0, store.audioProgress.value / dur))
})

const activeLyricText = computed(() => {
  const lines = store.currentLyrics.value
  if (!lines.length) return ''
  const t = store.audioProgress.value
  let txt = lines[0]?.text || ''
  for (let i = 0; i < lines.length; i++) {
    if (t >= lines[i].time) txt = lines[i].text
    else break
  }
  return txt
})

function togglePlay() {
  if (!store.currentTrack.value && filteredTracks.value.length > 0) {
    void store.playTrack(filteredTracks.value[0])
    return
  }
  window.dispatchEvent(new CustomEvent('oarc-audio-toggle'))
}

function stepTrack(delta: number) {
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
  window.dispatchEvent(new CustomEvent('oarc-audio-seek', { detail: clamped * dur }))
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

async function createTrackShare(track: MediaItem) {
  const res = await fetch('/api/shares/from-silo', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key: track.key, size: track.size, ttlHours: 0 }),
  })
  if (res.ok) {
    const sh = await res.json()
    const url = shareFullUrl(sh.code)
    trackShares.value[track.key] = url
    await navigator.clipboard.writeText(url).catch(() => {})
    showToast('音频分享短链已生成并复制')
  }
}

function fmtSec(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '0:00'
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}
</script>

<template>
  <div class="music-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">Silo 音乐</h1>
        <p class="pane-subtitle">共 {{ store.audioCount.value }} 首曲目 · 支持无损流式播放与同步歌词</p>
      </div>

      <div class="pane-actions search-bar-wrap">
        <div class="search-pill">
          <UiIcon name="search" :size="14.5" />
          <input v-model="search" placeholder="搜索曲目…" />
        </div>
        <button type="button" class="icon-circle-btn" title="刷新曲库" @click="store.loadMedia(true)">
          <UiIcon name="refresh" :size="14.5" />
        </button>
      </div>
    </header>

    <div class="pane-body--narrow music-stack">
      <!-- Top: Symmetrical Wide Hi-Fi Vinyl Deck (no uneven side columns!) -->
      <section class="glass-box turntable-deck">
        <div class="platter-box">
          <div
            class="vinyl-platter"
            :class="{ 'is-spinning': store.isPlaying.value && Boolean(store.currentTrack.value) }"
          >
            <div class="vinyl-grooves" />
            <div class="vinyl-sheen" />
            <div class="vinyl-label">
              <img v-if="store.currentCoverUrl.value" :src="store.currentCoverUrl.value" alt="" />
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
            <div class="arm-head" />
          </div>
        </div>

        <div class="deck-console">
          <div class="deck-status-line">
            <div class="now-pill">
              <div class="eq-bars" :class="{ 'is-paused': !store.isPlaying.value }">
                <span /><span /><span /><span />
              </div>
              <span class="mono">NOW PLAYING</span>
            </div>
            <span class="rpm-tag mono">
              <template v-if="store.currentMeta.value?.sampleRate">
                {{ store.currentMeta.value.format }} · {{ (store.currentMeta.value.sampleRate / 1000).toFixed(1) }}kHz / {{ store.currentMeta.value.bitsPerSample }}bit
              </template>
              <template v-else>{{ store.currentMeta.value?.format || '33⅓ RPM' }}</template>
            </span>
          </div>

          <div class="deck-title">
            {{ store.currentMeta.value?.title || '未选择曲目' }}
          </div>
          <div class="deck-sub">
            <template v-if="activeLyricText">
              <span class="live-lyric">{{ activeLyricText }}</span>
            </template>
            <template v-else>
              {{ store.currentMeta.value?.artist || (store.currentTrack.value ? store.currentTrack.value.folder : '点击下方列表即可播放，切换页面不会中断') }}
            </template>
          </div>

          <!-- Jelly Scrubber -->
          <div
            ref="scrubEl"
            class="jelly-scrubber"
            :class="{ 'is-dragging': isScrubbing }"
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

          <div class="deck-transport">
            <button type="button" class="icon-circle-btn" title="上一曲" @click="stepTrack(-1)">
              <UiIcon name="prev" :size="15" />
            </button>
            <button type="button" class="play-main-btn" @click="togglePlay">
              <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="17" />
            </button>
            <button type="button" class="icon-circle-btn" title="下一曲" @click="stepTrack(1)">
              <UiIcon name="next" :size="15" />
            </button>
          </div>
        </div>
      </section>

      <!-- Folder Filter -->
      <div v-if="audioFolders.length > 1" class="folder-strip">
        <button
          type="button"
          class="folder-chip"
          :class="{ 'is-on': activeFolder === '' }"
          @click="activeFolder = ''"
        >
          全部目录
        </button>
        <button
          v-for="f in audioFolders"
          :key="f"
          type="button"
          class="folder-chip mono"
          :class="{ 'is-on': activeFolder === f }"
          @click="activeFolder = f"
        >
          {{ f }}
        </button>
      </div>

      <!-- Bottom: Aligned Full-Width Track List -->
      <div v-if="filteredTracks.length === 0" class="glass-box lib-empty">
        Silo 桶内暂无音频 · 可在「快传投递」切到 `Silo (music/audio/)` 上传无损 FLAC 或 MP3
      </div>

      <div v-else class="glass-box track-table">
        <article
          v-for="(track, idx) in filteredTracks"
          :key="track.key"
          class="track-row"
          :class="{ 'is-playing': store.currentTrack.value?.key === track.key }"
          @click="store.playTrack(track)"
        >
          <span class="t-num mono">
            <span
              v-if="store.currentTrack.value?.key === track.key"
              class="mini-eq"
              :class="{ 'is-paused': !store.isPlaying.value }"
            >
              <i /><i /><i />
            </span>
            <template v-else>{{ String(idx + 1).padStart(2, '0') }}</template>
          </span>

          <div class="t-body">
            <div class="t-name">{{ track.name.replace(/\.[^.]+$/, '') }}</div>
            <div class="t-sub mono">{{ track.folder }} · {{ formatBytes(track.size) }}</div>
          </div>

          <span class="t-fmt mono">{{ track.ext }}</span>

          <div class="t-acts" @click.stop>
            <CopyPill
              v-if="track.publicUrl"
              :text="track.publicUrl"
              label="直链"
              copied-label="已复制"
              compact
            />
            <CopyPill
              v-if="trackShares[track.key]"
              :text="trackShares[track.key]"
              label="短链"
              copied-label="已复制"
              compact
            />
            <button
              v-else
              type="button"
              class="mini-share-btn"
              title="生成公开分享短链"
              @click="createTrackShare(track)"
            >
              <UiIcon name="link" :size="13.5" />
            </button>
          </div>
        </article>
      </div>
    </div>
  </div>
</template>

<style scoped>
.music-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

@media (max-width: 860px) {
  .search-bar-wrap {
    width: 100%;
  }
}

/* Horizontal Wide Vinyl Deck */
.turntable-deck {
  padding: 24px 28px;
  border-radius: 24px;
  display: grid;
  grid-template-columns: 188px minmax(0, 1fr);
  align-items: center;
  gap: 28px;
}

@media (max-width: 640px) {
  .turntable-deck {
    grid-template-columns: 1fr;
    justify-items: center;
    text-align: center;
    padding: 22px 18px;
    gap: 18px;
  }
  .deck-console {
    width: 100%;
  }
  .deck-transport {
    justify-content: center !important;
  }
}

.platter-box {
  position: relative;
  width: 184px;
  height: 176px;
  display: grid;
  place-items: center;
}

.vinyl-platter {
  position: relative;
  width: 168px;
  height: 168px;
  border-radius: 50%;
  background: #141519;
  box-shadow:
    0 16px 32px -10px rgba(0, 0, 0, 0.36),
    inset 0 0 0 2px rgba(255, 255, 255, 0.08);
  display: grid;
  place-items: center;
  animation: record-turn 8.5s linear infinite;
  animation-play-state: paused;
}

.vinyl-platter.is-spinning {
  animation-play-state: running;
}

@keyframes record-turn {
  to { transform: rotate(360deg); }
}

.vinyl-grooves {
  position: absolute;
  inset: 9px;
  border-radius: 50%;
  background: repeating-radial-gradient(
    circle at center,
    rgba(255, 255, 255, 0.045) 0px,
    rgba(255, 255, 255, 0.045) 1px,
    transparent 2px,
    transparent 5px
  );
}

.vinyl-sheen {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: conic-gradient(
    from 35deg,
    transparent 0deg,
    rgba(255, 255, 255, 0.14) 38deg,
    transparent 78deg,
    transparent 180deg,
    rgba(255, 255, 255, 0.12) 218deg,
    transparent 260deg
  );
}

.vinyl-label {
  position: relative;
  width: 62px;
  height: 62px;
  border-radius: 50%;
  overflow: hidden;
  box-shadow: 0 0 0 3px #0d0e11;
  display: grid;
  place-items: center;
}

.vinyl-label img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.label-fallback {
  width: 100%;
  height: 100%;
  background: linear-gradient(135deg, #fb923c, #f97316);
}

.spindle-hole {
  position: absolute;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--bg-window);
}

.tonearm {
  position: absolute;
  top: 4px;
  right: -2px;
  width: 48px;
  height: 118px;
  transform-origin: 34px 14px;
  transform: rotate(-26deg);
  transition: transform 0.6s var(--ease-spring);
  pointer-events: none;
}

.tonearm.is-engaged {
  transform: rotate(3deg);
}

.arm-pivot {
  position: absolute;
  top: 3px;
  right: 3px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: linear-gradient(145deg, #e2e8f0, #94a3b8);
  box-shadow: 0 3px 8px rgba(0, 0, 0, 0.24);
}

.arm-rod {
  position: absolute;
  top: 14px;
  right: 12px;
  width: 3px;
  height: 86px;
  border-radius: 99px;
  background: linear-gradient(90deg, #cbd5e1, #f8fafc, #94a3b8);
  transform: rotate(10deg);
  transform-origin: top center;
}

.arm-head {
  position: absolute;
  bottom: 8px;
  left: 8px;
  width: 10px;
  height: 15px;
  border-radius: 3px;
  background: #334155;
  transform: rotate(24deg);
}

.deck-console {
  min-width: 0;
}

.deck-status-line {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.now-pill {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 0.69rem;
  font-weight: 650;
  letter-spacing: 0.06em;
  color: var(--ink-2);
}

.eq-bars {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 12px;
}
.eq-bars span {
  width: 2.5px;
  border-radius: 2px;
  background: var(--amber-eq);
  animation: eq-mini 0.8s ease-in-out infinite alternate;
}
.eq-bars span:nth-child(2) { animation-delay: -0.2s; }
.eq-bars span:nth-child(3) { animation-delay: -0.45s; }
.eq-bars span:nth-child(4) { animation-delay: -0.15s; }
.eq-bars.is-paused span {
  animation-play-state: paused;
  height: 3px !important;
}

@keyframes eq-mini {
  from { height: 3px; }
  to { height: 12px; }
}

.rpm-tag {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.deck-title {
  font-size: 1.18rem;
  font-weight: 690;
  color: var(--ink-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.deck-sub {
  margin-top: 4px;
  min-height: 22px;
  font-size: 0.83rem;
  color: var(--ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.live-lyric {
  color: var(--ink-1);
  font-weight: 580;
}

.jelly-scrubber {
  margin-top: 14px;
  padding: 6px 0;
  cursor: pointer;
  user-select: none;
  touch-action: none;
  transition: transform 0.34s var(--ease-spring);
}

.scrub-track {
  height: 6px;
  border-radius: 999px;
  background: var(--surface-recessed-hover);
  overflow: hidden;
  transition: height 0.24s var(--ease-spring);
}

.jelly-scrubber:hover .scrub-track,
.jelly-scrubber.is-dragging .scrub-track {
  height: 11px;
}

.scrub-fill {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, #fb923c, var(--amber-eq));
  transition: width 0.1s linear;
}

.scrub-times {
  margin-top: 5px;
  display: flex;
  justify-content: space-between;
  font-size: 0.71rem;
  color: var(--ink-3);
}

.deck-transport {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.play-main-btn {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  border: 0;
  background: var(--cta-bg);
  color: var(--cta-fg);
  display: grid;
  place-items: center;
  cursor: pointer;
  box-shadow: 0 8px 20px -6px rgba(0, 0, 0, 0.28);
  transition: transform 0.22s var(--ease-spring);
}

.play-main-btn:hover {
  transform: scale(1.06);
}

.folder-strip {
  display: flex;
  gap: 6px;
  overflow-x: auto;
}

.folder-chip {
  height: 29px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.75rem;
  white-space: nowrap;
  cursor: pointer;
}

.folder-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.lib-empty {
  padding: 48px 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

/* Aligned Track Table */
.track-table {
  border-radius: 22px;
  overflow: hidden;
}

.track-row {
  padding: 13px 18px;
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--border-hairline);
  cursor: pointer;
  transition: background-color 0.18s ease;
}

.track-row:last-child {
  border-bottom: 0;
}

.track-row:hover {
  background: var(--surface-recessed);
}

.track-row.is-playing {
  background: rgba(249, 115, 22, 0.07);
}

@media (max-width: 560px) {
  .track-row {
    padding: 12px 14px;
    grid-template-columns: 26px minmax(0, 1fr) auto;
  }
  .t-fmt {
    display: none;
  }
}

.t-num {
  font-size: 0.77rem;
  color: var(--ink-3);
  display: flex;
  align-items: center;
}

.mini-eq {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 12px;
}
.mini-eq i {
  width: 2.5px;
  border-radius: 2px;
  background: var(--amber-eq);
  animation: eq-mini 0.75s ease-in-out infinite alternate;
}
.mini-eq i:nth-child(2) { animation-delay: -0.25s; }
.mini-eq i:nth-child(3) { animation-delay: -0.45s; }
.mini-eq.is-paused i {
  animation-play-state: paused;
  height: 3px !important;
}

.t-body {
  min-width: 0;
}

.t-name {
  font-size: 0.88rem;
  font-weight: 620;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.t-sub {
  font-size: 0.73rem;
  color: var(--ink-3);
  margin-top: 2px;
}

.t-fmt {
  font-size: 0.7rem;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.t-acts {
  display: flex;
  align-items: center;
  gap: 5px;
}

.mini-share-btn {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  cursor: pointer;
}
</style>
