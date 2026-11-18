export interface HeaderLink {
  label: string
  url: string
}

export interface ServerStatus {
  chunkSize: number
  diskTotal: number
  diskFree: number
  siloEnabled: boolean
  siloBucket: string
  siloPrefix: string
  gdriveEnabled: boolean
  gdriveTarget: string
  userEmail: string
  headerLinks: HeaderLink[]
}

export interface ClipItem {
  id: string
  kind: 'text' | 'image' | 'file'
  text?: string
  fileName?: string
  mimeType?: string
  size?: number
  sha256?: string
  shareCode?: string
  pinned: boolean
  createdAt: string
}

export interface ShareItem {
  code: string
  fileName: string
  mimeType: string
  size: number
  sha256?: string
  storage: 'local' | 'silo' | 'gdrive'
  storageKey: string
  createdAt: string
  expiresAt?: string
  maxDownloads?: number
  downloads: number
  hasPasscode: boolean
}

export interface MediaItem {
  key: string
  name: string
  folder: string
  kind: 'image' | 'audio'
  ext: string
  size: number
  lastModified: string
  publicUrl?: string
  streamUrl: string
  thumbUrl?: string
  lyricsKey?: string
}

export interface TrackMetadata {
  title: string
  artist: string
  album: string
  format: string
  sampleRate?: number
  bitsPerSample?: number
  durationSec?: number
  hasCover: boolean
}

export interface UploadTask {
  localId: string
  fileName: string
  size: number
  target: 'local' | 'silo' | 'gdrive'
  uploadedBytes: number
  speedBps: number
  status: 'uploading' | 'assembling' | 'done' | 'error' | 'canceled'
  error?: string
  shareCode?: string
  publicUrl?: string
  abortCtrl: AbortController
}

export interface LrcLine {
  time: number
  text: string
}
