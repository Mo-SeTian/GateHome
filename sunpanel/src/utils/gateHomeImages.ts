import { uploadImage } from '@/api/system/file'
import type { GateHomeRoute } from './gateHome'

export async function localizeGateHomeIcon(route: GateHomeRoute, cache = new Map<string, Promise<Panel.ItemIcon>>): Promise<Panel.ItemIcon | undefined> {
  if (!route.image || !route.imageData || route.imageUnavailable) return
  let pending = cache.get(route.image)
  if (!pending) {
    pending = (async () => {
      if (!/^data:image\/png;base64,[A-Za-z0-9+/]+=*$/.test(route.imageData!) || route.imageData!.length > 128 * 1024) throw new Error('反代图片数据无效')
      const decoded = atob(route.imageData!.split(',')[1])
      const file = new File([Uint8Array.from(decoded, char => char.charCodeAt(0))], 'gatehome-icon.png', { type: 'image/png' })
      const result = await uploadImage(file)
      if (result.code !== 0) throw new Error(result.msg || '反代图片保存失败，请重试')
      return { itemType: 2, src: result.data.imageUrl, backgroundColor: 'transparent' }
    })()
    cache.set(route.image, pending)
  }
  return pending
}
