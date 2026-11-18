<script setup lang="ts">
import { ref } from 'vue'
import {
  formatBytes,
  formatTimeShort,
  shareFullUrl,
  showToast,
  useDropStore,
} from '../composables/useDropStore'
import type { ClipItem } from '../types'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

const draftText = ref('')
const sendState = ref<'idle' | 'loading' | 'success'>('idle')
const quickFileInput = ref<HTMLInputElement | null>(null)

async function submitText() {
  const txt = draftText.value
  if (!txt.trim() || sendState.value !== 'idle') return
  sendState.value = 'loading'
  try {
    await store.sendTextClip(txt)
    draftText.value = ''
    sendState.value = 'success'
    window.setTimeout(() => {
      sendState.value = 'idle'
    }, 1100)
  } catch {
    sendState.value = 'idle'
  }
}

function handleComposerKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
    e.preventDefault()
    void submitText()
  }
}

async function handleComposerPaste(e: ClipboardEvent) {
  const files = e.clipboardData?.files
  if (files && files.length > 0) {
    e.preventDefault()
    for (const f of Array.from(files)) {
      await store.sendQuickPasteFile(f)
    }
  }
}

function onQuickFilesPicked(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return
  for (const f of Array.from(input.files)) {
    void store.sendQuickPasteFile(f)
  }
  input.value = ''
}

