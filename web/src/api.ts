import type { CalcRequest, CalcResponse, ErrorResponse } from './types'
import { ApiError } from './types'

/**
 * 调用 Go 后端的真实计算端点。
 * 前端不做任何声级合成计算：C 值、代入式、未舍入值、显示值全部以 API 响应为准。
 */
export async function calculate(req: CalcRequest): Promise<CalcResponse> {
  const res = await fetch('/api/calculate', {
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

  return (await res.json()) as CalcResponse
}
