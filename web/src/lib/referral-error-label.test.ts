import { describe, expect, it } from 'vitest'

import { referralErrorLabel } from './referral-error-label'

const zh = (key: string) =>
  ({
    'Affiliate not found': '未找到推广员',
    'Unknown error': '未知错误',
    'Referral exchange rate is missing': '缺少推广汇率',
  })[key] ?? key

describe('referral error labels', () => {
  it('translates known codes and hides unknown English on Chinese UI', () => {
    expect(referralErrorLabel('affiliate_not_found', 'zh-CN', zh)).toBe(
      '未找到推广员'
    )
    expect(referralErrorLabel('dial tcp 127.0.0.1:5432', 'zhCN', zh)).toBe(
      '未知错误'
    )
    expect(referralErrorLabel('金额过小', 'zh-TW', zh)).toBe('金额过小')
    expect(referralErrorLabel('fx_rate_missing', 'zh-CN', zh)).toBe(
      '缺少推广汇率'
    )
  })

  it('keeps unknown detail on the English UI', () => {
    const en = (key: string) => key
    expect(referralErrorLabel('affiliate_not_found', 'en', en)).toBe(
      'Affiliate not found'
    )
    expect(referralErrorLabel('dial tcp 127.0.0.1:5432', 'en-US', en)).toBe(
      'dial tcp 127.0.0.1:5432'
    )
  })
})
