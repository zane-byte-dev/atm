import type { Bootstrap } from './types'

type Envelope<T> = {
  data: T
  error?: { code: string; message: string; details?: Record<string, unknown> }
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: Record<string, unknown>

  constructor(
    status: number,
    code: string,
    message: string,
    details: Record<string, unknown> = {},
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

let csrf = ''
let boot: Promise<Bootstrap> | undefined

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', ...init })
  let body: Envelope<T>
  try {
    body = (await response.json()) as Envelope<T>
  } catch {
    throw new ApiError(response.status, 'invalid_response', '服务没有返回有效响应，请稍后重试。')
  }
  if (!response.ok || body.error) {
    if (response.status === 401) window.dispatchEvent(new Event('atm:session-expired'))
    throw new ApiError(
      response.status,
      body.error?.code ?? 'unavailable',
      body.error?.message ?? '暂时无法连接 ATM。',
      body.error?.details,
    )
  }
  return body.data
}

export function bootstrap(): Promise<Bootstrap> {
  if (!boot)
    boot = (async () => {
      const fragment = new URLSearchParams(location.hash.slice(1))
      const ticket = fragment.get('ticket')
      if (ticket) {
        history.replaceState(null, '', `${location.pathname}${location.search}`)
        await request<{ csrf_token: string }>('/api/v1/auth/exchange', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ticket }),
        })
      }
      const data = await request<Bootstrap>('/api/v1/bootstrap')
      csrf = data.csrf_token
      return data
    })()
  return boot
}

export function reconnect() {
  boot = undefined
  return bootstrap()
}

export function call<T>(
  method: string,
  input: unknown,
  signal?: AbortSignal,
  idempotencyKey?: string,
) {
  return request<T>(`/api/v1/${method}`, {
    method: 'POST',
    signal,
    headers: {
      'Content-Type': 'application/json',
      'X-ATM-CSRF': csrf,
      ...(idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : {}),
    },
    body: JSON.stringify(input),
  })
}

export function uploadTaskImage<T>(todoID: string, file: File, etag: string, signal?: AbortSignal) {
  const body = new FormData()
  body.append('file', file)
  body.append('expected_etag', etag)
  return request<T>(`/api/v1/tasks/${encodeURIComponent(todoID)}/images`, {
    method: 'POST',
    signal,
    headers: { 'X-ATM-CSRF': csrf },
    body,
  })
}

export function errorText(error: unknown): string {
  if (
    error instanceof ApiError &&
    error.status === 409 &&
    typeof error.details.idempotency_key === 'string'
  )
    return '这次创建已经对应一个任务，当前内容与首次提交不同。请查看已创建的任务，或明确另存为新任务。'
  if (
    error instanceof ApiError &&
    error.status === 409 &&
    ('expected_etag' in error.details || 'actual_etag' in error.details)
  )
    return '这个任务已在其他地方修改。请查看最新内容，合并后再保存。'
  if (error instanceof ApiError && error.status === 401)
    return '连接已过期。请重新运行 atm serve --open 连接工作台。'
  return error instanceof Error ? error.message : '操作未完成，请重试。'
}
