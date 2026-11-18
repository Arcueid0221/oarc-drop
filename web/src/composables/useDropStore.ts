import { computed, reactive, ref } from 'vue'
import type {
  ClipItem,
  LrcLine,
  MediaItem,
  ServerStatus,
  ShareItem,
  TrackMetadata,
  UploadTask,
} from '../types'

const status = ref<ServerStatus | null>(null)
const clips = ref<ClipItem[]>([])
const shares = ref<ShareItem[]>([])
const mediaItems = ref<MediaItem[]>([])
const mediaFolders = ref<string[]>([])
const imageCount = ref(0)
const audioCount = ref(0)
const uploads = ref<UploadTask[]>([])
const toastText = ref('')
let toastTimer: number | undefined

// Persistent Liquid Glass Music Player State (survives tab switches)
const currentTrack = ref<MediaItem | null>(null)
const currentMeta = ref<TrackMetadata | null>(null)
const currentCoverUrl = ref('')
const currentLyrics = ref<LrcLine[]>([])
const isPlaying = ref(false)
const audioProgress = ref(0)
const audioDuration = ref(0)

export function showToast(msg: string) {
  toastText.value = msg
  if (toastTimer) window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => {
    toastText.value = ''
  }, 2600)
}

export function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = bytes
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

export function formatTimeShort(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function shareFullUrl(code: string): string {
  return `${window.location.origin}/s/${code}`
}

export async function copyText(text: string, label = '已复制到剪贴板') {
  try {
    await navigator.clipboard.writeText(text)
    showToast(label)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    showToast(label)
  }
}

export function parseLrc(raw: string): LrcLine[] {
  if (!raw) return []
  const lines: LrcLine[] = []
  const re = /\[(\d{1,3}):(\d{2})(?:\.(\d{1,3}))?\]/g
  for (const row of raw.split(/\r?\n/)) {
    const matches = [...row.matchAll(re)]
    if (!matches.length) continue
    const text = row.replace(re, '').trim()
    if (!text) continue
    for (const m of matches) {
      const min = Number(m[1])
      const sec = Number(m[2])
      const ms = m[3] ? Number(m[3].padEnd(3, '0')) : 0
      lines.push({ time: min * 60 + sec + ms / 1000, text })
    }
  }
  return lines.sort((a, b) => a.time - b.time)
}

// Extract dominant vibrant hues from album cover to bleed into Liquid Glass ambient canvas
export function applyAmbientFromCover(imgUrl: string) {
  if (!imgUrl) {
    resetAmbientColors()
    return
  }
  const img = new Image()
  img.crossOrigin = 'anonymous'
  img.onload = () => {
    try {
      const cv = document.createElement('canvas')
      cv.width = 24
      cv.height = 24
      const ctx = cv.getContext('2d')
      if (!ctx) return
      ctx.drawImage(img, 0, 0, 24, 24)
      const data = ctx.getImageData(0, 0, 24, 24).data
      const candidates: Array<{ r: number; g: number; b: number; score: number }> = []
      for (let i = 0; i < data.length; i += 16) {
        const r = data[i]
        const g = data[i + 1]
        const b = data[i + 2]
        const max = Math.max(r, g, b)
        const min = Math.min(r, g, b)
        const sat = max === 0 ? 0 : (max - min) / max
        const lum = (max + min) / 510
        if (lum > 0.12 && lum < 0.9 && sat > 0.18) {
          candidates.push({ r, g, b, score: sat * (1 - Math.abs(lum - 0.52)) })
        }
      }
      candidates.sort((a, b) => b.score - a.score)
      if (candidates.length >= 1) {
        const c1 = candidates[0]
        document.documentElement.style.setProperty('--cover-rgb', `${c1.r}, ${c1.g}, ${c1.b}`)
      }
    } catch {
      // keep existing ambient palette
    }
  }
  img.src = imgUrl
}

export function resetAmbientColors() {
  document.documentElement.style.removeProperty('--cover-rgb')
}

export function useDropStore() {
  async function loadInitial() {
    const [stRes, clipRes, shareRes] = await Promise.all([
      fetch('/api/status'),
      fetch('/api/clips'),
      fetch('/api/shares'),
    ])
    if (stRes.ok) status.value = await stRes.json()
    if (clipRes.ok) {
      const d = await clipRes.json()
      clips.value = d.items || []
    }
    if (shareRes.ok) {
      const d = await shareRes.json()
      shares.value = d.items || []
    }
  }

  async function loadMedia(refresh = false) {
    const res = await fetch(`/api/media${refresh ? '?refresh=1' : ''}`)
    if (!res.ok) return
    const d = await res.json()
    mediaItems.value = d.items || []
    mediaFolders.value = d.folders || []
    imageCount.value = d.imageCount || 0
    audioCount.value = d.audioCount || 0
  }

  function connectSSE(): () => void {
    const es = new EventSource('/api/events')
    es.addEventListener('clip.created', (e) => {
      const item: ClipItem = JSON.parse((e as MessageEvent).data)
      if (!clips.value.some((c) => c.id === item.id)) {
        clips.value = [item, ...clips.value]
      }
    })
    es.addEventListener('clip.updated', (e) => {
      const item: ClipItem = JSON.parse((e as MessageEvent).data)
      clips.value = clips.value.map((c) => (c.id === item.id ? item : c))
    })
    es.addEventListener('clip.deleted', (e) => {
      const { id } = JSON.parse((e as MessageEvent).data)
      clips.value = clips.value.filter((c) => c.id !== id)
    })
    es.addEventListener('share.created', (e) => {
      const item: ShareItem = JSON.parse((e as MessageEvent).data)
      if (!shares.value.some((s) => s.code === item.code)) {
        shares.value = [item, ...shares.value]
      }
    })
    es.addEventListener('share.deleted', (e) => {
      const { code } = JSON.parse((e as MessageEvent).data)
      shares.value = shares.value.filter((s) => s.code !== code)
    })
    es.addEventListener('media.refresh', () => {
      void loadMedia(true)
    })
    return () => es.close()
  }

  async function sendTextClip(text: string) {
    const res = await fetch('/api/clips/text', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    })
    if (!res.ok) throw new Error('发送失败')
    const item: ClipItem = await res.json()
    if (!clips.value.some((c) => c.id === item.id)) {
      clips.value = [item, ...clips.value]
    }
    showToast('已同步到剪贴板')
  }

  async function sendQuickPasteFile(file: File) {
    // If file > 20 MiB, automatically route through the 20 MiB chunked uploader with addToClip=true!
    if (file.size > 20 * 1024 * 1024) {
      await uploadChunkedFile(file, {
        target: 'local',
        ttlHours: 168,
        maxDownloads: 0,
        passcode: '',
        siloPrefix: '',
        addToClip: true,
      })
      return
    }
    const fd = new FormData()
    fd.append('file', file)
    const res = await fetch('/api/clips/file', { method: 'POST', body: fd })
    if (!res.ok) throw new Error('粘贴上传失败')
    const item: ClipItem = await res.json()
    if (!clips.value.some((c) => c.id === item.id)) {
      clips.value = [item, ...clips.value]
    }
    showToast(`无损已同步：${file.name}`)
  }

  async function uploadChunkedFile(
    file: File,
    opts: {
      target: 'local' | 'silo' | 'gdrive'
      siloPrefix: string
      ttlHours: number
      maxDownloads: number
      passcode: string
      addToClip: boolean
    },
  ) {
    const abortCtrl = new AbortController()
    const task = reactive<UploadTask>({
      localId: `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
      fileName: file.name,
      size: file.size,
      target: opts.target,
      uploadedBytes: 0,
      speedBps: 0,
      status: 'uploading',
      abortCtrl,
    })
    uploads.value = [task, ...uploads.value]
    const startedAt = performance.now()

    let uploadId = ''
    try {
      const initRes = await fetch('/api/uploads/init', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          fileName: file.name,
          size: file.size,
          mimeType: file.type || 'application/octet-stream',
          target: opts.target,
          siloPrefix: opts.siloPrefix,
          ttlHours: opts.ttlHours,
          maxDownloads: opts.maxDownloads,
          passcode: opts.passcode,
          addToClip: opts.addToClip,
        }),
        signal: abortCtrl.signal,
      })
      if (!initRes.ok) {
        const errBody = await initRes.json().catch(() => ({ error: '初始化上传失败' }))
        throw new Error(errBody.error || '初始化上传失败')
      }
      const initData: { uploadId: string; chunkSize: number; totalParts: number } = await initRes.json()
      uploadId = initData.uploadId
      const chunkSize = initData.chunkSize
      const totalParts = initData.totalParts

      // Upload up to 3 chunks in parallel (each <= 20 MiB to pass Cloudflare cleanly)
      const partProgress = new Array<number>(totalParts).fill(0)
      let nextPart = 1

      const updateProgress = () => {
        const sum = partProgress.reduce((a, b) => a + b, 0)
        task.uploadedBytes = sum
        const elapsedSec = Math.max(0.1, (performance.now() - startedAt) / 1000)
        task.speedBps = sum / elapsedSec
      }

      const uploadOnePart = async (partNum: number) => {
        const start = (partNum - 1) * chunkSize
        const end = Math.min(file.size, start + chunkSize)
        const blob = file.slice(start, end)
        for (let attempt = 1; attempt <= 3; attempt++) {
          try {
            const pRes = await fetch(`/api/uploads/${uploadId}/part/${partNum}`, {
              method: 'PUT',
              body: blob,
              signal: abortCtrl.signal,
            })
            if (!pRes.ok) throw new Error(`分片 ${partNum} 状态 ${pRes.status}`)
            partProgress[partNum - 1] = blob.size
            updateProgress()
            return
          } catch (e) {
            if (abortCtrl.signal.aborted || attempt === 3) throw e
            await new Promise((r) => setTimeout(r, 500 * attempt))
          }
        }
      }

      const workers = Array.from({ length: Math.min(3, totalParts) }, async () => {
        while (nextPart <= totalParts) {
          const p = nextPart++
          await uploadOnePart(p)
        }
      })
      await Promise.all(workers)

      task.status = 'assembling'
      const compRes = await fetch(`/api/uploads/${uploadId}/complete`, {
        method: 'POST',
        signal: abortCtrl.signal,
      })
      if (!compRes.ok) {
        const errBody = await compRes.json().catch(() => ({ error: '合并入库失败' }))
        throw new Error(errBody.error || '合并入库失败')
      }
      const compData: { share: ShareItem; publicUrl?: string } = await compRes.json()
      task.status = 'done'
      task.shareCode = compData.share.code
      task.publicUrl = compData.publicUrl
      if (!shares.value.some((s) => s.code === compData.share.code)) {
        shares.value = [compData.share, ...shares.value]
      }
      if (opts.target === 'silo') {
        void loadMedia(true)
      }
      await copyText(shareFullUrl(compData.share.code), '上传完成，分享链接已复制')
    } catch (e: unknown) {
      if (abortCtrl.signal.aborted) {
        task.status = 'canceled'
        if (uploadId) void fetch(`/api/uploads/${uploadId}`, { method: 'DELETE' })
      } else {
        task.status = 'error'
        task.error = e instanceof Error ? e.message : '上传失败'
      }
    }
  }

  async function playTrack(track: MediaItem) {
    currentTrack.value = track
    currentMeta.value = {
      title: track.name.replace(/\.[^.]+$/, ''),
      artist: track.folder,
      album: '',
      format: track.ext,
      hasCover: false,
    }
    currentCoverUrl.value = ''
    currentLyrics.value = []
    isPlaying.value = true

    const res = await fetch(`/api/media/track-meta?key=${encodeURIComponent(track.key)}`)
    if (res.ok && currentTrack.value?.key === track.key) {
      const d: { meta: TrackMetadata; coverUrl: string; lyrics: string } = await res.json()
      currentMeta.value = d.meta
      currentCoverUrl.value = d.coverUrl || ''
      currentLyrics.value = parseLrc(d.lyrics || '')
      applyAmbientFromCover(d.coverUrl || '')
    }
  }

  const audioTracks = computed(() => mediaItems.value.filter((m) => m.kind === 'audio'))
  const imageItems = computed(() => mediaItems.value.filter((m) => m.kind === 'image'))

  return {
    status,
    clips,
    shares,
    mediaItems,
    mediaFolders,
    imageCount,
    audioCount,
    audioTracks,
    imageItems,
    uploads,
    toastText,
    currentTrack,
    currentMeta,
    currentCoverUrl,
    currentLyrics,
    isPlaying,
    audioProgress,
    audioDuration,
    loadInitial,
    loadMedia,
    connectSSE,
    sendTextClip,
    sendQuickPasteFile,
    uploadChunkedFile,
    playTrack,
  }
}