async function togglePin(item: ClipItem) {
  await fetch(`/api/clips/${item.id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinned: !item.pinned }),
  })
  item.pinned = !item.pinned
}

async function generateClipShare(item: ClipItem) {
  const res = await fetch(`/api/clips/${item.id}/share`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ttlHours: 168 }),
  })
  if (res.ok) {
    const sh = await res.json()
    item.shareCode = sh.code
    await navigator.clipboard.writeText(shareFullUrl(sh.code)).catch(() => {})
    showToast('已生成并复制分享短链')
  }
}

async function removeClip(id: string) {
  await fetch(`/api/clips/${id}`, { method: 'DELETE' })
  store.clips.value = store.clips.value.filter((c) => c.id !== id)
}
</script>

<template>
  <div class="clip-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">无损剪贴板</h1>
        <p class="pane-subtitle">多设备实时同步 · 任意页面按 ⌘V 直接投递文本或无损原图</p>
      </div>

      <div class="pane-actions">
        <button type="button" class="pill-btn" @click="quickFileInput?.click()">
          <UiIcon name="plus" :size="14" />
          <span>传原图 / 文件</span>
        </button>
      </div>
    </header>

    <div class="pane-body--narrow clip-body">
      <!-- Top Concentric Prompt-Box Capsule -->
      <div class="prompt-capsule">
        <button
          type="button"
          class="attach-circle"
          title="选择原图或文件"
          @click="quickFileInput?.click()"
        >
          <UiIcon name="plus" :size="17" />
        </button>
        <input ref="quickFileInput" type="file" multiple hidden @change="onQuickFilesPicked" />

        <textarea
          v-model="draftText"
          rows="1"
          class="prompt-field"
          placeholder="输入文本、代码，或在此直接粘贴截图原图… (⌘+Enter 发送)"
          @keydown="handleComposerKey"
          @paste="handleComposerPaste"
        />

        <button
          type="button"
          class="submit-circle"
          :class="`state-${sendState}`"
          :disabled="!draftText.trim() && sendState === 'idle'"
          title="发送"
          @click="submitText"
        >
          <Transition name="glyph-pop" mode="out-in">
            <span v-if="sendState === 'loading'" key="spin" class="mini-spinner" />
            <UiIcon v-else-if="sendState === 'success'" key="ok" name="check" :size="16" />
            <UiIcon v-else key="up" name="arrow-up" :size="16.5" />
          </Transition>
        </button>
      </div>

      <!-- Empty State -->
      <div v-if="store.clips.value.length === 0" class="glass-box empty-card">
        暂无剪贴板条目 · 复制文字或截图后按 <kbd class="mono">⌘V</kbd> 秒传至此
      </div>

      <!-- Uniform Single-Column Stream -->
      <TransitionGroup name="stack-slide" tag="div" class="clip-stream">
        <article
          v-for="item in store.clips.value"
          :key="item.id"
          class="glass-box clip-card"
          :class="{ 'is-pinned': item.pinned }"
        >
          <div class="clip-top">
            <div class="clip-tags">
              <span class="kind-dot" :class="`kind-${item.kind}`" />
              <span class="kind-name">
                {{ item.kind === 'text' ? '文本片段' : item.fileName || '无损原图' }}
              </span>
              <span v-if="item.pinned" class="pin-pill">置顶</span>
              <span class="clip-meta-txt mono">{{ formatBytes(item.size) }} · {{ formatTimeShort(item.createdAt) }}</span>
            </div>

            <div class="clip-toolbar">
              <CopyPill
                v-if="item.kind === 'text'"
                :text="item.text || ''"
                label="复制"
                copied-label="已复制"
                compact
              />
              <a
                v-else
                class="pill-btn pill-btn--mini"
                :href="`/api/clips/${item.id}/raw?download=1`"
              >
                <UiIcon name="download" :size="13.5" />
                <span>下载</span>
              </a>

              <CopyPill
                v-if="item.shareCode"
                :text="shareFullUrl(item.shareCode)"
                label="短链"
                copied-label="已复制"
                compact
              />
              <button
                v-else
                type="button"
                class="mini-icon-act"
                title="生成分享短链"
                @click="generateClipShare(item)"
              >
                <UiIcon name="link" :size="14" />
              </button>

              <button
                type="button"
                class="mini-icon-act"
                :class="{ 'is-pinned-btn': item.pinned }"
                title="置顶"
                @click="togglePin(item)"
              >
                <UiIcon name="pin" :size="14" />
              </button>
              <button
                type="button"
                class="mini-icon-act is-danger"
                title="删除"
                @click="removeClip(item.id)"
              >
                <UiIcon name="trash" :size="14" />
              </button>
            </div>
          </div>

          <pre v-if="item.kind === 'text'" class="clip-code mono">{{ item.text }}</pre>

          <a
            v-else-if="item.kind === 'image'"
            class="clip-figure"
            :href="`/api/clips/${item.id}/raw`"
            target="_blank"
            rel="noopener"
          >
            <img :src="`/api/clips/${item.id}/raw`" :alt="item.fileName" loading="lazy" />
          </a>
        </article>
      </TransitionGroup>
    </div>
  </div>
</template>

<style scoped>
.clip-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.prompt-capsule {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 54px;
  padding: 7px;
  border-radius: 27px;
  background: var(--surface-card);
  border: 1px solid var(--border-hairline);
  box-shadow: var(--shadow-card), var(--specular-inset);
}

.prompt-capsule:focus-within {
  border-color: var(--border-strong);
}

.attach-circle {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: 1px solid var(--border-hairline);
  background: var(--surface-recessed);
  color: var(--ink-2);
  display: grid;
  place-items: center;
  cursor: pointer;
  flex-shrink: 0;
  transition: transform 0.22s var(--ease-spring), color 0.18s ease;
}

.attach-circle:hover {
  color: var(--ink-1);
  transform: rotate(90deg);
}

.prompt-field {
  flex: 1;
  border: 0;
  background: transparent;
  color: var(--ink-1);
  font-size: 0.9rem;
  line-height: 1.48;
  resize: none;
  outline: none;
  max-height: 140px;
  padding: 6px 2px;
}

.prompt-field::placeholder {
  color: var(--ink-3);
}

.submit-circle {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: 0;
  background: var(--cta-bg);
  color: var(--cta-fg);
  display: grid;
  place-items: center;
  cursor: pointer;
  flex-shrink: 0;
  transition: transform 0.24s var(--ease-spring), background-color 0.25s ease, opacity 0.2s ease;
}

.submit-circle:disabled {
  opacity: 0.24;
  cursor: default;
}

.submit-circle.state-success {
  background: #10b981;
  color: #ffffff;
}

.mini-spinner {
  width: 15px;
  height: 15px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-right-color: transparent;
  animation: spin360 0.65s linear infinite;
}
@keyframes spin360 {
  to { transform: rotate(360deg); }
}

.empty-card {
  padding: 54px 20px;
  text-align: center;
  font-size: 0.85rem;
  color: var(--ink-3);
}

.empty-card kbd {
  padding: 2px 6px;
  border-radius: 6px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
}

.clip-stream {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.clip-card {
  padding: 15px 18px;
  border-radius: 20px;
}

.clip-card.is-pinned {
  border-color: var(--border-strong);
}

.clip-top {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.clip-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.kind-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.kind-dot.kind-text { background: #0ea5e9; }
.kind-dot.kind-image { background: #10b981; }
.kind-dot.kind-file { background: #f59e0b; }

.kind-name {
  font-size: 0.86rem;
  font-weight: 630;
  color: var(--ink-1);
}

.pin-pill {
  font-size: 0.68rem;
  padding: 1px 7px;
  border-radius: 999px;
  background: rgba(245, 158, 11, 0.14);
  color: #d97706;
  font-weight: 600;
}

.clip-meta-txt {
  font-size: 0.74rem;
  color: var(--ink-3);
}

.clip-toolbar {
  display: flex;
  align-items: center;
  gap: 5px;
}

.pill-btn--mini {
  height: 28px;
  padding: 0 10px;
  font-size: 0.75rem;
}

.mini-icon-act {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 0;
  background: transparent;
  color: var(--ink-3);
  display: inline-grid;
  place-items: center;
  cursor: pointer;
  transition: all 0.18s ease;
}

.mini-icon-act:hover {
  background: var(--surface-recessed-hover);
  color: var(--ink-1);
}

.mini-icon-act.is-pinned-btn {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.11);
}

.mini-icon-act.is-danger:hover {
  color: var(--danger-fg);
  background: var(--danger-bg);
}

.clip-code {
  margin: 12px 0 0;
  padding: 12px 14px;
  border-radius: 14px;
  background: var(--surface-recessed);
  font-size: 0.83rem;
  line-height: 1.56;
  color: var(--ink-1);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 280px;
  overflow-y: auto;
}

.clip-figure {
  display: flex;
  justify-content: center;
  margin-top: 12px;
  border-radius: 14px;
  overflow: hidden;
  background: var(--surface-recessed);
  max-height: 340px;
}

.clip-figure img {
  max-width: 100%;
  max-height: 340px;
  object-fit: contain;
  display: block;
}

.glyph-pop-enter-active,
.glyph-pop-leave-active {
  transition: opacity 0.15s ease, transform 0.22s var(--ease-spring);
}
.glyph-pop-enter-from,
.glyph-pop-leave-to {
  opacity: 0;
  transform: scale(0.65);
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
