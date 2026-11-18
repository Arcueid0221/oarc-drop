<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  copyImagePixels,
  copyText,
  describeSiloFolder,
  formatBytes,
  formatHtmlImage,
  formatMarkdownImage,
  formatTimeShort,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import type { ClipItem, GDriveFile, MediaItem, VaultObject } from '../types'
import CopyPill from './CopyPill.vue'
import DriveFolderSelect from './DriveFolderSelect.vue'
import MorphDeleteBtn from './MorphDeleteBtn.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

type PoolKey = 'silo' | 'gdrive' | 'local'
type SiloCategory = 'all' | 'imghost' | 'blog' | 'music' | 'other'

const activePool = ref<PoolKey>('silo')
const loading = ref(false)
const search = ref('')
const uploadInput = ref<HTMLInputElement | null>(null)

// Silo semantic two-level filter state
const siloCategory = ref<SiloCategory>('all')
const siloSubTag = ref<string>('all')

// Google Drive subfolder filter state
const driveFolderFilter = ref<string>('all')

// Local private pool kind filter state
const localKindFilter = ref<'all' | 'image' | 'file'>('all')

// Async busy states (for spinner indicators!)
const deletingMap = ref<Record<string, boolean>>({})
const renamingBusy = ref(false)
const sharingBusy = ref<Record<string, boolean>>({})
const promotingLocalSilo = ref<Record<string, boolean>>({})
const archivingLocalDrive = ref<Record<string, boolean>>({})

// Lightbox & Rename dialog
const previewObj = ref<{
  name: string
  streamUrl: string
  downloadUrl: string
  publicUrl?: string
  size: number
  kind: string
  spec: string
  siloAudioKey?: string
} | null>(null)

const renameTarget = ref<VaultObject | null>(null)
const renameDraft = ref('')

const pools = computed(() => [
  { key: 'silo' as const, label: 'Silo 存储桶', enabled: Boolean(store.status.value?.siloEnabled) },
  { key: 'gdrive' as const, label: 'Google Drive', enabled: Boolean(store.status.value?.gdriveEnabled) },
  { key: 'local' as const, label: '私密互传', enabled: true },
])

const activePoolIdx = computed(() =>
  Math.max(0, pools.value.findIndex((p) => p.key === activePool.value)),
)

const activeVaultUploads = computed(() =>
  store.uploads.value.filter(
    (u) => u.status === 'uploading' || u.status === 'assembling',
  ),
)

// Semantic classification helpers for Silo folders
function classifySiloCategory(obj: VaultObject): SiloCategory {
  const f = (obj.folder || '/').replace(/^\/+|\/+$/g, '')
  if (f === 'music' || f.startsWith('music/')) return 'music'
  if (
    f === 'articles' ||
    f.startsWith('articles/') ||
    f === 'photos' ||
    f.startsWith('photos/') ||
    f === 'talks' ||
    f.startsWith('talks/') ||
    f === 'config' ||
    f.startsWith('config/') ||
    f === 'avatar' ||
    f.startsWith('avatar/') ||
    f === 'character' ||
    f.startsWith('character/')
  ) {
    return 'blog'
  }
  if (
    f === 'images' ||
    f.startsWith('images/') ||
    f === 'videos' ||
    f.startsWith('videos/') ||
    f === 'drop' ||
    f.startsWith('drop/') ||
    obj.kind === 'video'
  ) {
    return 'imghost'
  }
  return 'other'
}

function selectSiloCategory(cat: SiloCategory) {
  if (siloCategory.value === cat) {
    if (cat !== 'all') {
      siloCategory.value = 'all'
      siloSubTag.value = 'all'
    }
    return
  }
  siloCategory.value = cat
  siloSubTag.value = 'all'
}

interface SubTagOption {
  key: string
  label: string
  pathHint?: string
  desc: string
  count: number
}

const siloOverviewCount = computed(
  () =>
    store.siloObjects.value.filter(
      (it) => it.folder !== 'music/covers/tracks' && it.folder !== 'music/lyrics',
    ).length,
)

const siloCategoryCounts = computed(() => {
  let imghost = 0
  let blog = 0
  let music = 0
  let other = 0
  for (const it of store.siloObjects.value) {
    const c = classifySiloCategory(it)
    if (c === 'imghost') imghost++
    else if (c === 'blog') blog++
    else if (c === 'music') {
      // Count primary tracks + playlist covers cleanly; subtags expose covers/lyrics explicitly
      if (it.folder !== 'music/covers/tracks' && it.folder !== 'music/lyrics') music++
    } else other++
  }
  return { imghost, blog, music, other }
})

const activeCategoryDesc = computed(() => {
  switch (siloCategory.value) {
    case 'imghost':
      return '图床与快传区（公网只读 CDN 加速 · 可直接改名或删除）'
    case 'blog':
      return 'Aurora 博客生产目录（关联线上文章、相册、说说与主题配置 · 默认加锁防误删）'
    case 'music':
      return '博客自托管曲库（音频、歌单封面、单曲封面与 .lrc 滚动歌词）'
    case 'other':
      return '其他自定义存储目录'
    default:
      return 'Silo 对象存储全览（点击上方分类可展开具体文件夹用途说明）'
  }
})

const currentSiloSubTags = computed<SubTagOption[]>(() => {
  const all = store.siloObjects.value
  if (siloCategory.value === 'imghost') {
    const inCat = all.filter((x) => classifySiloCategory(x) === 'imghost')
    const imgCnt = all.filter(
      (x) => (x.folder === 'images' || x.folder.startsWith('images/')) && x.kind === 'image',
    ).length
    const vidCnt = all.filter(
      (x) => x.kind === 'video' || x.folder === 'videos' || x.folder.startsWith('videos/'),
    ).length
    const dropCnt = all.filter((x) => x.folder === 'drop' || x.folder.startsWith('drop/')).length
    const otherImgHost = inCat.filter(
      (x) =>
        !(
          ((x.folder === 'images' || x.folder.startsWith('images/')) && x.kind === 'image') ||
          x.kind === 'video' ||
          x.folder === 'videos' ||
          x.folder.startsWith('videos/') ||
          x.folder === 'drop' ||
          x.folder.startsWith('drop/')
        ),
    ).length
    const list: SubTagOption[] = [
      { key: 'all', label: '全部图床相关', desc: '所有图床图片、视频与快传文件', count: inCat.length },
      { key: 'tag:image', label: '图片', pathHint: 'images', desc: 'Markdown / 外链图床图片', count: imgCnt },
      { key: 'tag:video', label: '视频', pathHint: 'video', desc: '视频流媒体文件', count: vidCnt },
      { key: 'dir:drop', label: '快传归档', pathHint: 'drop', desc: '收发台归档文件', count: dropCnt },
    ]
    if (otherImgHost > 0) {
      list.push({ key: 'tag:other-imghost', label: '其他附件', pathHint: 'images/*', desc: '图床目录内其他格式文件', count: otherImgHost })
    }
    return list
  }

  if (siloCategory.value === 'blog') {
    const inCat = all.filter((x) => classifySiloCategory(x) === 'blog')
    const countPrefix = (p: string) =>
      inCat.filter((x) => x.folder === p || x.folder.startsWith(`${p}/`)).length
    return [
      { key: 'all', label: '全部博客相关', desc: '博客全站静态资源', count: inCat.length },
      { key: 'dir:articles', label: '文章封面与插图', pathHint: 'articles', desc: '博文内嵌配图与文章头图', count: countPrefix('articles') },
      { key: 'dir:photos', label: '相册原图', pathHint: 'photos', desc: '博客摄影相册高清大图', count: countPrefix('photos') },
      { key: 'dir:talks', label: '说说配图', pathHint: 'talks', desc: '日常动态与说说图片', count: countPrefix('talks') },
      { key: 'dir:config', label: '站点配置图', pathHint: 'config', desc: '登录页背景与全局配置媒体', count: countPrefix('config') },
      { key: 'dir:avatar', label: '头像', pathHint: 'avatar', desc: '博主与站点头像', count: countPrefix('avatar') },
      { key: 'dir:character', label: '3D 看板娘', pathHint: 'character', desc: 'Live / GLB 角色模型资产', count: countPrefix('character') },
    ].filter((t) => t.key === 'all' || t.count > 0)
  }

  if (siloCategory.value === 'music') {
    const inCat = all.filter((x) => classifySiloCategory(x) === 'music')
    const audioCnt = inCat.filter((x) => x.folder === 'music/audio').length
    const plCoverCnt = inCat.filter((x) => x.folder === 'music/covers/playlists').length
    const trCoverCnt = inCat.filter((x) => x.folder === 'music/covers/tracks').length
    const lrcCnt = inCat.filter((x) => x.folder === 'music/lyrics').length
    return [
      { key: 'all', label: '音频与歌单封面', desc: '主曲库与歌单封面（默认折叠伴生单曲图与歌词）', count: audioCnt + plCoverCnt },
      { key: 'exact:music/audio', label: '歌曲音频', pathHint: 'music/audio', desc: 'MP3 / FLAC 无损音源', count: audioCnt },
      { key: 'exact:music/covers/playlists', label: '歌单封面', pathHint: 'playlists', desc: '博客公开歌单封面图', count: plCoverCnt },
      { key: 'exact:music/covers/tracks', label: '单曲封面', pathHint: 'tracks', desc: '每首歌配对的专辑封面图', count: trCoverCnt },
      { key: 'exact:music/lyrics', label: '滚动歌词', pathHint: 'lyrics', desc: '.lrc 时间轴逐行歌词', count: lrcCnt },
    ]
  }

  if (siloCategory.value === 'other') {
    const inCat = all.filter((x) => classifySiloCategory(x) === 'other')
    const dirs = Array.from(new Set(inCat.map((x) => x.folder))).sort()
    return [
      { key: 'all', label: '全部其他目录', desc: '未归入标准分类的目录', count: inCat.length },
      ...dirs.map((d) => ({
        key: `exact:${d}`,
        label: d,
        desc: '自定义目录',
        count: inCat.filter((x) => x.folder === d).length,
      })),
    ]
  }

  return []
})

