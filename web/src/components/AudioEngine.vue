<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useDropStore } from '../composables/useDropStore'

const store = useDropStore()
const audioEl = ref<HTMLAudioElement | null>(null)

function stepTrack(delta: number) {
  const list = store.audioTracks.value
  if (!list.length || !store.currentTrack.value) return
  const idx = list.findIndex((t) => t.key === store.currentTrack.value?.key)
  const next = list[(idx + delta + list.length) % list.length]
  if (next) void store.playTrack(next)
}

function onToggleEvent() {
  const el = audioEl.value
  if (!el) return
  if (el.paused) void el.play()
  else el.pause()
}

function onStepEvent(e: Event) {
  const delta = (e as CustomEvent<number>).detail || 1
  stepTrack(delta)
}

function onSeekEvent(e: Event) {
  const sec = (e as CustomEvent<number>).detail
  if (audioEl.value && Number.isFinite(sec)) {
    audioEl.value.currentTime = sec
    store.audioProgress.value = sec
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
    el.src = url
    void el.play().catch(() => {
      store.isPlaying.value = false
    })
  },
)

onMounted(() => {
  window.addEventListener('oarc-audio-toggle', onToggleEvent)
  window.addEventListener('oarc-audio-step', onStepEvent)
  window.addEventListener('oarc-audio-seek', onSeekEvent)
})

onBeforeUnmount(() => {
  window.removeEventListener('oarc-audio-toggle', onToggleEvent)
  window.removeEventListener('oarc-audio-step', onStepEvent)
  window.removeEventListener('oarc-audio-seek', onSeekEvent)
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
    @ended="stepTrack(1)"
  />
</template>
