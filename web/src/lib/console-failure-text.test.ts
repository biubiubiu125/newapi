import i18n from 'i18next'
import { describe, expect, it } from 'vitest'

import {
  consoleFailureText,
  currentConsoleFailureText,
} from './console-failure-text'

const zh = (key: string) =>
  ({
    'Request failed': '请求失败',
    'Failed to test channel': '测试渠道失败',
  })[key] ?? key

describe('console failure text', () => {
  it('hides untranslated English on Chinese UI and keeps Chinese detail', () => {
    expect(
      consoleFailureText(
        'dial tcp 127.0.0.1:443: connect: connection refused',
        'zh-CN',
        zh,
        'Request failed'
      )
    ).toBe('请求失败')
    expect(
      consoleFailureText(
        'upstream error: invalid api key',
        'zhCN',
        zh,
        'Failed to test channel'
      )
    ).toBe('测试渠道失败')
    expect(consoleFailureText('金额过小', 'zh-TW', zh, 'Request failed')).toBe(
      '金额过小'
    )
    expect(
      consoleFailureText(
        '连接失败: dial tcp 127.0.0.1:11434: connect: connection refused',
        'zh-CN',
        zh,
        'Request failed'
      )
    ).toBe('连接失败')
    expect(consoleFailureText('失败 timeout', 'zhCN', zh, 'Request failed')).toBe(
      '失败'
    )
    expect(
      consoleFailureText('不是合法 JSON', 'zh-CN', zh, 'Request failed')
    ).toBe('不是合法 JSON')
    expect(consoleFailureText('  ', 'zh-CN', zh, 'Failed to test channel')).toBe(
      '测试渠道失败'
    )
  })

  it('keeps upstream text on the English UI', () => {
    const en = (key: string) => key
    expect(
      consoleFailureText('HTTP 404: Not Found', 'en', en, 'Request failed')
    ).toBe('HTTP 404: Not Found')
    expect(
      consoleFailureText(
        'upstream error: invalid api key',
        'en-US',
        en,
        'Failed to test channel'
      )
    ).toBe('upstream error: invalid api key')
  })

  it('follows the active interface language', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Request failed': '请求失败' },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
    expect(
      currentConsoleFailureText(
        'dial tcp 127.0.0.1:443: connect: connection refused',
        'Request failed'
      )
    ).toBe('请求失败')
    await i18n.changeLanguage('en')
    expect(
      currentConsoleFailureText('HTTP 404: Not Found', 'Request failed')
    ).toBe('HTTP 404: Not Found')
    await i18n.changeLanguage('zhCN')
  })
})
