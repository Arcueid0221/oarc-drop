<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useDropStore } from '../composables/useDropStore'

const store = useDropStore()
const audioEl = ref<HTMLAudioElement | null>(null)

function getActiveList() {
  const all = store.audioTracks.value
  if (!store.activeQueueKeys.value.length) return all
  const keySet = new Set(store.activeQueueKeys.value)
  const filtered = all.filter((t) => keySet.has(t.key))
  return filtered.length > 0 ? filtered : all
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

function syncMediaSession() {
  if (!('mediaSession' in navigator) || !store.currentTrack.value) return
  const meta = store.currentMeta.value
  const artwork = store.currentCoverUrl.value
    ? [{ src: store.currentCoverUrl.value, sizes: '512x512' }]
    : []
  navigator.mediaSession.metadata = new MediaMetadata({
    title: meta?.title || store.currentTrack.value.name,
    artist: meta?.artist || 'O.Arc Music',
    album: meta?.album || 'Aurora Silo',
    artwork,
  })
}

function onToggleEvent() {
  const el = audioEl.value
  if (!el) return
  if (el.paused) void el.play()
  else el.pause()
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
  }
}

function onVolumeEvent(e: Event) {
  const v = (e as CustomEvent<number>).detail
  if (audioEl.value && Number.isFinite(v)) {
    audioEl.value.volume = Math.min(1, Math.max(0, v))
  }
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
  window.addEventListener('oarc-audio-step', onStepEvent)
  window.addEventListener('oarc-audio-seek', onSeekEvent)
  window.addEventListener('oarc-audio-volume', onVolumeEvent)

  if ('mediaSession' in navigator) {
    navigator.mediaSession.setActionHandler('play', () => void audioEl.value?.play())
    navigator.mediaSession.setActionHandler('pause', () => audioEl.value?.pause())
    navigator.mediaSession.setActionHandler('previoustrack', () => stepTrack(-1, false))
    navigator.mediaSession.setActionHandler('nexttrack', () => stepTrack(1, false))
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('oarc-audio-toggle', onToggleEvent)
  window.removeEventListener('oarc-audio-step', onStepEvent)
  window.removeEventListener('oarc-audio-seek', onSeekEvent)
  window.removeEventListener('oarc-audio-volume', onVolumeEvent)
})
</script>

<template>
  <audio
    ref="audioEl"
    preload="metadata"
    @play="store.isPlaying.value = true"
    @pause="store.isPlaying.value = false"
    @timeupdate="store.audioProgress.value = audioEl?.currentTime || 0"
    @loadedmetadata="store.audioDuration.value = audioEl?.duration || 0"
    @ended="stepTrack(1, true)"
  />
</template>