// Switch storage pool WITHOUT ever mutating vaultViewMode!
async function switchPool(key: PoolKey, enabled: boolean) {
  if (!enabled) {
    showToast(key === 'gdrive' ? '尚未配置 Google Drive' : '尚未连接 Silo')
    return
  }
  activePool.value = key
  if (key === 'silo' && store.siloObjects.value.length === 0) {
    await refreshCurrent()
  } else if (key === 'gdrive' && store.gdriveFiles.value.length === 0) {
    await refreshCurrent()
  }
}

async function refreshCurrent() {
  loading.value = true
  try {
    if (activePool.value === 'silo') {
      await store.loadSiloVault(false)
    } else if (activePool.value === 'gdrive') {
      await Promise.all([store.loadGDriveFiles(), store.loadGDriveFolders()])
    } else {
      await store.loadInitial()
    }
  } finally {
    loading.value = false
  }
}

function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (previewObj.value) previewObj.value = null
    else if (renameTarget.value && !renamingBusy.value) renameTarget.value = null
  }
}

onMounted(() => {
  void refreshCurrent()
  window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
})

const filteredSilo = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.siloObjects.value.filter((it) => {
    if (siloCategory.value !== 'all') {
      if (classifySiloCategory(it) !== siloCategory.value) return false
      const sub = siloSubTag.value
      if (sub === 'all') {
        if (
          siloCategory.value === 'music' &&
          !q &&
          (it.folder === 'music/covers/tracks' || it.folder === 'music/lyrics')
        ) {
          return false
        }
      } else if (sub === 'tag:image') {
        if (!((it.folder === 'images' || it.folder.startsWith('images/')) && it.kind === 'image')) {
          return false
        }
      } else if (sub === 'tag:video') {
        if (!(it.kind === 'video' || it.folder === 'videos' || it.folder.startsWith('videos/'))) {
          return false
        }
      } else if (sub === 'tag:other-imghost') {
        if (
          ((it.folder === 'images' || it.folder.startsWith('images/')) && it.kind === 'image') ||
          it.kind === 'video' ||
          it.folder === 'videos' ||
          it.folder.startsWith('videos/') ||
          it.folder === 'drop' ||
          it.folder.startsWith('drop/')
        ) {
          return false
        }
      } else if (sub.startsWith('dir:')) {
        const p = sub.slice(4)
        if (!(it.folder === p || it.folder.startsWith(`${p}/`))) return false
      } else if (sub.startsWith('exact:')) {
        const p = sub.slice(6)
        if (it.folder !== p) return false
      }
    } else if (!q) {
      if (it.folder === 'music/covers/tracks' || it.folder === 'music/lyrics') return false
    }

    if (q) {
      const folderDesc = describeSiloFolder(it.folder).toLowerCase()
      if (!it.key.toLowerCase().includes(q) && !folderDesc.includes(q)) return false
    }
    return true
  })
})

const driveAvailableFolders = computed(() => {
  const set = new Set<string>(['/'])
  for (const f of store.gdriveFolders.value) set.add(f || '/')
  for (const it of store.gdriveFiles.value) set.add(it.folder || '/')
  return ['/', ...Array.from(set).filter((x) => x !== '/').sort()]
})

const filteredDrive = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.gdriveFiles.value.filter((it) => {
    if (driveFolderFilter.value !== 'all') {
      const f = it.folder || '/'
      if (f !== driveFolderFilter.value) return false
    }
    if (q && !it.path.toLowerCase().includes(q)) return false
    return true
  })
})

const localFileClips = computed(() => {
  const q = search.value.trim().toLowerCase()
  return store.clips.value.filter((c) => {
    if (c.kind === 'text') return false
    if (c.storage && c.storage !== 'local') return false
    if (localKindFilter.value !== 'all' && c.kind !== localKindFilter.value) return false
    if (q && !(c.fileName || '').toLowerCase().includes(q)) return false
    return true
  })
})

const activeCount = computed(() => {
  if (activePool.value === 'silo') return filteredSilo.value.length
  if (activePool.value === 'gdrive') return filteredDrive.value.length
  return localFileClips.value.length
})

const activeTotalBytes = computed(() => {
  if (activePool.value === 'silo') {
    return filteredSilo.value.reduce((acc, x) => acc + (x.size || 0), 0)
  }
  if (activePool.value === 'gdrive') {
    return filteredDrive.value.reduce((acc, x) => acc + (x.size || 0), 0)
  }
  return localFileClips.value.reduce((acc, x) => acc + (x.size || 0), 0)
})

function resolveUploadSiloPrefix(): string {
  if (siloCategory.value === 'imghost') {
    if (siloSubTag.value === 'dir:drop') return 'drop/'
    if (siloSubTag.value === 'tag:video') return 'videos/'
    return 'images/'
  }
  if (siloCategory.value === 'blog' && siloSubTag.value.startsWith('dir:')) {
    return `${siloSubTag.value.slice(4)}/`
  }
  if (siloCategory.value === 'music' && siloSubTag.value.startsWith('exact:')) {
    return `${siloSubTag.value.slice(6)}/`
  }
  return 'images/'
}

function onPickedVaultUpload(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return
  const prefix = activePool.value === 'silo' ? resolveUploadSiloPrefix() : 'drop/'
  const gdriveFolder =
    activePool.value === 'gdrive' && driveFolderFilter.value !== 'all'
      ? driveFolderFilter.value
      : '/'
  for (const f of Array.from(input.files)) {
    if (activePool.value === 'local' && f.size <= 20 * 1024 * 1024) {
      void store.sendQuickPasteFile(f, { target: 'local', autoCopyFormat: 'none' })
      continue
    }
    void store.uploadChunkedFile(f, {
      target: activePool.value,
      siloPrefix: prefix,
      gdriveFolder,
      ttlHours: 0,
      maxDownloads: 0,
      passcode: '',
      addToClip: true,
      skipShare: true,
      autoCopyFormat: activePool.value === 'silo' && f.type.startsWith('image/') ? 'md' : 'none',
    })
  }
  input.value = ''
}

