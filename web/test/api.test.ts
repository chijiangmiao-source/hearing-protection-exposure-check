import { afterEach, describe, expect, it, vi } from 'vitest'
import { calculate, compare } from '../src/api'
import { ApiError } from '../src/types'

const validPayload = {
  bands: [125, 250, 500, 1000, 2000, 4000].map((f) => ({
    frequency: f,
    level: 90,
    attenuation: 10
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('calculate API 客户端', () => {
  it('200 时原样返回 API 的真实计算结果', async () => {
    const body = {
      rows: [
        { frequency: 125, level: 90, attenuation: 10, corrected: 80 },
        { frequency: 250, level: 90, attenuation: 10, corrected: 80 },
        { frequency: 500, level: 90, attenuation: 10, corrected: 80 },
        { frequency: 1000, level: 90, attenuation: 10, corrected: 80 },
        { frequency: 2000, level: 90, attenuation: 10, corrected: 80 },
        { frequency: 4000, level: 90, attenuation: 10, corrected: 80 }
      ],
      formula: 'E = 10 × log10( Σ 10^(C/10) )，其中 C = L − A',
      substitution: 'E = 10 × log10( 10^(80.0/10) + … )',
      exactLevel: 87.78151250383643,
      displayLevel: '87.8'
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )

    const res = await calculate(validPayload)
    expect(res.exactLevel).toBe(87.78151250383643)
    expect(res.displayLevel).toBe('87.8')
    expect(res).toEqual(body)

    // 确认请求真实发往 Go 端点且报文为六行
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/calculate')
    expect((init as RequestInit).method).toBe('POST')
    const sent = JSON.parse((init as RequestInit).body as string)
    expect(sent.bands).toHaveLength(6)
  })

  it('422 时抛出带字段定位的 ApiError', async () => {
    const errBody = {
      error: '整单校验未通过，请修正标红字段后重新提交',
      fields: [{ field: 'level', frequency: 250, message: '250 Hz 的现场声级 L 必须在 40.0 至 140.0 dB 之间' }]
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(errBody), { status: 422, headers: { 'Content-Type': 'application/json' } })
    )

    await expect(calculate(validPayload)).rejects.toMatchObject({
      name: 'ApiError',
      status: 422,
      fields: errBody.fields
    })
  })

  it('422 缺行/多行时定位 bands', async () => {
    const errBody = {
      error: '整单校验未通过',
      fields: [{ field: 'bands', message: '必须恰好提交 6 个倍频带（125–4000 Hz），实际收到 5 行' }]
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(errBody), { status: 422, headers: { 'Content-Type': 'application/json' } })
    )

    try {
      await calculate(validPayload)
      throw new Error('应当抛出 ApiError')
    } catch (e) {
      expect(e).toBeInstanceOf(ApiError)
      expect((e as ApiError).fields[0].field).toBe('bands')
    }
  })

  it('非 JSON 的 500 响应也被包装为 ApiError', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('internal error', { status: 500 })
    )
    await expect(calculate(validPayload)).rejects.toMatchObject({ status: 500 })
  })
})

const validComparePayload = {
  levels: [90, 90, 90, 90, 90, 90],
  candidates: (['甲', '乙'] as const).map((label) => ({
    label,
    bands: [125, 250, 500, 1000, 2000, 4000].map((f) => ({ frequency: f, attenuation: 10 }))
  }))
}

describe('compare API 客户端', () => {
  it('200 时原样返回两份候选结果与服务端结论', async () => {
    const cand = (label: '甲' | '乙', exact: number) => ({
      label,
      rows: [125, 250, 500, 1000, 2000, 4000].map((f) => ({
        frequency: f,
        level: 90,
        attenuation: 10,
        corrected: label === '甲' ? 80 : 75
      })),
      formula: 'f',
      substitution: `s-${label}`,
      exactLevel: exact,
      displayLevel: exact.toFixed(1)
    })
    const body = {
      candidates: [cand('甲', 87.78), cand('乙', 82.78)],
      winner: '乙',
      tie: false,
      conclusion: '候选耳罩乙更优'
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )

    const res = await compare(validComparePayload)
    expect(res).toEqual(body)
    expect(res.winner).toBe('乙')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/compare')
    expect((init as RequestInit).method).toBe('POST')
    const sent = JSON.parse((init as RequestInit).body as string)
    expect(sent.levels).toHaveLength(6)
    expect(sent.candidates).toHaveLength(2)
    expect(sent.candidates[0].bands[0]).not.toHaveProperty('level')
  })

  it('422 时抛出携带候选标识与频带的 ApiError', async () => {
    const errBody = {
      error: '对比整单校验未通过，请修正标红字段后重新提交',
      fields: [
        { candidate: '乙', field: 'attenuation', frequency: 250, message: '候选乙 250 Hz 越界' }
      ]
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(errBody), { status: 422, headers: { 'Content-Type': 'application/json' } })
    )

    await expect(compare(validComparePayload)).rejects.toMatchObject({
      name: 'ApiError',
      status: 422,
      fields: errBody.fields
    })
  })
})
