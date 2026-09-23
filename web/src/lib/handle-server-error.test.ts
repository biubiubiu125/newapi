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
import {
  AxiosError,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios'
import i18n from 'i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  getServerErrorDisplayMessage,
  getUnhandledConsoleErrorMessage,
  notifyQueryCacheError,
} from './handle-server-error'

function axiosError(data: unknown): AxiosError {
  const error = new AxiosError('Request failed')
  error.response = {
    data,
    status: 200,
    statusText: 'OK',
    headers: {},
    config: {} as InternalAxiosRequestConfig,
  } as AxiosResponse
  return error
}

describe('getServerErrorDisplayMessage', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        'Something went wrong!': '出现错误！',
        'Session expired!': '登录已过期！',
        'Unable to connect to the server': '无法连接服务器',
        Unauthorized: '未授权',
        'Internal Server Error': '服务器内部错误',
        'This login session has been revoked.': '该登录会话已被撤销',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  it('prefers the backend message over title', () => {
    expect(
      getServerErrorDisplayMessage(
        axiosError({
          title: 'Internal Server Error',
          message: '用户名或密码错误，或用户已被封禁',
        })
      )
    ).toBe('用户名或密码错误，或用户已被封禁')
  })

  it('falls back to the localized generic copy when the payload is empty', () => {
    expect(getServerErrorDisplayMessage(axiosError({}))).toBe('出现错误！')
  })

  it('uses the session expired copy for HTTP 401 even when the payload is empty', () => {
    const error = axiosError({})
    if (error.response) error.response.status = 401
    expect(getServerErrorDisplayMessage(error)).toBe('登录已过期！')
  })

  it('uses the session expired copy for HTTP 401 instead of the backend message', () => {
    const error = axiosError({
      success: false,
      message: '未登录',
    })
    if (error.response) error.response.status = 401
    expect(getServerErrorDisplayMessage(error)).toBe('登录已过期！')
  })

  it('localizes axios Network Error when there is no response', () => {
    expect(getServerErrorDisplayMessage(new AxiosError('Network Error'))).toBe(
      '无法连接服务器'
    )
  })

  it('localizes HTTP StatusText payload messages', () => {
    expect(
      getServerErrorDisplayMessage(
        axiosError({ success: false, message: 'Unauthorized' })
      )
    ).toBe('未授权')
    expect(
      getServerErrorDisplayMessage(
        axiosError({ title: 'Internal Server Error' })
      )
    ).toBe('服务器内部错误')
  })

  it('localizes mapped AUTH_SESSION_REVOKED codes', () => {
    expect(
      getServerErrorDisplayMessage(
        axiosError({
          success: false,
          code: 'AUTH_SESSION_REVOKED',
          message: 'Unauthorized',
        })
      )
    ).toBe('该登录会话已被撤销')
  })

  it('shows the backend message for global 500 query cache errors', () => {
    const error = axiosError({
      message: '数据库出错，请联系管理员',
    })
    if (error.response) error.response.status = 500
    const toasts: string[] = []
    let navigated = false
    notifyQueryCacheError(
      error,
      (message) => toasts.push(message),
      () => {
        navigated = true
      }
    )
    expect(toasts).toEqual(['数据库出错，请联系管理员'])
    expect(navigated).toBe(true)
  })
})

describe('getUnhandledConsoleErrorMessage', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      {
        '出现错误！': 'Something went wrong!',
        'Something went wrong!': '出现错误！',
        'Unable to connect to the server': '无法连接服务器',
        'Request failed': '请求失败',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  it('does not re-toast axios errors already handled by the interceptor', () => {
    expect(
      getUnhandledConsoleErrorMessage(new AxiosError('Network Error'))
    ).toBeNull()
    expect(
      getUnhandledConsoleErrorMessage(
        new AxiosError('Request failed with status code 500')
      )
    ).toBeNull()
  })

  it('localizes skipped interceptor errors instead of axios English', () => {
    const error = new AxiosError('Request failed with status code 500')
    error.config = { skipErrorHandler: true } as InternalAxiosRequestConfig
    expect(getUnhandledConsoleErrorMessage(error)).toBe('请求失败')
  })

  it('localizes thrown axios English from skipBusinessError callers', () => {
    expect(
      getUnhandledConsoleErrorMessage(
        new Error('Request failed with status code 403')
      )
    ).toBe('请求失败')
  })
})