async function shareVaultItem(
  busyKey: string,
  storage: 'silo' | 'gdrive',
  key: string,
  fileName: string,
  size: number,
) {
  if (sharingBusy.value[busyKey]) return
  sharingBusy.value[busyKey] = true
  try {
    const res = await fetch('/api/shares/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ storage, key, fileName, size, ttlHours: 168 }),
    })
    if (res.ok) {
      const sh = await res.json()
      if (!store.shares.value.some((s) => s.code === sh.code)) {
        store.shares.value = [sh, ...store.shares.value]
      }
      const url = shareFullUrl(sh.code)
      await copyText(url, '公开短链已生成并复制')
      store.openQrModal(fileName, url, '可在下方设置提取码、有效期或阅后即焚次数', sh.code)
    }
  } finally {
    sharingBusy.value[busyKey] = false
  }
}

async function shareLocalClip(lc: ClipItem) {
  const bKey = `local:${lc.id}`
  if (sharingBusy.value[bKey]) return
  if (lc.shareCode && store.shares.value.some((s) => s.code === lc.shareCode)) {
    const url = shareFullUrl(lc.shareCode)
    await copyText(url, '已复制公开分享短链')
    store.openQrModal(lc.fileName || '互传文件', url, '可在下方修改提取码、有效期或次数上限', lc.shareCode)
    return
  }
  sharingBusy.value[bKey] = true
  try {
    const res = await fetch(`/api/clips/${lc.id}/share`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ttlHours: 168, passcode: '' }),
    })
    if (res.ok) {
      const sh = await res.json()
      lc.shareCode = sh.code
      if (!store.shares.value.some((s) => s.code === sh.code)) {
        store.shares.value = [sh, ...store.shares.value]
      }
      const url = shareFullUrl(sh.code)
      await copyText(url, '公开短链已生成并复制')
      store.openQrModal(lc.fileName || '互传文件', url, '可在下方设置提取码、有效期或阅后即焚次数', sh.code)
    }
  } finally {
    sharingBusy.value[bKey] = false
  }
}

function playSiloAudioInDeck(obj: VaultObject) {
  const existing = store.audioTracks.value.find((t) => t.key === obj.key)
  const track: MediaItem = existing || {
    key: obj.key,
    name: obj.name,
    folder: obj.folder,
    kind: 'audio',
    ext: obj.ext,
    size: obj.size,
    lastModified: obj.lastModified,
    publicUrl: obj.publicUrl,
    streamUrl: obj.streamUrl,
    thumbUrl: obj.thumbUrl,
  }
  void store.playTrack(track)
  showToast(`正在播放：${obj.name}`)
}

function startRename(item: VaultObject) {
  renameTarget.value = item
  renameDraft.value = item.name
}

async function confirmRename() {
  const target = renameTarget.value
  if (!target || renamingBusy.value) return
  const newName = renameDraft.value.trim()
  if (!newName || newName === target.name) {
    renameTarget.value = null
    return
  }
  renamingBusy.value = true
  try {
    const force = Boolean(target.protected)
    const res = await fetch('/api/silo/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: target.key, newName, force }),
    })
    if (res.ok) {
      renameTarget.value = null
      showToast('重命名成功')
      await store.loadSiloVault(false)
    } else {
      const err = await res.json().catch(() => ({ error: '重命名失败' }))
      showToast(err.error || '重命名失败')
    }
  } finally {
    renamingBusy.value = false
  }
}

async function deleteSilo(item: VaultObject, force = false) {
  const bKey = `silo:${item.key}`
  if (deletingMap.value[bKey]) return
  deletingMap.value[bKey] = true
  try {
    const res = await fetch(
      `/api/silo/object?key=${encodeURIComponent(item.key)}&force=${force ? '1' : '0'}`,
      { method: 'DELETE' },
    )
    if (res.ok) {
      store.siloObjects.value = store.siloObjects.value.filter((x) => x.key !== item.key)
      showToast(`已删除：${item.name}`)
    } else {
      const err = await res.json().catch(() => ({ error: '删除失败' }))
      showToast(err.error || '删除失败')
    }
  } finally {
    deletingMap.value[bKey] = false
  }
}

async function deleteDrive(item: GDriveFile) {
  const bKey = `gd:${item.path}`
  if (deletingMap.value[bKey]) return
  deletingMap.value[bKey] = true
  try {
    const res = await fetch(`/api/gdrive/file?path=${encodeURIComponent(item.path)}`, {
      method: 'DELETE',
    })
    if (res.ok) {
      store.gdriveFiles.value = store.gdriveFiles.value.filter((x) => x.path !== item.path)
      showToast(`已从 Drive 删除：${item.name}`)
    } else {
      const err = await res.json().catch(() => ({ error: '删除失败' }))
      showToast(err.error || '删除失败')
    }
  } finally {
    deletingMap.value[bKey] = false
  }
}

async function deleteLocalClip(id: string, name?: string) {
  const bKey = `local:${id}`
  if (deletingMap.value[bKey]) return
  deletingMap.value[bKey] = true
  try {
    const res = await fetch(`/api/clips/${id}`, { method: 'DELETE' })
    if (res.ok) {
      store.clips.value = store.clips.value.filter((c) => c.id !== id)
      showToast(`已删除：${name || '文件'}`)
    }
  } finally {
    deletingMap.value[bKey] = false
  }
}

async function promoteLocalToSilo(lc: ClipItem) {
  if (promotingLocalSilo.value[lc.id]) return
  promotingLocalSilo.value[lc.id] = true
  try {
    await store.promoteClipToSilo(lc, 'md')
  } finally {
    promotingLocalSilo.value[lc.id] = false
  }
}

async function archiveLocalToDrive(lc: ClipItem, folder: string) {
  if (!folder || archivingLocalDrive.value[lc.id]) return
  archivingLocalDrive.value[lc.id] = true
  try {
    await store.archiveClipToGDrive(lc, folder)
  } finally {
    archivingLocalDrive.value[lc.id] = false
  }
}

function openPreviewSilo(item: VaultObject) {
  if (item.kind === 'audio' || item.kind === 'video') {
    store.pauseAudio()
  }
  previewObj.value = {
    name: item.name,
    streamUrl: item.streamUrl,
    downloadUrl: `${item.streamUrl}&download=1`,
    publicUrl: item.publicUrl,
    size: item.size,
    kind: item.kind,
    spec: `${describeSiloFolder(item.folder)} · ${formatBytes(item.size)} · ${formatTimeShort(item.lastModified)}`,
    siloAudioKey: item.kind === 'audio' ? item.key : undefined,
  }
}

function openPreviewDrive(gd: GDriveFile) {
  if (gd.kind === 'audio' || gd.kind === 'video') {
    store.pauseAudio()
  }
  previewObj.value = {
    name: gd.name,
    streamUrl: `${gd.streamUrl}&size=${gd.size}`,
    downloadUrl: `${gd.streamUrl}&size=${gd.size}&download=1`,
    size: gd.size,
    kind: gd.kind,
    spec: `Drive: ${gd.path} · ${formatBytes(gd.size)} · ${formatTimeShort(gd.lastModified)}`,
  }
}

