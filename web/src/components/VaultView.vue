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
import VaultCopyModal, { type VaultTransferSource } from './VaultCopyModal.vue'

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

// Folder creation & deletion states
const showNewFolderBar = ref(false)
const siloNewPrefix = ref<'images/' | 'drop/' | ''>('images/')
const newFolderName = ref('')
const creatingFolderBusy = ref(false)
const deletingFolderBusy = ref(false)
const folderDeleteConfirm = ref<{
  pool: 'silo' | 'gdrive'
  folder: string
  fileCount: number
  totalBytes: number
} | null>(null)

// Cross-pool / cross-folder copy & move modal state
const transferModal = ref<{
  items: VaultTransferSource[]
  mode: 'copy' | 'move'
} | null>(null)

// Multi-select batch mode state
const selectMode = ref(false)
const selectedKeys = ref<Set<string>>(new Set())
const batchDeleteConfirmOpen = ref(false)
const batchDeleting = ref(false)
const batchDeleteDone = ref(0)

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
  transferSource?: VaultTransferSource
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

// Semantic classification helpers for Silo folders & objects
function classifyFolderName(rawFolder: string): SiloCategory {
  const f = (rawFolder || '/').replace(/^\/+|\/+$/g, '')
  if (!f || f === '/') return 'other'
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
    f.startsWith('drop/')
  ) {
    return 'imghost'
  }
  return 'other'
}

function classifySiloCategory(obj: VaultObject): SiloCategory {
  const byDir = classifyFolderName(obj.folder || '/')
  if (byDir !== 'other') return byDir
  if (obj.kind === 'video') return 'imghost'
  return 'other'
}

function isProtectedSiloFolder(rawFolder: string): boolean {
  const cat = classifyFolderName(rawFolder)
  return cat === 'blog' || cat === 'music'
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

const customOtherFolders = computed(() => {
  const set = new Set<string>()
  for (const f of store.siloFolders.value) {
    const clean = (f || '').replace(/^\/+|\/+$/g, '')
    if (!clean || clean === '/' || clean.endsWith('/thumbs') || clean === 'thumbs') continue
    if (classifyFolderName(clean) === 'other') set.add(clean)
  }
  for (const it of store.siloObjects.value) {
    if (classifySiloCategory(it) === 'other' && it.folder && it.folder !== '/') {
      set.add(it.folder)
    }
  }
  return Array.from(set).sort((a, b) => a.localeCompare(b))
})

const customImghostSubfolders = computed(() => {
  const standard = new Set(['images', 'videos', 'drop'])
  const set = new Set<string>()
  for (const f of store.siloFolders.value) {
    const clean = (f || '').replace(/^\/+|\/+$/g, '')
    if (!clean || standard.has(clean) || clean.endsWith('/thumbs')) continue
    if (classifyFolderName(clean) === 'imghost') set.add(clean)
  }
  return Array.from(set).sort((a, b) => a.localeCompare(b))
})

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
      if (it.folder !== 'music/covers/tracks' && it.folder !== 'music/lyrics') music++
    } else other++
  }
  return { imghost, blog, music, other }
})

