<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  formatBytes,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

const target = ref<'local' | 'silo' | 'gdrive'>('local')
const siloPrefix = ref('aurora/drop/')
const ttlHours = ref<number>(168)
const maxDownloads = ref<number>(0)
const passcode = ref('')
const syncToClip = ref(false)
const isDragOver = ref(false)
const isHoverPod = ref(false)
const dropInput = ref<HTMLInputElement | null>(null)

const openTTLMenu = ref(false)
const openProtectPopover = ref(false)
const controlsWrap = ref<HTMLElement | null>(null)

const ttlChoices = [
  { value: 24, label: '24 小时' },
  { value: 168, label: '7 天有效' },
  { value: 720, label: '30 天有效' },
  { value: 0, label: '长期保留' },
]

const currentTTLLabel = computed(
  () => ttlChoices.find((c) => c.value === ttlHours.value)?.label || '7 天有效',
)

const protectSummary = computed(() => {
  const parts: string[] = []
  parts.push(passcode.value.trim() ? '已设提取码' : '免密公开')
  if (maxDownloads.value > 0) parts.push(`限 ${maxDownloads.value} 次`)
  if (syncToClip.value) parts.push('同步剪贴板')
  return parts.join(' · ')
})

const targetOptions = computed(() => [
  { key: 'local' as const, label: '本机暂存', enabled: true },
  { key: 'silo' as const, label: 'Silo 媒体桶', enabled: Boolean(store.status.value?.siloEnabled) },
  { key: 'gdrive' as const, label: 'Google Drive', enabled: Boolean(store.status.value?.gdriveEnabled) },
])

const activeTargetIdx = computed(() =>
  Math.max(0, targetOptions.value.findIndex((o) => o.key === target.value)),
)

function selectTarget(key: 'local' | 'silo' | 'gdrive', enabled: boolean) {
  if (!enabled) {
    showToast(key === 'gdrive' ? '尚未配置 Google Drive 远程目录' : '尚未连接 Silo 存储')
    return
  }
  target.value = key
  if (key !== 'local' && ttlHours.value === 168) {
    ttlHours.value = 0
  } else if (key === 'local' && ttlHours.value === 0) {
    ttlHours.value = 168
  }
}

function onPickedDropFiles(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return
  queueFiles(Array.from(input.files))
  input.value = ''
}

function onDropFiles(e: DragEvent) {
  isDragOver.value = false
  if (!e.dataTransfer?.files?.length) return
  queueFiles(Array.from(e.dataTransfer.files))
}

function queueFiles(files: File[]) {
  for (const f of files) {
    void store.uploadChunkedFile(f, {
      target: target.value,
      siloPrefix: siloPrefix.value,
      ttlHours: ttlHours.value,
      maxDownloads: maxDownloads.value || 0,
      passcode: passcode.value,
      addToClip: syncToClip.value,
    })
  }
}

function stepMaxDownloads(delta: number) {
  maxDownloads.value = Math.max(0, (maxDownloads.value || 0) + delta)
}

function onDocClick(e: MouseEvent) {
  if (controlsWrap.value && !controlsWrap.value.contains(e.target as Node)) {
    openTTLMenu.value = false
    openProtectPopover.value = false
  }
}

onMounted(() => document.addEventListener('mousedown', onDocClick))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick))
</script>

