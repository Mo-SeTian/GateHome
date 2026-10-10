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

// Each home page checks LAN addresses in the background; clicks only read results.
export function createAutoUrlResolver() {
  const probes = new Map<string, Promise<void>>()
  const reachable = new Set<string>()
  return {
    check(items: { url: string; lanUrl?: string }[]) {
      const pending: Promise<void>[] = []
      for (const item of items) {
        const lanUrl = item.lanUrl?.trim()
        if (!lanUrl || lanUrl === item.url)
          continue
        let probe = probes.get(lanUrl)
        if (!probe) {
          probe = resolveAutoUrl(item.url, lanUrl).then((chosen) => {
            if (chosen === lanUrl)
              reachable.add(lanUrl)
          })
          probes.set(lanUrl, probe)
        }
        pending.push(probe)
      }
      return Promise.all(pending)
    },
    resolve(url: string, lanUrl?: string) {
      const lan = lanUrl?.trim()
      return lan && reachable.has(lan) ? lan : url
    },
  }
}
