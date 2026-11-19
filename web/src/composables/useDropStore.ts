import { computed, reactive, ref } from 'vue'
import type {
  CatalogPlaylist,
  CatalogTrackInfo,
  ClipItem,
  GDriveFile,
  LrcLine,
  MediaItem,
  PlayMode,
  QrModalPayload,
  ServerStatus,
  ShareItem,
  TrackMetadata,
  UploadTask,
  VaultObject,
} from '../types'

const status = ref<ServerStatus | null>(null)
const onlineClients = ref<number>(1)
const clips = ref<ClipItem[]>([])
const shares = ref<ShareItem[]>([])
const mediaItems = ref<MediaItem[]>([])
const mediaFolders = ref<string[]>([])
const imageCount = ref(0)
const audioCount = ref(0)

// Full Vault State — global unified viewMode persisted across all storage pools!
const vaultViewMode = ref<'grid' | 'list'>(
  (localStorage.getItem('oarc_vault_view') as 'grid' | 'list') || 'grid',
)
const siloObjects = ref<VaultObject[]>([])
const siloFolders = ref<string[]>([])
const gdriveFiles = ref<GDriveFile[]>([])
const gdriveFolders = ref<string[]>(['/'])

// Blog Music Catalog State
const playlists = ref<CatalogPlaylist[]>([])
const catalogTracks = ref<Record<string, CatalogTrackInfo>>({})

const uploads = ref<UploadTask[]>([])
const toastText = ref('')
let toastTimer: number | undefined

const qrModal = ref<QrModalPayload | null>(null)

// Persistent Music Player State (survives tab switches)
const currentTrack = ref<MediaItem | null>(null)
const currentMeta = ref<TrackMetadata | null>(null)
const currentCoverUrl = ref('')
const currentLyrics = ref<LrcLine[]>([])
const isPlaying = ref(false)
const audioProgress = ref(0)
const audioDuration = ref(0)
const playMode = ref<PlayMode>(
  (localStorage.getItem('oarc_play_mode') as PlayMode) || 'sequence',
)
const audioVolume = ref<number>(
  Number(localStorage.getItem('oarc_volume') ?? '0.9') || 0.9,
)
const activeQueueKeys = ref<string[]>([])

const SILO_FOLDER_LABELS: Record<string, string> = {
  images: '图床图片',
  videos: '图床视频',
  drop: '快传归档',
  articles: '文章配图',
  photos: '相册原图',
  talks: '说说配图',
  config: '站点配置',
  avatar: '站长头像',
  character: '3D 看板娘',
  'character/models': '3D 看板娘模型',
  'music/audio': '歌曲音频',
  'music/covers/playlists': '歌单封面',
  'music/covers/tracks': '单曲封面',
  'music/lyrics': '滚动歌词',
  '/': '桶根目录',
}

export function describeSiloFolder(folder: string): string {
  const clean = (folder || '/').replace(/^\/+|\/+$/g, '') || '/'
  if (SILO_FOLDER_LABELS[clean]) {
    return clean === '/' ? '桶根目录 (/)' : `${SILO_FOLDER_LABELS[clean]} (${clean})`
  }
  if (clean.startsWith('articles/')) return `文章配图 (${clean})`
  if (clean.startsWith('photos/')) return `相册资源 (${clean})`
  if (clean.startsWith('talks/')) return `说说配图 (${clean})`
  if (clean.startsWith('config/')) return `站点配置 (${clean})`
  if (clean.startsWith('character/')) return `看板娘资源 (${clean})`
  if (clean.startsWith('music/')) return `音乐资源 (${clean})`
  if (clean.startsWith('images/')) return `图床图片 (${clean})`
  if (clean.startsWith('drop/')) return `快传归档 (${clean})`
  return clean
}

export function sortClips(list: ClipItem[]): ClipItem[] {
  return list.slice().sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
    return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
  })
}

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

export function formatMarkdownImage(name: string, url: string): string {
  const alt = (name || 'image').replace(/\.[^.]+$/, '')
  return `![${alt}](${url})`
}

