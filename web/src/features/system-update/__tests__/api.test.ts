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
import { afterEach, describe, expect, test, vi } from 'vitest'

import { fetchLatestSystemRelease } from '../api'
import { compareRunningToRelease } from '../releases'

const newer = 'b'.repeat(40)
const older = 'a'.repeat(40)
const current = 'c'.repeat(40)

function json(body: unknown, headers?: HeadersInit): Response {
  return new Response(JSON.stringify(body), { status: 200, headers })
}

function release(tag: string, publishedAt = '2026-09-22T00:00:00Z') {
  return {
    tag_name: tag,
    draft: false,
    prerelease: false,
    published_at: publishedAt,
  }
}

function deepPageLink(page: number): HeadersInit {
  return {
    Link: `<https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=${page + 1}>; rel="next", <https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=30>; rel="last"`,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('fetchLatestSystemRelease commit ancestry', () => {
  test('asks GitHub before treating an unlisted running commit as behind', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/compare/')) {
        return json({ status: 'ahead', ahead_by: 30, behind_by: 0 })
      }
      if (url.includes('/commits?')) return json([{ sha: newer }])
      return json([release('main-bbbbbbbbb')])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe('main-bbbbbbbbb')
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
    expect(fetchMock.mock.calls.map((call) => String(call[0]))).toEqual(
      expect.arrayContaining([
        expect.stringContaining(
          '/compare/aaaaaaaaa...bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
        ),
      ])
    )
  })

  test('does not report an update when the unlisted running commit is newer', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/compare/')) return json({ status: 'behind' })
      if (url.includes('/commits?')) return json([{ sha: newer }])
      return json([release('main-bbbbbbbbb')])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-ccccccccc'
    )

    expect(
      compareRunningToRelease(
        'main-ccccccccc',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(1)
  })

  test('keeps looking past the newest 100 commits when that page has no release', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/compare/')) return json({ status: 'ahead' })
      if (url.includes('/commits?')) {
        if (url.includes('page=2'))
          return json([{ sha: newer }, { sha: older }])
        return json([{ sha: current }], {
          Link: '<https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=2>; rel="next", <https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=2>; rel="last"',
        })
      }
      return json([release('main-bbbbbbbbb')])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe('main-bbbbbbbbb')
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
    expect(
      fetchMock.mock.calls.some((call) => String(call[0]).includes('page=2'))
    ).toBe(true)
  })

  test('does not page further when the running commit is already newer than the first page', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/commits?')) {
        return json([{ sha: current }], {
          Link: '<https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=2>; rel="next", <https://api.github.com/repos/biubiubiu125/newapi/commits?sha=main&per_page=100&page=8>; rel="last"',
        })
      }
      return json([release('main-bbbbbbbbb')])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-ccccccccc'
    )

    expect(checked.release).toBeNull()
    expect(
      fetchMock.mock.calls.some(
        (call) =>
          String(call[0]).includes('&page=') ||
          String(call[0]).includes('/compare/')
      )
    ).toBe(false)
  })

  test('finds the newest commit release past the first ten pages', async () => {
    const newerRelease = 'b'.repeat(9)
    const olderRelease = 'd'.repeat(9)
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) {
        return json({
          data: {
            repository: {
              ref: { target: { history: { totalCount: 3000 } } },
              c0: { history: { totalCount: 1000 } },
              c1: { history: { totalCount: 1900 } },
            },
          },
        })
      }
      if (url.includes('/compare/'))
        return json({ status: 'ahead', ahead_by: 1100 })
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        return json([{ sha: current }], deepPageLink(page))
      }
      return json([
        release(`main-${olderRelease}`, '2026-09-28T00:00:00Z'),
        release(`main-${newerRelease}`, '2026-09-20T00:00:00Z'),
      ])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe(`main-${newerRelease}`)
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
    expect(
      fetchMock.mock.calls.some((call) => String(call[0]).includes('/graphql'))
    ).toBe(true)
  })

  test('uses compare distances when the history query is unavailable', async () => {
    const newerRelease = 'b'.repeat(9)
    const olderRelease = 'd'.repeat(9)
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) return new Response('', { status: 401 })
      if (url.includes('/compare/')) {
        if (url.includes(`${olderRelease}...main`))
          return json({ status: 'ahead', ahead_by: 2500, behind_by: 0 })
        if (url.includes(`${newerRelease}...main`))
          return json({ status: 'ahead', ahead_by: 400, behind_by: 0 })
        return json({ status: 'ahead', ahead_by: 400, behind_by: 0 })
      }
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        return json([{ sha: current }], deepPageLink(page))
      }
      return json([
        release(`main-${olderRelease}`, '2026-09-28T00:00:00Z'),
        release(`main-${newerRelease}`, '2026-09-20T00:00:00Z'),
      ])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe(`main-${newerRelease}`)
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
  })

  test('does not call a partial deep search up to date', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) return new Response('', { status: 401 })
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        return json([{ sha: current }], deepPageLink(page))
      }
      return json(
        [
          release('main-bbbbbbbbb', '2026-09-20T00:00:00Z'),
          release('main-ddddddddd', '2026-09-28T00:00:00Z'),
          release('main-eeeeeeeee', '2026-09-27T00:00:00Z'),
        ],
        { 'X-RateLimit-Remaining': '1' }
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      fetchLatestSystemRelease(new AbortController().signal, 'main-aaaaaaaaa')
    ).rejects.toMatchObject({
      name: 'UpdateCheckError',
      code: 'rate-limit',
    })
  })

  test('finds a deep release from its distance when history ranking is unavailable', async () => {
    const newerRelease = 'b'.repeat(9)
    const olderRelease = 'd'.repeat(9)
    const decoys = Array.from({ length: 15 }, (_, index) =>
      (index + 2).toString(16).padStart(9, 'a')
    )
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) return new Response('', { status: 403 })
      if (url.includes('/compare/')) {
        return json({ status: 'ahead', ahead_by: 2500, behind_by: 0 })
      }
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        if (page === 25) {
          return json([{ sha: 'f'.repeat(40) }], deepPageLink(page))
        }
        if (page === 26) {
          return json([
            { sha: 'b'.repeat(40) },
            { sha: '1'.repeat(40) },
            { sha: 'd'.repeat(40) },
          ])
        }
        return json([{ sha: current }], deepPageLink(page))
      }
      return json(
        [
          release(`main-${olderRelease}`, '2026-09-28T00:00:00Z'),
          release(`main-${newerRelease}`, '2026-09-01T00:00:00Z'),
          ...decoys.map((sha) =>
            release(`main-${sha}`, '2026-01-01T00:00:00Z')
          ),
        ],
        { 'X-RateLimit-Remaining': '26' }
      )
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe(`main-${newerRelease}`)
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
    expect(
      fetchMock.mock.calls.filter((call) =>
        String(call[0]).includes('/compare/')
      ).length
    ).toBeLessThanOrEqual(2)
  })

  test('keeps a newer commit when it is more than one page ahead of the latest published release', async () => {
    const newerRelease = 'b'.repeat(9)
    const olderRelease = 'd'.repeat(9)
    const decoys = Array.from({ length: 10 }, (_, index) =>
      (index + 2).toString(16).padStart(9, 'a')
    )
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) return new Response('', { status: 403 })
      if (url.includes('/compare/')) {
        return json({ status: 'ahead', ahead_by: 2500, behind_by: 0 })
      }
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        if (page === 14)
          return json([{ sha: 'b'.repeat(40) }], deepPageLink(page))
        if (page === 26) {
          return json([{ sha: 'd'.repeat(40) }], deepPageLink(page))
        }
        return json([{ sha: current }], deepPageLink(page))
      }
      return json([
        release(`main-${olderRelease}`, '2026-09-28T00:00:00Z'),
        release(`main-${newerRelease}`, '2026-09-01T00:00:00Z'),
        ...decoys.map((sha) => release(`main-${sha}`, '2026-01-01T00:00:00Z')),
      ])
    })
    vi.stubGlobal('fetch', fetchMock)

    const checked = await fetchLatestSystemRelease(
      new AbortController().signal,
      'main-aaaaaaaaa'
    )

    expect(checked.release?.tag_name).toBe(`main-${newerRelease}`)
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        checked.release?.tag_name,
        checked.commitShas
      )
    ).toBe(-1)
  })

  test('does not treat an unfinished deep page search as the latest published release', async () => {
    const newerRelease = 'b'.repeat(9)
    const olderRelease = 'd'.repeat(9)
    const decoys = Array.from({ length: 10 }, (_, index) =>
      (index + 2).toString(16).padStart(9, 'a')
    )
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/graphql')) return new Response('', { status: 403 })
      if (url.includes('/compare/')) {
        return json({ status: 'ahead', ahead_by: 2500, behind_by: 0 })
      }
      if (url.includes('/commits?')) {
        const page = Number(/[?&]page=(\d+)/.exec(url)?.[1] ?? '1')
        if (page === 24)
          return json([{ sha: 'b'.repeat(40) }], deepPageLink(page))
        if (page === 26) {
          return json([{ sha: 'd'.repeat(40) }], deepPageLink(page))
        }
        return json([{ sha: current }], deepPageLink(page))
      }
      return json([
        release(`main-${olderRelease}`, '2026-09-28T00:00:00Z'),
        release(`main-${newerRelease}`, '2026-09-01T00:00:00Z'),
        ...decoys.map((sha) => release(`main-${sha}`, '2026-01-01T00:00:00Z')),
      ])
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      fetchLatestSystemRelease(new AbortController().signal, 'main-aaaaaaaaa')
    ).rejects.toMatchObject({
      name: 'UpdateCheckError',
      code: 'rate-limit',
    })
  })
})
