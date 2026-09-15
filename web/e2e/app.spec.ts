import { expect, test } from '@playwright/test'

const FREQUENCIES = [125, 250, 500, 1000, 2000, 4000]

// 与 Go 端相同的独立复算（测试侧验证，不参与产品链路）。
function combine(cs: number[]): number {
  return 10 * Math.log10(cs.reduce((s, c) => s + Math.pow(10, c / 10), 0))
}
function round1(v: number): string {
  // 十进制四舍五入一位（页面显示的期望值，按常见小数输入场景足够精确）
  return (Math.sign(v) * Math.round(Math.abs(v) * 10) / 10).toFixed(1)
}

test.beforeEach(async ({ page }) => {
  await page.goto('/')
})

test.describe('耳罩合成声级页面联调', () => {
  test('六行合法输入：展示六个 C 值、公式、代入式、未舍入值与一位小数显示值', async ({ page }) => {
    const levels = ['90.0', '92.0', '95.5', '100.0', '98.0', '94.0']
    const attenuations = ['10.0', '12.0', '15.5', '20.0', '18.0', '14.0']
    const cs = levels.map((l, i) => Number(l) - Number(attenuations[i]))

    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(levels[i])
      await page.getByLabel('耳罩衰减 A').nth(i).fill(attenuations[i])
    }
    await page.getByRole('button', { name: '计算佩戴后合成声级' }).click()

    await expect(page.getByTestId('result')).toBeVisible()

    // 六个 C = L − A
    const cells = page.getByTestId('corrected-cell')
    await expect(cells).toHaveCount(6)
    await expect(cells).toHaveText(cs.map((c) => c.toFixed(1)))

    // 公式与代入依据
    await expect(page.getByTestId('formula')).toContainText('E = 10')
    const substitution = await page.getByTestId('substitution').innerText()
    for (const c of cs) {
      expect(substitution).toContain(`10^(${c.toFixed(1)}/10)`)
    }

    // 未舍入值必须是真实计算值，显示值与其四舍五入一致
    const exactText = (await page.getByTestId('exact-level').innerText()).replace(' dB', '')
    const exact = Number(exactText)
    const expectedExact = combine(cs)
    expect(Math.abs(exact - expectedExact)).toBeLessThan(1e-9)

    const display = await page.getByTestId('display-level').innerText()
    expect(display).toBe(`${round1(expectedExact)} dB`)
    // 本数据集六行 C 全部为 80.0：E = 80 + 10·log10(6) = 87.7815… → 87.8 dB
    expect(display).toBe('87.8 dB')
  })

  test('422 整单拒绝：越界字段定位、无任何结果或部分 C 值', async ({ page }) => {
    const levels = ['90.0', '141', '95.0', '100.0', '98.0', '94.0']
    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(levels[i])
      await page.getByLabel('耳罩衰减 A').nth(i).fill('10.0')
    }
    await page.getByRole('button', { name: '计算佩戴后合成声级' }).click()

    // 不展示结果区与任何 C 值
    await expect(page.getByTestId('result')).toHaveCount(0)
    await expect(page.getByTestId('corrected-cell')).toHaveCount(0)

    // 错误横幅 + 250 Hz 的 L 字段标红并被聚焦
    await expect(page.locator('.banner.error')).toContainText('整单校验未通过')
    const bad = page.getByLabel('现场声级 L').nth(1)
    await expect(bad).toHaveAttribute('aria-invalid', 'true')
    await expect(page.locator('.field-error').first()).toContainText('250')
    await expect(bad).toBeFocused()
  })

  test('空表单提交：整单 422，十二个字段全部定位', async ({ page }) => {
    await page.getByRole('button', { name: '计算佩戴后合成声级' }).click()
    await expect(page.getByTestId('result')).toHaveCount(0)
    await expect(page.locator('input[aria-invalid="true"]')).toHaveCount(12)
    // 首个出错字段（125 Hz 的 L）获得焦点
    await expect(page.getByLabel('现场声级 L').nth(0)).toBeFocused()
  })

  test('成功后修改任意输入，旧结果立即消失；再次提交合法数据得到新结果', async ({ page }) => {
    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill('90.0')
      await page.getByLabel('耳罩衰减 A').nth(i).fill('10.0')
    }
    await page.getByRole('button', { name: '计算佩戴后合成声级' }).click()
    await expect(page.getByTestId('display-level')).toHaveText('87.8 dB')

    // 改动 1000 Hz 的 L
    await page.getByLabel('现场声级 L').nth(3).fill('100.0')
    await expect(page.getByTestId('result')).toHaveCount(0)

    // 重新提交：C 分别为 80,80,80,90,80,80 → 与之前不同的真实结果
    await page.getByRole('button', { name: '计算佩戴后合成声级' }).click()
    await expect(page.getByTestId('result')).toBeVisible()
    const exact = Number((await page.getByTestId('exact-level').innerText()).replace(' dB', ''))
    const expected = combine([80, 80, 80, 90, 80, 80])
    expect(Math.abs(exact - expected)).toBeLessThan(1e-9)
    await expect(page.getByTestId('display-level')).toHaveText(`${round1(expected)} dB`)
  })

  test('固定为六个频带行，不可增减', async ({ page }) => {
    await expect(page.locator('table.bands tbody tr')).toHaveCount(6)
    const freqTexts = await page.locator('table.bands tbody td.freq').allInnerTexts()
    expect(freqTexts.map(Number)).toEqual(FREQUENCIES)
  })
})
