<script setup lang="ts">
import { ref } from 'vue'
import {
  canNativeShare,
  formatBytes,
  formatTimeShort,
  nativeShareLink,
  shareFullUrl,
  useDropStore,
} from '../composables/useDropStore'
import CopyPill from './CopyPill.vue'
import MorphDeleteBtn from './MorphDeleteBtn.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()
const supportsNativeShare = canNativeShare()
const revokingMap = ref<Record<string, boolean>>({})

async function revokeShare(code: string) {
  if (revokingMap.value[code]) return
  revokingMap.value[code] = true
  try {
    const res = await fetch(`/api/shares/${code}`, { method: 'DELETE' })
    if (res.ok) {
      store.shares.value = store.shares.value.filter((s) => s.code !== code)
    }
  } finally {
    revokingMap.value[code] = false
  }
}

function storageLabel(st: string): string {
  if (st === 'silo') return 'SILO'
  if (st === 'gdrive') return 'DRIVE'
  return 'PRIVATE'
}
</script>

<template>
  <div class="shares-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">外链管理</h1>
        <span class="pane-count">{{ store.shares.value.length }}</span>
      </div>
    </header>

    <div class="pane-body--narrow">
      <div v-if="store.shares.value.length === 0" class="glass-box sh-empty">
        当前无活跃的公开分享链接
      </div>

      <TransitionGroup v-else name="row-fade" tag="div" class="sh-list">
        <article v-for="sh in store.shares.value" :key="sh.code" class="glass-box sh-card">
          <div class="sh-info">
            <div class="sh-top-line">
              <span class="st-pill mono">{{ storageLabel(sh.storage) }}</span>
              <span v-if="sh.hasPasscode" class="lock-pill">
                <UiIcon name="lock" :size="11.5" />
                <span>加密</span>
              </span>
              <span class="sh-name">{{ sh.fileName }}</span>
            </div>
            <div class="sh-spec mono">
              <span>/s/{{ sh.code }}</span>
              <span>· {{ formatBytes(sh.size) }}</span>
              <span>· 下载 {{ sh.downloads }}{{ sh.maxDownloads ? `/${sh.maxDownloads}` : '' }}</span>
              <span>· {{ sh.expiresAt ? `至 ${formatTimeShort(sh.expiresAt)}` : '长期有效' }}</span>
            </div>
          </div>

          <div class="sh-actions">
            <CopyPill :text="shareFullUrl(sh.code)" label="复制链接" copied-label="已复制" />
            <button
              v-if="supportsNativeShare"
              type="button"
              class="icon-circle-btn"
              title="调起系统分享"
              @click="nativeShareLink({ title: sh.fileName, url: shareFullUrl(sh.code) })"
            >
              <UiIcon name="share-ios" :size="14" />
            </button>
            <button
              type="button"
              class="icon-circle-btn"
              title="查看二维码 / 修改提取码与有效期"
              @click="
                store.openQrModal(
                  sh.fileName,
                  shareFullUrl(sh.code),
                  '免登录扫码取件 · 可修改权限设置',
                  sh.code,
                )
              "
            >
              <UiIcon name="qr" :size="14" />
            </button>
            <a
              class="icon-circle-btn"
              :href="`/s/${sh.code}`"
              target="_blank"
              rel="noopener"
              title="打开公开取件页"
            >
              <UiIcon name="external" :size="14" />
            </a>
            <MorphDeleteBtn
              title="作废外链"
              confirm-label="确认作废"
              busy-label="作废中…"
              :busy="Boolean(revokingMap[sh.code])"
              @confirm="revokeShare(sh.code)"
            />
          </div>
        </article>
      </TransitionGroup>
    </div>
  </div>
</template>

<style scoped>
.sh-empty {
  padding: 50px 20px;
  text-align: center;
  font-size: 0.84rem;
  color: var(--ink-3);
}

.sh-list {
  display: flex;
  flex-direction: column;
  gap: 9px;
}

.sh-card {
  padding: 13px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.sh-info {
  min-width: 0;
  flex: 1;
}

.sh-top-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.st-pill {
  font-size: 0.64rem;
  font-weight: 600;
  letter-spacing: 0.05em;
  padding: 2px 6px;
  border-radius: 5px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.lock-pill {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 0.68rem;
  padding: 2px 7px;
  border-radius: 5px;
  background: rgba(234, 88, 12, 0.12);
  color: var(--led-orange);
}

.sh-name {
  font-size: 0.88rem;
  font-weight: 630;
  word-break: break-word;
}

.sh-spec {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 0.73rem;
  color: var(--ink-3);
}

.sh-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

@media (max-width: 720px), (max-height: 520px) and (pointer: coarse) {
  .sh-card {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    padding: 12px 13px;
    border-radius: 16px;
  }
  .sh-actions {
    width: 100%;
    padding-top: 8px;
    border-top: 1px solid var(--border-hairline);
    justify-content: flex-start;
    flex-wrap: wrap;
    gap: 7px;
  }
}

.row-fade-enter-active,
.row-fade-leave-active {
  transition: all 0.26s var(--ease-spring);
}
.row-fade-enter-from,
.row-fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
