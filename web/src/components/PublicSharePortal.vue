<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { formatBytes, formatTimeShort } from '../composables/useDropStore'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const props = defineProps<{ code: string }>()

interface PublicMeta {
  code: string
  hasPasscode: boolean
  unlocked: boolean
  fileName?: string
  mimeType?: string
  size?: number
  sha256?: string
  storage?: string
  downloads?: number
  maxDownloads?: number
  createdAt: string
  expiresAt?: string
}

const loading = ref(true)
const verifying = ref(false)
const errorMsg = ref('')
const meta = ref<PublicMeta | null>(null)
const passcode = ref('')
const unlockErr = ref('')
const shakeLock = ref(false)
const previewText = ref('')

async function fetchMeta() {
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await fetch(`/s/${props.code}/meta`)
    const data = await res.json()
    if (!res.ok) {
      errorMsg.value = data.error || '分享不可用'
      return
    }
    meta.value = data
    if (data.unlocked && data.mimeType?.startsWith('text/') && (data.size || 0) < 256 * 1024) {
      const tr = await fetch(`/s/${props.code}/raw?preview=1`)
      if (tr.ok) previewText.value = await tr.text()
    }
  } catch {
    errorMsg.value = '网络请求失败'
  } finally {
    loading.value = false
  }
}

