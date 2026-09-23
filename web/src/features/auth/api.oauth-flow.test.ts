/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { createOAuthFlow, login2fa, wechatLoginByCode } from './api'

describe('createOAuthFlow interceptor flags', () => {
  it('skips interceptor toasts so callers can show one localized error', async () => {
    const postSpy = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { flow_token: 'login-state' } },
    } as never)

    try {
      await expect(createOAuthFlow('github', 'login')).resolves.toBe(
        'login-state'
      )
      expect(postSpy.mock.calls[0]?.[2]).toMatchObject({
        skipAuthRefresh: true,
        skipBusinessError: true,
        skipErrorHandler: true,
      })
    } finally {
      postSpy.mockRestore()
    }
  })

  it('throws the backend console message instead of a raw English fallback', async () => {
    const postSpy = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        success: false,
        message: '管理员未开启通过 GitHub 登录以及注册',
      },
    } as never)

    try {
      await expect(createOAuthFlow('github', 'login')).rejects.toThrow(
        '管理员未开启通过 GitHub 登录以及注册'
      )
    } finally {
      postSpy.mockRestore()
    }
  })

  it('skips interceptor toasts for 2FA so the form shows one localized error', async () => {
    const postSpy = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: '验证码错误或已过期' },
    } as never)

    try {
      await login2fa({ code: '123456', flow_token: 'flow' })
      expect(postSpy.mock.calls[0]?.[2]).toMatchObject({
        skipAuthRefresh: true,
        skipBusinessError: true,
        skipErrorHandler: true,
      })
    } finally {
      postSpy.mockRestore()
    }

    const otpForm = fs.readFileSync(
      path.join(
        path.dirname(fileURLToPath(import.meta.url)),
        'otp/components/otp-form.tsx'
      ),
      'utf8'
    )
    expect(otpForm).toContain('getUnhandledConsoleErrorMessage')
    expect(otpForm).not.toContain('error.message')
  })

  it('skips interceptor toasts for WeChat login requests', async () => {
    const postSpy = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { flow_token: 'wx-state' } },
    } as never)
    const getSpy = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { access_token: 'tok' } },
    } as never)

    try {
      await wechatLoginByCode('wx-code')
      expect(getSpy.mock.calls[0]?.[1]).toMatchObject({
        skipAuthRefresh: true,
        skipBusinessError: true,
        skipErrorHandler: true,
      })
    } finally {
      postSpy.mockRestore()
      getSpy.mockRestore()
    }
  })
})