<template>
  <div class="drop-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">快传投递</h1>
        <p class="pane-subtitle">20 MB 自动切片直穿 Cloudflare · 保持原始字节无损入库</p>
      </div>

      <div class="pane-actions target-bar">
        <div class="tab-pill-group" :style="{ gridTemplateColumns: 'repeat(3, 1fr)' }">
          <div
            class="tab-pill-indicator"
            :style="{
              width: 'calc((100% - 7px) / 3)',
              transform: `translateX(${activeTargetIdx * 100}%)`,
            }"
          />
          <button
            v-for="opt in targetOptions"
            :key="opt.key"
            type="button"
            class="tab-pill-btn"
            :class="{ 'is-active': target === opt.key, 'is-disabled': !opt.enabled }"
            @click="selectTarget(opt.key, opt.enabled)"
          >
            {{ opt.label }}
          </button>
        </div>
      </div>
    </header>

    <div class="pane-body--narrow drop-stack">
      <!-- Optional Silo Subfolder Strip -->
      <Transition name="tray-expand">
        <div v-if="target === 'silo'" class="glass-box silo-bar">
          <span class="silo-bar__lbl">写入目录</span>
          <div class="silo-chips">
            <button
              v-for="pre in [
                { val: 'aurora/drop/', txt: 'aurora/drop/' },
                { val: 'aurora/photos/', txt: 'aurora/photos/' },
                { val: 'aurora/music/', txt: 'aurora/music/' },
              ]"
              :key="pre.val"
              type="button"
              class="silo-chip mono"
              :class="{ 'is-on': siloPrefix === pre.val }"
              @click="siloPrefix = pre.val"
            >
              {{ pre.txt }}
            </button>
          </div>
          <input v-model="siloPrefix" class="silo-custom mono" placeholder="自定义前缀/" />
        </div>
      </Transition>

      <!-- Centered Spacious Drop Stage -->
      <div class="glass-box drop-card">
        <div
          class="drop-hitarea"
          :class="{ 'is-fanned': isHoverPod || isDragOver, 'is-drag': isDragOver }"
          @mouseenter="isHoverPod = true"
          @mouseleave="isHoverPod = false"
          @dragover.prevent="isDragOver = true"
          @dragleave.prevent="isDragOver = false"
          @drop.prevent="onDropFiles"
          @click="dropInput?.click()"
        >
          <input ref="dropInput" type="file" multiple hidden @change="onPickedDropFiles" />

          <div class="fan-deck" aria-hidden="true">
            <div class="fan-card fan-card--left">
              <UiIcon name="image" :size="19" />
              <span class="mono">RAW</span>
            </div>
            <div class="fan-card fan-card--right">
              <UiIcon name="disc" :size="19" />
              <span class="mono">FLAC</span>
            </div>
            <div class="fan-card fan-card--center">
              <UiIcon name="arrow-up" :size="22" />
            </div>
          </div>

          <div class="stage-headline">拖入任意体积文件，或点击选择文件</div>
          <div class="stage-sub">
            当前目标：<strong>{{
              target === 'local'
                ? '本机暂存'
                : target === 'silo'
                  ? `Silo (${siloPrefix})`
                  : 'Google Drive (隔离目录)'
            }}</strong>
            · 上传完成后自动复制 `/s/短码`
          </div>
        </div>

        <!-- Strictly Aligned Bottom Parameter Footer inside the Drop Card -->
        <footer ref="controlsWrap" class="drop-footer">
          <div class="footer-left">
            <!-- Spring Dropdown -->
            <div class="pop-anchor">
              <button
                type="button"
                class="pill-btn"
                @click="openTTLMenu = !openTTLMenu; openProtectPopover = false"
              >
                <UiIcon name="clock" :size="14" />
                <span>{{ currentTTLLabel }}</span>
                <UiIcon name="chevron-down" :size="13" />
              </button>

              <Transition name="spring-menu">
                <div v-if="openTTLMenu" class="liquid-chrome spring-listbox">
                  <button
                    v-for="ch in ttlChoices"
                    :key="ch.value"
                    type="button"
                    class="listbox-opt"
                    :class="{ 'is-selected': ttlHours === ch.value }"
                    @click="ttlHours = ch.value; openTTLMenu = false"
                  >
                    <span>{{ ch.label }}</span>
                    <UiIcon v-if="ttlHours === ch.value" name="check" :size="14" />
                  </button>
                </div>
              </Transition>
            </div>

            <!-- Morph Popover -->
            <div class="pop-anchor">
              <button
                type="button"
                class="pill-btn"
                :class="{ 'is-highlighted': passcode.trim() || maxDownloads > 0 || syncToClip }"
                @click="openProtectPopover = !openProtectPopover; openTTLMenu = false"
              >
                <UiIcon :name="passcode.trim() ? 'lock' : 'sliders'" :size="14" />
                <span>{{ protectSummary }}</span>
              </button>

              <Transition name="spring-menu">
                <div v-if="openProtectPopover" class="liquid-chrome morph-popover">
                  <div class="pop-row">
                    <label class="pop-lbl">提取码</label>
                    <input
                      v-model="passcode"
                      class="pop-input mono"
                      maxlength="24"
                      placeholder="留空免密直下"
                    />
                  </div>

                  <div class="pop-row pop-row--inline">
                    <span class="pop-lbl">下载次数上限</span>
                    <div class="stepper">
                      <button type="button" class="step-btn" @click="stepMaxDownloads(-1)">−</button>
                      <span class="step-val mono">{{ maxDownloads === 0 ? '不限' : maxDownloads }}</span>
                      <button type="button" class="step-btn" @click="stepMaxDownloads(1)">+</button>
                    </div>
                  </div>

                  <div class="pop-row pop-row--inline">
                    <span class="pop-lbl">同时挂入剪贴板</span>
                    <button
                      type="button"
                      role="switch"
                      :aria-checked="syncToClip"
                      class="spring-switch"
                      :class="{ 'is-on': syncToClip }"
                      @click="syncToClip = !syncToClip"
                    >
                      <span class="switch-thumb" />
                    </button>
                  </div>
                </div>
              </Transition>
            </div>
          </div>

          <button type="button" class="pill-btn pill-btn--solid" @click="dropInput?.click()">
            <UiIcon name="plus" :size="14.5" />
            <span>选择文件</span>
          </button>
        </footer>
      </div>

      <!-- Transfer Queue -->
      <div v-if="store.uploads.value.length > 0" class="queue-section">
        <div class="sec-caption">传输记录</div>
        <TransitionGroup name="stack-slide" tag="div" class="transfer-stack">
          <div v-for="u in store.uploads.value" :key="u.localId" class="glass-box transfer-item">
            <div class="tr-main">
              <div class="tr-title-row">
                <span class="tr-name">{{ u.fileName }}</span>
                <span class="tr-badge mono">{{ u.target }}</span>
              </div>
              <div class="tr-sub mono">
                <template v-if="u.status === 'uploading'">
                  {{ formatBytes(u.uploadedBytes) }} / {{ formatBytes(u.size) }} · {{ formatBytes(u.speedBps) }}/s
                </template>
                <template v-else-if="u.status === 'assembling'">正在校验合并分片…</template>
                <template v-else-if="u.status === 'done'">
                  {{ formatBytes(u.size) }} · /s/{{ u.shareCode }}
                </template>
                <template v-else-if="u.status === 'canceled'">已取消</template>
                <template v-else class="err-txt">{{ u.error }}</template>
              </div>

              <div v-if="u.status === 'uploading' || u.status === 'assembling'" class="tr-rail">
                <div
                  class="tr-fill"
                  :style="{ width: `${u.size > 0 ? Math.min(100, (u.uploadedBytes / u.size) * 100) : 100}%` }"
                />
              </div>
            </div>

            <div class="tr-act">
              <button
                v-if="u.status === 'uploading'"
                type="button"
                class="icon-circle-btn"
                title="取消上传"
                @click="u.abortCtrl.abort()"
              >
                <UiIcon name="close" :size="14" />
              </button>
              <CopyPill
                v-if="u.shareCode"
                :text="shareFullUrl(u.shareCode)"
                label="复制短链"
                copied-label="已复制"
              />
              <CopyPill
                v-if="u.publicUrl"
                :text="u.publicUrl"
                label="CDN 直链"
                copied-label="已复制"
              />
            </div>
          </div>
        </TransitionGroup>
      </div>
    </div>
  </div>