function openPreviewLocal(lc: ClipItem) {
  const m = (lc.mimeType || '').toLowerCase()
  const kind =
    lc.kind === 'image'
      ? 'image'
      : m.startsWith('video/')
        ? 'video'
        : m.startsWith('audio/')
          ? 'audio'
          : 'other'
  if (kind === 'audio' || kind === 'video') {
    store.pauseAudio()
  }
  previewObj.value = {
    name: lc.fileName || '文件',
    streamUrl: `/api/clips/${lc.id}/raw`,
    downloadUrl: `/api/clips/${lc.id}/raw?download=1`,
    publicUrl: lc.publicUrl,
    size: lc.size || 0,
    kind,
    spec: `私密互传 · ${formatBytes(lc.size)} · ${formatTimeShort(lc.createdAt)}`,
  }
}

function iconForKind(kind: string): 'disc' | 'video' | 'image' | 'file' {
  if (kind === 'audio') return 'disc'
  if (kind === 'video') return 'video'
  if (kind === 'image') return 'image'
  return 'file'
}
</script>

<template>
  <div class="vault-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">文件柜</h1>
        <span class="pane-count">{{ activeCount }}</span>
      </div>

      <div class="pane-actions vault-top-acts">
        <div class="tab-pill-group" :style="{ gridTemplateColumns: 'repeat(3, 1fr)' }">
          <div
            class="tab-pill-indicator"
            :style="{
              width: 'calc((100% - 6px) / 3)',
              transform: `translateX(${activePoolIdx * 100}%)`,
            }"
          />
          <button
            v-for="p in pools"
            :key="p.key"
            type="button"
            class="tab-pill-btn"
            :class="{ 'is-active': activePool === p.key, 'is-disabled': !p.enabled }"
            @click="switchPool(p.key, p.enabled)"
          >
            {{ p.label }}
          </button>
        </div>
      </div>
    </header>

    <div class="pane-body vault-body">
      <!-- Unified Toolbar (View Mode Switch stays constant across ALL storage pools!) -->
      <div class="vault-toolbar">
        <div class="search-pill">
          <UiIcon name="search" :size="14" />
          <input v-model="search" placeholder="搜索文件名、目录用途或路径…" />
          <button
            v-if="search"
            type="button"
            class="search-clear"
            title="清空搜索"
            @click="search = ''"
          >
            <UiIcon name="close" :size="12" />
          </button>
        </div>

        <div class="vt-right">
          <button type="button" class="pill-btn pill-btn--solid" @click="uploadInput?.click()">
            <UiIcon
              :name="activeVaultUploads.length > 0 ? 'spinner' : 'plus'"
              :size="14"
              :class="{ 'spin-anim': activeVaultUploads.length > 0 }"
            />
            <span>{{ activeVaultUploads.length > 0 ? `上传中 (${activeVaultUploads.length})` : '上传' }}</span>
          </button>
          <input ref="uploadInput" type="file" multiple hidden @change="onPickedVaultUpload" />

          <div class="view-switch" role="group" aria-label="显示样式切换">
            <button
              type="button"
              class="icon-circle-btn"
              :class="{ 'is-on': store.vaultViewMode.value === 'grid' }"
              title="缩略图画廊视图（切换存储位置保持不变）"
              @click="store.setVaultViewMode('grid')"
            >
              <UiIcon name="grid" :size="14.5" />
            </button>
            <button
              type="button"
              class="icon-circle-btn"
              :class="{ 'is-on': store.vaultViewMode.value === 'list' }"
              title="信息列表视图（切换存储位置保持不变）"
              @click="store.setVaultViewMode('list')"
            >
              <UiIcon name="list" :size="14.5" />
            </button>
          </div>

          <button type="button" class="icon-circle-btn" title="刷新列表" @click="refreshCurrent">
            <UiIcon name="refresh" :size="14.5" :class="{ 'spin-anim': loading }" />
          </button>
        </div>
      </div>

      <!-- Inline Upload Progress Banner inside Vault -->
      <TransitionGroup name="up-fold" tag="div" class="vault-up-list">
        <div v-for="u in activeVaultUploads" :key="u.localId" class="glass-box v-up-row">
          <UiIcon name="spinner" :size="13.5" class="spin-anim" />
          <span class="v-up-name">{{ u.fileName }}</span>
          <span class="v-up-pct mono">
            {{ u.status === 'assembling' ? '入库中…' : `${u.size ? Math.min(100, Math.round((u.uploadedBytes / u.size) * 100)) : 0}%` }}
          </span>
        </div>
      </TransitionGroup>

      <!-- Pool-Specific Semantic Info & Directory Filter Bar (smoothly switches with storage pool!) -->
      <Transition name="info-swap" mode="out-in">
        <!-- 1. SILO TWO-LEVEL SEMANTIC FOLDER GUIDE -->
        <section v-if="activePool === 'silo'" key="info-silo" class="glass-box pool-info-panel">
          <div class="cat-top-row">
            <div class="cat-pills">
              <button
                type="button"
                class="cat-btn"
                :class="{ 'is-on': siloCategory === 'all' }"
                @click="selectSiloCategory('all')"
              >
                <span>全部</span>
                <span class="cat-cnt mono">{{ siloOverviewCount }}</span>
              </button>

              <button
                type="button"
                class="cat-btn"
                :class="{ 'is-on': siloCategory === 'imghost' }"
                @click="selectSiloCategory('imghost')"
              >
                <UiIcon name="image" :size="13.5" />
                <span>图床相关</span>
                <span class="cat-cnt mono">{{ siloCategoryCounts.imghost }}</span>
              </button>

              <button
                type="button"
                class="cat-btn"
                :class="{ 'is-on': siloCategory === 'blog' }"
                @click="selectSiloCategory('blog')"
              >
                <UiIcon name="lock" :size="12.5" />
                <span>博客相关</span>
                <span class="cat-cnt mono">{{ siloCategoryCounts.blog }}</span>
              </button>

              <button
                type="button"
                class="cat-btn"
                :class="{ 'is-on': siloCategory === 'music' }"
                @click="selectSiloCategory('music')"
              >
                <UiIcon name="disc" :size="13.5" />
                <span>音乐相关</span>
                <span class="cat-cnt mono">{{ siloCategoryCounts.music }}</span>
              </button>

              <button
                v-if="siloCategoryCounts.other > 0"
                type="button"
                class="cat-btn"
                :class="{ 'is-on': siloCategory === 'other' }"
                @click="selectSiloCategory('other')"
              >
                <UiIcon name="folder" :size="13.5" />
                <span>其他目录</span>
                <span class="cat-cnt mono">{{ siloCategoryCounts.other }}</span>
              </button>
            </div>

            <span class="pool-stat mono">{{ activeCount }} 项 · {{ formatBytes(activeTotalBytes) }}</span>
          </div>

          <div class="cat-desc-line">{{ activeCategoryDesc }}</div>

          <!-- Second-level Semantic Sub-Tags Drawer -->
          <Transition name="sub-drawer">
            <div v-if="currentSiloSubTags.length > 0" class="subtag-strip">
              <button
                v-for="tag in currentSiloSubTags"
                :key="tag.key"
                type="button"
                class="subtag-chip"
                :class="{ 'is-on': siloSubTag === tag.key }"
                :title="tag.desc"
                @click="siloSubTag = tag.key"
              >
                <span class="st-label">{{ tag.label }}</span>
                <span v-if="tag.pathHint" class="st-path mono">{{ tag.pathHint }}</span>
                <span class="st-cnt mono">{{ tag.count }}</span>
              </button>
            </div>
          </Transition>
        </section>

        <!-- 2. GOOGLE DRIVE SUBFOLDER & INFO BAR -->
        <section v-else-if="activePool === 'gdrive'" key="info-gdrive" class="glass-box pool-info-panel">
          <div class="cat-top-row">
            <div class="cat-pills">
              <button
                type="button"
                class="cat-btn"
                :class="{ 'is-on': driveFolderFilter === 'all' }"
                @click="driveFolderFilter = 'all'"
              >
                <span>全部子目录</span>
                <span class="cat-cnt mono">{{ store.gdriveFiles.value.length }}</span>
              </button>
              <button
                v-for="df in driveAvailableFolders"
                :key="df"
                type="button"
                class="cat-btn mono"
                :class="{ 'is-on': driveFolderFilter === df }"
                @click="driveFolderFilter = df"
              >
                <UiIcon name="folder" :size="13" />
                <span>{{ df === '/' ? '/ (根目录)' : df }}</span>
              </button>
            </div>

            <span class="pool-stat mono">{{ activeCount }} 项 · {{ formatBytes(activeTotalBytes) }}</span>
          </div>
          <div class="cat-desc-line">
            Google Drive 隔离归档目录（<span class="mono">{{ store.status.value?.gdriveTarget || 'gdrive:OArc-Drop' }}</span> · 同名上传自动加后缀防覆盖）
          </div>
        </section>

        <!-- 3. LOCAL PRIVATE POOL FILTER & INFO BAR -->
        <section v-else key="info-local" class="glass-box pool-info-panel">
          <div class="cat-top-row">
            <div class="cat-pills">
              <button
                v-for="opt in [
                  { k: 'all' as const, l: '全部互传文件' },
                  { k: 'image' as const, l: '图片与截图' },
                  { k: 'file' as const, l: '文档与压缩包' },
                ]"
                :key="opt.k"
                type="button"
                class="cat-btn"
                :class="{ 'is-on': localKindFilter === opt.k }"
                @click="localKindFilter = opt.k"
              >
                <span>{{ opt.l }}</span>
              </button>
            </div>

            <span class="pool-stat mono">{{ activeCount }} 项 · {{ formatBytes(activeTotalBytes) }}</span>
          </div>
          <div class="cat-desc-line">
            本机私密互传池（仅经过 Cloudflare Access 验证的本人设备可见 · 可随时一键转存图床或 Google Drive）
          </div>
        </section>
      </Transition>

      <!-- ================= POOL 1: SILO ================= -->
      <template v-if="activePool === 'silo'">
        <div v-if="filteredSilo.length === 0" class="glass-box empty-state">
          当前筛选下暂无对象
        </div>

        <!-- Unified Grid Mode -->
        <div v-else-if="store.vaultViewMode.value === 'grid'" class="lens-grid">
          <article
            v-for="obj in filteredSilo"
            :key="obj.key"
            class="lens-tile"
            @click="openPreviewSilo(obj)"
          >
            <img v-if="obj.thumbUrl" :src="obj.thumbUrl" :alt="obj.name" loading="lazy" />
            <div v-else class="non-img-tile">
              <UiIcon :name="iconForKind(obj.kind)" :size="28" />
              <span class="ext-tag mono">{{ obj.ext || 'FILE' }}</span>
            </div>

            <button
              v-if="obj.kind === 'audio'"
              type="button"
              class="tile-audio-badge mono"
              title="送入音乐厅唱机播放"
              @click.stop="playSiloAudioInDeck(obj)"
            >
              <UiIcon name="play" :size="10" />
              <span>{{ obj.ext }}</span>
            </button>

            <div class="tile-scrim" @click.stop>
              <div class="tile-info" @click="openPreviewSilo(obj)">
                <div class="tile-name">
                  <UiIcon v-if="obj.protected" name="lock" :size="11" class="inline-lock" />
                  <span>{{ obj.name }}</span>
                </div>
                <div class="tile-meta">{{ describeSiloFolder(obj.folder) }} · {{ formatBytes(obj.size) }}</div>
              </div>

              <div class="tile-btns">
                <CopyPill
                  v-if="obj.publicUrl && obj.kind === 'image'"
                  :text="formatMarkdownImage(obj.name, obj.publicUrl)"
                  label="MD"
                  copied-label="已复制"
                  compact
                />
                <CopyPill
                  v-if="obj.publicUrl"
                  :text="obj.publicUrl"
                  label="外链"
                  copied-label="已复制"
                  compact
                />
                <a class="scrim-circle" :href="`${obj.streamUrl}&download=1`" title="下载">
                  <UiIcon name="download" :size="12.5" />
                </a>
                <button
                  type="button"
                  class="scrim-circle"
                  title="生成公开分享码"
                  :disabled="sharingBusy[`silo:${obj.key}`]"
                  @click="shareVaultItem(`silo:${obj.key}`, 'silo', obj.key, obj.name, obj.size)"
                >
                  <UiIcon
                    :name="sharingBusy[`silo:${obj.key}`] ? 'spinner' : 'qr'"
                    :size="12.5"
                    :class="{ 'spin-anim': sharingBusy[`silo:${obj.key}`] }"
                  />
                </button>
                <button
                  type="button"
                  class="scrim-circle"
                  title="重命名"
                  @click="startRename(obj)"
                >
                  <UiIcon name="edit" :size="12" />
                </button>
                <MorphDeleteBtn
                  compact
                  :busy="Boolean(deletingMap[`silo:${obj.key}`])"
                  :protected-item="obj.protected"
                  :protected-title="`删除 ${obj.name}`"
                  @confirm="deleteSilo(obj, $event)"
                />
              </div>
            </div>
          </article>
        </div>

        <!-- Unified List Mode (Two-row mobile-safe layout — zero 1-char vertical squishing!) -->
        <div v-else class="file-list">
          <div v-for="obj in filteredSilo" :key="obj.key" class="glass-box file-row">
            <div class="fr-main" @click="openPreviewSilo(obj)">
              <img
                v-if="obj.thumbUrl"
                :src="obj.thumbUrl"
                class="fr-thumb"
                alt=""
                loading="lazy"
              />
              <div v-else class="fr-icon">
                <UiIcon :name="iconForKind(obj.kind)" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">
                  <UiIcon v-if="obj.protected" name="lock" :size="12" class="lock-muted" />
                  <span>{{ obj.name }}</span>
                </div>
                <div class="fr-sub">
                  <span class="fr-purpose">{{ describeSiloFolder(obj.folder) }}</span>
                  <span class="mono"> · {{ formatBytes(obj.size) }} · {{ formatTimeShort(obj.lastModified) }}</span>
                </div>
              </div>
            </div>

            <div class="fr-acts">
              <button
                v-if="obj.kind === 'audio'"
                type="button"
                class="mini-act"
                title="送入唱机播放"
                @click="playSiloAudioInDeck(obj)"
              >
                <UiIcon name="play" :size="13" />
              </button>
              <CopyPill
                v-if="obj.publicUrl && obj.kind === 'image'"
                :text="formatMarkdownImage(obj.name, obj.publicUrl)"
                label="MD"
                copied-label="已复制"
                compact
              />
              <CopyPill
                v-if="obj.publicUrl"
                :text="obj.publicUrl"
                label="直链"
                copied-label="已复制"
                compact
              />
              <a class="mini-act" :href="`${obj.streamUrl}&download=1`" title="下载">
                <UiIcon name="download" :size="14" />
              </a>
              <button
                type="button"
                class="mini-act"
                title="生成公开取件短链 + 二维码"
                :disabled="sharingBusy[`silo:${obj.key}`]"
                @click="shareVaultItem(`silo:${obj.key}`, 'silo', obj.key, obj.name, obj.size)"
              >
                <UiIcon
                  :name="sharingBusy[`silo:${obj.key}`] ? 'spinner' : 'qr'"
                  :size="14"
                  :class="{ 'spin-anim': sharingBusy[`silo:${obj.key}`] }"
                />
              </button>
              <button type="button" class="mini-act" title="重命名" @click="startRename(obj)">
                <UiIcon name="edit" :size="14" />
              </button>
              <MorphDeleteBtn
                compact
                :busy="Boolean(deletingMap[`silo:${obj.key}`])"
                :protected-item="obj.protected"
                :protected-title="`删除 ${obj.name}`"
                @confirm="deleteSilo(obj, $event)"
              />
            </div>
          </div>
        </div>
      </template>

      <!-- ================= POOL 2: GOOGLE DRIVE ================= -->
      <template v-else-if="activePool === 'gdrive'">
        <div v-if="filteredDrive.length === 0" class="glass-box empty-state">
          Google Drive 当前筛选下暂无文件
        </div>

        <!-- Unified Grid Mode for Google Drive -->
        <div v-else-if="store.vaultViewMode.value === 'grid'" class="lens-grid">
          <article
            v-for="gd in filteredDrive"
            :key="gd.path"
            class="lens-tile"
            @click="openPreviewDrive(gd)"
          >
            <img
              v-if="gd.kind === 'image'"
              :src="`${gd.streamUrl}&size=${gd.size}`"
              :alt="gd.name"
              loading="lazy"
            />
            <div v-else class="non-img-tile">
              <UiIcon :name="iconForKind(gd.kind)" :size="28" />
              <span class="ext-tag mono">{{ gd.ext || 'DRIVE' }}</span>
            </div>

            <div class="tile-scrim" @click.stop>
              <div class="tile-info" @click="openPreviewDrive(gd)">
                <div class="tile-name">
                  <span>{{ gd.name }}</span>
                </div>
                <div class="tile-meta mono">
                  {{ gd.folder === '/' ? 'OArc-Drop/' : `OArc-Drop/${gd.folder}` }} · {{ formatBytes(gd.size) }}
                </div>
              </div>

              <div class="tile-btns">
                <a class="scrim-circle" :href="`${gd.streamUrl}&size=${gd.size}&download=1`" title="下载">
                  <UiIcon name="download" :size="12.5" />
                </a>
                <button
                  type="button"
                  class="scrim-circle"
                  title="生成公开分享码"
                  :disabled="sharingBusy[`gd:${gd.path}`]"
                  @click="shareVaultItem(`gd:${gd.path}`, 'gdrive', gd.path, gd.name, gd.size)"
                >
                  <UiIcon
                    :name="sharingBusy[`gd:${gd.path}`] ? 'spinner' : 'qr'"
                    :size="12.5"
                    :class="{ 'spin-anim': sharingBusy[`gd:${gd.path}`] }"
                  />
                </button>
                <MorphDeleteBtn
                  compact
                  title="从 Drive 删除"
                  :busy="Boolean(deletingMap[`gd:${gd.path}`])"
                  @confirm="deleteDrive(gd)"
                />
              </div>
            </div>
          </article>
        </div>

        <!-- Unified List Mode for Google Drive -->
        <div v-else class="file-list">
          <div v-for="gd in filteredDrive" :key="gd.path" class="glass-box file-row">
            <div class="fr-main" @click="openPreviewDrive(gd)">
              <img
                v-if="gd.kind === 'image'"
                :src="`${gd.streamUrl}&size=${gd.size}`"
                class="fr-thumb"
                alt=""
                loading="lazy"
              />
              <div v-else class="fr-icon">
                <UiIcon name="cloud" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">
                  <span>{{ gd.name }}</span>
                </div>
                <div class="fr-sub">
                  <span class="fr-purpose mono">Drive · {{ gd.folder === '/' ? '根目录' : gd.folder }}</span>
                  <span class="mono"> · {{ formatBytes(gd.size) }} · {{ formatTimeShort(gd.lastModified) }}</span>
                </div>
              </div>
            </div>
            <div class="fr-acts">
              <a
                class="pill-btn pill-btn--mini"
                :href="`${gd.streamUrl}&size=${gd.size}&download=1`"
              >
                <UiIcon name="download" :size="13.5" />
                <span>下载</span>
              </a>
              <button
                type="button"
                class="mini-act"
                title="生成公开分享短链 + 二维码"
                :disabled="sharingBusy[`gd:${gd.path}`]"
                @click="shareVaultItem(`gd:${gd.path}`, 'gdrive', gd.path, gd.name, gd.size)"
              >
                <UiIcon
                  :name="sharingBusy[`gd:${gd.path}`] ? 'spinner' : 'qr'"
                  :size="14"
                  :class="{ 'spin-anim': sharingBusy[`gd:${gd.path}`] }"
                />
              </button>
              <MorphDeleteBtn
                compact
                title="从 Drive 删除"
                :busy="Boolean(deletingMap[`gd:${gd.path}`])"
                @confirm="deleteDrive(gd)"
              />
            </div>
          </div>
        </div>
      </template>

      <!-- ================= POOL 3: LOCAL PRIVATE FILES ================= -->
      <template v-else>
        <div v-if="localFileClips.length === 0" class="glass-box empty-state">
          当前暂无私密互传文件
        </div>

        <!-- Unified Grid Mode for Local Private Pool -->
        <div v-else-if="store.vaultViewMode.value === 'grid'" class="lens-grid">
          <article
            v-for="lc in localFileClips"
            :key="lc.id"
            class="lens-tile"
            @click="openPreviewLocal(lc)"
          >
            <img
              v-if="lc.kind === 'image'"
              :src="`/api/clips/${lc.id}/raw`"
              :alt="lc.fileName"
              loading="lazy"
            />
            <div v-else class="non-img-tile">
              <UiIcon name="file" :size="28" />
              <span class="ext-tag mono">
                {{ (lc.fileName?.split('.').pop() || 'FILE').slice(0, 5).toUpperCase() }}
              </span>
            </div>

            <div class="tile-scrim" @click.stop>
              <div class="tile-info" @click="openPreviewLocal(lc)">
                <div class="tile-name">
                  <span>{{ lc.fileName }}</span>
                </div>
                <div class="tile-meta mono">私密互传 · {{ formatBytes(lc.size) }}</div>
              </div>

              <div class="tile-btns">
                <CopyPill
                  v-if="lc.publicUrl && lc.kind === 'image'"
                  :text="formatMarkdownImage(lc.fileName || 'image', lc.publicUrl)"
                  label="MD"
                  copied-label="已复制"
                  compact
                />
                <button
                  v-else-if="store.status.value?.siloEnabled"
                  type="button"
                  class="scrim-pill"
                  :disabled="promotingLocalSilo[lc.id]"
                  @click="promoteLocalToSilo(lc)"
                >
                  <UiIcon
                    :name="promotingLocalSilo[lc.id] ? 'spinner' : 'link'"
                    :size="11.5"
                    :class="{ 'spin-anim': promotingLocalSilo[lc.id] }"
                  />
                  <span>图床</span>
                </button>
                <a class="scrim-circle" :href="`/api/clips/${lc.id}/raw?download=1`" title="下载">
                  <UiIcon name="download" :size="12.5" />
                </a>
                <button
                  type="button"
                  class="scrim-circle"
                  title="生成公开分享码"
                  :disabled="sharingBusy[`local:${lc.id}`]"
                  @click="shareLocalClip(lc)"
                >
                  <UiIcon
                    :name="sharingBusy[`local:${lc.id}`] ? 'spinner' : 'qr'"
                    :size="12.5"
                    :class="{ 'spin-anim': sharingBusy[`local:${lc.id}`] }"
                  />
                </button>
                <MorphDeleteBtn
                  compact
                  title="删除此文件"
                  :busy="Boolean(deletingMap[`local:${lc.id}`])"
                  @confirm="deleteLocalClip(lc.id, lc.fileName)"
                />
              </div>
            </div>
          </article>
        </div>

        <!-- Unified List Mode for Local Private Pool -->
        <div v-else class="file-list">
          <div v-for="lc in localFileClips" :key="lc.id" class="glass-box file-row">
            <div class="fr-main" @click="openPreviewLocal(lc)">
              <img
                v-if="lc.kind === 'image'"
                :src="`/api/clips/${lc.id}/raw`"
                class="fr-thumb"
                alt=""
                loading="lazy"
              />
              <div v-else class="fr-icon">
                <UiIcon name="file" :size="17" />
              </div>
              <div class="fr-text">
                <div class="fr-name">
                  <span>{{ lc.fileName }}</span>
                </div>
                <div class="fr-sub">
                  <span class="fr-purpose">私密互传</span>
                  <span class="mono"> · {{ formatBytes(lc.size) }} · {{ formatTimeShort(lc.createdAt) }}</span>
                </div>
              </div>
            </div>
            <div class="fr-acts">
              <template v-if="store.status.value?.siloEnabled">
                <CopyPill
                  v-if="lc.publicUrl"
                  :text="lc.kind === 'image' ? formatMarkdownImage(lc.fileName || 'image', lc.publicUrl) : lc.publicUrl"
                  :label="lc.kind === 'image' ? '复制 MD' : '复制外链'"
                  copied-label="已复制"
                  compact
                />
                <button
                  v-else
                  type="button"
                  class="pill-btn pill-btn--mini"
                  :disabled="promotingLocalSilo[lc.id]"
                  @click="promoteLocalToSilo(lc)"
                >
                  <UiIcon
                    :name="promotingLocalSilo[lc.id] ? 'spinner' : 'link'"
                    :size="13"
                    :class="{ 'spin-anim': promotingLocalSilo[lc.id] }"
                  />
                  <span>存图床</span>
                </button>
              </template>

              <DriveFolderSelect
                v-if="store.status.value?.gdriveEnabled"
                mode="file"
                :saved-path="lc.gdrivePath"
                :busy="Boolean(archivingLocalDrive[lc.id])"
                @select="archiveLocalToDrive(lc, $event)"
              />

              <a class="pill-btn pill-btn--mini" :href="`/api/clips/${lc.id}/raw?download=1`">
                <UiIcon name="download" :size="13.5" />
                <span>下载</span>
              </a>
              <button
                type="button"
                class="mini-act"
                title="生成公开分享码"
                :disabled="sharingBusy[`local:${lc.id}`]"
                @click="shareLocalClip(lc)"
              >
                <UiIcon
                  :name="sharingBusy[`local:${lc.id}`] ? 'spinner' : 'qr'"
                  :size="14"
                  :class="{ 'spin-anim': sharingBusy[`local:${lc.id}`] }"
                />
              </button>
              <MorphDeleteBtn
                compact
                title="删除此文件"
                :busy="Boolean(deletingMap[`local:${lc.id}`])"
                @confirm="deleteLocalClip(lc.id, lc.fileName)"
              />
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Rename Modal -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="renameTarget" class="modal-backdrop" @click.self="!renamingBusy && (renameTarget = null)">
          <div class="liquid-chrome rename-box">
            <div class="rn-title">重命名对象</div>
            <div class="rn-sub">{{ describeSiloFolder(renameTarget.folder) }}</div>
            <div v-if="renameTarget.protected" class="rn-warn">
              该对象位于博客生产目录，改名可能导致线上文章或歌单旧地址失效。
            </div>
            <input
              v-model="renameDraft"
              class="rn-input mono"
              :disabled="renamingBusy"
              @keydown.enter="confirmRename"
            />
            <div class="rn-btns">
              <button type="button" class="pill-btn" :disabled="renamingBusy" @click="renameTarget = null">取消</button>
              <button type="button" class="pill-btn pill-btn--solid" :disabled="renamingBusy" @click="confirmRename">
                <UiIcon v-if="renamingBusy" name="spinner" :size="13.5" class="spin-anim" />
                <span>{{ renameTarget.protected ? '强制改名' : '确认改名' }}</span>
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Preview Lightbox (Image / Video / Audio / PDF across all 3 pools!) -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div v-if="previewObj" class="modal-backdrop" @click.self="previewObj = null">
          <div class="liquid-chrome preview-dialog">
            <button type="button" class="icon-circle-btn pd-close" @click="previewObj = null">
              <UiIcon name="close" :size="16" />
            </button>
            <div class="pd-stage">
              <img v-if="previewObj.kind === 'image'" :src="previewObj.streamUrl" :alt="previewObj.name" />
              <video v-else-if="previewObj.kind === 'video'" controls :src="previewObj.streamUrl" />
              <audio v-else-if="previewObj.kind === 'audio'" controls :src="previewObj.streamUrl" />
              <iframe
                v-else-if="previewObj.name.toLowerCase().endsWith('.pdf')"
                :src="previewObj.streamUrl"
                class="pd-pdf"
                title="Preview"
              />
              <div v-else class="pd-fallback">
                <UiIcon name="file" :size="40" />
                <div>{{ previewObj.name }}</div>
              </div>
            </div>
            <footer class="pd-bar">
              <span class="pd-spec">{{ previewObj.spec }}</span>
              <div class="pd-acts">
                <button
                  v-if="previewObj.siloAudioKey"
                  type="button"
                  class="pill-btn pill-btn--solid"
                  @click="
                    playSiloAudioInDeck(
                      store.siloObjects.value.find((x) => x.key === previewObj?.siloAudioKey)!,
                    );
                    previewObj = null
                  "
                >
                  <UiIcon name="disc" :size="14" />
                  <span>送入音乐厅唱机</span>
                </button>
                <button
                  v-if="previewObj.kind === 'image'"
                  type="button"
                  class="pill-btn"
                  @click="copyImagePixels(previewObj.streamUrl)"
                >
                  <UiIcon name="copy" :size="14" />
                  <span>复制原图</span>
                </button>
                <CopyPill
                  v-if="previewObj.publicUrl && previewObj.kind === 'image'"
                  :text="formatMarkdownImage(previewObj.name, previewObj.publicUrl)"
                  label="Markdown"
                  copied-label="已复制"
                />
                <CopyPill
                  v-if="previewObj.publicUrl && previewObj.kind === 'image'"
                  :text="formatHtmlImage(previewObj.name, previewObj.publicUrl)"
                  label="HTML"
                  copied-label="已复制"
                />
                <CopyPill
                  v-if="previewObj.publicUrl"
                  :text="previewObj.publicUrl"
                  label="复制直链"
                  copied-label="已复制"
                />
                <a class="pill-btn" :href="previewObj.downloadUrl">
                  <UiIcon name="download" :size="14" />
                  <span>下载</span>
                </a>
              </div>
            </footer>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.vault-top-acts .tab-pill-group {
  min-width: 288px;
}

