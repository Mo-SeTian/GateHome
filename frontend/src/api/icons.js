import { request } from './client.js'

export const iconSources = {
  builtin: '系统内置',
  imported: 'GitHub 导入',
  uploads: '本地上传'
}

export const iconStyles = {
  'border-radius': '圆角方块',
  circle: '圆形',
  svg: '矢量',
  original: '原始形态'
}

export const iconCategories = {
  media: '影音媒体',
  download: '下载传输',
  nas: 'NAS 与系统',
  tool: '运维网络',
  uploads: '我的上传',
  other: '其他'
}

export function routeImageUrl(ref) {
  if (ref.startsWith('/icons/')) {
    return '/api/icons/file/' + ref.slice('/icons/'.length).split('/').map(encodeURIComponent).join('/')
  }
  return '/api/route-images/' + encodeURIComponent(ref)
}

export const iconsApi = {
  list: (filters, signal) => {
    const params = new URLSearchParams(Object.entries(filters).filter(([, value]) => value !== ''))
    return request('/api/icons?' + params, { signal })
  },
  stats: signal => request('/api/icons/stats', { signal }),
  importStatus: signal => request('/api/icons/import-status', { signal }),
  importGitHub: body => request('/api/icons/import-github', { method: 'POST', body }),
  upload: files => {
    const body = new FormData()
    for (const file of files) body.append('file', file)
    return request('/api/icons/upload', { method: 'POST', body })
  },
  remove: id => request('/api/icons/' + id.split('/').map(encodeURIComponent).join('/'), { method: 'DELETE', body: {} }),
  reindex: () => request('/api/icons/reindex', { method: 'POST', body: {} })
}
