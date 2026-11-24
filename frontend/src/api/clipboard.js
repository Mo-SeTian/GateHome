export async function copyText(text) {
  const value = String(text ?? '')
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return
    }
  } catch {
    // Fall through to execCommand copy fallback on insecure HTTP origins or permission denial.
  }
  // LAN HTTP pages have no Clipboard API; copy during the original user gesture.
  if (
    typeof document === 'undefined' ||
    document.visibilityState === 'hidden' ||
    (typeof document.hasFocus === 'function' && !document.hasFocus())
  ) {
    throw new Error('Page inactive')
  }
  const active = document.activeElement
  const input = document.createElement('textarea')
  input.value = value
  input.readOnly = true
  input.style.cssText = 'position:fixed;left:-9999px;top:0;font-size:16px'
  document.body.append(input)
  try {
    input.select()
    if (typeof document.execCommand !== 'function' || !document.execCommand('copy')) {
      throw new Error('Copy rejected')
    }
  } finally {
    input.remove()
    if (document.visibilityState === 'visible' && (!document.hasFocus || document.hasFocus())) {
      active?.focus?.({ preventScroll: true })
    }
  }
}