export function formatHtmlImage(name: string, url: string): string {
  const alt = (name || 'image').replace(/"/g, '&quot;')
  return `<img src="${url}" alt="${alt}" loading="lazy" />`
}

export function fallbackCopyText(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.cssText =
    'position:fixed;top:0;left:0;width:1px;height:1px;padding:0;border:0;opacity:0;pointer-events:none;font-size:16px;'
  document.body.appendChild(ta)
  ta.focus({ preventScroll: true })
  ta.select()
  ta.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(ta)
  return ok
}

export async function copyText(text: string, label = '已复制到剪贴板') {
  try {
    await navigator.clipboard.writeText(text)
    showToast(label)
  } catch {
    if (fallbackCopyText(text)) {
      showToast(label)
    } else {
      showToast('复制受浏览器限制，请长按手动复制')
    }
  }
}

export function canNativeShare(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.share === 'function'
}

export async function nativeShareLink(payload: { title?: string; text?: string; url: string }) {
  if (!canNativeShare()) {
    await copyText(payload.url, '已复制分享链接')
    return
  }
  try {
    await navigator.share({
      title: payload.title || 'O.Arc DROP',
      text: payload.text,
      url: payload.url,
    })
  } catch (e: unknown) {
    if (e instanceof Error && e.name === 'AbortError') return
    await copyText(payload.url, '已复制分享链接')
  }
}

async function fetchImageAsPngBlob(srcUrl: string): Promise<Blob> {
  const res = await fetch(srcUrl)
  if (!res.ok) throw new Error('拉取原图失败')
  const blob = await res.blob()
  if (blob.type === 'image/png') return blob
  const bmpUrl = URL.createObjectURL(blob)
  try {
    return await new Promise<Blob>((resolve, reject) => {
      const img = new Image()
      img.onload = () => {
        const cv = document.createElement('canvas')
        cv.width = img.naturalWidth
        cv.height = img.naturalHeight
        const ctx = cv.getContext('2d')
        if (!ctx) {
          reject(new Error('canvas unavailable'))
          return
        }
        ctx.drawImage(img, 0, 0)
        cv.toBlob((b) => (b ? resolve(b) : reject(new Error('png encode failed'))), 'image/png')
      }
      img.onerror = () => reject(new Error('image decode failed'))
      img.src = bmpUrl
    })
  } finally {
    URL.revokeObjectURL(bmpUrl)
  }
}

// Writes actual image pixels into OS clipboard.
// Uses Promise-backed ClipboardItem synchronously inside the tap gesture so iOS Safari / WebKit permits clipboard write!
export async function copyImagePixels(srcUrl: string) {
  if (!navigator.clipboard || typeof ClipboardItem === 'undefined') {
    showToast('当前浏览器不支持复制图片，请长按图片保存或点下载')
    return
  }
  const pngPromise = fetchImageAsPngBlob(srcUrl)
  try {
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': pngPromise })])
    showToast('原图已写入系统剪贴板，可直接粘贴')
    return
  } catch {
    // Fallback for older Android WebViews that do not accept Promise values in ClipboardItem
    try {
      const pngBlob = await pngPromise
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': pngBlob })])
      showToast('原图已写入系统剪贴板，可直接粘贴')
    } catch {
      showToast('复制图片受浏览器权限限制，请长按图片或点下载')
    }
  }
}

export function parseTrackDisplayName(track: MediaItem, catalog?: Record<string, CatalogTrackInfo>) {
  const fallbackCover = track.thumbUrl || ''
  const c = catalog?.[track.key]
  if (c && c.title) {
    return {
      title: c.title,
      artist: c.artist || '',
      album: c.album || '',
      coverUrl: fallbackCover || c.coverUrl || '',
    }
  }
  const stem = track.name.replace(/\.[^.]+$/, '')
  const sepIdx = stem.indexOf(' - ')
  if (sepIdx > 0) {
    return {
      artist: stem.slice(0, sepIdx).trim(),
      title: stem.slice(sepIdx + 3).trim(),
      album: '',
      coverUrl: fallbackCover,
    }
  }
  return { title: stem, artist: track.folder, album: '', coverUrl: fallbackCover }
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
      // External cover without CORS keeps default ambient tint cleanly
    }
  }
  img.src = imgUrl
}

