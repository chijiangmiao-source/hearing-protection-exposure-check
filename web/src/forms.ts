/**
 * 前端表单侧的纯工具函数：仅做输入解析与展示格式化，
 * 不参与声级合成，也不做提交裁决——校验与合成完全由 Go API 完成。
 */

/** 解析输入框值（type=number 的 v-model 可能给出 number 或 string）；空、NaN、Infinity 一律视为无效。 */
export function parseNumber(raw: string | number): number | null {
  if (typeof raw === 'number') {
    return Number.isFinite(raw) ? raw : null
  }
  const t = raw.trim()
  if (t === '') return null
  const v = Number(t)
  if (!Number.isFinite(v)) return null
  return v
}

export interface FormRow {
  frequency: number
  level: string
  attenuation: string
}

/**
 * 数值在数学上为一位小数时按一位小数展示，
 * 规整二进制表示误差（如 79.99999999999999 → 80.0）。
 */
export function formatTenths(v: number): string {
  const r = Math.round((v + Number.EPSILON) * 10) / 10
  return r.toFixed(1)
}
