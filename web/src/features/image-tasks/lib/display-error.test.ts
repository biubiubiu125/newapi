import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import i18n from 'i18next'
import { beforeEach, describe, test } from 'vitest'

import zhCN from '@/i18n/locales/zh.json'

import { ImageTaskRequestError } from '../api'

import {
  getImageTaskDisplayError,
  getImageTaskStoredErrorLabel,
} from './display-error'

const zh = (zhCN as { translation: Record<string, string> }).translation

describe('getImageTaskDisplayError', () => {
  beforeEach(async () => {
    i18n.addResourceBundle('zhCN', 'translation', zh, true, true)
    await i18n.changeLanguage('zhCN')
  })

  test('does not paint protocol error codes as console chrome', () => {
    assert.equal(
      getImageTaskDisplayError(
        new ImageTaskRequestError(401, 'invalid_token', 'Invalid token')
      ),
      '无效令牌'
    )
  })

  test('localizes fetch transport English', () => {
    assert.equal(
      getImageTaskDisplayError(new TypeError('Failed to fetch')),
      '无法连接服务器'
    )
  })

  test('keeps already-Chinese backend copy', () => {
    assert.equal(
      getImageTaskDisplayError(new Error('任务不存在')),
      '任务不存在'
    )
  })

  test('localizes public image-task protocol sentences for console chrome', () => {
    const root = path.resolve(
      path.dirname(fileURLToPath(import.meta.url)),
      '../../../../..'
    )
    const source = fs.readFileSync(
      path.join(root, 'controller/image_task_public.go'),
      'utf8'
    )
    const messages = new Set<string>()
    const callRe =
      /publicImageTaskError\((?:[^()"]|"[^"]*")*,\s*"([^"]+)"\s*\)/g
    for (const match of source.matchAll(callRe)) messages.add(match[1])
    for (const match of source.matchAll(
      /PublicImageTaskError\{[^}]*Message:\s*"([^"]+)"/g
    )) {
      messages.add(match[1])
    }
    assert.ok(messages.has('image task failed'))
    assert.ok(messages.has('image task not found'))
    for (const message of messages) {
      const translated = zh[message]
      assert.equal(
        typeof translated,
        'string',
        `missing zh key ${message}`
      )
      assert.match(translated, /[\u4e00-\u9fff]/, message)
      assert.notEqual(translated, message, message)
      assert.equal(
        getImageTaskStoredErrorLabel({ message }),
        translated,
        message
      )
      assert.equal(
        getImageTaskDisplayError(
          new ImageTaskRequestError(409, 'image_task_failed', message)
        ),
        translated,
        message
      )
    }
  })
})
