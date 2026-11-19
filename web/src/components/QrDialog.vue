<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  canNativeShare,
  nativeShareLink,
  shareFullUrl,
  useDropStore,
} from '../composables/useDropStore'
import { useBodyScrollLock } from '../composables/useMobileViewport'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()
defineExpose({ shareFullUrl })
useBodyScrollLock(computed(() => Boolean(store.qrModal.value)))

const supportsNativeShare = canNativeShare()
const savingOpt = ref(false)
const passDraft = ref('')

function handleNativeShare() {
  const modal = store.qrModal.value
  if (!modal) return
  void nativeShareLink({
    title: modal.title,
    text: modal.subtitle,
    url: modal.url,
  })
}

const linkedShare = computed(() => {
  const code = store.qrModal.value?.shareCode
  if (!code) return null
  return store.shares.value.find((s) => s.code === code) || null
})

const currentTtlMode = computed<number>(() => {
  const sh = linkedShare.value
  if (!sh || !sh.expiresAt) return 0
  const hoursLeft = (new Date(sh.expiresAt).getTime() - Date.now()) / 3600000
  if (hoursLeft <= 26) return 24
  return 168
})

watch(
  () => store.qrModal.value?.shareCode,
  () => {
    passDraft.value = ''
  },
)

async function setTtl(hours: number) {
  const sh = linkedShare.value
  if (!sh || savingOpt.value) return
  savingOpt.value = true
  try {
    await store.updateShareOptions(sh.code, { ttlHours: hours })
  } finally {
    savingOpt.value = false
  }
}

async function setMaxDl(max: number) {
  const sh = linkedShare.value
  if (!sh || savingOpt.value) return
  savingOpt.value = true
  try {
    await store.updateShareOptions(sh.code, { maxDownloads: max })
  } finally {
    savingOpt.value = false
  }
}

async function applyPasscode() {
  const sh = linkedShare.value
  const code = passDraft.value.trim()
  if (!sh || !code || savingOpt.value) return
  savingOpt.value = true
  try {
    await store.updateShareOptions(sh.code, { passcode: code })
    passDraft.value = ''
  } finally {
    savingOpt.value = false
  }
}