const activeCategoryDesc = computed(() => {
  switch (siloCategory.value) {
    case 'imghost':
      return '图床与快传区（公网只读 CDN 加速 · 支持新建/删除子目录与跨容器复制移动）'
    case 'blog':
      return 'Aurora 博客生产目录（关联线上文章、相册、说说与主题配置 · 默认加锁防误删）'
    case 'music':
      return '博客自托管曲库（音频、歌单封面、单曲封面与 .lrc 滚动歌词）'
    case 'other':
      return '其他自定义存储目录（支持自由增删与文件归档）'
    default:
      return 'Silo 对象存储全览（点击上方分类展开具体文件夹用途，也可新建自定义文件夹）'
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
    for (const sub of customImghostSubfolders.value) {
      const cnt = all.filter((x) => x.folder === sub || x.folder.startsWith(`${sub}/`)).length
      list.push({
        key: `exact:${sub}`,
        label: sub.split('/').slice(1).join('/') || sub,
        pathHint: sub,
        desc: `自定义子文件夹 ${sub}`,
        count: cnt,
      })
    }
    if (otherImgHost > 0) {
      list.push({
        key: 'tag:other-imghost',
        label: '其他附件',
        pathHint: 'images/*',
        desc: '图床目录内其他格式文件',
        count: otherImgHost,
      })
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
    return [
      { key: 'all', label: '全部其他目录', desc: '未归入标准分类的目录', count: inCat.length },
      ...customOtherFolders.value.map((d) => ({
        key: `exact:${d}`,
        label: d,
        desc: '自定义目录',
        count: inCat.filter((x) => x.folder === d || x.folder.startsWith(`${d}/`)).length,
      })),
    ]
  }

  return []
})

// Currently selected deletable folder (never allows deleting root or protected blog/music folders)
const deletableSiloFolder = computed<string>(() => {
  if (activePool.value !== 'silo') return ''
  const sub = siloSubTag.value
  if (sub === 'dir:drop') return 'drop'
  if (sub.startsWith('exact:')) {
    const folder = sub.slice(6)
    if (folder && folder !== '/' && !isProtectedSiloFolder(folder)) {
      return folder
    }
  }
  return ''
})

const deletableDriveFolder = computed<string>(() => {
  if (activePool.value !== 'gdrive') return ''
  if (driveFolderFilter.value !== 'all' && driveFolderFilter.value !== '/') {
    return driveFolderFilter.value
  }
  return ''
})

// Switch storage pool WITHOUT ever mutating vaultViewMode!
async function switchPool(key: PoolKey, enabled: boolean) {
  if (!enabled) {
    showToast(key === 'gdrive' ? '尚未配置 Google Drive' : '尚未连接 Silo')
    return
  }
  activePool.value = key
  showNewFolderBar.value = false
  newFolderName.value = ''
  clearSelection()
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

async function submitCreateFolder() {
  const raw = newFolderName.value.trim().replace(/^\/+|\/+$/g, '')
  if (!raw || creatingFolderBusy.value) return
  creatingFolderBusy.value = true
  try {
    if (activePool.value === 'silo') {
      const full = `${siloNewPrefix.value}${raw}`.replace(/\/+/g, '/').replace(/^\/+|\/+$/g, '')
      const created = await store.createSiloFolder(full)
      if (created) {
        newFolderName.value = ''
        showNewFolderBar.value = false
        const cat = classifyFolderName(created)
        siloCategory.value = cat
        siloSubTag.value = `exact:${created}`
      }
    } else if (activePool.value === 'gdrive') {
      const created = await store.createGDriveFolder(raw)
      if (created) {
        newFolderName.value = ''
        showNewFolderBar.value = false
        driveFolderFilter.value = created
      }
    }
  } finally {
    creatingFolderBusy.value = false
  }
}

async function requestDeleteActiveFolder() {
  if (deletingFolderBusy.value) return
  if (activePool.value === 'silo' && deletableSiloFolder.value) {
    const target = deletableSiloFolder.value
    deletingFolderBusy.value = true
    try {
      const res = await store.deleteSiloFolder(target, false)
      if (res.ok) {
        siloSubTag.value = 'all'
      } else if (res.nonEmpty) {
        folderDeleteConfirm.value = {
          pool: 'silo',
          folder: target,
          fileCount: res.fileCount || 0,
          totalBytes: res.totalBytes || 0,
        }
      }
    } finally {
      deletingFolderBusy.value = false
    }
  } else if (activePool.value === 'gdrive' && deletableDriveFolder.value) {
    const target = deletableDriveFolder.value
    deletingFolderBusy.value = true
    try {
      const res = await store.deleteGDriveFolder(target, false)
      if (res.ok) {
        driveFolderFilter.value = 'all'
      } else if (res.nonEmpty) {
        folderDeleteConfirm.value = {
          pool: 'gdrive',
          folder: target,
          fileCount: res.fileCount || 0,
          totalBytes: 0,
        }
      }
    } finally {
      deletingFolderBusy.value = false
    }
  }
}

async function confirmForceDeleteFolder() {
  const pending = folderDeleteConfirm.value
  if (!pending || deletingFolderBusy.value) return
  deletingFolderBusy.value = true
  try {
    if (pending.pool === 'silo') {
      const res = await store.deleteSiloFolder(pending.folder, true)
      if (res.ok) {
        folderDeleteConfirm.value = null
        siloSubTag.value = 'all'
      }
    } else {
      const res = await store.deleteGDriveFolder(pending.folder, true)
      if (res.ok) {
        folderDeleteConfirm.value = null
        driveFolderFilter.value = 'all'
      }
    }
  } finally {
    deletingFolderBusy.value = false
  }
}

function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (previewObj.value) previewObj.value = null
    else if (renameTarget.value && !renamingBusy.value) renameTarget.value = null
    else if (folderDeleteConfirm.value && !deletingFolderBusy.value) folderDeleteConfirm.value = null
    else if (batchDeleteConfirmOpen.value && !batchDeleting.value) batchDeleteConfirmOpen.value = false
    else if (selectMode.value && !batchDeleting.value) exitSelectMode()
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
        if (!(it.folder === p || it.folder.startsWith(`${p}/`))) return false
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

// Normalized current visible items for multi-select operations
const visibleTransferItems = computed<VaultTransferSource[]>(() => {
  if (activePool.value === 'silo') {
    return filteredSilo.value.map((obj) => ({
      storage: 'silo',
      key: obj.key,
      name: obj.name,
      folder: obj.folder,
      size: obj.size,
      protected: obj.protected,
    }))
  }
  if (activePool.value === 'gdrive') {
    return filteredDrive.value.map((gd) => ({
      storage: 'gdrive',
      key: gd.path,
      name: gd.name,
      folder: gd.folder,
      size: gd.size,
    }))
  }
  return localFileClips.value.map((lc) => ({
    storage: 'local',
    key: lc.id,
    name: lc.fileName || '互传文件',
    size: lc.size || 0,
  }))
})

function selToken(storage: PoolKey, key: string): string {
  return `${storage}:${key}`
}

function isItemSelected(storage: PoolKey, key: string): boolean {
  return selectedKeys.value.has(selToken(storage, key))
}

function toggleSelect(storage: PoolKey, key: string) {
  const tok = selToken(storage, key)
  const next = new Set(selectedKeys.value)
  if (next.has(tok)) next.delete(tok)
  else next.add(tok)
  selectedKeys.value = next
}

function clearSelection() {
  selectedKeys.value = new Set()
}

function toggleSelectMode() {
  if (selectMode.value) {
    exitSelectMode()
  } else {
    selectMode.value = true
  }
}

function exitSelectMode() {
  selectMode.value = false
  clearSelection()
}

const allVisibleSelected = computed(() => {
  if (visibleTransferItems.value.length === 0) return false
  return visibleTransferItems.value.every((it) =>
    selectedKeys.value.has(selToken(it.storage, it.key)),
  )
})

function toggleSelectAllVisible() {
  const next = new Set(selectedKeys.value)
  if (allVisibleSelected.value) {
    for (const it of visibleTransferItems.value) {
      next.delete(selToken(it.storage, it.key))
    }
  } else {
    for (const it of visibleTransferItems.value) {
      next.add(selToken(it.storage, it.key))
    }
  }
  selectedKeys.value = next
}

const selectedTransferItems = computed<VaultTransferSource[]>(() => {
  const res: VaultTransferSource[] = []
  for (const obj of store.siloObjects.value) {
    if (selectedKeys.value.has(selToken('silo', obj.key))) {
      res.push({
        storage: 'silo',
        key: obj.key,
        name: obj.name,
        folder: obj.folder,
        size: obj.size,
        protected: obj.protected,
      })
    }
  }
  for (const gd of store.gdriveFiles.value) {
    if (selectedKeys.value.has(selToken('gdrive', gd.path))) {
      res.push({
        storage: 'gdrive',
        key: gd.path,
        name: gd.name,
        folder: gd.folder,
        size: gd.size,
      })
    }
  }
  for (const lc of store.clips.value) {
    if (selectedKeys.value.has(selToken('local', lc.id))) {
      res.push({
        storage: 'local',
        key: lc.id,
        name: lc.fileName || '互传文件',
        size: lc.size || 0,
      })
    }
  }
  return res
})

const selectedTotalBytes = computed(() =>
  selectedTransferItems.value.reduce((acc, x) => acc + (x.size || 0), 0),
)

const selectedProtectedCount = computed(
  () => selectedTransferItems.value.filter((x) => Boolean(x.protected)).length,
)

function openSingleTransfer(src: VaultTransferSource, mode: 'copy' | 'move' = 'copy') {
  transferModal.value = { items: [src], mode }
}

function openBatchTransfer(mode: 'copy' | 'move') {
  if (selectedTransferItems.value.length === 0) return
  transferModal.value = { items: [...selectedTransferItems.value], mode }
}

function onTransferCompleted(payload: { mode: 'copy' | 'move'; succeeded: number }) {
  transferModal.value = null
  if (payload.mode === 'move' && selectMode.value) {
    exitSelectMode()
  }
}

async function executeBatchDelete() {
  const list = [...selectedTransferItems.value]
  if (list.length === 0 || batchDeleting.value) return
  batchDeleting.value = true
  batchDeleteDone.value = 0
  let succeeded = 0

  try {
    for (const it of list) {
      if (it.storage === 'silo') {
        const res = await fetch(
          `/api/silo/object?key=${encodeURIComponent(it.key)}&force=${it.protected ? '1' : '0'}`,
          { method: 'DELETE' },
        )
        if (res.ok) {
          store.siloObjects.value = store.siloObjects.value.filter((x) => x.key !== it.key)
          succeeded++
        }
      } else if (it.storage === 'gdrive') {
        const res = await fetch(`/api/gdrive/file?path=${encodeURIComponent(it.key)}`, {
          method: 'DELETE',
        })
        if (res.ok) {
          store.gdriveFiles.value = store.gdriveFiles.value.filter((x) => x.path !== it.key)
          succeeded++
        }
      } else {
        const res = await fetch(`/api/clips/${it.key}`, { method: 'DELETE' })
        if (res.ok) {
          store.clips.value = store.clips.value.filter((c) => c.id !== it.key)
          succeeded++
        }
      }
      batchDeleteDone.value++
    }
    showToast(`已批量删除 ${succeeded} 个文件`)
    batchDeleteConfirmOpen.value = false
    exitSelectMode()
    if (list.some((x) => x.storage === 'silo')) {
      void store.loadMedia(true)
    }
  } finally {
    batchDeleting.value = false
  }
}

function resolveUploadSiloPrefix(): string {
  if (siloSubTag.value.startsWith('exact:')) {
    return `${siloSubTag.value.slice(6)}/`
  }
  if (siloCategory.value === 'imghost') {
    if (siloSubTag.value === 'dir:drop') return 'drop/'
    if (siloSubTag.value === 'tag:video') return 'videos/'
    return 'images/'
  }
  if (siloCategory.value === 'blog' && siloSubTag.value.startsWith('dir:')) {
    return `${siloSubTag.value.slice(4)}/`
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

function handleSiloCardClick(item: VaultObject) {
  if (selectMode.value) {
    toggleSelect('silo', item.key)
    return
  }
  openPreviewSilo(item)
}

function handleDriveCardClick(gd: GDriveFile) {
  if (selectMode.value) {
    toggleSelect('gdrive', gd.path)
    return
  }
  openPreviewDrive(gd)
}

function handleLocalCardClick(lc: ClipItem) {
  if (selectMode.value) {
    toggleSelect('local', lc.id)
    return
  }
  openPreviewLocal(lc)
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
    transferSource: {
      storage: 'silo',
      key: item.key,
      name: item.name,
      folder: item.folder,
      size: item.size,
      protected: item.protected,
    },
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
    transferSource: {
      storage: 'gdrive',
      key: gd.path,
      name: gd.name,
      folder: gd.folder,
      size: gd.size,
    },
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
    transferSource: {
      storage: 'local',
      key: lc.id,
      name: lc.fileName || '文件',
      size: lc.size || 0,
    },
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
  <div class="vault-page" :class="{ 'has-batch-dock': selectMode && selectedTransferItems.length > 0 }">
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
          <button
            type="button"
            class="pill-btn"
            :class="{ 'pill-btn--solid': selectMode }"
            @click="toggleSelectMode"
          >
            <UiIcon name="check-square" :size="14" />
            <span>{{ selectMode ? '退出多选' : '多选' }}</span>
          </button>

          <button
            v-if="selectMode && visibleTransferItems.length > 0"
            type="button"
            class="pill-btn"
            @click="toggleSelectAllVisible"
          >
            <span>{{ allVisibleSelected ? '取消全选' : `全选 (${visibleTransferItems.length})` }}</span>
          </button>

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

      <!-- Pool-Specific Semantic Info & Directory Filter Bar -->
      <Transition name="info-swap" mode="out-in">
        <!-- 1. SILO TWO-LEVEL SEMANTIC FOLDER GUIDE + FOLDER CRUD -->
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
                v-if="siloCategoryCounts.other > 0 || customOtherFolders.length > 0"
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

            <div class="pool-head-right">
              <button
                v-if="deletableSiloFolder"
                type="button"
                class="folder-del-btn"
                :disabled="deletingFolderBusy"
                @click="requestDeleteActiveFolder"
              >
                <UiIcon
                  :name="deletingFolderBusy ? 'spinner' : 'trash'"
                  :size="12.5"
                  :class="{ 'spin-anim': deletingFolderBusy }"
                />
                <span>删除目录 {{ deletableSiloFolder }}</span>
              </button>

              <button
                type="button"
                class="folder-add-btn"
                :class="{ 'is-open': showNewFolderBar }"
                @click="showNewFolderBar = !showNewFolderBar"
              >
                <UiIcon name="folder-plus" :size="13.5" />
                <span>新建文件夹</span>
              </button>

              <span class="pool-stat mono">{{ activeCount }} 项 · {{ formatBytes(activeTotalBytes) }}</span>
            </div>
          </div>

          <div class="cat-desc-line">{{ activeCategoryDesc }}</div>

          <!-- Inline Silo New Folder Creator -->
          <Transition name="sub-drawer">
            <form v-if="showNewFolderBar" class="new-folder-bar" @submit.prevent="submitCreateFolder">
              <div class="nf-prefixes">
                <button
                  v-for="pf in [
                    { v: 'images/' as const, l: '图床 images/' },
                    { v: 'drop/' as const, l: '快传 drop/' },
                    { v: '' as const, l: '自定义根目录' },
                  ]"
                  :key="pf.l"
                  type="button"
                  class="nf-chip mono"
                  :class="{ 'is-on': siloNewPrefix === pf.v }"
                  @click="siloNewPrefix = pf.v"
                >
                  {{ pf.l }}
                </button>
              </div>
              <div class="nf-input-row">
                <span v-if="siloNewPrefix" class="nf-prefix-tag mono">{{ siloNewPrefix }}</span>
                <input
                  v-model="newFolderName"
                  class="nf-input mono"
                  :placeholder="siloNewPrefix ? '输入子目录名（如 memes 或 2026）' : '输入目录路径（如 archive/docs）'"
                  :disabled="creatingFolderBusy"
                />
                <button
                  type="submit"
                  class="pill-btn pill-btn--solid pill-btn--mini"
                  :disabled="creatingFolderBusy || !newFolderName.trim()"
                >
                  <UiIcon v-if="creatingFolderBusy" name="spinner" :size="12.5" class="spin-anim" />
                  <span>{{ creatingFolderBusy ? '创建中…' : '创建目录' }}</span>
                </button>
              </div>
            </form>
          </Transition>

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

        <!-- 2. GOOGLE DRIVE SUBFOLDER & INFO BAR + FOLDER CRUD -->
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

            <div class="pool-head-right">
              <button
                v-if="deletableDriveFolder"
                type="button"
                class="folder-del-btn"
                :disabled="deletingFolderBusy"
                @click="requestDeleteActiveFolder"
              >
                <UiIcon
                  :name="deletingFolderBusy ? 'spinner' : 'trash'"
                  :size="12.5"
                  :class="{ 'spin-anim': deletingFolderBusy }"
                />
                <span>删除目录 {{ deletableDriveFolder }}</span>
              </button>

              <button
                type="button"
                class="folder-add-btn"
                :class="{ 'is-open': showNewFolderBar }"
                @click="showNewFolderBar = !showNewFolderBar"
              >
                <UiIcon name="folder-plus" :size="13.5" />
                <span>新建文件夹</span>
              </button>

              <span class="pool-stat mono">{{ activeCount }} 项 · {{ formatBytes(activeTotalBytes) }}</span>
            </div>
          </div>

          <div class="cat-desc-line">
            Google Drive 隔离归档目录（<span class="mono">{{ store.status.value?.gdriveTarget || 'gdrive:OArc-Drop' }}</span> · 支持新建/删除子文件夹与跨容器复制移动）
          </div>

          <Transition name="sub-drawer">
            <form v-if="showNewFolderBar" class="new-folder-bar" @submit.prevent="submitCreateFolder">
              <div class="nf-input-row">
                <span class="nf-prefix-tag mono">OArc-Drop/</span>
                <input
                  v-model="newFolderName"
                  class="nf-input"
                  placeholder="输入要新建的 Drive 子文件夹名（如 2026-资料）"
                  :disabled="creatingFolderBusy"
                />
                <button
                  type="submit"
                  class="pill-btn pill-btn--solid pill-btn--mini"
                  :disabled="creatingFolderBusy || !newFolderName.trim()"
                >
                  <UiIcon v-if="creatingFolderBusy" name="spinner" :size="12.5" class="spin-anim" />
                  <span>{{ creatingFolderBusy ? '创建中…' : '创建目录' }}</span>
                </button>
              </div>
            </form>
          </Transition>
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
            本机私密互传池（仅经过 Cloudflare Access 验证的本人设备可见 · 支持一键复制或移动到 Silo / Drive 任意具体文件夹）
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
            :class="{ 'is-selected': isItemSelected('silo', obj.key) }"
            @click="handleSiloCardClick(obj)"
          >
            <button
              v-if="selectMode"
              type="button"
              class="sel-badge"
              :class="{ 'is-checked': isItemSelected('silo', obj.key) }"
              @click.stop="toggleSelect('silo', obj.key)"
            >
              <UiIcon v-if="isItemSelected('silo', obj.key)" name="check" :size="12.5" />
            </button>

            <img v-if="obj.thumbUrl" :src="obj.thumbUrl" :alt="obj.name" loading="lazy" />
            <div v-else class="non-img-tile">
              <UiIcon :name="iconForKind(obj.kind)" :size="28" />
              <span class="ext-tag mono">{{ obj.ext || 'FILE' }}</span>
            </div>

            <button
              v-if="obj.kind === 'audio' && !selectMode"
              type="button"
              class="tile-audio-badge mono"
              title="送入音乐厅唱机播放"
              @click.stop="playSiloAudioInDeck(obj)"
            >
              <UiIcon name="play" :size="10" />
              <span>{{ obj.ext }}</span>
            </button>

            <div class="tile-scrim" @click.stop>
              <div class="tile-info" @click="handleSiloCardClick(obj)">
                <div class="tile-name">
                  <UiIcon v-if="obj.protected" name="lock" :size="11" class="inline-lock" />
                  <span>{{ obj.name }}</span>
                </div>
                <div class="tile-meta">{{ describeSiloFolder(obj.folder) }} · {{ formatBytes(obj.size) }}</div>
              </div>

              <div v-if="!selectMode" class="tile-btns">
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
                <button
                  type="button"
                  class="scrim-circle"
                  title="复制或移动到其他容器/文件夹"
                  @click="
                    openSingleTransfer({
                      storage: 'silo',
                      key: obj.key,
                      name: obj.name,
                      folder: obj.folder,
                      size: obj.size,
                      protected: obj.protected,
                    })
                  "
                >
                  <UiIcon name="move" :size="12.5" />
                </button>
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

        <!-- Unified List Mode -->
        <div v-else class="file-list">
          <div
            v-for="obj in filteredSilo"
            :key="obj.key"
            class="glass-box file-row"
            :class="{ 'is-selected': isItemSelected('silo', obj.key) }"
          >
            <div class="fr-main" @click="handleSiloCardClick(obj)">
              <button
                v-if="selectMode"
                type="button"
                class="sel-check-inline"
                :class="{ 'is-checked': isItemSelected('silo', obj.key) }"
                @click.stop="toggleSelect('silo', obj.key)"
              >
                <UiIcon v-if="isItemSelected('silo', obj.key)" name="check" :size="12" />
              </button>

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

            <div v-if="!selectMode" class="fr-acts">
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
              <button
                type="button"
                class="mini-act"
                title="复制或移动到其他容器/文件夹"
                @click="
                  openSingleTransfer({
                    storage: 'silo',
                    key: obj.key,
                    name: obj.name,
                    folder: obj.folder,
                    size: obj.size,
                    protected: obj.protected,
                  })
                "
              >
                <UiIcon name="move" :size="14" />
              </button>
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
            :class="{ 'is-selected': isItemSelected('gdrive', gd.path) }"
            @click="handleDriveCardClick(gd)"
          >
            <button
              v-if="selectMode"
              type="button"
              class="sel-badge"
              :class="{ 'is-checked': isItemSelected('gdrive', gd.path) }"
              @click.stop="toggleSelect('gdrive', gd.path)"
            >
              <UiIcon v-if="isItemSelected('gdrive', gd.path)" name="check" :size="12.5" />
            </button>

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
              <div class="tile-info" @click="handleDriveCardClick(gd)">
                <div class="tile-name">
                  <span>{{ gd.name }}</span>
                </div>
                <div class="tile-meta mono">
                  {{ gd.folder === '/' ? 'OArc-Drop/' : `OArc-Drop/${gd.folder}` }} · {{ formatBytes(gd.size) }}
                </div>
              </div>

              <div v-if="!selectMode" class="tile-btns">
                <button
                  type="button"
                  class="scrim-circle"
                  title="复制或移动到其他容器/文件夹"
                  @click="
                    openSingleTransfer({
                      storage: 'gdrive',
                      key: gd.path,
                      name: gd.name,
                      folder: gd.folder,
                      size: gd.size,
                    })
                  "
                >
                  <UiIcon name="move" :size="12.5" />
                </button>
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
          <div
            v-for="gd in filteredDrive"
            :key="gd.path"
            class="glass-box file-row"
            :class="{ 'is-selected': isItemSelected('gdrive', gd.path) }"
          >
            <div class="fr-main" @click="handleDriveCardClick(gd)">
              <button
                v-if="selectMode"
                type="button"
                class="sel-check-inline"
                :class="{ 'is-checked': isItemSelected('gdrive', gd.path) }"
                @click.stop="toggleSelect('gdrive', gd.path)"
              >
                <UiIcon v-if="isItemSelected('gdrive', gd.path)" name="check" :size="12" />
              </button>

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
            <div v-if="!selectMode" class="fr-acts">
              <button
                type="button"
                class="pill-btn pill-btn--mini"
                @click="
                  openSingleTransfer({
                    storage: 'gdrive',
                    key: gd.path,
                    name: gd.name,
                    folder: gd.folder,
                    size: gd.size,
                  })
                "
              >
                <UiIcon name="move" :size="13.5" />
                <span>复制/移动</span>
              </button>
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
            :class="{ 'is-selected': isItemSelected('local', lc.id) }"
            @click="handleLocalCardClick(lc)"
          >
            <button
              v-if="selectMode"
              type="button"
              class="sel-badge"
              :class="{ 'is-checked': isItemSelected('local', lc.id) }"
              @click.stop="toggleSelect('local', lc.id)"
            >
              <UiIcon v-if="isItemSelected('local', lc.id)" name="check" :size="12.5" />
            </button>

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
              <div class="tile-info" @click="handleLocalCardClick(lc)">
                <div class="tile-name">
                  <span>{{ lc.fileName }}</span>
                </div>
                <div class="tile-meta mono">私密互传 · {{ formatBytes(lc.size) }}</div>
              </div>

              <div v-if="!selectMode" class="tile-btns">
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
                <button
                  type="button"
                  class="scrim-circle"
                  title="复制或移动到指定文件夹"
                  @click="
                    openSingleTransfer({
                      storage: 'local',
                      key: lc.id,
                      name: lc.fileName || '文件',
                      size: lc.size || 0,
                    })
                  "
                >
                  <UiIcon name="move" :size="12.5" />
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
          <div
            v-for="lc in localFileClips"
            :key="lc.id"
            class="glass-box file-row"
            :class="{ 'is-selected': isItemSelected('local', lc.id) }"
          >
            <div class="fr-main" @click="handleLocalCardClick(lc)">
              <button
                v-if="selectMode"
                type="button"
                class="sel-check-inline"
                :class="{ 'is-checked': isItemSelected('local', lc.id) }"
                @click.stop="toggleSelect('local', lc.id)"
              >
                <UiIcon v-if="isItemSelected('local', lc.id)" name="check" :size="12" />
              </button>

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
            <div v-if="!selectMode" class="fr-acts">
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

              <button
                type="button"
                class="pill-btn pill-btn--mini"
                @click="
                  openSingleTransfer({
                    storage: 'local',
                    key: lc.id,
                    name: lc.fileName || '文件',
                    size: lc.size || 0,
                  })
                "
              >
                <UiIcon name="move" :size="13.5" />
                <span>复制/移动</span>
              </button>

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

    <!-- Floating Multi-Select Batch Action Dock -->
    <Teleport to="body">
      <Transition name="dock-pop">
        <div
          v-if="selectMode && selectedTransferItems.length > 0"
          class="liquid-chrome batch-dock"
          :class="{ 'is-above-player': Boolean(store.currentTrack.value) }"
        >
          <div class="bd-info">
            <span class="bd-cnt">已选 {{ selectedTransferItems.length }} 项</span>
            <span class="bd-bytes mono">{{ formatBytes(selectedTotalBytes) }}</span>
          </div>

          <div class="bd-acts">
            <button type="button" class="pill-btn pill-btn--mini" @click="openBatchTransfer('copy')">
              <UiIcon name="copy" :size="13" />
              <span>复制到…</span>
            </button>
            <button type="button" class="pill-btn pill-btn--mini" @click="openBatchTransfer('move')">
              <UiIcon name="move" :size="13.5" />
              <span>移动到…</span>
            </button>
            <button
              type="button"
              class="pill-btn pill-btn--mini bd-danger-btn"
              @click="batchDeleteConfirmOpen = true"
            >
              <UiIcon name="trash" :size="13" />
              <span>批量删除</span>
            </button>
            <button type="button" class="icon-circle-btn bd-close" title="取消选择" @click="exitSelectMode">
              <UiIcon name="close" :size="13.5" />
            </button>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Cross-Pool / Cross-Folder Copy & Move Modal -->
    <VaultCopyModal
      v-if="transferModal"
      :items="transferModal.items"
      :initial-mode="transferModal.mode"
      @close="transferModal = null"
      @completed="onTransferCompleted"
    />

    <!-- Non-Empty Folder Deletion Confirmation Modal -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div
          v-if="folderDeleteConfirm"
          class="modal-backdrop"
          @click.self="!deletingFolderBusy && (folderDeleteConfirm = null)"
        >
          <div class="liquid-chrome rename-box">
            <div class="rn-title">删除非空文件夹</div>
            <div class="rn-sub mono">
              {{ folderDeleteConfirm.pool === 'silo' ? `Silo · ${folderDeleteConfirm.folder}` : `Drive · OArc-Drop/${folderDeleteConfirm.folder}` }}
            </div>
            <div class="rn-warn">
              该文件夹内仍有 <strong>{{ folderDeleteConfirm.fileCount }}</strong> 个文件
              <template v-if="folderDeleteConfirm.totalBytes > 0">
                （合计 {{ formatBytes(folderDeleteConfirm.totalBytes) }}）
              </template>
              ，确认删除将连同文件夹内所有内容一并永久移除。
            </div>
            <div class="rn-btns">
              <button
                type="button"
                class="pill-btn"
                :disabled="deletingFolderBusy"
                @click="folderDeleteConfirm = null"
              >
                取消
              </button>
              <button
                type="button"
                class="pill-btn bd-danger-solid"
                :disabled="deletingFolderBusy"
                @click="confirmForceDeleteFolder"
              >
                <UiIcon v-if="deletingFolderBusy" name="spinner" :size="13.5" class="spin-anim" />
                <span>{{ deletingFolderBusy ? '正在清空删除…' : '强制清空并删除' }}</span>
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- Batch Multi-Delete Confirmation Modal -->
    <Teleport to="body">
      <Transition name="lens-modal">
        <div
          v-if="batchDeleteConfirmOpen"
          class="modal-backdrop"
          @click.self="!batchDeleting && (batchDeleteConfirmOpen = false)"
        >
          <div class="liquid-chrome rename-box">
            <div class="rn-title">确认批量删除 {{ selectedTransferItems.length }} 个文件？</div>
            <div class="rn-sub mono">合计大小：{{ formatBytes(selectedTotalBytes) }}</div>
            <div v-if="selectedProtectedCount > 0" class="rn-warn">
              注意：其中包含 {{ selectedProtectedCount }} 个博客或音乐加锁生产文件，删除后对应公网资源将失效。
            </div>
            <div v-if="batchDeleting" class="rn-sub mono">
              正在删除 {{ batchDeleteDone }} / {{ selectedTransferItems.length }}…
            </div>
            <div class="rn-btns">
              <button
                type="button"
                class="pill-btn"
                :disabled="batchDeleting"
                @click="batchDeleteConfirmOpen = false"
              >
                取消
              </button>
              <button
                type="button"
                class="pill-btn bd-danger-solid"
                :disabled="batchDeleting"
                @click="executeBatchDelete"
              >
                <UiIcon v-if="batchDeleting" name="spinner" :size="13.5" class="spin-anim" />
                <span>{{ batchDeleting ? '批量删除中…' : '确认永久删除' }}</span>
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

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
                  v-if="previewObj.transferSource"
                  type="button"
                  class="pill-btn"
                  @click="
                    openSingleTransfer(previewObj.transferSource);
                    previewObj = null
                  "
                >
                  <UiIcon name="move" :size="14" />
                  <span>复制 / 移动…</span>
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
.vault-page.has-batch-dock {
  padding-bottom: 72px;
}

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
  flex-wrap: wrap;
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

.pool-head-right {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.folder-add-btn {
  height: 28px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-1);
  font-size: 0.74rem;
  font-weight: 590;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
  transition: all 0.16s ease;
}

.folder-add-btn:hover,
.folder-add-btn.is-open {
  background: var(--accent-soft);
  border-color: var(--accent-border);
}

.folder-del-btn {
  height: 28px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid rgba(220, 38, 38, 0.28);
  background: var(--danger-bg);
  color: var(--danger-fg);
  font-size: 0.73rem;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
}

.new-folder-bar {
  padding-top: 8px;
  border-top: 1px dashed var(--border-hairline);
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.nf-prefixes {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}

.nf-chip {
  height: 25px;
  padding: 0 9px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.7rem;
  cursor: pointer;
}

.nf-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.nf-input-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.nf-prefix-tag {
  font-size: 0.74rem;
  color: var(--ink-3);
  padding-left: 4px;
  flex-shrink: 0;
}

.nf-input {
  flex: 1;
  min-width: 0;
  height: 31px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.78rem;
  outline: none;
}

.nf-input:focus {
  background: var(--surface-pill);
  border-color: var(--border-strong);
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
  transition: transform 0.18s var(--ease-spring), box-shadow 0.18s ease, border-color 0.18s ease;
}

.lens-tile.is-selected {
  border-color: var(--cta-bg);
  box-shadow: 0 0 0 2px var(--cta-bg);
}

.sel-badge {
  position: absolute;
  top: 9px;
  left: 9px;
  z-index: 4;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  border: 1.5px solid rgba(255, 255, 255, 0.85);
  background: rgba(11, 13, 17, 0.45);
  -webkit-backdrop-filter: blur(6px);
  backdrop-filter: blur(6px);
  color: #fff;
  display: grid;
  place-items: center;
  padding: 0;
  cursor: pointer;
}

.sel-badge.is-checked {
  background: var(--emerald-fg);
  border-color: #fff;
}

.sel-check-inline {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  border: 1.5px solid var(--border-strong);
  background: var(--surface-recessed);
  color: var(--cta-fg);
  display: grid;
  place-items: center;
  padding: 0;
  flex-shrink: 0;
  cursor: pointer;
}

.sel-check-inline.is-checked {
  background: var(--cta-bg);
  border-color: var(--cta-bg);
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

/* Unified List Rows */
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
  transition: border-color 0.16s ease, box-shadow 0.16s ease;
}

.file-row.is-selected {
  border-color: var(--cta-bg);
  box-shadow: 0 0 0 1.5px var(--cta-bg);
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

/* Floating Batch Action Dock */
.batch-dock {
  position: fixed;
  left: 50%;
  transform: translateX(-50%);
  bottom: max(22px, calc(env(safe-area-inset-bottom, 0px) + 16px));
  z-index: 95;
  border-radius: 16px;
  padding: 9px 13px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px 16px;
  max-width: calc(100vw - 24px);
}

.bd-info {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.bd-cnt {
  font-size: 0.82rem;
  font-weight: 660;
  color: var(--ink-1);
}

.bd-bytes {
  font-size: 0.72rem;
  color: var(--ink-3);
}

.bd-acts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.bd-danger-btn {
  color: var(--danger-fg);
  border-color: rgba(220, 38, 38, 0.28);
  background: var(--danger-bg);
}

.bd-danger-solid {
  background: var(--danger-solid);
  color: #fff;
  border-color: transparent;
}

.bd-close {
  width: 29px;
  height: 29px;
}

.dock-pop-enter-active,
.dock-pop-leave-active {
  transition: opacity 0.2s ease, transform 0.28s var(--ease-spring);
}
.dock-pop-enter-from,
.dock-pop-leave-to {
  opacity: 0;
  transform: translate(-50%, 14px) scale(0.96);
}

@media (max-width: 860px) {
  .batch-dock {
    left: calc(12px + env(safe-area-inset-left, 0px));
    right: calc(12px + env(safe-area-inset-right, 0px));
    transform: none;
    max-width: none;
    bottom: calc(78px + env(safe-area-inset-bottom, 0px));
  }
  .batch-dock.is-above-player {
    bottom: calc(132px + env(safe-area-inset-bottom, 0px));
  }
  .dock-pop-enter-from,
  .dock-pop-leave-to {
    opacity: 0;
    transform: translateY(14px) scale(0.97);
  }
}

/* Mobile Responsive Fix */
@media (max-width: 720px) {
  .vault-top-acts {
    width: 100%;
  }
  .vault-top-acts .tab-pill-group {
    min-width: 0;
    width: 100%;
  }

  .nf-input,
  .rn-input {
    font-size: 16px;
    height: 36px;
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

/* Modals (with 4-side Dynamic Island & Home Indicator safe-area insets) */
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 250;
  background: rgba(15, 23, 42, 0.48);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  padding:
    calc(18px + env(safe-area-inset-top, 0px))
    calc(18px + env(safe-area-inset-right, 0px))
    calc(18px + env(safe-area-inset-bottom, 0px))
    calc(18px + env(safe-area-inset-left, 0px));
}

.rename-box {
  width: min(392px, 100%);
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
