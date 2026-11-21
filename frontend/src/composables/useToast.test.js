import test from 'node:test'
import assert from 'node:assert/strict'
import { useToast } from './useToast.js'

test('routine success and info toasts do not clutter Notification Center while warnings, errors and fault history persist', () => {
  const toast = useToast()
  toast.clearAllNotifications()

  const successId = toast.success('全局配置已保存')
  const infoId = toast.info('已退出管理会话')
  assert.equal(toast.toasts.value.length, 2)
  assert.equal(toast.notifications.value.length, 0)
  assert.equal(toast.unreadCount.value, 0)

  const warnId = toast.warn('配置已保存，监听状态刷新失败，请稍后刷新')
  const errorId = toast.error('保存失败，请重试')
  const explicitId = toast.info('后台同步完成', null, null, { persist: true })

  assert.equal(toast.notifications.value.length, 3)
  assert.equal(toast.unreadCount.value, 3)
  assert.equal(toast.notifications.value[0].message, '后台同步完成')
  assert.equal(toast.notifications.value[1].type, 'error')
  assert.equal(toast.notifications.value[2].type, 'warn')

  toast.mergeFaultHistory([
    {
      id: 'srv-resolved-1',
      type: 'upstream',
      status: 'resolved',
      resource: 'nas.home.arpa',
      message: '反代上游已恢复可达',
      time: new Date().toISOString()
    }
  ])
  assert.equal(toast.notifications.value.some(item => item.id === 'srv-resolved-1' && item.type === 'success'), true)

  for (const id of [successId, infoId, warnId, errorId, explicitId]) toast.removeToast(id)
  toast.clearAllNotifications()
})
