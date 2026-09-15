import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import App from '../src/App.vue'

function sixRows(level = '90.0', attenuation = '10.0') {
  return [125, 250, 500, 1000, 2000, 4000].map((f) => ({
    frequency: f,
    level: level,
    attenuation: attenuation
  }))
}

const successBody = {
  rows: sixRows().map((r) => ({ ...r, corrected: 80 })),
  formula: 'E = 10 × log10( Σ 10^(C/10) )，其中 C = L − A',
  substitution:
    'E = 10 × log10( 10^(80.0/10) + 10^(80.0/10) + 10^(80.0/10) + 10^(80.0/10) + 10^(80.0/10) + 10^(80.0/10) )',
  exactLevel: 87.78151250383643,
  displayLevel: '87.8'
}

afterEach(() => vi.restoreAllMocks())

describe('App 页面联调', () => {
  it('合法提交后展示六个 C 值、代入式、未舍入值与显示值', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(successBody), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    )

    const wrapper = mount(App)
    const levelInputs = wrapper.findAll('input[aria-label="现场声级 L"]')
    const attInputs = wrapper.findAll('input[aria-label="耳罩衰减 A"]')
    expect(levelInputs).toHaveLength(6)
    expect(attInputs).toHaveLength(6)

    for (let i = 0; i < 6; i++) {
      await levelInputs[i].setValue('90.0')
      await attInputs[i].setValue('10.0')
    }
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // 六个 C 值
    const corrected = wrapper.findAll('[data-testid="corrected-cell"]')
    expect(corrected).toHaveLength(6)
    expect(corrected.every((c) => c.text() === '80.0')).toBe(true)

    // 公式与代入依据
    expect(wrapper.get('[data-testid="formula"]').text()).toContain('E = 10')
    expect(wrapper.get('[data-testid="substitution"]').text()).toContain('10^(80.0/10)')

    // 未舍入值与显示值均以 API 返回为准（JS/Go libm 末位可能差 1 ULP，比较前 14 位）
    expect(wrapper.get('[data-testid="exact-level"]').text()).toContain('87.7815125038')
    expect(wrapper.get('[data-testid="display-level"]').text()).toBe('87.8 dB')

    // 发送的报文确实是六行固定频带
    const [, init] = vi.mocked(globalThis.fetch).mock.calls[0]
    const payload = JSON.parse(init!.body as string)
    expect(payload.bands.map((b: { frequency: number }) => b.frequency)).toEqual([
      125, 250, 500, 1000, 2000, 4000
    ])
  })

  it('422 后清除旧结果、不展示部分计算，并定位首个出错字段', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(successBody), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    )

    const wrapper = mount(App, { attachTo: document.body })
    for (let i = 0; i < 6; i++) {
      await wrapper.findAll('input[aria-label="现场声级 L"]')[i].setValue('90.0')
      await wrapper.findAll('input[aria-label="耳罩衰减 A"]')[i].setValue('10.0')
    }
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="result"]').exists()).toBe(true)

    // 第二次提交：250 Hz 的 L 越界，整单 422
    const errBody = {
      error: '整单校验未通过，请修正标红字段后重新提交',
      fields: [
        { field: 'level', frequency: 250, message: '250 Hz 的现场声级 L 未通过服务端校验（模拟整单拒绝）' }
      ]
    }
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify(errBody), {
        status: 422,
        headers: { 'Content-Type': 'application/json' }
      })
    )
    // 输入值在本地看来合法，但服务端仍整单拒绝（服务端是最终裁决者）
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    // 旧结果必须被清除，页面上不存在任何 C 值/结果区
    expect(wrapper.find('[data-testid="result"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="corrected-cell"]')).toHaveLength(0)

    // 错误横幅与字段级错误
    expect(wrapper.get('.banner.error').text()).toContain('整单校验未通过')
    const invalid = wrapper.findAll('input[aria-invalid="true"]')
    expect(invalid).toHaveLength(1)
    expect(wrapper.find('.field-error').text()).toContain('250')

    // 焦点定位到首个出错字段
    const focused = document.activeElement as HTMLInputElement
    expect(focused instanceof HTMLInputElement).toBe(true)
    expect(focused.getAttribute('aria-label')).toBe('现场声级 L')
  })

  it('空值整单提交：服务端 422 定位全部缺填字段，不产生结果', async () => {
    const errBody = {
      error: '整单校验未通过，请修正标红字段后重新提交',
      fields: [125, 250, 500, 1000, 2000, 4000].flatMap((f) => [
        { field: 'level', frequency: f, message: `${f} Hz 的现场声级 L 缺少数值` },
        { field: 'attenuation', frequency: f, message: `${f} Hz 的耳罩衰减 A 缺少数值` }
      ])
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(errBody), {
        status: 422,
        headers: { 'Content-Type': 'application/json' }
      })
    )
    const wrapper = mount(App, { attachTo: document.body })
    // 全部留空直接提交：仍然整单发往 API
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [, init] = fetchMock.mock.calls[0]
    const payload = JSON.parse(init!.body as string)
    expect(payload.bands).toHaveLength(6)
    expect(payload.bands[0].level).toBeNull()
    expect(wrapper.findAll('input[aria-invalid="true"]').length).toBe(12)
    expect(wrapper.find('[data-testid="result"]').exists()).toBe(false)
  })

  it('改动任一输入会立即使旧结果失效', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(successBody), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    )
    const wrapper = mount(App)
    for (let i = 0; i < 6; i++) {
      await wrapper.findAll('input[aria-label="现场声级 L"]')[i].setValue('90.0')
      await wrapper.findAll('input[aria-label="耳罩衰减 A"]')[i].setValue('10.0')
    }
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-testid="result"]').exists()).toBe(true)

    // 修改 1000 Hz 的 L
    await wrapper.findAll('input[aria-label="现场声级 L"]')[3].setValue('91.0')
    expect(wrapper.find('[data-testid="result"]').exists()).toBe(false)
  })
})