.vault-body {
  display: flex;
  flex-direction: column;
  gap: 13px;
}

.vault-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.search-clear {
  border: 0;
  background: transparent;
  color: var(--ink-3);
  padding: 2px;
  display: inline-grid;
  place-items: center;
  cursor: pointer;
}
.search-clear:hover {
  color: var(--ink-1);
}

.vt-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.view-switch {
  display: flex;
  gap: 4px;
}

.icon-circle-btn.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.pill-btn--mini {
  height: 29px;
  padding: 0 10px;
  font-size: 0.75rem;
}

/* Vault Upload Progress Strip */
.vault-up-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.vault-up-list:empty {
  display: none;
}
.v-up-row {
  padding: 8px 13px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 0.78rem;
}
.v-up-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 590;
}
.v-up-pct {
  font-size: 0.72rem;
  color: var(--ink-3);
}

/* Semantic Pool Info & Two-Level Category Navigation */
.pool-info-panel {
  padding: 11px 14px;
  border-radius: 15px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.cat-top-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
}

.cat-pills {
  display: flex;
  align-items: center;
  gap: 6px;
  overflow-x: auto;
  max-width: 100%;
  scrollbar-width: none;
}
.cat-pills::-webkit-scrollbar {
  display: none;
}

.cat-btn {
  height: 31px;
  padding: 0 11px;
  border-radius: 9px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.78rem;
  font-weight: 580;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  flex-shrink: 0;
  cursor: pointer;
  transition:
    background-color 0.18s ease,
    color 0.18s ease,
    border-color 0.18s ease,
    transform 0.18s var(--ease-spring);
}

