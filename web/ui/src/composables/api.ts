import { getCsrf } from './useCsrf'

export const api = {
  async get<T = any>(path: string, params?: Record<string, string>): Promise<T> {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    const res = await fetch('/api' + path + qs)
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },
  async post<T = any>(path: string, body?: Record<string, string>): Promise<T> {
    const params: Record<string, string> = {}
    if (body) {
      const csrf = await getCsrf()
      params.csrf_token = csrf
      Object.assign(params, body)
    }
    const res = await fetch('/api' + path, {
      method: 'POST',
      body: body ? new URLSearchParams(params).toString() : undefined,
    })
    if (!res.ok) throw new Error(await res.text())
    return res.json()
  },
}
