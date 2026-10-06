// 极简 API 封装：409（业务冲突）时抛出带 details 的错误供页面展示。
const BASE = ''

async function request(path, options = {}) {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
    body: options.body ? JSON.stringify(options.body) : undefined
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const err = new Error(data.error || `请求失败 (${res.status})`)
    err.status = res.status
    err.details = data.details
    throw err
  }
  return data
}

export const api = {
  get: (p) => request(p),
  post: (p, body) => request(p, { method: 'POST', body }),

  actors: () => request('/api/actors'),
  createActor: (b) => request('/api/actors', { method: 'POST', body: b }),

  costumes: () => request('/api/costumes'),
  createCostume: (b) => request('/api/costumes', { method: 'POST', body: b }),
  care: (id) => request(`/api/costumes/${id}/care`),
  startCare: (id, b) => request(`/api/costumes/${id}/care`, { method: 'POST', body: b }),
  completeCare: (id, b) => request(`/api/care/${id}/complete`, { method: 'POST', body: b }),

  performances: () => request('/api/performances'),
  performance: (id) => request(`/api/performances/${id}`),
  createPerformance: (b) => request('/api/performances', { method: 'POST', body: b }),

  createAssignment: (b) => request('/api/assignments', { method: 'POST', body: b }),
  recast: (id, b) => request(`/api/assignments/${id}/recast`, { method: 'POST', body: b }),

  scan: (b) => request('/api/scan', { method: 'POST', body: b }),

  check: (perfId, b) => request(`/api/performances/${perfId}/check`, { method: 'POST', body: b }),
  checks: (perfId) => request(`/api/performances/${perfId}/checks`),

  incidents: (status = '') => request(`/api/incidents${status ? `?status=${status}` : ''}`),
  resolveIncident: (id, b) => request(`/api/incidents/${id}/resolve`, { method: 'POST', body: b })
}
