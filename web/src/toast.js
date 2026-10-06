import { reactive } from 'vue'

// 全局轻提示。错误以红色展示，业务冲突时把每条明细一并列出。
export const toastState = reactive({ items: [] })
let seq = 0

export function toast(message, type = 'ok', details = null) {
  const id = ++seq
  toastState.items.push({ id, message, type, details })
  setTimeout(() => {
    const i = toastState.items.findIndex((x) => x.id === id)
    if (i >= 0) toastState.items.splice(i, 1)
  }, 6000)
}

export function toastError(err) {
  toast(err.message || '操作失败', 'error', err.details || null)
}
