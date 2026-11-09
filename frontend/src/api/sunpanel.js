export function sunPanelLaunchURL(config, hostname, external = false) {
  const value = (external ? config.external_url : config.launch_url)?.trim() || ''
  if (!value) return external ? '' : `http://${hostname}:${Number(config.port) || 16680}/`
  if (/[\s\\]/.test(value)) return ''
  try {
    const url = new URL(value, 'http://gatehome.invalid')
    if (url.username || url.password || !['http:', 'https:'].includes(url.protocol)) return ''
    if (value.startsWith('/')) return !external && !value.startsWith('//') ? value : ''
    return /^https?:\/\//i.test(value) ? value : ''
  } catch {
    return ''
  }
}
