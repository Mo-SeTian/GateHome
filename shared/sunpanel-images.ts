interface RouteImage { id: string; image?: string; imageData?: string; imageUnavailable?: boolean }

async function thumbnail(response: Response) {
  const blob = await response.blob()
  const src = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = () => reject(new Error('图片读取失败'))
    reader.readAsDataURL(blob)
  })
  const image = new Image()
  image.src = src
  await image.decode()
  const canvas = document.createElement('canvas')
  canvas.width = canvas.height = 128
  const context = canvas.getContext('2d')
  if (!context || !image.naturalWidth || !image.naturalHeight) throw new Error('图片无法解码')
  const scale = 128 / Math.max(image.naturalWidth, image.naturalHeight)
  const width = image.naturalWidth * scale, height = image.naturalHeight * scale
  context.drawImage(image, (128 - width) / 2, (128 - height) / 2, width, height)
  return canvas.toDataURL('image/png')
}

// Both frontends fetch with their own GateHome session, then transfer bounded PNGs.
export async function withRouteImages<T extends RouteImage>(routes: T[], signal?: AbortSignal): Promise<T[]> {
  const cache = new Map<string, Promise<{ image: string; imageData: string }>>()
  const result: T[] = []
  for (const route of routes) {
    if (signal?.aborted) throw new DOMException('图片读取已取消', 'AbortError')
    if (!route.image) { result.push({ ...route, image: '' }); continue }
    try {
      let pending = cache.get(route.image)
      if (!pending) {
        pending = (async () => {
          const response = await fetch(`/api/sunpanel/routes/${encodeURIComponent(route.id)}/image`, { credentials: 'same-origin', signal })
          if (!response.ok) throw new Error('图片暂不可读取')
          const image = response.headers.get('ETag')?.replaceAll('"', '') || ''
          if (!/^[a-f0-9]{64}$/.test(image)) throw new Error('图片校验信息无效')
          return { image, imageData: await thumbnail(response) }
        })()
        cache.set(route.image, pending)
      }
      result.push({ ...route, ...await pending })
    } catch (error) {
      if (signal?.aborted) throw new DOMException('图片读取已取消', 'AbortError')
      result.push({ ...route, imageUnavailable: true })
    }
  }
  return result
}
