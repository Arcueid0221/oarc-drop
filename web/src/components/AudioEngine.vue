<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useDropStore } from '../composables/useDropStore'

const store = useDropStore()
const audioEl = ref<HTMLAudioElement | null>(null)

function getActiveList() {
  const all = store.audioTracks.value
  if (!store.activeQueueKeys.value.length) return all
  const byKey = new Map(all.map((t) => [t.key, t]))
  const ordered = store.activeQueueKeys.value
    .map((k) => byKey.get(k))
    .filter((t): t is (typeof all)[number] => Boolean(t))
  return ordered.length > 0 ? ordered : all
}

function stepTrack(delta: number, fromEnded = false) {
  const list = getActiveList()
  if (!list.length || !store.currentTrack.value) return
  if (fromEnded && store.playMode.value === 'one') {
    if (audioEl.value) {
      audioEl.value.currentTime = 0
      void audioEl.value.play()
    }
    return
  }
  const idx = list.findIndex((t) => t.key === store.currentTrack.value?.key)
  if (store.playMode.value === 'shuffle' && list.length > 1) {
    let randIdx = Math.floor(Math.random() * list.length)
    if (randIdx === idx) randIdx = (idx + 1) % list.length
    void store.playTrack(list[randIdx])
    return
  }
  const baseIdx = idx >= 0 ? idx : 0
  const next = list[(baseIdx + delta + list.length) % list.length]
  if (next) void store.playTrack(next)
}

function syncPositionState() {
  if (!('mediaSession' in navigator) || typeof navigator.mediaSession.setPositionState !== 'function') {
    return
  }
  const el = audioEl.value
  if (!el || !Number.isFinite(el.duration) || el.duration <= 0) return
  try {
    navigator.mediaSession.setPositionState({
      duration: el.duration,
      playbackRate: el.playbackRate || 1,
      position: Math.min(el.duration, Math.max(0, el.currentTime || 0)),
    })
  } catch {
    // Some browsers reject position updates during transient loading states
  }
}

function syncMediaSession() {
  if (!('mediaSession' in navigator) || !store.currentTrack.value) return
  const meta = store.currentMeta.value
  const artwork = store.currentCoverUrl.value
    ? [
        { src: store.currentCoverUrl.value, sizes: '256x256' },
        { src: store.currentCoverUrl.value, sizes: '512x512' },
      ]
    : []
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta?.title || store.currentTrack.value.name,
    artist: meta?.artist || 'O.Arc Music',
    album: meta?.album || 'Aurora Silo',
    artwork,
  })
  syncPositionState()
}

function onToggleEvent() {
  const el = audioEl.value
  if (!el) return
  if (el.paused) void el.play()
  else el.pause()
}

function onPauseEvent() {
  audioEl.value?.pause()
}

function onStepEvent(e: Event) {
  const delta = (e as CustomEvent<number>).detail || 1
  stepTrack(delta, false)
}

function onSeekEvent(e: Event) {
  const sec = (e as CustomEvent<number>).detail
  if (audioEl.value && Number.isFinite(sec)) {
    audioEl.value.currentTime = sec
    store.audioProgress.value = sec
    syncPositionState()
  }
}

function onVolumeEvent(e: Event) {
  const v = (e as CustomEvent<number>).detail
  if (audioEl.value && Number.isFinite(v)) {
    audioEl.value.volume = Math.min(1, Math.max(0, v))
  }
}

let lastPosSyncMs = 0

function onTimeUpdate() {
  const cur = audioEl.value?.currentTime || 0
  store.audioProgress.value = cur
  const now = performance.now()
  if (now - lastPosSyncMs > 4000) {
    lastPosSyncMs = now
    syncPositionState()
  }
}

function onAudioPlay() {
  store.isPlaying.value = true
  if ('mediaSession' in navigator) {
    navigator.mediaSession.playbackState = 'playing'
  }
  syncPositionState()
}

function onAudioPause() {
  store.isPlaying.value = false
  if ('mediaSession' in navigator) {
    navigator.mediaSession.playbackState = 'paused'
  }
  syncPositionState()
}

function onLoadedMetadata() {
  store.audioDuration.value = audioEl.value?.duration || 0
  syncPositionState()
}

watch(
  () => store.currentTrack.value?.streamUrl,
  (url) => {
    const el = audioEl.value
    if (!el) return
    if (!url) {
      el.pause()
      el.removeAttribute('src')
      return
    }
    el.volume = store.audioVolume.value
    el.src = url
    void el.play().catch(() => {
      store.isPlaying.value = false
    })
    syncMediaSession()
  },
)

watch([() => store.currentMeta.value, () => store.currentCoverUrl.value], () => {
  syncMediaSession()
})

onMounted(() => {
  if (audioEl.value) {
    audioEl.value.volume = store.audioVolume.value
  }
  window.addEventListener('oarc-audio-toggle', onToggleEvent)
  window.addEventListener('oarc-audio-pause', onPauseEvent)
  window.addEventListener('oarc-audio-step', onStepEvent)
  window.addEventListener('oarc-audio-seek', onSeekEvent)
  window.addEventListener('oarc-audio-volume', onVolumeEvent)

  if ('mediaSession' in navigator) {
    const safeHandle = (action: MediaSessionAction, fn: MediaSessionActionHandler) => {
      try {
        navigator.mediaSession.setActionHandler(action, fn)
      } catch {
        // unsupported action on this platform
      }
    }
    safeHandle('play', () => void audioEl.value?.play())
    safeHandle('pause', () => audioEl.value?.pause())
    safeHandle('previoustrack', () => stepTrack(-1, false))
    safeHandle('nexttrack', () => stepTrack(1, false))
    safeHandle('seekto', (details) => {
      if (audioEl.value && typeof details.seekTime === 'number' && Number.isFinite(details.seekTime)) {
        audioEl.value.currentTime = details.seekTime
        store.audioProgress.value = details.seekTime
        syncPositionState()
      }
    })
    safeHandle('seekbackward', (details) => {
      if (!audioEl.value) return
      const offset = details.seekOffset || 10
      audioEl.value.currentTime = Math.max(0, audioEl.value.currentTime - offset)
      store.audioProgress.value = audioEl.value.currentTime
      syncPositionState()
    })
    safeHandle('seekforward', (details) => {
      if (!audioEl.value) return
      const offset = details.seekOffset || 10
      const dur = audioEl.value.duration || Infinity
      audioEl.value.currentTime = Math.min(dur, audioEl.value.currentTime + offset)
      store.audioProgress.value = audioEl.value.currentTime
      syncPositionState()
    })
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('oarc-audio-toggle', onToggleEvent)
  window.removeEventListener('oarc-audio-pause', onPauseEvent)
  window.removeEventListener('oarc-audio-step', onStepEvent)
  window.removeEventListener('oarc-audio-seek', onSeekEvent)
  window.removeEventListener('oarc-audio-volume', onVolumeEvent)
})
</script>

<template>
  <audio
    ref="audioEl"
    preload="metadata"
    @play="onAudioPlay"
    @pause="onAudioPause"
    @timeupdate="onTimeUpdate"
    @loadedmetadata="onLoadedMetadata"
    @ended="stepTrack(1, true)"
  />
</template>
