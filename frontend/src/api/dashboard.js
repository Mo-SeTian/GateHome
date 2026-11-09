export const dashboardWidgets = [
  {
    id: 'server',
    title: '服务器状态'
  },
  {
    id: 'cpu',
    title: 'CPU 占用'
  },
  {
    id: 'memory',
    title: '内存占用'
  },
  {
    id: 'storage',
    title: '存储空间'
  },
  {
    id: 'traffic',
    title: '上传 / 下载速率'
  },
  {
    id: 'transfer',
    title: '上传 / 下载数据量'
  },
  {
    id: 'requests',
    title: '访问数量'
  },
  {
    id: 'blocked',
    title: '拦截数量'
  },
  {
    id: 'trend',
    title: '访问趋势'
  },
  {
    id: 'provinces',
    title: '省份 / IP 分析'
  },
  {
    id: 'map',
    title: '中国访问地图'
  },
  {
    id: 'services',
    title: '代理服务'
  },
  {
    id: 'listeners',
    title: '监听入口'
  },
  {
    id: 'events',
    title: '最近活动'
  }
]

export const provincePoints = {
  北京: [116.4, 39.9],
  天津: [117.2, 39.1],
  河北: [114.5, 38.0],
  山西: [112.5, 37.9],
  内蒙古: [111.7, 40.8],
  辽宁: [123.4, 41.8],
  吉林: [125.3, 43.9],
  黑龙江: [126.6, 45.8],
  上海: [121.5, 31.2],
  江苏: [118.8, 32.1],
  浙江: [120.2, 30.3],
  安徽: [117.3, 31.9],
  福建: [119.3, 26.1],
  江西: [115.9, 28.7],
  山东: [117.0, 36.7],
  河南: [113.6, 34.8],
  湖北: [114.3, 30.6],
  湖南: [113.0, 28.2],
  广东: [113.3, 23.1],
  广西: [108.3, 22.8],
  海南: [110.3, 20.0],
  重庆: [106.5, 29.6],
  四川: [104.1, 30.7],
  贵州: [106.7, 26.6],
  云南: [102.7, 25.0],
  西藏: [91.1, 29.7],
  陕西: [108.9, 34.3],
  甘肃: [103.8, 36.1],
  青海: [101.8, 36.6],
  宁夏: [106.3, 38.5],
  新疆: [87.6, 43.8],
  台湾: [121.5, 25.0],
  香港: [114.2, 22.3],
  澳门: [113.5, 22.2]
}

export function dashboardWidgetIDs(config) {
  return config?.widgets ?? dashboardWidgets.map((item) => item.id)
}

export function reorderWidget(order, id, target) {
  if (id === target || !order.includes(id) || !order.includes(target)) return [...order]
  const next = order.filter((item) => item !== id)
  next.splice(next.indexOf(target), 0, id)
  return next
}

export function dashboardRecord(snapshot, form, order, selected) {
  return {
    ...snapshot.config,
    dashboard: { ...snapshot.config.dashboard, ...form, widgets: order.filter((id) => selected.includes(id)) }
  }
}

export function mapPoint(province) {
  const [longitude, latitude] = provincePoints[province] || [104, 35]
  return [(longitude - 73) * 8, (54 - latitude) * 8]
}

export function serviceURL(value) {
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : ''
  } catch {
    return ''
  }
}