</template>

<style scoped>
.drop-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

@media (max-width: 860px) {
  .target-bar {
    width: 100%;
  }
  .target-bar .tab-pill-group {
    width: 100%;
  }
  .target-bar .tab-pill-btn {
    padding: 7px 6px;
    font-size: 0.78rem;
  }
}

.silo-bar {
  padding: 10px 14px;
  border-radius: 16px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.silo-bar__lbl {
  font-size: 0.78rem;
  font-weight: 580;
  color: var(--ink-2);
}

.silo-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.silo-chip {
  height: 28px;
  padding: 0 11px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  font-size: 0.74rem;
  cursor: pointer;
}

.silo-chip.is-on {
  background: var(--cta-bg);
  color: var(--cta-fg);
  border-color: transparent;
}

.silo-custom {
  flex: 1;
  min-width: 120px;
  height: 30px;
  border: 0;
  background: transparent;
  color: var(--ink-1);
  font-size: 0.8rem;
  outline: none;
  padding: 0 6px;
}

/* Main Drop Card */
.drop-card {
  border-radius: 24px;
  overflow: visible;
}

.drop-hitarea {
  padding: 56px 24px 48px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  cursor: pointer;
  border-radius: 24px 24px 0 0;
  transition: background-color 0.25s ease;
}

@media (max-width: 640px) {
  .drop-hitarea {
    padding: 40px 16px 34px;
  }
}

.drop-hitarea.is-drag {
  background: var(--surface-recessed);
}

.fan-deck {
  position: relative;
  width: 148px;
  height: 86px;
  margin-bottom: 20px;
  display: grid;
  place-items: center;
}

.fan-card {
  position: absolute;
  width: 64px;
  height: 78px;
  border-radius: 16px;
  background: var(--surface-pill);
  border: 1px solid var(--border-hairline);
  box-shadow: var(--shadow-card), var(--specular-inset);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 5px;
  color: var(--ink-2);
  font-size: 0.64rem;
  font-weight: 630;
  transform-origin: bottom center;
  transition: transform 0.46s var(--ease-spring), color 0.25s ease;
}

.fan-card--left {
  transform: translateX(-18px) translateY(3px) rotate(-8deg) scale(0.94);
}
.fan-card--right {
  transform: translateX(18px) translateY(3px) rotate(8deg) scale(0.94);
}
.fan-card--center {
  width: 68px;
  height: 82px;
  z-index: 2;
  color: var(--ink-1);
}

.drop-hitarea.is-fanned .fan-card--left {
  transform: translateX(-42px) translateY(-4px) rotate(-15deg) scale(0.96);
}
.drop-hitarea.is-fanned .fan-card--right {
  transform: translateX(42px) translateY(-4px) rotate(15deg) scale(0.96);
}
.drop-hitarea.is-fanned .fan-card--center {
  transform: translateY(-9px) scale(1.06);
}

.stage-headline {
  font-size: 1.04rem;
  font-weight: 660;
  color: var(--ink-1);
}

.stage-sub {
  margin-top: 6px;
  font-size: 0.82rem;
  color: var(--ink-2);
}

.drop-footer {
  padding: 13px 18px;
  border-top: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  border-radius: 0 0 24px 24px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.footer-left {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.pop-anchor {
  position: relative;
}

.pill-btn.is-highlighted {
  border-color: var(--border-strong);
}

.spring-listbox {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 0;
  z-index: 40;
  min-width: 162px;
  padding: 5px;
  border-radius: 18px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  transform-origin: bottom left;
}

.listbox-opt {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 34px;
  padding: 0 12px;
  border: 0;
  border-radius: 12px;
  background: transparent;
  color: var(--ink-2);
  font-size: 0.82rem;
  cursor: pointer;
}

.listbox-opt:hover {
  background: var(--surface-recessed-hover);
  color: var(--ink-1);
}

.listbox-opt.is-selected {
  color: var(--ink-1);
  font-weight: 630;
  background: var(--surface-recessed);
}

.morph-popover {
  position: absolute;
  bottom: calc(100% + 8px);
  left: 0;
  z-index: 40;
  width: min(276px, calc(100vw - 48px));
  padding: 15px;
  border-radius: 22px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  transform-origin: bottom left;
}

.pop-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pop-row--inline {
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
}

.pop-lbl {
  font-size: 0.79rem;
  font-weight: 570;
  color: var(--ink-2);
}

.pop-input {
  height: 36px;
  padding: 0 12px;
  border-radius: 11px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-1);
  font-size: 0.84rem;
  outline: none;
}

.stepper {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px;
  border-radius: 999px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
}

.step-btn {
  width: 25px;
  height: 25px;
  border-radius: 50%;
  border: 0;
  background: var(--surface-pill);
  color: var(--ink-1);
  font-size: 0.9rem;
  cursor: pointer;
}

.step-val {
  min-width: 34px;
  text-align: center;
  font-size: 0.77rem;
  font-weight: 600;
}

.spring-switch {
  width: 42px;
  height: 24px;
  padding: 2px;
  border-radius: 999px;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  cursor: pointer;
  display: flex;
  align-items: center;
  transition: background-color 0.25s ease;
}

.spring-switch.is-on {
  background: #10b981;
  border-color: transparent;
}

.switch-thumb {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #ffffff;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.18);
  transition: transform 0.34s var(--ease-spring);
}

.spring-switch.is-on .switch-thumb {
  transform: translateX(18px);
}

.sec-caption {
  font-size: 0.78rem;
  font-weight: 620;
  color: var(--ink-3);
  margin-bottom: 8px;
  padding: 0 4px;
}

.transfer-stack {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.transfer-item {
  padding: 13px 16px;
  border-radius: 18px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tr-main {
  min-width: 180px;
  flex: 1;
}

.tr-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tr-name {
  font-size: 0.87rem;
  font-weight: 620;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tr-badge {
  font-size: 0.68rem;
  text-transform: uppercase;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.tr-sub {
  margin-top: 3px;
  font-size: 0.75rem;
  color: var(--ink-3);
}

.err-txt {
  color: var(--danger-fg);
}

.tr-rail {
  margin-top: 8px;
  height: 4px;
  border-radius: 999px;
  background: var(--surface-recessed);
  overflow: hidden;
}

.tr-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--cta-bg);
  transition: width 0.25s ease;
}

.tr-act {
  display: flex;
  align-items: center;
  gap: 6px;
}

.spring-menu-enter-active,
.spring-menu-leave-active {
  transition: opacity 0.16s ease, transform 0.28s var(--ease-spring);
}
.spring-menu-enter-from,
.spring-menu-leave-to {
  opacity: 0;
  transform: scale(0.9) translateY(6px);
}

.tray-expand-enter-active,
.tray-expand-leave-active {
  transition: opacity 0.2s ease, transform 0.26s var(--ease-spring);
}
.tray-expand-enter-from,
.tray-expand-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

.stack-slide-enter-active,
.stack-slide-leave-active {
  transition: all 0.3s var(--ease-spring);
}
.stack-slide-enter-from,
.stack-slide-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