.cat-btn:hover {
  color: var(--ink-1);
  border-color: var(--border-strong);
}

.cat-btn.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
  box-shadow: 0 3px 10px -3px rgba(11, 13, 17, 0.25);
}

.cat-cnt {
  font-size: 0.68rem;
  opacity: 0.72;
}

.pool-stat {
  font-size: 0.72rem;
  color: var(--ink-3);
  white-space: nowrap;
}

.cat-desc-line {
  font-size: 0.74rem;
  color: var(--ink-3);
  line-height: 1.4;
}

.subtag-strip {
  padding-top: 8px;
  border-top: 1px dashed var(--border-hairline);
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.subtag-chip {
  height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-2);
  font-size: 0.74rem;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  cursor: pointer;
  transition: all 0.16s ease;
}

.subtag-chip:hover {
  border-color: var(--border-strong);
  color: var(--ink-1);
}

.subtag-chip.is-on {
  background: var(--accent-soft);
  color: var(--ink-1);
  border-color: var(--accent-border);
  font-weight: 640;
}

.st-path {
  font-size: 0.66rem;
  color: var(--ink-3);
}

.st-cnt {
  font-size: 0.66rem;
  opacity: 0.68;
}

.info-swap-enter-active,
.info-swap-leave-active {
  transition: opacity 0.16s ease, transform 0.22s var(--ease-spring);
}
.info-swap-enter-from {
  opacity: 0;
  transform: translateY(-4px);
}
.info-swap-leave-to {
  opacity: 0;
  transform: translateY(4px);
}

