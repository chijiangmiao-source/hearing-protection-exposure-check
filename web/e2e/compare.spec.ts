import { expect, test } from '@playwright/test'

const FREQUENCIES = [125, 250, 500, 1000, 2000, 4000]
const LEVELS = ['90.0', '92.0', '95.5', '100.0', '98.0', '94.0']

// 与 Go 端相同的独立复算（测试侧验证，不参与产品链路）。
function combine(cs: number[]): number {
  return 10 * Math.log10(cs.reduce((s, c) => s + Math.pow(10, c / 10), 0))
}

test.beforeEach(async ({ page }) => {
  await page.goto('/')
})

test.describe('耳罩对比模式', () => {
  test('默认仍是单耳罩核算；切到对比模式复用已录入的同一组现场声级', async ({ page }) => {
    // 默认入口为单耳罩表单
    await expect(page.getByRole('button', { name: '计算佩戴后合成声级' })).toBeVisible()
    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(LEVELS[i])
    }

    await page.getByTestId('mode-compare').click()
    await expect(page.locator('table.compare-bands')).toBeVisible()
    // 同一组六个现场声级被原样带到对比表单（type=number 的 .value 会去掉末尾 .0）
    const shared = await page.getByLabel('现场声级 L').evaluateAll(
      (els) => (els as HTMLInputElement[]).map((el) => Number(el.value))
    )
    expect(shared).toEqual(LEVELS.map(Number))
    await expect(page.getByLabel('候选耳罩甲衰减 A')).toHaveCount(6)
    await expect(page.getByLabel('候选耳罩乙衰减 A')).toHaveCount(6)
  })

  test('任一候选越界整单 422：聚焦首个错误、不残留另一候选结果；修正后可重新提交完成对比', async ({ page }) => {
    await page.getByTestId('mode-compare').click()

    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(LEVELS[i])
      await page.getByLabel('候选耳罩甲衰减 A').nth(i).fill('10.0')
      // 乙衰减更大，本应胜出；但先在 250 Hz 制造越界
      await page.getByLabel('候选耳罩乙衰减 A').nth(i).fill(i === 1 ? '40.5' : '15.0')
    }

    await page.getByRole('button', { name: '同时核算甲、乙耳罩' }).click()

    // 整单失败：没有任何对比结果或候选面板残留
    await expect(page.getByTestId('compare-result')).toHaveCount(0)
    await expect(page.getByTestId('compare-candidate')).toHaveCount(0)
    await expect(page.getByTestId('compare-corrected-cell')).toHaveCount(0)

    // 错误横幅 + 乙 250 Hz 衰减标红并被聚焦（首个可定位错误）
    await expect(page.locator('.banner.error')).toContainText('对比整单校验未通过')
    const bad = page.getByLabel('候选耳罩乙衰减 A').nth(1)
    await expect(bad).toHaveAttribute('aria-invalid', 'true')
    await expect(bad).toBeFocused()

    // 修正越界值后重新提交，对比完成
    await bad.fill('15.0')
    await page.getByRole('button', { name: '同时核算甲、乙耳罩' }).click()

    await expect(page.getByTestId('compare-result')).toBeVisible()
    const panels = page.getByTestId('compare-candidate')
    await expect(panels).toHaveCount(2)
    await expect(panels.nth(0)).toHaveAttribute('data-candidate', '甲')
    await expect(panels.nth(1)).toHaveAttribute('data-candidate', '乙')

    // 逐带修正值：甲 C=L−10，乙 C=L−15
    const corrected = await page.getByTestId('compare-corrected-cell').allInnerTexts()
    expect(corrected).toHaveLength(12)
    const csA = LEVELS.map((l) => Number(l) - 10)
    const csB = LEVELS.map((l) => Number(l) - 15)
    expect(corrected.slice(0, 6).map(Number)).toEqual(csA.map((v) => Math.round(v * 10) / 10))
    expect(corrected.slice(6, 12).map(Number)).toEqual(csB.map((v) => Math.round(v * 10) / 10))

    // 未舍入值与测试侧独立复算一致（< 1e-9）
    const exacts = await page.getByTestId('compare-exact-level').allInnerTexts()
    expect(Math.abs(Number(exacts[0].replace(' dB', '')) - combine(csA))).toBeLessThan(1e-9)
    expect(Math.abs(Number(exacts[1].replace(' dB', '')) - combine(csB))).toBeLessThan(1e-9)

    // 乙的合成声级更低，服务端判定乙更优；结论区只渲染服务端文案
    await expect(page.getByTestId('compare-conclusion')).toContainText('乙')
    await expect(panels.nth(1)).toHaveClass(/winner/)
    await expect(panels.nth(0)).not.toHaveClass(/winner/)
  })

  test('两款衰减相同时完成对比，结论为效果相同且无中标面板', async ({ page }) => {
    await page.getByTestId('mode-compare').click()
    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(LEVELS[i])
      await page.getByLabel('候选耳罩甲衰减 A').nth(i).fill('10.0')
      await page.getByLabel('候选耳罩乙衰减 A').nth(i).fill('10.0')
    }
    await page.getByRole('button', { name: '同时核算甲、乙耳罩' }).click()

    await expect(page.getByTestId('compare-result')).toBeVisible()
    await expect(page.getByTestId('compare-conclusion')).toContainText('相同')
    await expect(page.locator('.compare-panel.winner')).toHaveCount(0)
    // 两份未舍入值完全一致
    const exacts = await page.getByTestId('compare-exact-level').allInnerTexts()
    expect(exacts[0]).toBe(exacts[1])
  })

  test('对比成功后编辑任一输入立即清除整份对比结果', async ({ page }) => {
    await page.getByTestId('mode-compare').click()
    for (let i = 0; i < 6; i++) {
      await page.getByLabel('现场声级 L').nth(i).fill(LEVELS[i])
      await page.getByLabel('候选耳罩甲衰减 A').nth(i).fill('10.0')
      await page.getByLabel('候选耳罩乙衰减 A').nth(i).fill('15.0')
    }
    await page.getByRole('button', { name: '同时核算甲、乙耳罩' }).click()
    await expect(page.getByTestId('compare-result')).toBeVisible()

    // 改动甲的任意一行衰减 → 乙的结果也一并消失，不允许残留单边面板
    await page.getByLabel('候选耳罩甲衰减 A').nth(0).fill('11.0')
    await expect(page.getByTestId('compare-result')).toHaveCount(0)
    await expect(page.getByTestId('compare-candidate')).toHaveCount(0)
  })

  test('对比表同样固定六个频带行', async ({ page }) => {
    await page.getByTestId('mode-compare').click()
    await expect(page.locator('table.compare-bands tbody tr')).toHaveCount(6)
    const freqTexts = await page.locator('table.compare-bands tbody td.freq').allInnerTexts()
    expect(freqTexts.map(Number)).toEqual(FREQUENCIES)
  })
})
