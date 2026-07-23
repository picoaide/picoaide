import { getCsrf } from './useCsrf'

const BASE = '/api'

export const api = {
  async get<T = any>(path: string, params?: Record<string, string>): Promise<T> {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    const res = await fetch(BASE + path + qs, { credentials: 'include' })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },
  async post<T = any>(path: string, body?: Record<string, any>): Promise<T> {
    const heads: Record<string, string> = { 'Content-Type': 'application/json' }
    if (body) {
      heads['X-CSRF-Token'] = await getCsrf()
    }
    const res = await fetch(BASE + path, {
      method: 'POST',
      credentials: 'include',
      headers: heads,
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },

  async put<T = any>(path: string, body?: Record<string, any>): Promise<T> {
    const heads: Record<string, string> = { 'Content-Type': 'application/json' }
    if (body) heads['X-CSRF-Token'] = await getCsrf()
    const res = await fetch(BASE + path, {
      method: 'PUT', credentials: 'include', headers: heads,
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },

  async delete<T = any>(path: string): Promise<T> {
    const heads: Record<string, string> = {}
    heads['X-CSRF-Token'] = await getCsrf()
    const res = await fetch(BASE + path, {
      method: 'DELETE', credentials: 'include', headers: heads,
    })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },

  async postForm<T = any>(path: string, formData: FormData): Promise<T> {
    const csrf = await getCsrf()
    const qs = path.includes('?') ? '&' : '?'
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', BASE + path + qs + '_csrf=' + encodeURIComponent(csrf))
      xhr.withCredentials = true
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try { resolve(JSON.parse(xhr.responseText)) }
          catch { reject(new Error(xhr.responseText || '上传失败')) }
        } else {
          try {
            const err = JSON.parse(xhr.responseText)
            reject(new Error(err.error || err.message || '上传失败'))
          } catch { reject(new Error(xhr.responseText || '上传失败')) }
        }
      }
      xhr.onerror = () => {
        // XHR onerror fires for network-level failures.
        // If the server actually processed the request (status available via xhr.status),
        // we try to parse it. Otherwise reject.
        if (xhr.status > 0 && xhr.responseText) {
          try { resolve(JSON.parse(xhr.responseText)); return }
          catch { reject(new Error(xhr.responseText || '上传失败')); return }
        }
        reject(new Error('网络错误'))
      }
      xhr.send(formData)
    })
  },
}
