import { describe, expect, it } from 'vitest'
import { getTagStyle } from './color'

describe('getTagStyle', () => {
  it('只输出标签源色变量，配色交由样式表派生', () => {
    expect(getTagStyle('#3b82f6')).toEqual({ '--myobj-tag-color': '#3b82f6' })
  })

  it('无色值时不输出样式，交由 .myobj-tag 回退到主题主色', () => {
    expect(getTagStyle()).toEqual({})
    expect(getTagStyle('')).toEqual({})
  })
})
