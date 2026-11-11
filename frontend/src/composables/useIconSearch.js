import { computed, reactive, ref, watch } from 'vue'
import { iconsApi } from '@/api/icons'

export function useIconSearch(limit) {
  const filters = reactive({ keyword: '', source: '', category: '', style: '' })
  const items = ref([])
  const total = ref(0)
  const page = ref(1)
  const pageCount = computed(() => Math.max(1, Math.ceil(total.value / limit)))
  const loading = ref(true)
  const error = ref('')
  const revision = ref(0)

  watch(filters, () => { page.value = 1 }, { flush: 'sync' })
  watch([filters, page, revision], async (_, old, onCleanup) => {
    const controller = new AbortController()
    onCleanup(() => controller.abort())
    loading.value = true
    error.value = ''
    try {
      const result = await iconsApi.list({ ...filters, page: page.value, limit }, controller.signal)
      if (controller.signal.aborted) return
      total.value = result.total
      if (page.value > pageCount.value) {
        page.value = pageCount.value
        return
      }
      items.value = result.items
    } catch (err) {
      if (!controller.signal.aborted) {
        error.value = err.message
        items.value = []
        total.value = 0
      }
    } finally {
      if (!controller.signal.aborted) loading.value = false
    }
  }, { immediate: true })

  return { filters, items, total, page, pageCount, loading, error, refresh: () => revision.value++ }
}