async function verifyCode() {
  if (verifying.value) return
  unlockErr.value = ''
  verifying.value = true
  try {
    const res = await fetch(`/s/${props.code}/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ passcode: passcode.value }),
    })
    if (!res.ok) {
      const d = await res.json().catch(() => ({ error: '提取码不正确' }))
      unlockErr.value = d.error || '提取码不正确'
      shakeLock.value = true
      window.setTimeout(() => {
        shakeLock.value = false
      }, 420)
      return
    }
    await fetchMeta()
  } finally {
    verifying.value = false
  }
}

function isFineHoverDevice(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.matchMedia('(hover: hover) and (pointer: fine)').matches
  )
}

function onTicketMove(e: MouseEvent) {
  if (!isFineHoverDevice()) return
  const el = e.currentTarget as HTMLElement
  const r = el.getBoundingClientRect()
  const px = (e.clientX - r.left) / r.width
  const py = (e.clientY - r.top) / r.height
  el.style.setProperty('--tx', `${((0.5 - py) * 5).toFixed(2)}deg`)
  el.style.setProperty('--ty', `${((px - 0.5) * 5).toFixed(2)}deg`)
  el.style.setProperty('--gx', `${(px * 100).toFixed(1)}%`)
  el.style.setProperty('--gy', `${(py * 100).toFixed(1)}%`)
  el.style.setProperty('--g-alpha', '1')
}

function onTicketLeave(e: MouseEvent) {
  if (!isFineHoverDevice()) return
  const el = e.currentTarget as HTMLElement
  el.style.setProperty('--tx', '0deg')
  el.style.setProperty('--ty', '0deg')
  el.style.setProperty('--g-alpha', '0')
}

const isImage = computed(() => Boolean(meta.value?.mimeType?.startsWith('image/')))
const isAudio = computed(() => Boolean(meta.value?.mimeType?.startsWith('audio/')))
const isVideo = computed(() => Boolean(meta.value?.mimeType?.startsWith('video/')))
const isPdf = computed(
  () =>
    meta.value?.mimeType === 'application/pdf' ||
    Boolean(meta.value?.fileName?.toLowerCase().endsWith('.pdf')),
)

onMounted(() => {
  void fetchMeta()
})
</script>

<template>
  <main class="ticket-stage">
    <!-- Parallax Boarding-Pass / Ticket Stub -->
    <article
      class="glass-box parallax-ticket"
      :class="{ 'is-shaking': shakeLock }"
      @mousemove="onTicketMove"
      @mouseleave="onTicketLeave"
    >
      <!-- Ticket Top Header -->
      <header class="ticket-top">
        <div class="t-brand">
          <span class="t-serif">O.Arc</span>
          <span class="t-code mono">DROP</span>
        </div>
        <span class="t-status mono" :class="{ 'is-locked': meta && !meta.unlocked }">
          {{ loading ? 'CHECKING' : errorMsg ? 'EXPIRED' : meta?.unlocked ? 'LOSSLESS READY' : 'PROTECTED' }}
        </span>
      </header>

      <div v-if="loading" class="ticket-msg">正在校验取件凭证…</div>
      <div v-else-if="errorMsg" class="ticket-msg is-err">{{ errorMsg }}</div>

      <!-- Locked Passcode Form -->
      <form v-else-if="meta && !meta.unlocked" class="lock-gate" @submit.prevent="verifyCode">
        <div class="lock-orb">
          <UiIcon name="lock" :size="20" />
        </div>
        <h1 class="ticket-title">需要提取码</h1>
        <p class="ticket-caption">输入访问凭证后即可预览或无损下载原文件</p>

        <input
          v-model="passcode"
          class="code-field mono"
          placeholder="输入提取码"
          autocomplete="one-time-code"
          autocapitalize="off"
          autocorrect="off"
          spellcheck="false"
          enterkeyhint="go"
          autofocus
        />
        <p v-if="unlockErr" class="lock-err">{{ unlockErr }}</p>

        <button type="submit" class="pill-btn pill-btn--solid full-cta" :disabled="verifying">
          <UiIcon v-if="verifying" name="spinner" :size="14" class="spin-anim" />
          <span>{{ verifying ? '解锁中…' : '解锁取件单' }}</span>
        </button>
      </form>

      <!-- Unlocked Ticket Body -->
      <template v-else-if="meta">
        <div class="ticket-body">
          <h1 class="ticket-title">{{ meta.fileName }}</h1>

          <div class="ticket-grid mono">
            <div>
              <span class="kv-k">SIZE</span>
              <span class="kv-v">{{ formatBytes(meta.size) }}</span>
            </div>
            <div>
              <span class="kv-k">{{ meta.maxDownloads ? 'QUOTA' : 'CODE' }}</span>
              <span class="kv-v">
                {{ meta.maxDownloads ? `${meta.downloads || 0}/${meta.maxDownloads}` : meta.code }}
              </span>
            </div>
            <div>
              <span class="kv-k">EXPIRES</span>
              <span class="kv-v">{{ meta.expiresAt ? formatTimeShort(meta.expiresAt) : 'PERMANENT' }}</span>
            </div>
          </div>

          <div v-if="isImage" class="media-window">
            <img :src="`/s/${code}/raw?preview=1`" :alt="meta.fileName" />
          </div>

          <div v-else-if="isAudio" class="audio-window">
            <audio controls preload="metadata" :src="`/s/${code}/raw?preview=1`" />
          </div>

          <div v-else-if="isVideo" class="media-window">
            <video controls playsinline webkit-playsinline preload="metadata" :src="`/s/${code}/raw?preview=1`" />
          </div>

          <div v-else-if="isPdf" class="pdf-preview-bar">
            <a
              class="pill-btn full-cta"
              :href="`/s/${code}/raw?preview=1`"
              target="_blank"
              rel="noopener"
            >
              <UiIcon name="eye" :size="15" />
              <span>在线预览 PDF（不扣下载次数）</span>
            </a>
          </div>

          <pre v-else-if="previewText" class="text-window mono">{{ previewText }}</pre>
        </div>

        <!-- Perforated Tear Line (Kokonut Boarding Pass parity) -->
        <div class="perforation" aria-hidden="true">
          <span class="notch notch--l" />
          <span class="dash-line" />
          <span class="notch notch--r" />
        </div>

        <!-- Ticket Stub Footer -->
        <footer class="ticket-stub">
          <div v-if="meta.sha256" class="sha-line mono">
            SHA-256 · {{ meta.sha256 }}
          </div>
          <div class="stub-actions">
            <a class="pill-btn pill-btn--solid full-cta" :href="`/s/${code}/raw?download=1`">
              <UiIcon name="download" :size="15.5" />
              <span>无损下载原文件 ({{ formatBytes(meta.size) }})</span>
            </a>
            <CopyPill
              v-if="previewText"
              :text="previewText"
              label="复制全文"
              copied-label="全文已复制"
            />
          </div>
        </footer>
      </template>
    </article>
  </main>
</template>

<style scoped>
.ticket-stage {
  min-height: 100dvh;
  display: grid;
  place-items: center;
  padding:
    calc(28px + env(safe-area-inset-top, 0px))
    calc(16px + env(safe-area-inset-right, 0px))
    calc(28px + env(safe-area-inset-bottom, 0px))
    calc(16px + env(safe-area-inset-left, 0px));
  perspective: 1100px;
  position: relative;
  z-index: 2;
}

.parallax-ticket {
  --tx: 0deg;
  --ty: 0deg;
  --ticket-pad: 24px;
  width: min(500px, 100%);
  padding: var(--ticket-pad);
  transform: rotateX(var(--tx)) rotateY(var(--ty));
  transition: transform 0.2s ease-out;
  overflow: hidden;
}

.parallax-ticket.is-shaking {
  animation: ticket-shake 0.38s ease;
}

@keyframes ticket-shake {
  0%, 100% { transform: translateX(0); }
  25% { transform: translateX(-8px); }
  75% { transform: translateX(8px); }
}

.ticket-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.t-brand {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
}

.t-serif {
  font-family: var(--font-serif);
  font-style: italic;
  font-size: 1.18rem;
  color: var(--ink-1);
}

.t-code {
  font-size: 0.6rem;
  font-weight: 600;
  letter-spacing: 0.12em;
  padding: 2px 5px;
  border-radius: 5px;
  border: 1px solid var(--border-strong);
  background: var(--surface-recessed);
  color: var(--ink-2);
  transform: translateY(-1.5px);
}

.t-status {
  font-size: 0.69rem;
  font-weight: 640;
  letter-spacing: 0.05em;
  padding: 3px 9px;
  border-radius: 999px;
  background: var(--emerald-bg);
  color: var(--emerald-fg);
}

.t-status.is-locked {
  background: rgba(245, 158, 11, 0.14);
  color: #d97706;
}

.ticket-msg {
  padding: 36px 0;
  text-align: center;
  font-size: 0.86rem;
  color: var(--ink-2);
}
.ticket-msg.is-err {
  color: var(--danger-fg);
}

.lock-gate {
  text-align: center;
  padding: 8px 0 4px;
}

.lock-orb {
  width: 48px;
  height: 48px;
  margin: 0 auto 14px;
  border-radius: 50%;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  display: grid;
  place-items: center;
  color: var(--ink-1);
}

.ticket-title {
  margin: 0;
  font-size: 1.2rem;
  font-weight: 690;
  word-break: break-all;
}

.ticket-caption {
  margin: 6px 0 18px;
  font-size: 0.82rem;
  color: var(--ink-2);
}

.code-field {
  width: 100%;
  height: 44px;
  border-radius: 14px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  text-align: center;
  font-size: 0.96rem;
  letter-spacing: 0.14em;
  outline: none;
  margin-bottom: 12px;
}

.code-field:focus {
  border-color: var(--border-strong);
  background: var(--surface-pill);
}

.lock-err {
  margin: 0 0 12px;
  font-size: 0.79rem;
  color: var(--danger-fg);
}

.ticket-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  margin: 16px 0;
  padding: 12px;
  border-radius: 16px;
  background: var(--surface-recessed);
}

.kv-k {
  display: block;
  font-size: 0.65rem;
  color: var(--ink-3);
  margin-bottom: 3px;
}

.kv-v {
  font-size: 0.79rem;
  font-weight: 630;
  color: var(--ink-1);
}

.media-window {
  border-radius: 16px;
  overflow: hidden;
  background: var(--surface-recessed);
  display: grid;
  place-items: center;
}

.media-window img,
.media-window video {
  max-width: 100%;
  max-height: 48vh;
  display: block;
}

.audio-window audio {
  width: 100%;
}

.text-window {
  margin: 0;
  padding: 12px;
  border-radius: 14px;
  background: var(--surface-recessed);
  font-size: 0.79rem;
  line-height: 1.54;
  max-height: 260px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

/* Ticket Perforation */
.perforation {
  position: relative;
  display: flex;
  align-items: center;
  margin: 20px calc(var(--ticket-pad) * -1);
}

.notch {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--bg-outer);
  border: 1px solid var(--border-hairline);
  flex-shrink: 0;
}

.notch--l {
  margin-left: -9px;
}
.notch--r {
  margin-right: -9px;
}

.dash-line {
  flex: 1;
  border-top: 1.5px dashed var(--border-strong);
}

.pdf-preview-bar {
  margin-top: 4px;
}

.sha-line {
  font-size: 0.69rem;
  line-height: 1.45;
  color: var(--ink-3);
  text-align: center;
  margin-bottom: 12px;
  word-break: break-all;
  overflow-wrap: anywhere;
  padding: 0 4px;
}

.stub-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.full-cta {
  flex: 1;
  min-width: 0;
  height: 44px;
  font-size: 0.88rem;
}

@media (max-width: 520px) {
  .ticket-stage {
    padding:
      calc(16px + env(safe-area-inset-top, 0px))
      calc(12px + env(safe-area-inset-right, 0px))
      calc(20px + env(safe-area-inset-bottom, 0px))
      calc(12px + env(safe-area-inset-left, 0px));
  }

  .parallax-ticket {
    --ticket-pad: 18px;
    border-radius: 20px;
  }

  .ticket-title {
    font-size: 1.08rem;
    line-height: 1.35;
  }

  .ticket-grid {
    gap: 8px;
    padding: 10px;
    border-radius: 13px;
  }

  .kv-v {
    font-size: 0.74rem;
    word-break: break-word;
  }

  .media-window img,
  .media-window video {
    max-height: 44dvh;
  }

  .stub-actions {
    flex-direction: column;
  }

  .stub-actions > * {
    width: 100%;
    height: 44px;
  }
}
</style>