async function clearPasscode() {
  const sh = linkedShare.value
  if (!sh || savingOpt.value) return
  savingOpt.value = true
  try {
    await store.updateShareOptions(sh.code, { passcode: '' })
  } finally {
    savingOpt.value = false
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="lens-modal">
      <div v-if="store.qrModal.value" class="qr-backdrop" @click.self="store.closeQrModal">
        <div class="liquid-chrome qr-sheet">
          <div class="sheet-grabber" aria-hidden="true" />
          <div class="qr-head">
            <div class="qr-titles">
              <div class="qr-title">{{ store.qrModal.value.title }}</div>
              <div v-if="store.qrModal.value.subtitle" class="qr-sub">
                {{ store.qrModal.value.subtitle }}
              </div>
            </div>
            <button type="button" class="icon-circle-btn" @click="store.closeQrModal">
              <UiIcon name="close" :size="15" />
            </button>
          </div>

          <div class="qr-frame">
            <img
              :src="`/api/qr?text=${encodeURIComponent(store.qrModal.value.url)}`"
              alt="QR Code"
              class="qr-svg"
            />
          </div>

          <div class="qr-url mono">{{ store.qrModal.value.url }}</div>

          <!-- Built-in Share Security & Burn-After-Reading Controls -->
          <div v-if="linkedShare" class="qr-sec-box">
            <div class="sec-head">
              <span class="mono">SHARE POLICY</span>
              <UiIcon v-if="savingOpt" name="spinner" :size="12.5" class="spin-anim" />
            </div>

            <div class="sec-row">
              <span class="sec-lbl">有效期</span>
              <div class="sec-seg">
                <button
                  v-for="opt in [
                    { v: 24, l: '24小时' },
                    { v: 168, l: '7天' },
                    { v: 0, l: '长期' },
                  ]"
                  :key="opt.v"
                  type="button"
                  class="sec-chip"
                  :class="{ 'is-on': currentTtlMode === opt.v }"
                  :disabled="savingOpt"
                  @click="setTtl(opt.v)"
                >
                  {{ opt.l }}
                </button>
              </div>
            </div>

            <div class="sec-row">
              <span class="sec-lbl">次数上限</span>
              <div class="sec-seg">
                <button
                  v-for="opt in [
                    { v: 0, l: '不限' },
                    { v: 1, l: '1次即焚' },
                    { v: 5, l: '5次' },
                  ]"
                  :key="opt.v"
                  type="button"
                  class="sec-chip"
                  :class="{ 'is-on': (linkedShare.maxDownloads || 0) === opt.v }"
                  :disabled="savingOpt"
                  @click="setMaxDl(opt.v)"
                >
                  {{ opt.l }}
                </button>
              </div>
            </div>

            <div class="sec-row">
              <span class="sec-lbl">提取码</span>
              <div class="sec-pass-wrap">
                <template v-if="linkedShare.hasPasscode && !passDraft">
                  <span class="pass-active-tag">
                    <UiIcon name="lock" :size="11.5" />
                    <span>已加密保护</span>
                  </span>
                  <button
                    type="button"
                    class="pass-clear-btn"
                    :disabled="savingOpt"
                    @click="clearPasscode"
                  >
                    取消密码
                  </button>
                </template>
                <form v-else class="pass-form" @submit.prevent="applyPasscode">
                  <input
                    v-model="passDraft"
                    placeholder="可选：设提取码…"
                    class="pass-input mono"
                    maxlength="24"
                    autocapitalize="off"
                    autocomplete="off"
                    enterkeyhint="done"
                    :disabled="savingOpt"
                  />
                  <button
                    v-if="passDraft.trim()"
                    type="submit"
                    class="pass-save-btn"
                    :disabled="savingOpt"
                  >
                    保存
                  </button>
                </form>
              </div>
            </div>
          </div>

          <div class="qr-actions">
            <CopyPill :text="store.qrModal.value.url" label="复制链接" copied-label="已复制" />
            <button
              v-if="supportsNativeShare"
              type="button"
              class="pill-btn pill-btn--solid"
              @click="handleNativeShare"
            >
              <UiIcon name="share-ios" :size="14" />
              <span>系统分享</span>
            </button>
            <a
              v-if="store.qrModal.value.url.startsWith('http')"
              :href="store.qrModal.value.url"
              target="_blank"
              rel="noopener"
              class="pill-btn"
            >
              <UiIcon name="external" :size="14" />
              <span>打开链接</span>
            </a>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.qr-backdrop {
  position: fixed;
  inset: 0;
  z-index: 260;
  background: rgba(15, 23, 42, 0.42);
  -webkit-backdrop-filter: blur(16px);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  overscroll-behavior: contain;
  padding:
    calc(18px + env(safe-area-inset-top, 0px))
    calc(18px + env(safe-area-inset-right, 0px))
    calc(18px + env(safe-area-inset-bottom, 0px))
    calc(18px + env(safe-area-inset-left, 0px));
}

.qr-sheet {
  width: min(380px, 100%);
  max-height: calc(100dvh - 36px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px));
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  border-radius: 24px;
  padding: 18px 20px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}