.sub-drawer-enter-active,
.sub-drawer-leave-active {
  transition: opacity 0.18s ease, transform 0.22s var(--ease-spring);
}
.sub-drawer-enter-from,
.sub-drawer-leave-to {
  opacity: 0;
  transform: translateY(-5px);
}

.empty-state {
  padding: 48px 20px;
  border-radius: 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

/* Unified Thumbnail Grid */
.lens-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(206px, 1fr));
  gap: 13px;
}

.lens-tile {
  position: relative;
  aspect-ratio: 4 / 3;
  border-radius: 16px;
  overflow: hidden;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  cursor: pointer;
}

.lens-tile img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.tile-audio-badge {
  position: absolute;
  top: 9px;
  right: 9px;
  border: 0;
  padding: 3.5px 9px;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.76);
  -webkit-backdrop-filter: blur(8px);
  backdrop-filter: blur(8px);
  color: #fff;
  font-size: 0.66rem;
  font-weight: 660;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

.non-img-tile {
  width: 100%;
  height: 100%;
  padding-bottom: 38px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--ink-3);
}

.ext-tag {
  font-size: 0.7rem;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--surface-card);
  color: var(--ink-2);
}

.tile-scrim {
  position: absolute;
  inset: auto 0 0 0;
  padding: 22px 10px 9px;
  background: linear-gradient(to top, rgba(11, 14, 22, 0.86), rgba(11, 14, 22, 0.42) 68%, transparent);
  color: #fff;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.tile-name {
  font-size: 0.77rem;
  font-weight: 620;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: flex;
  align-items: center;
  gap: 4px;
}

