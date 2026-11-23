import { describe, expect, it } from 'vitest'
import { compareMoney, isZeroMoney, subtractMoney, sumMoney } from '../money'

const BRL = (value: string) => ({ value, currency: 'BRL' })

describe('money arithmetic (RNF-16: decimal text, never floating point)', () => {
  it('compares values of the same currency exactly', () => {
    expect(compareMoney(BRL('10.0000000001'), BRL('10'))).toBe(1)
    expect(compareMoney(BRL('0.1'), BRL('0.10'))).toBe(0)
    expect(compareMoney(BRL('-1'), BRL('0'))).toBe(-1)
  })

  it('does not compare other currencies, missing or malformed values', () => {
    expect(compareMoney(BRL('1'), { value: '1', currency: 'USD' })).toBeUndefined()
    expect(compareMoney(undefined, BRL('1'))).toBeUndefined()
    expect(compareMoney(BRL('1,5'), BRL('1'))).toBeUndefined()
  })

  it('subtracts without float drift', () => {
    expect(subtractMoney(BRL('0.3'), BRL('0.1'))).toEqual(BRL('0.2'))
    expect(subtractMoney(BRL('100.00'), BRL('130.5'))).toEqual(BRL('-30.5'))
    expect(subtractMoney(BRL('5'), BRL('5'))).toEqual(BRL('0'))
  })

  it('sums one currency and refuses a mix', () => {
    expect(sumMoney([BRL('0.1'), BRL('0.2'), null])).toEqual(BRL('0.3'))
    expect(sumMoney([BRL('1'), { value: '1', currency: 'USD' }])).toBeUndefined()
    expect(sumMoney([])).toBeUndefined()
  })

  it('knows zero', () => {
    expect(isZeroMoney(BRL('0.000'))).toBe(true)
    expect(isZeroMoney(undefined)).toBe(true)
    expect(isZeroMoney(BRL('0.01'))).toBe(false)
  })
})