.qr-head {
  width: 100%;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.qr-titles {
  min-width: 0;
}

.qr-title {
  font-size: 0.95rem;
  font-weight: 660;
  word-break: break-word;
}

.qr-sub {
  margin-top: 2px;
  font-size: 0.73rem;
  color: var(--ink-3);
}

.qr-frame {
  width: 188px;
  height: 188px;
  padding: 10px;
  border-radius: 18px;
  background: #ffffff;
  box-shadow: 0 10px 26px rgba(15, 23, 42, 0.08);
  display: grid;
  place-items: center;
  flex-shrink: 0;
}

.qr-svg {
  width: 100%;
  height: 100%;
  display: block;
}

.qr-url {
  width: 100%;
  padding: 7px 10px;
  border-radius: 10px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  font-size: 0.72rem;
  color: var(--ink-2);
  word-break: break-all;
  text-align: center;
}

/* Share Policy Controls */
.qr-sec-box {
  width: 100%;
  padding: 10px 12px;
  border-radius: 13px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.63rem;
  font-weight: 600;
  letter-spacing: 0.07em;
  color: var(--ink-3);
}

.sec-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.sec-lbl {
  font-size: 0.74rem;
  font-weight: 580;
  color: var(--ink-2);
  flex-shrink: 0;
}

.sec-seg {
  display: inline-flex;
  padding: 2px;
  border-radius: 8px;
  background: var(--surface-card);
  border: 1px solid var(--border-hairline);
  gap: 2px;
}

.sec-chip {
  border: 0;
  background: transparent;
  color: var(--ink-2);
  font-size: 0.71rem;
  font-weight: 560;
  padding: 3.5px 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.sec-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  font-weight: 630;
}

.sec-pass-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  min-width: 0;
}

.pass-active-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 0.72rem;
  font-weight: 610;
  padding: 3px 8px;
  border-radius: 6px;
  background: rgba(234, 88, 12, 0.12);
  color: var(--led-orange);
}

.pass-clear-btn {
  border: 0;
  background: transparent;
  color: var(--ink-3);
  font-size: 0.72rem;
  cursor: pointer;
  padding: 2px 4px;
}
.pass-clear-btn:hover {
  color: var(--danger-fg);
}

.pass-form {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  max-width: 196px;
}

.pass-input {
  flex: 1;
  min-width: 0;
  height: 28px;
  padding: 0 8px;
  border-radius: 7px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-card);
  color: var(--ink-1);
  font-size: 0.74rem;
  outline: none;
}

.pass-save-btn {
  height: 28px;
  padding: 0 9px;
  border-radius: 7px;
  border: 0;
  background: var(--cta-bg);
  color: var(--cta-fg);
  font-size: 0.72rem;
  font-weight: 620;
  cursor: pointer;
  flex-shrink: 0;
}

.qr-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  align-items: center;
  gap: 8px;
  width: 100%;
}

@media (max-width: 720px), (max-height: 520px) and (pointer: coarse) {
  .qr-backdrop {
    display: flex;
    align-items: flex-end;
    justify-content: center;
    padding: calc(16px + env(safe-area-inset-top, 0px)) 0 0 0;
    -webkit-backdrop-filter: blur(6px);
    backdrop-filter: blur(6px);
  }

  .qr-sheet {
    width: 100%;
    max-height: calc(var(--vv-height, 100dvh) - 18px - env(safe-area-inset-top, 0px));
    border-radius: 22px 22px 0 0;
    border-bottom: 0;
    padding:
      12px
      calc(16px + env(safe-area-inset-right, 0px))
      calc(18px + env(safe-area-inset-bottom, 0px) + var(--kb-inset, 0px))
      calc(16px + env(safe-area-inset-left, 0px));
  }

  .qr-frame {
    width: 168px;
    height: 168px;
  }

  .sec-row {
    flex-wrap: wrap;
    gap: 6px 8px;
  }

  .sec-seg {
    flex: 1 1 auto;
    justify-content: space-between;
  }

  .sec-chip {
    flex: 1;
    padding: 7px 6px;
    font-size: 0.76rem;
    text-align: center;
  }

  .sec-pass-wrap {
    flex: 1 1 200px;
  }

  .pass-form {
    max-width: none;
    gap: 6px;
  }

  .pass-input {
    height: 38px;
    padding: 0 10px;
    border-radius: 9px;
  }

  .pass-save-btn {
    height: 38px;
    padding: 0 14px;
    border-radius: 9px;
    font-size: 0.8rem;
  }

  .pass-clear-btn {
    padding: 6px 8px;
    font-size: 0.78rem;
  }

  .qr-actions {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
  }

  .qr-actions > * {
    width: 100%;
  }
}
</style>
