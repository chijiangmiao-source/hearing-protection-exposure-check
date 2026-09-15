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

// 两款候选耳罩对比：一组现场声级 + 甲、乙各自的六行逐带衰减。
export type CandidateLabel = '甲' | '乙'

export interface CompareCandidateInput {
  label: CandidateLabel
  /** 候选只提交逐带衰减，现场声级由顶层 levels 复用 */
  bands: Array<{ frequency: number; attenuation: number | null }>
}

export interface CompareRequest {
  /** 六个频带共用的现场声级 L（与单耳罩表单同一组数据） */
  levels: Array<number | null>
  candidates: CompareCandidateInput[]
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

/** 单个候选耳罩的完整核算结果，字段与 CalcResponse 一致并附带候选标识。 */
export interface CompareCandidateResult extends CalcResponse {
  label: CandidateLabel
}

export interface CompareResponse {
  candidates: CompareCandidateResult[]
  /** 较优候选标识 "甲"/"乙"；效果相同时为空串 */
  winner: '' | CandidateLabel
  tie: boolean
  /** 仅由服务端依据未舍入合成声级生成的结论文案，页面原样渲染 */
  conclusion: string
}

export interface FieldError {
  /** 仅对比端点出现：问题所属候选耳罩（甲/乙）；顶层声级错误时省略 */
  candidate?: CandidateLabel
  field: 'frequency' | 'level' | 'attenuation' | 'bands' | 'levels' | 'candidates'
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
