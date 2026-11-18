<script setup lang="ts">
withDefaults(
  defineProps<{
    name:
      | 'arrow-up'
      | 'plus'
      | 'copy'
      | 'check'
      | 'share'
      | 'pin'
      | 'trash'
      | 'download'
      | 'external'
      | 'sun'
      | 'moon'
      | 'layers'
      | 'image'
      | 'disc'
      | 'link'
      | 'search'
      | 'refresh'
      | 'sliders'
      | 'clock'
      | 'lock'
      | 'play'
      | 'pause'
      | 'prev'
      | 'next'
      | 'close'
      | 'chevron-down'
      | 'file'
      | 'sparkles'
    size?: number
  }>(),
  { size: 16 },
)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.85"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    class="ui-icon"
  >
    <template v-if="name === 'arrow-up'">
      <path d="M12 19V5" />
      <path d="m5 12 7-7 7 7" />
    </template>
    <template v-else-if="name === 'plus'">
      <path d="M12 5v14M5 12h14" />
    </template>
    <template v-else-if="name === 'copy'">
      <rect x="9" y="9" width="11" height="11" rx="2.5" />
      <path d="M5 15H4.5A2.5 2.5 0 0 1 2 12.5v-8A2.5 2.5 0 0 1 4.5 2h8A2.5 2.5 0 0 1 15 4.5V5" />
    </template>
    <template v-else-if="name === 'check'">
      <path d="M20 6 9 17l-5-5" stroke-width="2.2" />
    </template>
    <template v-else-if="name === 'share' || name === 'link'">
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </template>
    <template v-else-if="name === 'pin'">
      <path d="M12 17v5" />
      <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16h14v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 1 1 0 0 0 1-1V4a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v1a1 1 0 0 0 1 1 1 1 0 0 1 1 1z" />
    </template>
    <template v-else-if="name === 'trash'">
      <path d="M3 6h18" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
    </template>
    <template v-else-if="name === 'download'">
      <path d="M12 3v12" />
      <path d="m7 10 5 5 5-5" />
      <path d="M5 21h14" />
    </template>
    <template v-else-if="name === 'external'">
      <path d="M7 17 17 7" />
      <path d="M8 7h9v9" />
    </template>
    <template v-else-if="name === 'sun'">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2m-7.07-14.07 1.41 1.41m11.32 11.32 1.41 1.41M2 12h2m16 0h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </template>
    <template v-else-if="name === 'moon'">
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </template>
    <template v-else-if="name === 'layers'">
      <path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z" />
      <path d="m22 12.5-8.97 4.08a2 2 0 0 1-1.66 0L2 12.5" />
      <path d="m22 17.5-8.97 4.08a2 2 0 0 1-1.66 0L2 17.5" />
    </template>
    <template v-else-if="name === 'image'">
      <rect width="18" height="18" x="3" y="3" rx="3" />
      <circle cx="9" cy="9" r="2" />
      <path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21" />
    </template>
    <template v-else-if="name === 'disc'">
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="2.5" />
      <path d="M12 7a5 5 0 0 1 5 5" />
    </template>
    <template v-else-if="name === 'search'">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </template>
    <template v-else-if="name === 'refresh'">
      <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
      <path d="M3 3v5h5" />
      <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" />
      <path d="M16 16h5v5" />
    </template>
    <template v-else-if="name === 'sliders'">
      <line x1="4" x2="10" y1="6" y2="6" />
      <line x1="14" x2="20" y1="6" y2="6" />
      <circle cx="12" cy="6" r="2" />
      <line x1="4" x2="14" y1="12" y2="12" />
      <line x1="18" x2="20" y1="12" y2="12" />
      <circle cx="16" cy="12" r="2" />
      <line x1="4" x2="8" y1="18" y2="18" />
      <line x1="12" x2="20" y1="18" y2="18" />
      <circle cx="10" cy="18" r="2" />
    </template>
    <template v-else-if="name === 'clock'">
      <circle cx="12" cy="12" r="9" />
      <polyline points="12 7 12 12 15.5 14" />
    </template>
    <template v-else-if="name === 'lock'">
      <rect width="16" height="11" x="4" y="11" rx="2.5" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </template>
    <template v-else-if="name === 'play'">
      <polygon points="7 4 20 12 7 20 7 4" fill="currentColor" stroke="none" />
    </template>
    <template v-else-if="name === 'pause'">
      <rect x="6" y="4" width="4" height="16" rx="1.2" fill="currentColor" stroke="none" />
      <rect x="14" y="4" width="4" height="16" rx="1.2" fill="currentColor" stroke="none" />
    </template>
    <template v-else-if="name === 'prev'">
      <polygon points="19 20 9 12 19 4 19 20" fill="currentColor" stroke="none" />
      <line x1="5" x2="5" y1="5" y2="19" stroke-width="2.2" />
    </template>
    <template v-else-if="name === 'next'">
      <polygon points="5 4 15 12 5 20 5 4" fill="currentColor" stroke="none" />
      <line x1="19" x2="19" y1="5" y2="19" stroke-width="2.2" />
    </template>
    <template v-else-if="name === 'close'">
      <path d="M18 6 6 18M6 6l12 12" />
    </template>
    <template v-else-if="name === 'chevron-down'">
      <path d="m6 9 6 6 6-6" />
    </template>
    <template v-else-if="name === 'file'">
      <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z" />
      <polyline points="14 2 14 8 20 8" />
    </template>
    <template v-else-if="name === 'sparkles'">
      <path d="m12 3-1.9 5.8a2 2 0 0 1-1.3 1.3L3 12l5.8 1.9a2 2 0 0 1 1.3 1.3L12 21l1.9-5.8a2 2 0 0 1 1.3-1.3L21 12l-5.8-1.9a2 2 0 0 1-1.3-1.3Z" />
    </template>
  </svg>
</template>

<style scoped>
.ui-icon {
  flex-shrink: 0;
  display: block;
}
</style>
