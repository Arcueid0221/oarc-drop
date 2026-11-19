import { onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

const isKeyboardOpen = ref(false)
const isCoarsePointer = ref(false)
const viewportHeight = ref(typeof window !== 'undefined' ? window.innerHeight : 800)

let viewportUsers = 0
let baselineHeight = typeof window !== 'undefined' ? window.innerHeight : 800
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

function isCompactDevice(): boolean {
  if (typeof window === 'undefined') return false
  return (
    window.innerWidth <= 860 ||
    (window.innerHeight <= 520 && window.matchMedia('(pointer: coarse)').matches)
  )
}

function attachViewportListeners() {
  if (typeof window === 'undefined' || cleanupViewport) return

  const mq = window.matchMedia('(pointer: coarse)')
  const syncPointer = () => {
    isCoarsePointer.value = mq.matches || navigator.maxTouchPoints > 0
  }
  syncPointer()

  const applyKeyboardState = (open: boolean) => {
    const next = open && isCompactDevice()
    isKeyboardOpen.value = next
    if (next) {
      document.documentElement.setAttribute('data-keyboard-open', '1')
    } else {
      document.documentElement.removeAttribute('data-keyboard-open')
    }
  }

  let focusBlurTimer: number | undefined

  const syncMetrics = () => {
    const vv = window.visualViewport
    const curH = vv ? Math.round(vv.height) : window.innerHeight
    viewportHeight.value = curH
    document.documentElement.style.setProperty('--vv-height', `${curH}px`)

    const editing = isEditableTarget(document.activeElement)
    if (!editing) {
      // Track maximum non-keyboard height (updates on orientation change)
      baselineHeight = Math.max(curH, window.innerHeight)
      document.documentElement.style.setProperty('--kb-inset', '0px')
      applyKeyboardState(false)
      return
    }

    const layoutH = Math.max(window.innerHeight, baselineHeight)
    const shrunkBy = layoutH - curH
    // On iOS Safari, window.innerHeight stays full height while visualViewport shrinks; expose exact bottom keyboard inset
    const iosKbInset = vv ? Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop)) : 0
    document.documentElement.style.setProperty(
      '--kb-inset',
      iosKbInset > 80 && isCompactDevice() ? `${iosKbInset}px` : '0px',
    )
    // Android Chrome & iOS Safari shrink visualViewport by >= 120px when software keyboard opens.
    // On some Android WebViews with resizes-content, layoutH itself drops vs baselineHeight.
    const kbOpen = shrunkBy > 120 || (isCoarsePointer.value && editing && window.innerWidth <= 860)
    applyKeyboardState(kbOpen)
  }

  const onFocusIn = () => {
    if (focusBlurTimer) window.clearTimeout(focusBlurTimer)
    if (isEditableTarget(document.activeElement) && isCompactDevice()) {
      // Update immediately & re-check after keyboard slide animation settles
      syncMetrics()
      focusBlurTimer = window.setTimeout(syncMetrics, 220)
    }
  }

  const onFocusOut = () => {
    if (focusBlurTimer) window.clearTimeout(focusBlurTimer)
    focusBlurTimer = window.setTimeout(syncMetrics, 120)
  }

  const onOrientation = () => {
    baselineHeight = window.innerHeight
    window.setTimeout(() => {
      baselineHeight = window.innerHeight
      syncMetrics()
    }, 260)
  }

  syncMetrics()
  window.visualViewport?.addEventListener('resize', syncMetrics)
  window.visualViewport?.addEventListener('scroll', syncMetrics)
  window.addEventListener('resize', syncMetrics)
  window.addEventListener('orientationchange', onOrientation)
  document.addEventListener('focusin', onFocusIn, true)
  document.addEventListener('focusout', onFocusOut, true)
  mq.addEventListener?.('change', syncPointer)

  cleanupViewport = () => {
    if (focusBlurTimer) window.clearTimeout(focusBlurTimer)
    window.visualViewport?.removeEventListener('resize', syncMetrics)
    window.visualViewport?.removeEventListener('scroll', syncMetrics)
    window.removeEventListener('resize', syncMetrics)
    window.removeEventListener('orientationchange', onOrientation)
    document.removeEventListener('focusin', onFocusIn, true)
    document.removeEventListener('focusout', onFocusOut, true)
    mq.removeEventListener?.('change', syncPointer)
    document.documentElement.removeAttribute('data-keyboard-open')
    document.documentElement.style.removeProperty('--kb-inset')
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
    viewportHeight,
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
