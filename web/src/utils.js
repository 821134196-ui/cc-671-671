export const STATUS_TEXT = {
  available: '可用',
  in_use: '穿着中',
  returned: '待洗护',
  washing: '洗护中',
  repairing: '维修中',
  lost: '少件',
  damaged: '破损',
  retired: '已报废',
  pending: '待执行',
  signed: '已签收',
  collected: '已交回',
  skipped: '已取消'
}

export const CONFLICT_TEXT = {
  change_time: '换装耗时不足',
  transfer: '转场时间不足',
  clean: '清洁间隔不足',
  actor_overlap: '演员上场重叠',
  actor_rush: '演员来不及换装',
  unwashed: '未经洗护',
  care_in_progress: '洗护/维修中',
  incident_open: '少件/破损未处理',
  retired: '已报废',
  in_use: '仍被穿着',
  signed_locked: '已签收锁定'
}

export function statusText(s) {
  return STATUS_TEXT[s] || s
}

export function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d)) return t
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function fmtHM(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d)) return t
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}`
}

// datetime-local 控件需要的本地时间格式。
export function toLocalInput(t) {
  const d = t ? new Date(t) : new Date()
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

export function fmtDuration(sec) {
  if (sec == null) return '—'
  if (sec < 60) return `${sec} 秒`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const parts = []
  if (h) parts.push(`${h} 小时`)
  if (m) parts.push(`${m} 分`)
  if (s && !h) parts.push(`${s} 秒`)
  return parts.join(' ') || '0 秒'
}
