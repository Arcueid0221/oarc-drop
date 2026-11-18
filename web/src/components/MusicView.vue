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

// Auto-center active lyric line smoothly inside scrolling lyrics drawer
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

function seekToTime(sec: number) {
  window.dispatchEvent(new CustomEvent('oarc-audio-seek', { detail: sec }))
}

function playFromFiltered(track: MediaItem) {
  void store.playTrack(track, filteredTracks.value)
}

function togglePlay() {
  if (!store.currentTrack.value && filteredTracks.value.length > 0) {
    playFromFiltered(filteredTracks.value[0])
    return
  }
  window.dispatchEvent(new CustomEvent('oarc-audio-toggle'))
}

function stepTrack(delta: number) {
  if (!store.currentTrack.value && filteredTracks.value.length > 0) {
    playFromFiltered(filteredTracks.value[0])
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

const playModeTitle = computed(() => {
  if (store.playMode.value === 'shuffle') return '当前：随机播放（点击切换）'
  if (store.playMode.value === 'one') return '当前：单曲循环（点击切换）'
  return '当前：列表循环（点击切换）'
})
</script>

<template>
  <div class="music-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">音乐厅</h1>
        <p class="pane-subtitle">
          共 {{ store.audioCount.value }} 首真名曲库 · 联动博客歌单封面与 262 份可点选同步歌词
        </p>
      </div>

      <div class="pane-actions search-bar-wrap">
        <div class="search-pill">
          <UiIcon name="search" :size="14.5" />
          <input v-model="search" placeholder="搜歌手、歌名或专辑…" />
        </div>
        <button type="button" class="icon-circle-btn" title="刷新曲库与歌单" @click="refreshMusic">
          <UiIcon name="refresh" :size="14.5" />
        </button>
      </div>
    </header>

    <div class="pane-body--narrow music-stack">
      <!-- Top: Vinyl Turntable + Interactive Scrolling Lyrics Drawer -->
      <section class="glass-box deck-wrapper">
        <div class="turntable-deck">
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

              <button
                v-if="store.currentLyrics.value.length > 0"
                type="button"
                class="lrc-toggle-pill"
                :class="{ 'is-on': showLyricsSheet }"
                @click="showLyricsSheet = !showLyricsSheet"
              >
                {{ showLyricsSheet ? '收起词谱' : '展开词谱' }}
              </button>
            </div>

            <div class="deck-title">
              {{ store.currentMeta.value?.title || '点击下方歌曲开始播放' }}
            </div>
            <div class="deck-sub">
              <template v-if="store.currentMeta.value?.artist">
                {{ store.currentMeta.value.artist }}
                <template v-if="store.currentMeta.value?.album">
                  · {{ store.currentMeta.value.album }}
                </template>
              </template>
              <template v-else-if="activeLyricText">{{ activeLyricText }}</template>
              <template v-else>支持随机播放、点击任意歌词跳进度、锁屏封面切歌</template>
            </div>

            <!-- Jelly Scrubber -->
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

            <!-- Upgraded Transport: Mode + Prev/Play/Next + Volume -->
            <div class="deck-transport">
              <button
                type="button"
                class="icon-circle-btn"
                :class="{ 'mode-active': store.playMode.value !== 'sequence' }"
                :title="playModeTitle"
                @click="store.cyclePlayMode"
              >
                <UiIcon :name="playModeIcon" :size="15" />
              </button>

              <button type="button" class="icon-circle-btn" title="上一曲" @click="stepTrack(-1)">
                <UiIcon name="prev" :size="15" />
              </button>
              <button type="button" class="play-main-btn" @click="togglePlay">
                <UiIcon :name="store.isPlaying.value ? 'pause' : 'play'" :size="17" />
              </button>
              <button type="button" class="icon-circle-btn" title="下一曲" @click="stepTrack(1)">
                <UiIcon name="next" :size="15" />
              </button>

              <div class="vol-wrap hide-xs" title="调节音量">
                <UiIcon name="volume" :size="14" class="vol-ico" />
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

        <!-- Full Interactive Scrolling Lyrics Drawer -->
        <Transition name="tray-expand">
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
        </Transition>
      </section>

      <!-- Blog Playlists Filter Pills -->
      <div class="playlist-strip">
        <button
          type="button"
          class="pl-chip"
          :class="{ 'is-on': selectedPlaylistId === 'all' }"
          @click="selectedPlaylistId = 'all'"
        >
          <span>全部曲库 ({{ store.audioTracks.value.length }})</span>
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

      <!-- Track List -->
      <div v-if="filteredTracks.length === 0" class="glass-box empty-box">
        没有匹配的歌曲
      </div>

      <div v-else class="track-list">
        <div
          v-for="(tr, idx) in filteredTracks"
          :key="tr.key"
          class="glass-box track-row"
          :class="{ 'is-playing': store.currentTrack.value?.key === tr.key }"
          @click="playFromFiltered(tr)"
        >
          <span class="tr-idx mono">{{ String(idx + 1).padStart(2, '0') }}</span>

          <img
            v-if="store.catalogTracks.value[tr.key]?.coverUrl"
            :src="store.catalogTracks.value[tr.key]?.coverUrl"
            class="tr-cover"
            alt=""
            loading="lazy"
          />
          <div v-else class="tr-cover tr-cover--empty">
            <UiIcon name="disc" :size="15" />
          </div>

          <div class="tr-meta">
            <div class="tr-title">
              {{ parseTrackDisplayName(tr, store.catalogTracks.value).title }}
              <span v-if="tr.lyricsKey" class="lrc-badge mono">LRC</span>
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
              title="生成歌曲分享短链 + 二维码"
              @click="shareTrack(tr)"
            >
              <UiIcon name="qr" :size="13.5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.music-stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.deck-wrapper {
  border-radius: 26px;
  padding: 20px 22px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.turntable-deck {
  display: grid;
  grid-template-columns: 168px 1fr;
  align-items: center;
  gap: 24px;
}

.platter-box {
  position: relative;
  width: 156px;
  height: 156px;
  display: grid;
  place-items: center;
}

.vinyl-platter {
  width: 148px;
  height: 148px;
  border-radius: 50%;
  background: radial-gradient(circle, #1e293b 0%, #0f172a 68%, #020617 100%);
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.28);
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
  inset: 10px;
  border-radius: 50%;
  border: 1px dashed rgba(255, 255, 255, 0.08);
}

.vinyl-label {
  width: 68px;
  height: 68px;
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
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #0f172a;
  border: 2px solid rgba(255, 255, 255, 0.7);
}

.tonearm {
  position: absolute;
  top: 6px;
  right: -4px;
  width: 34px;
  height: 92px;
  transform-origin: 22px 12px;
  transform: rotate(-18deg);
  transition: transform 0.45s cubic-bezier(0.22, 1, 0.36, 1);
  pointer-events: none;
}

.tonearm.is-engaged {
  transform: rotate(12deg);
}

.arm-pivot {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: linear-gradient(135deg, #e2e8f0, #94a3b8);
}

.arm-rod {
  position: absolute;
  top: 14px;
  right: 11px;
  width: 3px;
  height: 66px;
  background: linear-gradient(to bottom, #cbd5e1, #64748b);
  border-radius: 2px;
}

.deck-console {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
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
  font-size: 0.69rem;
  font-weight: 650;
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
  font-size: 1.12rem;
  font-weight: 700;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.deck-sub {
  font-size: 0.81rem;
  color: var(--ink-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.jelly-scrubber {
  margin-top: 4px;
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
  background: var(--accent);
  border-radius: 999px;
}

.scrub-times {
  margin-top: 4px;
  display: flex;
  justify-content: space-between;
  font-size: 0.7rem;
  color: var(--ink-3);
}

.deck-transport {
  margin-top: 2px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.mode-active {
  color: var(--accent);
  background: var(--accent-soft);
}

.play-main-btn {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  border: none;
  background: var(--accent);
  color: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
  box-shadow: 0 6px 18px rgba(0, 113, 227, 0.28);
}

.vol-wrap {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--ink-3);
}

.vol-slider {
  width: 82px;
  accent-color: var(--accent);
  cursor: pointer;
}

.lyrics-drawer {
  max-height: 184px;
  overflow-y: auto;
  padding: 10px 8px;
  border-top: 1px solid var(--border-hairline);
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.lrc-row {
  border: none;
  background: transparent;
  padding: 5px 10px;
  border-radius: 10px;
  display: flex;
  align-items: baseline;
  gap: 12px;
  text-align: left;
  color: var(--ink-3);
  font-size: 0.82rem;
  cursor: pointer;
  transition: all 0.2s ease;
}

.lrc-row:hover {
  background: var(--surface-recessed);
  color: var(--ink-1);
}

.lrc-row.is-current {
  color: var(--accent);
  font-weight: 660;
  font-size: 0.9rem;
  background: var(--accent-soft);
}

.lrc-time {
  font-size: 0.69rem;
  opacity: 0.65;
  flex-shrink: 0;
}

.playlist-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.pl-chip {
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.76rem;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}

.pl-chip.is-on {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: rgba(0, 113, 227, 0.28);
  font-weight: 630;
}

.pl-ava {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  object-fit: cover;
}

.track-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.track-row {
  padding: 9px 14px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  gap: 11px;
  cursor: pointer;
  transition: transform 0.15s ease, border-color 0.15s ease;
}

.track-row.is-playing {
  border-color: rgba(0, 113, 227, 0.36);
  background: var(--accent-soft);
}

.tr-idx {
  width: 22px;
  font-size: 0.73rem;
  color: var(--ink-3);
  text-align: center;
}

.tr-cover {
  width: 38px;
  height: 38px;
  border-radius: 9px;
  object-fit: cover;
  flex-shrink: 0;
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
  font-size: 0.86rem;
  font-weight: 630;
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

@media (max-width: 720px) {
  .turntable-deck {
    grid-template-columns: 1fr;
    justify-items: center;
    text-align: center;
  }
  .deck-console {
    width: 100%;
  }
  .deck-transport {
    justify-content: center;
  }
  .hide-xs {
    display: none;
  }
}
</style>
