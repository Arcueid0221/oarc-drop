<script setup lang="ts">
import {
  formatBytes,
  formatTimeShort,
  shareFullUrl,
  useDropStore,
} from '../composables/useDropStore'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()

async function revokeShare(code: string) {
  const res = await fetch(`/api/shares/${code}`, { method: 'DELETE' })
  if (res.ok) {
    store.shares.value = store.shares.value.filter((s) => s.code !== code)
  }
}

function storageLabel(st: string): string {
  if (st === 'silo') return 'Silo'
  if (st === 'gdrive') return 'Drive'
  return 'Local'
}
</script>

<template>
  <div class="shares-page">
    <header class="pane-header">
      <div class="pane-title-wrap">
        <h1 class="pane-title">外链管理</h1>
        <p class="pane-subtitle">集中管理你对外公开分享的 `/s/短码` · 支持扫码免登录取件与随时作废</p>
      </div>

      <div class="pane-actions">
        <span class="pill-count mono">{{ store.shares.value.length }} 条有效</span>
      </div>
    </header>

    <div class="pane-body--narrow">
      <div v-if="store.shares.value.length === 0" class="glass-box sh-empty">
        暂无活跃的公开分享链接
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
              type="button"
              class="icon-circle-btn"
              title="查看取件二维码"
              @click="store.openQrModal(sh.fileName, shareFullUrl(sh.code), '外部访客扫码免登录取件')"
            >
              <UiIcon name="qr" :size="14.5" />
            </button>
            <a
              class="icon-circle-btn"
              :href="`/s/${sh.code}`"
              target="_blank"
              rel="noopener"
              title="打开公开取件页"
            >
              <UiIcon name="external" :size="14.5" />
            </a>
            <button
              type="button"
              class="icon-circle-btn danger-hover"
              title="撤销分享"
              @click="revokeShare(sh.code)"
            >
              <UiIcon name="trash" :size="14.5" />
            </button>
          </div>
        </article>
      </TransitionGroup>
    </div>
  </div>
</template>

<style scoped>
.pill-count {
  padding: 4px 11px;
  border-radius: 999px;
  font-size: 0.76rem;
  background: var(--surface-recessed);
  color: var(--ink-2);
  border: 1px solid var(--border-hairline);
}

.sh-empty {
  padding: 56px 20px;
  text-align: center;
  font-size: 0.85rem;
  color: var(--ink-3);
}

.sh-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.sh-card {
  padding: 14px 18px;
  border-radius: 20px;
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}

.sh-top-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}

.st-pill {
  font-size: 0.69rem;
  font-weight: 620;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--surface-recessed);
  color: var(--ink-2);
}

.lock-pill {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 0.7rem;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(245, 158, 11, 0.13);
  color: #d97706;
}

.sh-name {
  font-size: 0.89rem;
  font-weight: 630;
  word-break: break-all;
}

.sh-spec {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 0.74rem;
  color: var(--ink-3);
}

.sh-actions {
  display: flex;
  align-items: center;
  gap: 7px;
}

.danger-hover:hover {
  color: var(--danger-fg);
  border-color: rgba(225, 29, 72, 0.3);
}

.row-fade-enter-active,
.row-fade-leave-active {
  transition: all 0.28s var(--ease-spring);
}
.row-fade-enter-from,
.row-fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
