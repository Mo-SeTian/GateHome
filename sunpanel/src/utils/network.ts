// Probe from the browser: the server may be on a different network.
export async function resolveAutoUrl(url: string, lanUrl?: string): Promise<string> {
  if (!lanUrl || lanUrl === url)
    return url

  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    const target = new URL(lanUrl)
    if (!['http:', 'https:'].includes(target.protocol) || target.username || target.password)
      return url

    // An opaque response (including a login page or HTTP error) proves reachability.
    const probe = fetch(lanUrl, {
      method: 'HEAD',
      mode: 'no-cors',
      cache: 'no-store',
      credentials: 'omit',
      referrerPolicy: 'no-referrer',
      redirect: 'follow',
      signal: controller.signal,
    }).then(() => lanUrl, () => url)
    const fallback = new Promise<string>((resolve) => {
      timer = setTimeout(() => {
        resolve(url)
        controller.abort()
      }, 1500)
    })
    return await Promise.race([probe, fallback])
  }
  catch {
    return url
  }
  finally {
    clearTimeout(timer)
  }
}

const autoUrlCacheMs = 60_000

// Each home page checks LAN addresses in the background; clicks only read results.
export function createAutoUrlResolver(onChange?: () => void) {
  const probes = new Map<string, Promise<void>>()
  const reachable = new Set<string>()
  const checkedAt = new Map<string, number>()
  const lastCheckedAt = new Map<string, number>()
  return {
    invalidate() {
      probes.clear()
      reachable.clear()
      checkedAt.clear()
      onChange?.()
    },
    check(items: { url: string; lanUrl?: string }[]) {
      const pending: Promise<void>[] = []
      for (const item of items) {
        const lanUrl = item.lanUrl?.trim()
        if (!lanUrl || lanUrl === item.url)
          continue
        let probe = probes.get(lanUrl)
        const checked = checkedAt.get(lanUrl)
        if (checked !== undefined && Date.now() - checked >= autoUrlCacheMs) {
          probe = undefined
          reachable.delete(lanUrl)
          checkedAt.delete(lanUrl)
        }
        if (!probe) {
          probe = resolveAutoUrl(item.url, lanUrl).then((chosen) => {
            if (probes.get(lanUrl) !== probe)
              return
            if (chosen === lanUrl)
              reachable.add(lanUrl)
            checkedAt.set(lanUrl, Date.now())
            lastCheckedAt.set(lanUrl, Date.now())
            onChange?.()
          })
          probes.set(lanUrl, probe)
        }
        pending.push(probe)
      }
      return Promise.all(pending)
    },
    inspect(url: string, lanUrl?: string) {
      const lan = lanUrl?.trim()
      const checked = lan ? checkedAt.get(lan) : undefined
      const state = !lan ? 'unconfigured' : lan === url ? 'same' : checked === undefined ? (probes.has(lan) ? 'checking' : 'unchecked') : Date.now() - checked >= autoUrlCacheMs ? 'expired' : reachable.has(lan) ? 'reachable' : 'unreachable'
      return { state, checkedAt: lan ? lastCheckedAt.get(lan) : undefined }
    },
    resolve(url: string, lanUrl?: string) {
      const lan = lanUrl?.trim()
      const checked = lan ? checkedAt.get(lan) : undefined
      if (lan && reachable.has(lan) && checked !== undefined && Date.now() - checked < autoUrlCacheMs)
        return lan
      return url
    },
  }
}

export function startAutoUrlRefresh(
  resolver: ReturnType<typeof createAutoUrlResolver>,
  getItems: () => { url: string; lanUrl?: string }[],
  isEnabled: () => boolean,
) {
  const connection = (navigator as Navigator & { connection?: EventTarget }).connection
  let networkTimer: ReturnType<typeof setTimeout> | undefined
  function check() {
    if (isEnabled() && document.visibilityState !== 'hidden' && navigator.onLine !== false)
      resolver.check(getItems())
  }
  function refreshVisible() {
    if (document.visibilityState === 'hidden')
      return
    refreshNetwork()
  }
  function refreshNetwork() {
    resolver.invalidate()
    clearTimeout(networkTimer)
    networkTimer = setTimeout(check, 200)
  }
  function restorePage(event: PageTransitionEvent) {
    if (event.persisted)
      refreshVisible()
  }
  const timer = setInterval(refreshVisible, autoUrlCacheMs)
  document.addEventListener('visibilitychange', refreshVisible)
  window.addEventListener('online', refreshNetwork)
  window.addEventListener('offline', refreshNetwork)
  window.addEventListener('pageshow', restorePage)
  connection?.addEventListener('change', refreshNetwork)
  return () => {
    clearInterval(timer)
    clearTimeout(networkTimer)
    document.removeEventListener('visibilitychange', refreshVisible)
    window.removeEventListener('online', refreshNetwork)
    window.removeEventListener('offline', refreshNetwork)
    window.removeEventListener('pageshow', restorePage)
    connection?.removeEventListener('change', refreshNetwork)
    resolver.invalidate()
  }
}
