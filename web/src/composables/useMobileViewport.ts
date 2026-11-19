import { onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

const isKeyboardOpen = ref(false)
const isCoarsePointer = ref(false)

let viewportUsers = 0
let cleanupViewport: (() => void) | null = null

function isEditableTarget(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (tag === 'INPUT') {
    const type = ((el as HTMLInputElement).type || 'text').toLowerCase()
    return !['button', 'submit', 'reset', 'checkbox', 'radio', 'range', 'file', 'color'].includes(
      type,
    )
  }
  return (el as HTMLElement).isContentEditable === true
}

function attachViewportListeners() {
  if (typeof window === 'undefined' || cleanupViewport) return

  const mq = window.matchMedia('(pointer: coarse)')
  const syncPointer = () => {
    isCoarsePointer.value = mq.matches || navigator.maxTouchPoints > 0
  }
  syncPointer()

  let blurTimer: number | undefined

  const setKbOpen = (open: boolean) => {
    const next = open && window.innerWidth <= 860 && isCoarsePointer.value
    if (isKeyboardOpen.value === next) return
    isKeyboardOpen.value = next
    // Only toggle a lightweight body class to fade out fixed bottom dock (never mutate root layout geometry or CSS vars!)
    document.body.classList.toggle('oarc-kb-open', next)
  }

  const onFocusIn = (e: FocusEvent) => {
    if (blurTimer) window.clearTimeout(blurTimer)
    if (isEditableTarget(e.target as Element | null)) {
      setKbOpen(true)
    }
  }

  const onFocusOut = () => {
    if (blurTimer) window.clearTimeout(blurTimer)
    blurTimer = window.setTimeout(() => {
      if (!isEditableTarget(document.activeElement)) {
        setKbOpen(false)
      }
    }, 120)
  }

  document.addEventListener('focusin', onFocusIn, true)
  document.addEventListener('focusout', onFocusOut, true)
  mq.addEventListener?.('change', syncPointer)

  cleanupViewport = () => {
    if (blurTimer) window.clearTimeout(blurTimer)
    document.removeEventListener('focusin', onFocusIn, true)
    document.removeEventListener('focusout', onFocusOut, true)
    mq.removeEventListener?.('change', syncPointer)
    document.body.classList.remove('oarc-kb-open')
    isKeyboardOpen.value = false
    cleanupViewport = null
  }
}

export function useMobileViewport() {
  onMounted(() => {
    viewportUsers++
    attachViewportListeners()
  })

  onBeforeUnmount(() => {
    viewportUsers = Math.max(0, viewportUsers - 1)
    if (viewportUsers === 0 && cleanupViewport) {
      cleanupViewport()
    }
  })

  return {
    isKeyboardOpen,
    isCoarsePointer,
  }
}

// Reference-counted body scroll lock for modals/bottom-sheets (prevents iOS background scroll bleed)
let scrollLockCount = 0

function acquireScrollLock() {
  if (typeof document === 'undefined') return
  scrollLockCount++
  if (scrollLockCount === 1) {
    document.documentElement.classList.add('oarc-scroll-locked')
    document.body.classList.add('oarc-scroll-locked')
  }
}

function releaseScrollLock() {
  if (typeof document === 'undefined') return
  scrollLockCount = Math.max(0, scrollLockCount - 1)
  if (scrollLockCount === 0) {
    document.documentElement.classList.remove('oarc-scroll-locked')
    document.body.classList.remove('oarc-scroll-locked')
  }
}

export function useBodyScrollLock(active: Ref<boolean>) {
  let held = false
  const sync = (val: boolean) => {
    if (val && !held) {
      held = true
      acquireScrollLock()
    } else if (!val && held) {
      held = false
      releaseScrollLock()
    }
  }
  watch(active, (val) => sync(Boolean(val)), { immediate: true })
  onBeforeUnmount(() => sync(false))
}
