// 与 Go API 一一对应的类型定义。

export const FREQUENCIES = [125, 250, 500, 1000, 2000, 4000] as const
export type Frequency = (typeof FREQUENCIES)[number]

export const LEVEL_MIN = 40.0
export const LEVEL_MAX = 140.0
export const ATTENUATION_MIN = 0.0
export const ATTENUATION_MAX = 40.0

export interface BandInput {
  frequency: number
  /** 空输入以 null 提交，由服务端整单判为缺填并 422 */
  level: number | null
  attenuation: number | null
}

export interface CalcRequest {
  bands: BandInput[]
}

export interface RowOutput {
  frequency: number
  level: number
  attenuation: number
  /** 佩戴耳罩后的频带声级 C = L − A */
  corrected: number
}

export interface CalcResponse {
  rows: RowOutput[]
  formula: string
  substitution: string
  /** 内部保留的未舍入合成声级 */
  exactLevel: number
  /** 四舍五入保留一位小数后的显示值（字符串，避免浮点展示问题） */
  displayLevel: string
}

export interface FieldError {
  field: 'frequency' | 'level' | 'attenuation' | 'bands'
  frequency?: number
  message: string
}

export interface ErrorResponse {
  error: string
  fields?: FieldError[]
}

/** 422 等业务错误，携带服务端定位信息。 */
export class ApiError extends Error {
  status: number
  fields: FieldError[]
  constructor(status: number, message: string, fields: FieldError[]) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.fields = fields
  }
}
