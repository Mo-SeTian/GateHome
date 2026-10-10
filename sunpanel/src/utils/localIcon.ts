import { buildIcon, loadIcon } from '@iconify/vue'

export async function createLocalIconPng(name: string, color: string): Promise<File> {
  const icon = buildIcon(await loadIcon(name.trim()), { height: 128 })
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  for (const [key, value] of Object.entries(icon.attributes))
    svg.setAttribute(key, value)
  svg.setAttribute('color', color)
  svg.innerHTML = icon.body

  const url = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(svg)], { type: 'image/svg+xml' }))
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('Icon image could not be loaded'))
      image.src = url
    })
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 256
    const context = canvas.getContext('2d')
    if (!context)
      throw new Error('Canvas is unavailable')
    // Match the online icon's half-size glyph and transparent, editable background.
    const scale = 128 / Math.max(image.width, image.height)
    const width = image.width * scale
    const height = image.height * scale
    context.drawImage(image, (256 - width) / 2, (256 - height) / 2, width, height)
    const blob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('Icon image could not be encoded')), 'image/png')
    })
    return new File([blob], 'online-icon.png', { type: 'image/png' })
  }
  finally {
    URL.revokeObjectURL(url)
  }
}
