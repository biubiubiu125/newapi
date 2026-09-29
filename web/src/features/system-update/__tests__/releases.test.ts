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
import { describe, expect, test } from 'vitest'

import {
  compareRunningToRelease,
  compareSystemVersions,
  getSystemReleaseApiUrl,
  getSystemReleaseUrl,
  orderedCommitHistory,
  parseCompareStatus,
  selectLatestRelease,
} from '../releases'

describe('system release ordering', () => {
  test.each([
    ['v0.13.2', 'v1.0.0-alpha.1', -1],
    ['v1.0.0-alpha.2', 'v1.0.0-beta.1', -1],
    ['v1.0.0-beta.1', 'v1.0.0-rc.1', -1],
    ['v1.0.0-rc.9', 'v1.0.0-rc.10', -1],
    ['v1.0.0-rc.19', 'v1.0.0-rc.19-i18nfix.1', -1],
    ['v1.0.0-rc.19-i18nfix.2', 'v1.0.0-rc.20', -1],
    ['v1.0.0-rc.36', 'v1.0.0', -1],
    ['v0.13.1', 'v0.13.1-patch.1', -1],
    ['v0.13.1-patch.9', 'v0.13.1-patch.10', -1],
    ['v0.13.1-patch.10', 'v0.13.2', -1],
    ['v0.8.8.3.2', 'v0.8.8.3.3', -1],
    ['v0.9.3.0', 'v0.9.3', 0],
    ['v1.0.0+build.1', '1.0.0+build.2', 0],
    ['v1.0.0-rc.37', 'v1.0.0-rc.36', 1],
    ['v1.0.0', 'v1.0.0', 0],
    ['v0.0.0', 'v1.0.0', null],
    ['dev', 'v1.0.0', null],
    ['', 'v1.0.0', null],
    ['v1.0.0-custom.1', 'v1.0.0', null],
    ['v1.0.0-rc.36-2-gabcdef', 'v1.0.0', null],
  ])('compares %s against %s as %s', (current, latest, expected) => {
    expect(compareSystemVersions(current, latest)).toBe(expected)
  })

  test('selects the highest published version including pre-releases, regardless of order', () => {
    const releases = [
      { tag_name: 'v1.0.0-rc.19-i18nfix.2', draft: false, prerelease: true },
      { tag_name: 'v0.13.2', draft: false, prerelease: false },
      { tag_name: 'v2.0.0', draft: true, prerelease: false },
      { tag_name: 'v1.0.0-rc.36', draft: false, prerelease: true },
      { tag_name: 'nightly', draft: false, prerelease: true },
    ]
    expect(selectLatestRelease(releases)?.tag_name).toBe('v1.0.0-rc.36')
  })

  test('returns no release for an empty list or a list containing only drafts', () => {
    expect(selectLatestRelease([])).toBeNull()
    expect(
      selectLatestRelease([
        { tag_name: 'v2.0.0', draft: true, prerelease: false },
      ])
    ).toBeNull()
  })

  test('rejects malformed payloads instead of reporting that the system is current', () => {
    expect(() => selectLatestRelease({ message: 'bad response' })).toThrow()
    expect(() => selectLatestRelease([{ tag_name: 42 }])).toThrow()
  })

  test('selects the newest same-branch commit by history, not publication time', () => {
    const releases = [
      {
        tag_name: 'main-aaaaaaaaa',
        draft: false,
        prerelease: false,
        published_at: '2026-09-20T00:00:00Z',
      },
      {
        tag_name: 'v9.9.9',
        draft: false,
        prerelease: false,
        published_at: '2026-09-25T00:00:00Z',
      },
      {
        tag_name: 'main-bbbbbbbbb',
        draft: false,
        prerelease: false,
        published_at: '2026-09-26T00:00:00Z',
      },
      {
        tag_name: 'main-ccccccccc',
        draft: true,
        prerelease: false,
        published_at: '2026-09-27T00:00:00Z',
      },
      {
        tag_name: 'main-ddddddddd',
        draft: false,
        prerelease: false,
      },
      {
        tag_name: 'other-eeeeeeeee',
        draft: false,
        prerelease: false,
        published_at: '2026-09-28T00:00:00Z',
      },
    ]
    const history = [
      `${'a'.repeat(9)}${'a'.repeat(31)}`,
      `${'b'.repeat(9)}${'b'.repeat(31)}`,
    ]
    expect(
      selectLatestRelease(releases, 'main-aaaaaaaaa', history)?.tag_name
    ).toBe('main-aaaaaaaaa')
    expect(selectLatestRelease(releases, 'main-aaaaaaaaa')).toBeNull()
  })

  test('keeps semver selection for a release build and when the running version is unknown', () => {
    const releases = [
      {
        tag_name: 'main-bbbbbbbbb',
        draft: false,
        prerelease: false,
        published_at: '2026-09-22T00:00:00Z',
      },
      {
        tag_name: 'v1.0.0-rc.36',
        draft: false,
        prerelease: true,
        published_at: '2026-09-08T13:01:00Z',
      },
      { tag_name: 'v2.0.0', draft: true, prerelease: false },
      {
        tag_name: 'v1.0.0',
        draft: false,
        prerelease: false,
        published_at: '2026-09-01T00:00:00Z',
      },
    ]
    expect(selectLatestRelease(releases, 'v1.0.0-rc.35')?.tag_name).toBe(
      'v1.0.0'
    )
    expect(selectLatestRelease(releases)?.tag_name).toBe('v1.0.0')
    expect(selectLatestRelease(releases, 'dev')?.tag_name).toBe('v1.0.0')
  })

  test('compares commit builds by branch history and leaves unordered tags unknown', () => {
    const newer = `${'b'.repeat(9)}${'b'.repeat(31)}`
    const older = `${'a'.repeat(9)}${'a'.repeat(31)}`
    const history = [newer, older]
    expect(
      compareRunningToRelease('main-aaaaaaaaa', 'main-bbbbbbbbb', history)
    ).toBe(-1)
    expect(
      compareRunningToRelease('main-bbbbbbbbb', 'main-aaaaaaaaa', history)
    ).toBe(1)
    expect(
      compareRunningToRelease('main-bbbbbbbbb', 'main-bbbbbbbbb', history)
    ).toBe(0)
    expect(compareRunningToRelease('main-bbbbbbbbb', 'main-bbbbbbbbb')).toBe(0)
    expect(
      compareRunningToRelease('main-aaaaaaaaa', 'main-bbbbbbbbb')
    ).toBeNull()
    expect(
      compareRunningToRelease('main-aaaaaaaaa', 'main-bbbbbbbbb', [newer])
    ).toBeNull()
    expect(
      compareRunningToRelease('main-bbbbbbbbb', 'main-aaaaaaaaa', [newer])
    ).toBeNull()
    expect(
      compareRunningToRelease('main-bbbbbbbbb', 'v1.0.0', history)
    ).toBeNull()
    expect(compareRunningToRelease('v1.0.0-rc.35', 'v1.0.0-rc.36')).toBe(-1)
    expect(compareRunningToRelease(undefined, 'main-bbbbbbbbb')).toBeNull()
    const colliding = `${'b'.repeat(9)}1${'c'.repeat(30)}`
    expect(
      compareRunningToRelease('main-bbbbbbbbb', 'main-aaaaaaaaa', [
        newer,
        colliding,
      ])
    ).toBeNull()
  })

  test('points release checks and links at biubiubiu125/newapi', () => {
    expect(getSystemReleaseApiUrl()).toBe(
      'https://api.github.com/repos/biubiubiu125/newapi/releases?per_page=100'
    )
    expect(
      getSystemReleaseUrl({
        tag_name: 'main-bbbbbbbbb',
        prerelease: false,
      })
    ).toBe('https://github.com/biubiubiu125/newapi/releases/tag/main-bbbbbbbbb')
    expect(getSystemReleaseApiUrl()).not.toContain('QuantumNous/new-api')
  })

  test('turns a confirmed GitHub comparison into history the checker can reuse', () => {
    const newer = `${'b'.repeat(40)}`
    const older = `${'a'.repeat(9)}`
    expect(parseCompareStatus({ status: 'ahead' })).toBe(-1)
    expect(parseCompareStatus({ status: 'behind' })).toBe(1)
    expect(parseCompareStatus({ status: 'identical' })).toBe(0)
    expect(parseCompareStatus({ status: 'diverged' })).toBeNull()
    expect(() => parseCompareStatus({ status: 'weird' })).toThrow()
    expect(
      orderedCommitHistory('main-aaaaaaaaa', 'main-bbbbbbbbb', [newer], -1)
    ).toEqual([newer, older])
    expect(
      compareRunningToRelease(
        'main-aaaaaaaaa',
        'main-bbbbbbbbb',
        orderedCommitHistory('main-aaaaaaaaa', 'main-bbbbbbbbb', [newer], -1)
      )
    ).toBe(-1)
    expect(
      compareRunningToRelease(
        'main-ccccccccc',
        'main-bbbbbbbbb',
        orderedCommitHistory('main-ccccccccc', 'main-bbbbbbbbb', [newer], 1)
      )
    ).toBe(1)
  })
})
