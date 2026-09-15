import type { CalcRequest, CalcResponse, CompareRequest, CompareResponse, ErrorResponse } from './types'
import { ApiError } from './types'

/**
 * 调用 Go 后端的真实计算端点。
 * 前端不做任何声级合成计算：C 值、代入式、未舍入值、显示值全部以 API 响应为准。
 */
export async function calculate(req: CalcRequest): Promise<CalcResponse> {
  return postJSON('/api/calculate', req)
}

/**
 * 调用耳罩对比端点：复用同一组六个现场声级，分别套用甲、乙衰减，
 * 两份完整结果与较优结论均以 API 响应为准，前端不做任何判定。
 */
export async function compare(req: CompareRequest): Promise<CompareResponse> {
  return postJSON('/api/compare', req)
}

async function postJSON<T>(url: string, req: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req)
  })

  if (!res.ok) {
    let body: ErrorResponse | null = null
    try {
      body = (await res.json()) as ErrorResponse
    } catch {
      // 响应不是 JSON 时按网络/服务错误处理
    }
    throw new ApiError(
      res.status,
      body?.error ?? `请求失败（HTTP ${res.status}）`,
      body?.fields ?? []
    )
  }

  return (await res.json()) as T
}
