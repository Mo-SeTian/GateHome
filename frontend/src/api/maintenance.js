import { request } from './client.js'

const post = (path, body = {}, options = {}) => request(path, { method: 'POST', body, ...options })
export const maintenanceApi = {
  capacity: (signal) => request('/api/maintenance/capacity', { signal }),
  history: (signal) => request('/api/maintenance/update-backups', { signal }),
  checkRelease: (channel, signal) => post('/api/maintenance/check-online-update', { channel }, { signal }),
  onlineStatus: (signal) => request('/api/maintenance/online-update-status', { signal }),
  updateOnline: (channel, version) => post('/api/maintenance/download-online-update', { channel, version }),
  inspect: (kind, file, password, signal) => {
    const body = new FormData()
    body.append('file', file)
    if (kind === 'backup') body.append('password', password)
    return post(`/api/maintenance/inspect-${kind}`, body, { signal })
  },
  apply: (kind, id) => post(`/api/maintenance/apply-${kind}`, { id }),
  rollback: (id) => post('/api/maintenance/rollback-update', { id }),
  backup: (password, signal) =>
    post('/api/maintenance/backup', { password }, { responseType: 'blob', signal }),
  webdav: (signal) => request('/api/backups/webdav', { signal }),
  saveWebDAV: (body) => request('/api/backups/webdav', { method: 'PUT', body }),
  testWebDAV: (signal) => post('/api/backups/webdav/test', {}, { signal }),
  runWebDAV: () => post('/api/backups/webdav/run'),
  files: (signal) => request('/api/backups/webdav/files', { signal }),
  inspectRemote: (body, signal) => post('/api/backups/webdav/inspect', body, { signal }),
  downloadRemote: (name, signal) =>
    post('/api/backups/webdav/download', { name }, { responseType: 'blob', signal })
}

export function webDAVPayload(snapshot, config, secrets) {
  if (secrets.backupPassword !== secrets.confirmPassword) throw new Error('两次输入的备份加密密码不一致')
  const body = { revision: snapshot.revision, config: { ...config } }
  if (secrets.clearPassword) body.password = ''
  else if (secrets.password) body.password = secrets.password
  if (secrets.backupPassword) body.backup_password = secrets.backupPassword
  return body
}

export function onlineUpdateActive(phase) {
  return ['checking', 'downloading', 'verifying', 'restarting'].includes(phase)
}

export function maintenancePhase(phase) {
  return (
    {
      checking: '正在检查发行版本',
      downloading: '正在下载更新包',
      verifying: '正在校验与安排更新',
      restarting: '已安排重启',
      error: '更新失败',
      snapshot: '正在生成一致快照',
      encrypt: '正在加密备份',
      upload: '正在上传、校验与清理',
      done: '备份完成',
      failed: '备份失败'
    }[phase] || '等待任务'
  )
}

export function downloadBackup(blob, name) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 30000)
}
