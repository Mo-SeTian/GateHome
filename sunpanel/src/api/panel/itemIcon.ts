import { post } from '@/utils/request'

export function getIconList() {
  return post<Common.ListResponse<Panel.ItemInfo[]>>({ url: '/panel/itemIcon/getIconList' })
}

export function editIcon(item: Panel.ItemInfo, icon: Panel.ItemIcon | null) {
  return post<Panel.ItemInfo>({ url: '/panel/itemIcon/editIcon', data: { id: item.id, updateTime: item.updateTime, icon } })
}

export function addMultiple<T>(req: Panel.ItemInfo[]) {
  return post<T>({
    url: '/panel/itemIcon/addMultiple',
    data: req,
  })
}

export function edit<T>(req: Panel.ItemInfo) {
  return post<T>({
    url: '/panel/itemIcon/edit',
    data: req,
  })
}

// export function getInfo<T>(id: number) {
//   return post<T>({
//     url: '/aiApplet/getInfo',
//     data: { id },
//   })
// }

export function getListByGroupId<T>(itemIconGroupId: number | undefined) {
  return post<T>({
    url: '/panel/itemIcon/getListByGroupId',
    data: { itemIconGroupId },
  })
}

export function deletes<T>(ids: number[]) {
  return post<T>({
    url: '/panel/itemIcon/deletes',
    data: { ids },
  })
}

export function saveSort<T>(data: Panel.ItemIconSortRequest) {
  return post<T>({
    url: '/panel/itemIcon/saveSort',
    data,
  })
}

export function getSiteFavicon<T>(url: string) {
  return post<T>({
    url: '/panel/itemIcon/getSiteFavicon',
    data: { url },
  })
}
