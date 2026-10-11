import { post } from '@/utils/request'

export function uploadImage(file: File) {
  const data = new FormData()
  data.append('imgfile', file)
  return post<{ imageUrl: string }>({ url: '/file/uploadImg', data })
}

export function importImage(url: string) {
  return post<{ imageUrl: string }>({ url: '/file/importImg', data: { url: url.trim() } })
}

export function getList<T>() {
  return post<T>({
    url: '/file/getList',
  })
}

export function deletes<T>(ids: number[]) {
  return post<T>({
    url: '/file/deletes',
    data: { ids },
  })
}