.inline-lock {
  opacity: 0.85;
  flex-shrink: 0;
}

.tile-meta {
  font-size: 0.67rem;
  opacity: 0.8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tile-btns {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
}

.scrim-circle {
  width: 26px;
  height: 26px;
  border-radius: 7px;
  border: 1px solid rgba(255, 255, 255, 0.24);
  background: rgba(255, 255, 255, 0.15);
  color: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
  padding: 0;
}

.scrim-pill {
  height: 26px;
  padding: 0 8px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.24);
  background: rgba(255, 255, 255, 0.16);
  color: #fff;
  font-size: 0.7rem;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

/* Unified List Rows — Desktop Horizontal, Mobile Clean Two-Tier! */
.file-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.file-row {
  padding: 11px 14px;
  border-radius: 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.fr-main {
  display: flex;
  align-items: center;
  gap: 11px;
  min-width: 0;
  flex: 1;
  cursor: pointer;
}

.fr-thumb {
  width: 40px;
  height: 40px;
  border-radius: 9px;
  object-fit: cover;
  flex-shrink: 0;
  border: 1px solid var(--border-hairline);
}

.fr-icon {
  width: 40px;
  height: 40px;
  border-radius: 9px;
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  flex-shrink: 0;
}

.fr-text {
  min-width: 0;
  flex: 1;
}

.fr-name {
  font-size: 0.86rem;
  font-weight: 630;
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--ink-1);
}

.fr-name > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lock-muted {
  color: var(--ink-3);
  flex-shrink: 0;
}

.fr-sub {
  margin-top: 2.5px;
  font-size: 0.73rem;
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fr-purpose {
  color: var(--ink-2);
  font-weight: 540;
}

.fr-acts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.mini-act {
  width: 29px;
  height: 29px;
  border-radius: 8px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  cursor: pointer;
  text-decoration: none;
  padding: 0;
}

.mini-act:hover {
  background: var(--surface-pill);
  color: var(--ink-1);
  border-color: var(--border-strong);
}

/* Mobile Responsive Fix: Never squish filenames into 1-char vertical noodles! */
@media (max-width: 720px) {
  .vault-top-acts {
    width: 100%;
  }
  .vault-top-acts .tab-pill-group {
    min-width: 0;
    width: 100%;
  }

  .subtag-strip {
    flex-wrap: nowrap;
    overflow-x: auto;
    scrollbar-width: none;
    padding-bottom: 2px;
  }
  .subtag-strip::-webkit-scrollbar {
    display: none;
  }

  .lens-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }

  .lens-tile {
    aspect-ratio: 1 / 1.05;
    border-radius: 14px;
  }

  .file-row {
    flex-direction: column;
    align-items: stretch;
    gap: 9px;
    padding: 11px 12px;
  }

  .fr-main {
    width: 100%;
  }

  .fr-name > span {
    white-space: normal;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    word-break: break-word;
  }

  .fr-sub {
    white-space: normal;
    word-break: break-word;
    line-height: 1.38;
  }

  .fr-acts {
    width: 100%;
    padding-top: 8px;
    border-top: 1px solid var(--border-hairline);
    justify-content: flex-end;
  }
}

/* Modals */
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 250;
  background: rgba(15, 23, 42, 0.48);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  padding: 18px;
}

.rename-box {
  width: min(380px, 100%);
  padding: 20px;
  border-radius: 22px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rn-title {
  font-size: 0.95rem;
  font-weight: 650;
}

.rn-sub {
  font-size: 0.74rem;
  color: var(--ink-2);
}

.rn-warn {
  padding: 8px 10px;
  border-radius: 9px;
  font-size: 0.76rem;
  line-height: 1.45;
  background: var(--danger-bg);
  color: var(--danger-fg);
}

.rn-input {
  width: 100%;
  padding: 8px 11px;
  border-radius: 11px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.84rem;
  outline: none;
}

.rn-btns {
  margin-top: 4px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.preview-dialog {
  width: min(920px, 100%);
  max-height: 90dvh;
  padding: 16px;
  border-radius: 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  position: relative;
}

.pd-close {
  position: absolute;
  top: 14px;
  right: 14px;
  z-index: 2;
}

.pd-stage {
  flex: 1;
  min-height: 0;
  display: grid;
  place-items: center;
  overflow: hidden;
}

.pd-stage img,
.pd-stage video {
  max-width: 100%;
  max-height: 70dvh;
  border-radius: 14px;
  object-fit: contain;
}

.pd-stage audio {
  width: min(440px, 100%);
}

.pd-pdf {
  width: 100%;
  height: 70dvh;
  border: none;
  border-radius: 12px;
}

.pd-fallback {
  padding: 48px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  color: var(--ink-2);
}

.pd-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 0.76rem;
  color: var(--ink-2);
}

.pd-spec {
  word-break: break-word;
}

.pd-acts {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.up-fold-enter-active,
.up-fold-leave-active {
  transition: all 0.22s var(--ease-spring);
}
.up-fold-enter-from,
.up-fold-leave-to {
  opacity: 0;
  transform: translateY(-6px) scale(0.98);
}
</style>