export function resetAmbientColors() {
  document.documentElement.style.removeProperty('--cover-rgb')
}

export function useDropStore() {
  function setVaultViewMode(mode: 'grid' | 'list') {
    vaultViewMode.value = mode
    localStorage.setItem('oarc_vault_view', mode)
  }

  function openQrModal(title: string, url: string, subtitle?: string, shareCode?: string) {
    qrModal.value = { title, url, subtitle, shareCode }
  }

  function closeQrModal() {
    qrModal.value = null
  }

  function dismissUpload(localId: string) {
    uploads.value = uploads.value.filter((u) => u.localId !== localId)
  }

  function pauseAudio() {
    window.dispatchEvent(new CustomEvent('oarc-audio-pause'))
  }

  function setPlayMode(mode: PlayMode) {
    playMode.value = mode
    localStorage.setItem('oarc_play_mode', mode)
  }

  function cyclePlayMode() {
    const order: PlayMode[] = ['sequence', 'shuffle', 'one']
    const next = order[(order.indexOf(playMode.value) + 1) % order.length]
    setPlayMode(next)
    const labels: Record<PlayMode, string> = {
      sequence: '列表循环',
      shuffle: '随机播放',
      one: '单曲循环',
    }
    showToast(`已切换为：${labels[next]}`)
  }

  function setVolume(val: number) {
    const clamped = Math.min(1, Math.max(0, val))
    audioVolume.value = clamped
    localStorage.setItem('oarc_volume', String(clamped))
    window.dispatchEvent(new CustomEvent('oarc-audio-volume', { detail: clamped }))
  }

  async function loadInitial() {
    try {
      const [stRes, clipRes, shareRes] = await Promise.all([
        fetch('/api/status'),
        fetch('/api/clips'),
        fetch('/api/shares'),
      ])
      if (stRes.ok) {
        const stData: ServerStatus = await stRes.json()
        status.value = stData
        if (typeof stData.onlineClients === 'number' && stData.onlineClients > 0) {
          onlineClients.value = stData.onlineClients
        }
      }
      if (clipRes.ok) {
        const d = await clipRes.json()
        clips.value = sortClips(d.items || [])
      }
      if (shareRes.ok) {
        const d = await shareRes.json()
        shares.value = d.items || []
      }
    } catch {
      // ignore transient offline errors on background resync
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

  async function loadSiloVault(includeThumbs = false) {
    const res = await fetch(`/api/silo/objects?includeThumbs=${includeThumbs ? '1' : '0'}`)
    if (!res.ok) return
    const d = await res.json()
    siloObjects.value = d.items || []
    siloFolders.value = d.folders || []
  }

  async function loadGDriveFiles() {
    const res = await fetch('/api/gdrive/files')
    if (!res.ok) return
    const d = await res.json()
    gdriveFiles.value = d.items || []
  }

  async function loadGDriveFolders() {
    const res = await fetch('/api/gdrive/folders')
    if (!res.ok) return
    const d = await res.json()
    if (Array.isArray(d.folders) && d.folders.length > 0) {
      gdriveFolders.value = d.folders
    }
  }

  async function createGDriveFolder(folderName: string): Promise<string | null> {
    const clean = folderName.trim().replace(/^\/+|\/+$/g, '')
    if (!clean) return null
    const res = await fetch('/api/gdrive/folders', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ folder: clean }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: '创建文件夹失败' }))
      showToast(err.error || '创建文件夹失败')
      return null
    }
    const d: { folder: string } = await res.json()
    if (!gdriveFolders.value.includes(d.folder)) {
      gdriveFolders.value = ['/', ...gdriveFolders.value.filter((f) => f !== '/'), d.folder].sort(
        (a, b) => (a === '/' ? -1 : b === '/' ? 1 : a.localeCompare(b)),
      )
    }
    showToast(`已创建 Drive 文件夹：${d.folder}`)
    return d.folder
  }

  async function createSiloFolder(folderName: string): Promise<string | null> {
    const clean = folderName.trim().replace(/^\/+|\/+$/g, '')
    if (!clean) return null
    const res = await fetch('/api/silo/folders', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ folder: clean }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: '创建 Silo 文件夹失败' }))
      showToast(err.error || '创建 Silo 文件夹失败')
      return null
    }
    const d: { folder: string } = await res.json()
    if (!siloFolders.value.includes(d.folder)) {
      siloFolders.value = [...siloFolders.value, d.folder].sort((a, b) => a.localeCompare(b))
    }
    showToast(`已创建 Silo 文件夹：${d.folder}`)
    return d.folder
  }

  async function deleteSiloFolder(
    folder: string,
    force = false,
  ): Promise<{
    ok: boolean
    nonEmpty?: boolean
    fileCount?: number
    totalBytes?: number
    deletedFiles?: number
    error?: string
  }> {
    const res = await fetch(
      `/api/silo/folders?folder=${encodeURIComponent(folder)}&force=${force ? '1' : '0'}`,
      { method: 'DELETE' },
    )
    const d = await res.json().catch(() => ({}))
    if (res.status === 409 && d.nonEmpty) {
      return {
        ok: false,
        nonEmpty: true,
        fileCount: d.fileCount || 0,
        totalBytes: d.totalBytes || 0,
        error: d.error,
      }
    }
    if (!res.ok) {
      const msg = d.error || '删除文件夹失败'
      showToast(msg)
      return { ok: false, error: msg }
    }
    siloFolders.value = siloFolders.value.filter(
      (f) => f !== folder && !f.startsWith(`${folder}/`),
    )
    siloObjects.value = siloObjects.value.filter(
      (it) => it.folder !== folder && !it.folder.startsWith(`${folder}/`),
    )
    showToast(`已删除 Silo 文件夹：${folder}`)
    void loadMedia(true)
    return { ok: true, deletedFiles: d.deletedFiles || 0 }
  }

  async function deleteGDriveFolder(
    folder: string,
    force = false,
  ): Promise<{
    ok: boolean
    nonEmpty?: boolean
    fileCount?: number
    error?: string
  }> {
    const res = await fetch(
      `/api/gdrive/folders?folder=${encodeURIComponent(folder)}&force=${force ? '1' : '0'}`,
      { method: 'DELETE' },
    )
    const d = await res.json().catch(() => ({}))
    if (res.status === 409 && d.nonEmpty) {
      return {
        ok: false,
        nonEmpty: true,
        fileCount: d.fileCount || 0,
        error: d.error,
      }
    }
    if (!res.ok) {
      const msg = d.error || '删除 Drive 文件夹失败'
      showToast(msg)
      return { ok: false, error: msg }
    }
    gdriveFolders.value = gdriveFolders.value.filter(
      (f) => f !== folder && !f.startsWith(`${folder}/`),
    )
    gdriveFiles.value = gdriveFiles.value.filter(
      (it) => it.folder !== folder && !it.folder.startsWith(`${folder}/`),
    )
    showToast(`已删除 Drive 文件夹：${folder}`)
    return { ok: true }
  }

  async function transferVaultItem(opts: {
    srcStorage: 'silo' | 'gdrive' | 'local'
    srcKey: string
    dstStorage: 'silo' | 'gdrive' | 'local'
    dstFolder?: string
    move?: boolean
    force?: boolean
    silent?: boolean
  }): Promise<{ ok: boolean; dstKey?: string; publicUrl?: string; noop?: boolean; error?: string }> {
    const res = await fetch('/api/vault/copy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        srcStorage: opts.srcStorage,
        srcKey: opts.srcKey,
        dstStorage: opts.dstStorage,
        dstFolder: opts.dstFolder || '',
        move: Boolean(opts.move),
        force: Boolean(opts.force),
      }),
    })
    const d = await res.json().catch(() => ({}))
    if (!res.ok) {
      const msg = d.error || (opts.move ? '移动失败' : '复制失败')
      if (!opts.silent) showToast(msg)
      return { ok: false, error: msg }
    }
    if (!opts.silent) {
      if (d.noop) {
        showToast('文件已在目标目录中')
      } else {
        const poolName =
          opts.dstStorage === 'silo'
            ? 'Silo'
            : opts.dstStorage === 'gdrive'
              ? 'Google Drive'
              : '私密互传'
        showToast(`${opts.move ? '已移至' : '已复制到'} ${poolName}`)
      }
    }
    return {
      ok: true,
      dstKey: d.dstKey,
      publicUrl: d.publicUrl,
      noop: Boolean(d.noop),
    }
  }

  async function promoteClipToSilo(item: ClipItem, autoCopyFmt: 'md' | 'url' = 'md') {
    if (item.publicUrl) {
      const out =
        autoCopyFmt === 'md'
          ? formatMarkdownImage(item.fileName || 'image', item.publicUrl)
          : item.publicUrl
      await copyText(out, autoCopyFmt === 'md' ? '已复制 Markdown 外链' : '已复制图床直链')
      return item
    }
    const res = await fetch(`/api/clips/${item.id}/silo`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ prefix: 'images/' }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: '转存图床失败' }))
      showToast(err.error || '转存图床失败')
      return null
    }
    const updated: ClipItem = await res.json()
    clips.value = sortClips(clips.value.map((c) => (c.id === updated.id ? updated : c)))
    if (updated.publicUrl) {
      const isImg = updated.kind === 'image' || (updated.mimeType || '').startsWith('image/')
      const out =
        isImg && autoCopyFmt === 'md'
          ? formatMarkdownImage(updated.fileName || 'image', updated.publicUrl)
          : updated.publicUrl
      await copyText(
        out,
        isImg && autoCopyFmt === 'md' ? '已存图床并复制 Markdown 外链' : '已存图床并复制公网直链',
      )
    }
    void loadMedia(true)
    if (siloObjects.value.length > 0) void loadSiloVault(false)
    return updated
  }

  async function archiveClipToGDrive(item: ClipItem, folder: string) {
    const res = await fetch(`/api/clips/${item.id}/gdrive`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ folder }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: '存入 Google Drive 失败' }))
      showToast(err.error || '存入 Google Drive 失败')
      return null
    }
    const updated: ClipItem = await res.json()
    clips.value = sortClips(clips.value.map((c) => (c.id === updated.id ? updated : c)))
    const label = !folder || folder === '/' ? 'OArc-Drop/' : `OArc-Drop/${folder}/`
    showToast(`已存入 Drive：${label}${updated.gdrivePath ? updated.gdrivePath.split('/').pop() : ''}`)
    if (gdriveFiles.value.length > 0) void loadGDriveFiles()
    return updated
  }

  async function toggleClipPin(item: ClipItem) {
    const res = await fetch(`/api/clips/${item.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pinned: !item.pinned }),
    })
    if (!res.ok) return null
    const updated: ClipItem = await res.json()
    clips.value = sortClips(clips.value.map((c) => (c.id === updated.id ? updated : c)))
    return updated
  }

  async function renameClip(id: string, newFileName: string) {
    const clean = newFileName.trim()
    if (!clean) return null
    const res = await fetch(`/api/clips/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ fileName: clean }),
    })
    if (!res.ok) {
      showToast('修改文件名失败')
      return null
    }
    const updated: ClipItem = await res.json()
    clips.value = sortClips(clips.value.map((c) => (c.id === updated.id ? updated : c)))
    showToast(`已改名为：${updated.fileName}`)
    return updated
  }

  async function clearUnpinnedClips() {
    const res = await fetch('/api/clips/clear-unpinned', { method: 'POST' })
    if (!res.ok) {
      showToast('清理失败')
      return 0
    }
    const d: { removed: number; ids?: string[] } = await res.json()
    clips.value = clips.value.filter((c) => c.pinned)
    showToast(d.removed > 0 ? `已清理 ${d.removed} 条未置顶内容` : '暂无可清理的未置顶项')
    return d.removed
  }

  async function updateShareOptions(
    code: string,
    opts: { ttlHours?: number; maxDownloads?: number; passcode?: string },
  ) {
    const res = await fetch(`/api/shares/${code}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(opts),
    })
    if (!res.ok) {
      showToast('更新外链设置失败')
      return null
    }
    const updated: ShareItem = await res.json()
    shares.value = shares.value.map((s) => (s.code === code ? updated : s))
    showToast('外链权限已实时生效')
    return updated
  }

  async function loadMusicCatalog(refresh = false) {
    const res = await fetch(`/api/music/catalog${refresh ? '?refresh=1' : ''}`)
    if (!res.ok) return
    const d = await res.json()
    playlists.value = d.playlists || []
    catalogTracks.value = d.tracks || {}
  }

  function connectSSE(): () => void {
    const es = new EventSource('/api/events')
    let hasConnectedOnce = false

    es.addEventListener('connected', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data)
        if (typeof d.online === 'number') onlineClients.value = d.online
      } catch {
        // ignore
      }
      if (hasConnectedOnce) {
        void loadInitial()
      }
      hasConnectedOnce = true
    })

    es.addEventListener('presence', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data)
        if (typeof d.online === 'number') onlineClients.value = d.online
      } catch {
        // ignore
      }
    })

    es.addEventListener('clip.created', (e) => {
      const item: ClipItem = JSON.parse((e as MessageEvent).data)
      if (!clips.value.some((c) => c.id === item.id)) {
        clips.value = sortClips([item, ...clips.value])
      }
    })
    es.addEventListener('clip.updated', (e) => {
      const item: ClipItem = JSON.parse((e as MessageEvent).data)
      clips.value = sortClips(clips.value.map((c) => (c.id === item.id ? item : c)))
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
    es.addEventListener('share.updated', (e) => {
      const item: ShareItem = JSON.parse((e as MessageEvent).data)
      shares.value = shares.value.map((s) => (s.code === item.code ? item : s))
    })
    es.addEventListener('share.deleted', (e) => {
      const { code } = JSON.parse((e as MessageEvent).data)
      shares.value = shares.value.filter((s) => s.code !== code)
    })
    es.addEventListener('media.refresh', () => {
      void loadMedia(true)
      if (siloObjects.value.length > 0) void loadSiloVault(false)
    })

    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        void loadInitial()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)

    return () => {
      es.close()
      document.removeEventListener('visibilitychange', onVisibility)
    }
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
      clips.value = sortClips([item, ...clips.value])
    }
    showToast('文本已同步至收发台')
    return item
  }

  // Smooth real-time progress bar even for quick <=20 MiB pastes & screenshots (never abruptly pops in!)
  async function sendQuickPasteFile(
    file: File,
    opts?: {
      target?: 'local' | 'silo'
      prefix?: string
      autoCopyFormat?: 'md' | 'url' | 'none'
    },
  ) {
    const target = opts?.target || 'local'
    const prefix = opts?.prefix || (target === 'silo' ? 'images/' : '')
    const autoCopy = opts?.autoCopyFormat || (target === 'silo' ? 'md' : 'none')

    if (file.size > 20 * 1024 * 1024) {
      await uploadChunkedFile(file, {
        target,
        siloPrefix: prefix || 'drop/',
        ttlHours: 0,
        maxDownloads: 0,
        passcode: '',
        addToClip: true,
        skipShare: true,
        autoCopyFormat: autoCopy,
      })
      return
    }

    const abortCtrl = new AbortController()
    const totalSize = Math.max(1, file.size)
    const task = reactive<UploadTask>({
      localId: `paste-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
      fileName: file.name || '粘贴文件',
      size: totalSize,
      target,
      uploadedBytes: 0,
      speedBps: 0,
      status: 'uploading',
      abortCtrl,
    })
    uploads.value = [task, ...uploads.value]
    const startedAt = performance.now()

    const fd = new FormData()
    fd.append('file', file)
    fd.append('target', target)
    if (prefix) fd.append('prefix', prefix)

    try {
      const item = await new Promise<ClipItem>((resolve, reject) => {
        const xhr = new XMLHttpRequest()
        xhr.open('POST', '/api/clips/file')

        const onAbort = () => xhr.abort()
        abortCtrl.signal.addEventListener('abort', onAbort, { once: true })

        xhr.upload.onprogress = (ev) => {
          if (ev.lengthComputable) {
            task.uploadedBytes = Math.min(totalSize, ev.loaded)
            const elapsedSec = Math.max(0.08, (performance.now() - startedAt) / 1000)
            task.speedBps = ev.loaded / elapsedSec
          }
        }
        xhr.onload = () => {
          abortCtrl.signal.removeEventListener('abort', onAbort)
          if (xhr.status >= 200 && xhr.status < 300) {
            try {
              resolve(JSON.parse(xhr.responseText))
            } catch {
              reject(new Error('解析响应失败'))
            }
          } else {
            try {
              const errObj = JSON.parse(xhr.responseText)
              reject(new Error(errObj.error || `上传失败 (${xhr.status})`))
            } catch {
              reject(new Error(`上传失败 (${xhr.status})`))
            }
          }
        }
        xhr.onerror = () => {
          abortCtrl.signal.removeEventListener('abort', onAbort)
          reject(new Error('网络传输中断'))
        }
        xhr.onabort = () => {
          abortCtrl.signal.removeEventListener('abort', onAbort)
          reject(new Error('aborted'))
        }
        xhr.send(fd)
      })

      task.uploadedBytes = totalSize
      // Ensure ultra-fast local pastes still play a crisp 220ms progress fill animation before collapsing
      const elapsed = performance.now() - startedAt
      if (elapsed < 240) {
        await new Promise((r) => window.setTimeout(r, 240 - elapsed))
      }
      task.status = 'done'

      if (!clips.value.some((c) => c.id === item.id)) {
        clips.value = sortClips([item, ...clips.value])
      }
      if (target === 'silo' && item.publicUrl) {
        if (autoCopy === 'md') {
          await copyText(
            formatMarkdownImage(item.fileName || file.name, item.publicUrl),
            '已传图床并复制 Markdown 链接',
          )
        } else if (autoCopy === 'url') {
          await copyText(item.publicUrl, '已传图床并复制公网直链')
        } else {
          showToast(`已传图床：${item.fileName}`)
        }
        void loadMedia(true)
      } else {
        showToast(`已同步：${item.fileName || file.name}`)
      }
    } catch (e: unknown) {
      if (abortCtrl.signal.aborted) {
        task.status = 'canceled'
      } else {
        task.status = 'error'
        task.error = e instanceof Error ? e.message : '上传失败'
      }
    }
  }

  async function uploadChunkedFile(
    file: File,
    opts: {
      target: 'local' | 'silo' | 'gdrive'
      siloPrefix: string
      gdriveFolder?: string
      ttlHours: number
      maxDownloads: number
      passcode: string
      addToClip: boolean
      skipShare?: boolean
      autoCopyFormat?: 'md' | 'url' | 'none'
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
    const skipShare = opts.skipShare ?? true

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
          gdriveFolder: opts.gdriveFolder || '',
          ttlHours: opts.ttlHours,
          maxDownloads: opts.maxDownloads,
          passcode: opts.passcode,
          addToClip: opts.addToClip,
          skipShare,
        }),
        signal: abortCtrl.signal,
      })
      if (!initRes.ok) {
        const errBody = await initRes.json().catch(() => ({ error: '初始化上传失败' }))
        throw new Error(errBody.error || '初始化上传失败')
      }
      const initData: { uploadId: string; chunkSize: number; totalParts: number } =
        await initRes.json()
      uploadId = initData.uploadId
      const chunkSize = initData.chunkSize
      const totalParts = initData.totalParts

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
      const compData: { share?: ShareItem; publicUrl?: string; clip?: ClipItem } =
        await compRes.json()
      task.status = 'done'
      task.publicUrl = compData.publicUrl
      if (compData.clip && !clips.value.some((c) => c.id === compData.clip?.id)) {
        clips.value = sortClips([compData.clip, ...clips.value])
      }
      if (compData.share) {
        task.shareCode = compData.share.code
        if (!shares.value.some((s) => s.code === compData.share?.code)) {
          shares.value = [compData.share, ...shares.value]
        }
      }
      if (opts.target === 'silo') {
        void loadMedia(true)
        if (siloObjects.value.length > 0) void loadSiloVault(false)
      } else if (opts.target === 'gdrive') {
        if (gdriveFiles.value.length > 0) void loadGDriveFiles()
      }

      if (compData.share) {
        await copyText(shareFullUrl(compData.share.code), '上传完成，公开短链已复制')
      } else if (
        opts.target === 'silo' &&
        compData.publicUrl &&
        file.type.startsWith('image/') &&
        opts.autoCopyFormat &&
        opts.autoCopyFormat !== 'none'
      ) {
        if (opts.autoCopyFormat === 'md') {
          await copyText(
            formatMarkdownImage(compData.clip?.fileName || file.name, compData.publicUrl),
            '已传图床并复制 Markdown 链接',
          )
        } else {
          await copyText(compData.publicUrl, '已传图床并复制直链')
        }
      } else {
        showToast(`上传完成：${compData.clip?.fileName || file.name}`)
      }
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

  async function playTrack(track: MediaItem, queue?: MediaItem[]) {
    if (queue && queue.length > 0) {
      activeQueueKeys.value = queue.map((t) => t.key)
    }
    const parsed = parseTrackDisplayName(track, catalogTracks.value)
    audioProgress.value = 0
    audioDuration.value = 0
    currentTrack.value = track
    currentMeta.value = {
      title: parsed.title,
      artist: parsed.artist,
      album: parsed.album,
      format: track.ext,
      hasCover: Boolean(parsed.coverUrl),
    }
    currentCoverUrl.value = parsed.coverUrl || ''
    currentLyrics.value = []
    isPlaying.value = true

    const res = await fetch(`/api/media/track-meta?key=${encodeURIComponent(track.key)}`)
    if (res.ok && currentTrack.value?.key === track.key) {
      const d: { meta: TrackMetadata; coverUrl: string; lyrics: string } = await res.json()
      currentMeta.value = {
        ...d.meta,
        title: parsed.title || d.meta.title,
        artist: parsed.artist || d.meta.artist,
        album: parsed.album || d.meta.album,
      }
      const bestCover = d.coverUrl || parsed.coverUrl || ''
      currentCoverUrl.value = bestCover
      currentLyrics.value = parseLrc(d.lyrics || '')
      applyAmbientFromCover(bestCover)
    }
  }

  const audioTracks = computed(() => mediaItems.value.filter((m) => m.kind === 'audio'))
  const imageItems = computed(() => mediaItems.value.filter((m) => m.kind === 'image'))

  return {
    status,
    onlineClients,
    clips,
    shares,
    mediaItems,
    mediaFolders,
    imageCount,
    audioCount,
    audioTracks,
    imageItems,
    vaultViewMode,
    setVaultViewMode,
    siloObjects,
    siloFolders,
    gdriveFiles,
    gdriveFolders,
    playlists,
    catalogTracks,
    uploads,
    toastText,
    qrModal,
    currentTrack,
    currentMeta,
    currentCoverUrl,
    currentLyrics,
    isPlaying,
    audioProgress,
    audioDuration,
    playMode,
    audioVolume,
    activeQueueKeys,
    openQrModal,
    closeQrModal,
    dismissUpload,
    pauseAudio,
    setPlayMode,
    cyclePlayMode,
    setVolume,
    loadInitial,
    loadMedia,
    loadSiloVault,
    loadGDriveFiles,
    loadGDriveFolders,
    createGDriveFolder,
    createSiloFolder,
    deleteSiloFolder,
    deleteGDriveFolder,
    transferVaultItem,
    promoteClipToSilo,
    archiveClipToGDrive,
    toggleClipPin,
    renameClip,
    clearUnpinnedClips,
    updateShareOptions,
    loadMusicCatalog,
    connectSSE,
    sendTextClip,
    sendQuickPasteFile,
    uploadChunkedFile,
    playTrack,
  }
}
