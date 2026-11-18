<script setup lang="ts">
import { shareFullUrl, useDropStore } from '../composables/useDropStore'
import CopyPill from './CopyPill.vue'
import UiIcon from './UiIcon.vue'

const store = useDropStore()
defineExpose({ shareFullUrl })
</script>

<template>
  <Teleport to="body">
    <Transition name="lens-modal">
      <div v-if="store.qrModal.value" class="qr-backdrop" @click.self="store.closeQrModal">
        <div class="liquid-chrome qr-sheet">
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

          <div class="qr-actions">
            <CopyPill :text="store.qrModal.value.url" label="复制链接 / 内容" copied-label="已复制" />
            <a
              v-if="store.qrModal.value.url.startsWith('http')"
              :href="store.qrModal.value.url"
              target="_blank"
              rel="noopener"
              class="pill-btn"
            >
              <UiIcon name="external" :size="14" />
              <span>新窗口打开</span>
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
  background: rgba(15, 23, 42, 0.36);
  backdrop-filter: blur(16px);
  display: grid;
  place-items: center;
  padding: 20px;
}

.qr-sheet {
  width: min(360px, 100%);
  border-radius: 26px;
  padding: 20px 22px 22px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
}

.qr-head {
  width: 100%;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.qr-title {
  font-size: 0.96rem;
  font-weight: 660;
  word-break: break-word;
}

.qr-sub {
  margin-top: 2px;
  font-size: 0.74rem;
  color: var(--ink-3);
}

.qr-frame {
  width: 208px;
  height: 208px;
  padding: 10px;
  border-radius: 20px;
  background: #ffffff;
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.08);
  display: grid;
  place-items: center;
}

.qr-svg {
  width: 100%;
  height: 100%;
  display: block;
}

.qr-url {
  width: 100%;
  padding: 8px 11px;
  border-radius: 12px;
  background: var(--surface-recessed);
  border: 1px solid var(--border-hairline);
  font-size: 0.73rem;
  color: var(--ink-2);
  word-break: break-all;
  max-height: 76px;
  overflow-y: auto;
  text-align: center;
}

.qr-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
