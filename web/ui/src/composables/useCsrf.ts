export async function getCsrf(): Promise<string> {
  const res = await fetch('/api/csrf')
  const data = await res.json()
  return data.csrf_token || data.token
}
