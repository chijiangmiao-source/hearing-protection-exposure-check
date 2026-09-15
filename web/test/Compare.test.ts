import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import App from '../src/App.vue'
import type { CompareResponse } from '../src/types'

const FREQUENCIES = [125, 250, 500, 1000, 2000, 4000]

function candidateBody(label: '甲' | '乙', correctedBase: number): CompareResponse['candidates'][number] {
  return {
    label,
    rows: FREQUENCIES.map((f) => ({
      frequency: f,
      level: 90,
      attenuation: 10,
      corrected: correctedBase
    })),
    formula: 'E = 10 × log10( Σ 10^(C/10) )，其中 C = L − A',
    substitution: `E = 10 × log10( 10^(${correctedBase.toFixed(1)}/10) × 6 ) — ${label}`,
    exactLevel: correctedBase + 7.781512503836436,
    displayLevel: (correctedBase + 7.8).toFixed(1)
  }
}

// 乙衰减更大 → 佩戴后声级更低；但结论文案完全由服务端给出。
const successBody: CompareResponse = {
  candidates: [candidateBody('甲', 80), candidateBody('乙', 75)],
  winner: '乙',
  tie: false,
  conclusion: '【服务端结论】候选耳罩乙更优，建议选用乙（此文案必须原样展示）'
}

