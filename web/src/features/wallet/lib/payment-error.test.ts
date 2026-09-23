import i18n from 'i18next'
import { describe, expect, it } from 'vitest'

import { paymentFailureText } from './payment-error'

describe('payment failure text', () => {
  it('shows the Chinese reason stored beside message error', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Payment request failed': '支付请求失败' },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')

    expect(
      paymentFailureText({ message: 'error', data: '支付启动失败' })
    ).toBe('支付启动失败')
    expect(paymentFailureText({ message: 'error', data: { checkout_url: '' } })).toBe(
      '支付请求失败'
    )
    expect(
      paymentFailureText({
        message: '连接失败: connection refused',
      })
    ).toBe('连接失败')
  })
})
