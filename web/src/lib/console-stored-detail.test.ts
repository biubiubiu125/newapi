import { describe, expect, it } from 'vitest'

import {
  paymentOrphanErrorText,
  paymentOrphanReasonText,
  storedTaskErrorText,
} from './console-stored-detail'

const zhCatalog: Record<string, string> = {
  'Failed channel checks: {{count}}': '渠道检查失败：{{count}}',
  'Failed channel updates: {{count}}': '渠道更新失败：{{count}}',
  'Runtime channel cache refresh failed': '运行时渠道缓存刷新失败',
  'Batch apply was saved, but the runtime cache refresh failed':
    '批量应用已保存，但运行时缓存刷新失败',
  'Task was cancelled': '任务已取消',
  'Task timed out': '任务超时',
  'Failed to save the task result': '任务结果保存失败',
  'Request failed': '请求失败',
  'Local order insert failed': '本地订单写入失败',
  'Payment review required': '这笔支付需要人工核对',
  'BEpusdt top-up payment requires manual review after payment succeeded':
    'BEpusdt 充值成功后需要人工核对',
}

function zh(key: string, options?: Record<string, unknown>) {
  const value = zhCatalog[key] ?? key
  if (!options) return value
  return value.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    String(options[name] ?? '')
  )
}

const en = (key: string) => key

describe('stored console details', () => {
  it('translates known task failures and drops untranslated English on Chinese UI', () => {
    expect(storedTaskErrorText('failed channel checks: 3', 'zh-CN', zh)).toBe(
      '渠道检查失败：3'
    )
    expect(
      storedTaskErrorText('failed channel updates: 2', 'zhCN', zh)
    ).toBe('渠道更新失败：2')
    expect(
      storedTaskErrorText(
        'runtime channel cache refresh failed: dial tcp 127.0.0.1:5432: connect: connection refused',
        'zh-TW',
        zh
      )
    ).toBe('运行时渠道缓存刷新失败')
    expect(
      storedTaskErrorText(
        'batch apply persisted but runtime cache refresh failed: context deadline exceeded',
        'zh-CN',
        zh
      )
    ).toBe('批量应用已保存，但运行时缓存刷新失败')
    expect(
      storedTaskErrorText(
        'runtime channel cache refresh failed: 数据库出错',
        'zh-CN',
        zh
      )
    ).toBe('运行时渠道缓存刷新失败：数据库出错')
    expect(storedTaskErrorText('context canceled', 'zh-CN', zh)).toBe(
      '任务已取消'
    )
    expect(storedTaskErrorText('task cancelled by user', 'zh-CN', zh)).toBe(
      '任务已取消'
    )
    expect(storedTaskErrorText('context deadline exceeded', 'zh-CN', zh)).toBe(
      '任务超时'
    )
    expect(
      storedTaskErrorText(
        'failed to persist task terminal result: pq: password authentication failed',
        'zh-CN',
        zh
      )
    ).toBe('任务结果保存失败')
    expect(storedTaskErrorText('上游返回额度不足', 'zh-CN', zh)).toBe(
      '上游返回额度不足'
    )
    expect(storedTaskErrorText('dial tcp 127.0.0.1:5432', 'zh-CN', zh)).toBe('')
    expect(
      storedTaskErrorText('dial tcp 127.0.0.1:5432', 'zh-CN', zh, 'Request failed')
    ).toBe('请求失败')
    expect(storedTaskErrorText('   ', 'zh-CN', zh, 'Request failed')).toBe('')
  })

  it('keeps task diagnostics on the English UI', () => {
    expect(
      storedTaskErrorText(
        'failed channel checks: 3',
        'en',
        en
      )
    ).toBe('failed channel checks: 3')
    expect(
      storedTaskErrorText(
        'runtime channel cache refresh failed: dial tcp',
        'en-US',
        en,
        'Request failed'
      )
    ).toBe('runtime channel cache refresh failed: dial tcp')
  })

  it('translates payment orphan reasons and hides untranslated errors on Chinese UI', () => {
    expect(
      paymentOrphanReasonText(
        'local order insert failed: dial tcp 127.0.0.1:5432: connect: connection refused',
        'zh-CN',
        zh
      )
    ).toBe('本地订单写入失败')
    expect(
      paymentOrphanReasonText('local order insert failed: 金额过小', 'zh-TW', zh)
    ).toBe('本地订单写入失败：金额过小')
    expect(
      paymentOrphanReasonText(
        'BEpusdt top-up payment requires manual review after payment succeeded',
        'zhCN',
        zh
      )
    ).toBe('BEpusdt 充值成功后需要人工核对')
    expect(paymentOrphanReasonText('金额过小', 'zh-CN', zh)).toBe('金额过小')
    expect(paymentOrphanReasonText('some gateway exploded', 'zh-CN', zh)).toBe(
      '这笔支付需要人工核对'
    )
    expect(paymentOrphanReasonText('  ', 'zh-CN', zh)).toBe('')
    expect(
      paymentOrphanErrorText(
        'pq: password authentication failed for user newapi',
        'zh-CN',
        zh
      )
    ).toBe('')
    expect(paymentOrphanErrorText('金额过小', 'zh-CN', zh)).toBe('金额过小')
  })

  it('keeps payment orphan diagnostics on the English UI', () => {
    const raw =
      'local order insert failed: dial tcp 127.0.0.1:5432: connect: connection refused'
    expect(paymentOrphanReasonText(raw, 'en', en)).toBe(raw)
    expect(
      paymentOrphanErrorText('pq: password authentication failed', 'en-US', en)
    ).toBe('pq: password authentication failed')
  })
})