const tieBody: CompareResponse = {
  candidates: [candidateBody('甲', 80), candidateBody('乙', 80)],
  winner: '',
  tie: true,
  conclusion: '【服务端结论】甲、乙降噪效果相同'
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

async function openCompare(wrapper: ReturnType<typeof mount>) {
  await wrapper.get('[data-testid="mode-compare"]').trigger('click')
}

async function fillCompare(wrapper: ReturnType<typeof mount>, levels = '90.0', att = '10.0') {
  for (let i = 0; i < 6; i++) {
    await wrapper.findAll('input[aria-label="现场声级 L"]')[i].setValue(levels)
    await wrapper.findAll('input[aria-label="候选耳罩甲衰减 A"]')[i].setValue(att)
    await wrapper.findAll('input[aria-label="候选耳罩乙衰减 A"]')[i].setValue(att)
  }
}

afterEach(() => vi.restoreAllMocks())

describe('对比模式页面联调', () => {
  it('单耳罩核算仍是默认入口，切换后才出现对比表单', async () => {
    const wrapper = mount(App)
    expect(wrapper.find('[data-testid="mode-single"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('table.compare-bands').exists()).toBe(false)
    // 默认页面仍是原单耳罩表单
    expect(wrapper.findAll('input[aria-label="耳罩衰减 A"]')).toHaveLength(6)
    expect(wrapper.findAll('input[aria-label="候选耳罩甲衰减 A"]')).toHaveLength(0)

    await openCompare(wrapper)
    expect(wrapper.find('table.compare-bands').exists()).toBe(true)
    expect(wrapper.findAll('input[aria-label="现场声级 L"]')).toHaveLength(6)
    expect(wrapper.findAll('input[aria-label="候选耳罩甲衰减 A"]')).toHaveLength(6)
    expect(wrapper.findAll('input[aria-label="候选耳罩乙衰减 A"]')).toHaveLength(6)
  })

  it('对比成功：并排展示两组逐带修正值、代入依据、精确值与一位小数显示值', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(jsonResponse(successBody))

    const wrapper = mount(App)
    await openCompare(wrapper)
    await fillCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // 请求确实发往对比端点，且六个现场声级只提交一次
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/compare')
    const payload = JSON.parse(init!.body as string)
    expect(payload.levels).toEqual([90, 90, 90, 90, 90, 90])
    expect(payload.candidates.map((c: { label: string }) => c.label)).toEqual(['甲', '乙'])
    expect(payload.candidates[0].bands).toHaveLength(6)
    expect(payload.candidates[0].bands[0]).not.toHaveProperty('level')

    // 两个候选面板并排
    const panels = wrapper.findAll('[data-testid="compare-candidate"]')
    expect(panels).toHaveLength(2)
    expect(panels[0].attributes('data-candidate')).toBe('甲')
    expect(panels[1].attributes('data-candidate')).toBe('乙')

    // 每组各六个逐带修正值
    const corrected = wrapper.findAll('[data-testid="compare-corrected-cell"]')
    expect(corrected).toHaveLength(12)
    expect(corrected.slice(0, 6).every((c) => c.text() === '80.0')).toBe(true)
    expect(corrected.slice(6, 12).every((c) => c.text() === '75.0')).toBe(true)

    // 代入依据、未舍入精确值、一位小数显示值均原样展示服务端内容
    const substitutions = wrapper.findAll('[data-testid="compare-substitution"]')
    expect(substitutions).toHaveLength(2)
    expect(substitutions[0].text()).toContain('80.0/10')
    expect(substitutions[1].text()).toContain('75.0/10')
    const exacts = wrapper.findAll('[data-testid="compare-exact-level"]')
    const displays = wrapper.findAll('[data-testid="compare-display-level"]')
    expect(exacts.map((e) => e.text())).toEqual([
      `${successBody.candidates[0].exactLevel} dB`,
      `${successBody.candidates[1].exactLevel} dB`
    ])
    expect(displays.map((d) => d.text())).toEqual(['87.8 dB', '82.8 dB'])
  })

  it('结论只渲染服务端返回的文案，页面不自行拼装判定', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(successBody))
    const wrapper = mount(App)
    await openCompare(wrapper)
    await fillCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    const conclusion = wrapper.get('[data-testid="compare-conclusion"]')
    expect(conclusion.text()).toBe(successBody.conclusion)
    // 结果区除该结论元素外，不再出现任何“更优/建议选用/相同”等客户端判定字样
    const resultText = wrapper.get('[data-testid="compare-result"]').text()
    const withoutConclusion = resultText.replace(successBody.conclusion, '')
    expect(withoutConclusion).not.toContain('建议选用')
    expect(withoutConclusion).not.toMatch(/效果相同/)
  })

  it('效果相同时同样只展示服务端结论', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(tieBody))
    const wrapper = mount(App)
    await openCompare(wrapper)
    await fillCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="compare-conclusion"]').text()).toBe(tieBody.conclusion)
    // 没有候选面板被标记为 winner
    expect(wrapper.findAll('.compare-panel.winner')).toHaveLength(0)
  })

  it('编辑任一输入（共用声级、甲衰减、乙衰减）都会清除整份对比结果', async () => {
    // 该用例连续提交三次，每次都返回一份全新的 Response（避免 body 已被消费）。
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => Promise.resolve(jsonResponse(successBody)))

    const wrapper = mount(App)
    await openCompare(wrapper)

    // ① 修改共用的现场声级 → 整份对比结果消失
    await fillCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(true)
    await wrapper.findAll('input[aria-label="现场声级 L"]')[2].setValue('91.0')
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="compare-corrected-cell"]')).toHaveLength(0)

    // ② 修改候选甲的衰减 → 整份对比结果消失（乙的结果也不残留）
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(true)
    await wrapper.findAll('input[aria-label="候选耳罩甲衰减 A"]')[0].setValue('11.0')
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="compare-candidate"]')).toHaveLength(0)

    // ③ 修改候选乙的衰减 → 整份对比结果同样消失（甲的结果也不残留）
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(true)
    await wrapper.findAll('input[aria-label="候选耳罩乙衰减 A"]')[4].setValue('12.0')
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(false)

    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('对比 422：清除整份结果、不残留另一候选数据，并聚焦首个错误字段', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    fetchMock.mockResolvedValueOnce(jsonResponse(successBody))

    const wrapper = mount(App, { attachTo: document.body })
    await openCompare(wrapper)
    await fillCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(true)

    // 乙在 250 Hz 衰减越界 + 乙整体缺一行；整单 422
    const errBody = {
      error: '对比整单校验未通过，请修正标红字段后重新提交',
      fields: [
        { candidate: '乙', field: 'bands', message: '候选耳罩乙必须恰好提交 6 个倍频带，实际收到 5 行' },
        {
          candidate: '乙',
          field: 'attenuation',
          frequency: 250,
          message: '250 Hz 的耳罩衰减 A 必须在 0.0 至 40.0 dB 之间，当前为 40.5'
        }
      ]
    }
    fetchMock.mockResolvedValueOnce(jsonResponse(errBody, 422))
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // 整份对比结果（含甲的结果）都不得残留
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="compare-candidate"]')).toHaveLength(0)
    expect(wrapper.findAll('[data-testid="compare-corrected-cell"]')).toHaveLength(0)

    // 横幅展示无法落到输入框的缺行定位；乙 250 Hz 输入框标红
    expect(wrapper.get('.banner.error').text()).toContain('必须恰好提交 6 个倍频带')
    const invalid = wrapper.findAll('input[aria-invalid="true"]')
    expect(invalid).toHaveLength(1)
    expect(wrapper.find('.field-error').text()).toContain('250')

    // 焦点落在首个出错输入（乙 250 Hz 衰减）
    const focused = document.activeElement as HTMLInputElement
    expect(focused instanceof HTMLInputElement).toBe(true)
    expect(focused.getAttribute('aria-label')).toBe('候选耳罩乙衰减 A')
  })

  it('对比空表单整单提交：服务端 422 定位声级与两候选全部缺填', async () => {
    const errBody = {
      error: '对比整单校验未通过，请修正标红字段后重新提交',
      fields: [
        ...FREQUENCIES.flatMap((f) => [
          { field: 'level', frequency: f, message: `${f} Hz 的现场声级 L 缺少数值` },
          { candidate: '甲', field: 'attenuation', frequency: f, message: `${f} Hz 的耳罩衰减 A 缺少数值` },
          { candidate: '乙', field: 'attenuation', frequency: f, message: `${f} Hz 的耳罩衰减 A 缺少数值` }
        ])
      ]
    }
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse(errBody, 422))
    const wrapper = mount(App, { attachTo: document.body })
    await openCompare(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.findAll('input[aria-invalid="true"]')).toHaveLength(18)
    expect(wrapper.find('[data-testid="compare-result"]').exists()).toBe(false)
    // 首个错误为 125 Hz 共用声级
    const focused = document.activeElement as HTMLInputElement
    expect(focused.getAttribute('aria-label')).toBe('现场声级 L')
  })
})
