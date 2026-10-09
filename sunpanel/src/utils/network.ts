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
