import { describe, expect, it } from 'vitest'
import { formatTenths, parseNumber } from '../src/forms'

describe('parseNumber', () => {
  it.each([
    ['', null],
    ['   ', null],
    ['abc', null],
    ['NaN', null],
    ['Infinity', null],
    ['1e999', null],
    ['85.0', 85],
    [' 92.3 ', 92.3],
    [85.1, 85.1],
    [NaN, null],
    [Infinity, null]
  ])('parseNumber(%j) = %j', (raw, want) => {
    expect(parseNumber(raw as string | number)).toEqual(want)
  })

  it('空输入解析为 null，整单提交后由服务端判为缺填并 422', () => {
    expect(parseNumber('')).toBeNull()
  })
})

describe('formatTenths', () => {
  it.each([
    [80, '80.0'],
    [80.5, '80.5'],
    [79.99999999999999, '80.0'], // 二进制误差被规整
    [0, '0.0']
  ])('formatTenths(%s) = %s', (v, want) => {
    expect(formatTenths(v)).toBe(want)
  })
})
