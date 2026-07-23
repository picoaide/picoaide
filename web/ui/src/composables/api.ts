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
    formData.append('_csrf', await getCsrf())
    const res = await fetch(BASE + path, {
      method: 'POST',
      credentials: 'include',
      body: formData,
    })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },
}
