import { describe, expect, it } from 'vitest'

import {
  ollamaActionFailureText,
  ollamaPullFailureText,
  ollamaPullStatusText,
} from './ollama-pull-message'

const t = (key: string) =>
  ({
    'Request failed': '请求失败',
    'Pulling...': '拉取中...',
    Success: '成功',
    'Failed to fetch models': '获取模型失败',
    'Failed to delete model': '删除模型失败',
  })[key] ?? key

describe('ollama pull copy', () => {
  it('hides English transport and upstream errors on Chinese UI', () => {
    expect(
      ollamaPullFailureText('HTTP 500: Internal Server Error', 'zhCN', t)
    ).toBe('请求失败')
    expect(
      ollamaPullFailureText(
        '拉取 Ollama 模型失败',
        'zh-CN',
        t
      )
    ).toBe('拉取 Ollama 模型失败')
    expect(ollamaPullStatusText('starting', 'zh-CN', t)).toBe('拉取中...')
    expect(ollamaPullStatusText('pulling manifest', 'zhTW', t)).toBe(
      '拉取中...'
    )
    expect(ollamaPullStatusText('success', 'zh-CN', t)).toBe('成功')
  })

  it('keeps upstream detail on the English UI', () => {
    expect(
      ollamaPullFailureText('HTTP 500: Internal Server Error', 'en', t)
    ).toBe('HTTP 500: Internal Server Error')
    expect(ollamaPullStatusText('downloading', 'en', t)).toBe('downloading')
  })

  it('hides English list and delete failures on Chinese UI', () => {
    expect(
      ollamaActionFailureText(
        'Failed to fetch',
        'zh-CN',
        t,
        'Failed to fetch models'
      )
    ).toBe('获取模型失败')
    expect(
      ollamaActionFailureText(
        '模型不存在',
        'zh-CN',
        t,
        'Failed to delete model'
      )
    ).toBe('模型不存在')
    expect(
      ollamaActionFailureText(
        'HTTP 404: Not Found',
        'en',
        t,
        'Failed to delete model'
      )
    ).toBe('HTTP 404: Not Found')
  })
})
