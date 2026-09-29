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
import { z } from 'zod'

export const systemReleaseSchema = z.object({
  tag_name: z.string().trim().min(1),
  name: z.string().nullable().optional(),
  body: z.string().nullable().optional(),
  published_at: z.iso.datetime().nullable().optional(),
  prerelease: z.boolean(),
})

export type SystemRelease = z.infer<typeof systemReleaseSchema>

export const SYSTEM_UPDATE_REPOSITORY = 'biubiubiu125/newapi'

const commitReleasePattern =
  /^([a-z0-9](?:[a-z0-9_.-]*[a-z0-9])?)-([0-9a-f]{7,40})$/

type SystemVersion = {
  core: bigint[]
  stage: number
  sequence: bigint
  revision: bigint
}

const releaseStages: Record<string, number> = {
  alpha: 0,
  beta: 1,
  rc: 2,
  stable: 3,
  patch: 4,
}

/** Project tags include post-release patches and historical numeric revisions. */
export function parseSystemVersion(
  value: string | null | undefined
): SystemVersion | null {
  const match = value
    ?.trim()
    .match(
      /^v?(\d+(?:\.\d+){2,})(?:-(alpha|beta|rc|patch)(?:\.(\d+))?(?:-i18nfix\.(\d+))?)?(?:\+[\da-zA-Z.-]+)?$/
    )
  if (!match || (match[4] && match[2] !== 'rc')) return null

  const core = match[1].split('.').map((part) => BigInt(part))
  if (core.every((part) => part === 0n)) return null

  return {
    core,
    stage: releaseStages[match[2] ?? 'stable'],
    sequence: BigInt(match[3] ?? '0'),
    revision: BigInt(match[4] ?? '0'),
  }
}

export function compareSystemVersions(
  left: string | null | undefined,
  right: string | null | undefined
): -1 | 0 | 1 | null {
  const a = parseSystemVersion(left)
  const b = parseSystemVersion(right)
  if (!a || !b) return null

  for (let index = 0; index < Math.max(a.core.length, b.core.length); index++) {
    const leftPart = a.core[index] ?? 0n
    const rightPart = b.core[index] ?? 0n
    if (leftPart !== rightPart) return leftPart < rightPart ? -1 : 1
  }
  if (a.stage !== b.stage) return a.stage < b.stage ? -1 : 1
  if (a.sequence !== b.sequence) return a.sequence < b.sequence ? -1 : 1
  if (a.revision !== b.revision) return a.revision < b.revision ? -1 : 1
  return 0
}

export function parseCommitRelease(
  value: string | null | undefined
): { prefix: string; sha: string } | null {
  const match = value?.trim().match(commitReleasePattern)
  if (!match || parseSystemVersion(value)) return null
  return { prefix: match[1], sha: match[2] }
}

const commitShaPattern = /^[0-9a-f]{7,40}$/

/** Newest-first full SHAs from the branch that produced the running build. */
export function parseCommitHistory(payload: unknown): string[] {
  if (!Array.isArray(payload)) throw new Error('Unexpected commit payload')
  if (payload.length === 0) return []

  const shas: string[] = []
  for (const item of payload) {
    if (!item || typeof item !== 'object' || !('sha' in item)) continue
    const sha =
      typeof item.sha === 'string' ? item.sha.trim().toLowerCase() : ''
    if (!commitShaPattern.test(sha) || shas.includes(sha)) continue
    shas.push(sha)
    if (shas.length === 100) break
  }
  if (shas.length === 0) throw new Error('Unexpected commit payload')
  return shas
}

function locateCommit(
  shortSha: string,
  commitShas: readonly string[]
): number | 'missing' | 'ambiguous' {
  const needle = shortSha.toLowerCase()
  if (!commitShaPattern.test(needle)) return 'missing'
  let found: number | null = null
  for (let index = 0; index < commitShas.length; index++) {
    const sha = commitShas[index]?.toLowerCase()
    if (!sha || (sha !== needle && !sha.startsWith(needle))) continue
    if (found !== null) return 'ambiguous'
    found = index
  }
  return found === null ? 'missing' : found
}

/**
 * Semver stays ordered. A commit build is newer only when its commit is ahead
 * of the running commit on the same branch. Publication time is not an order.
 */
export function compareRunningToRelease(
  current: string | null | undefined,
  latest: string | null | undefined,
  commitShas?: readonly string[]
): -1 | 0 | 1 | null {
  const currentCommit = parseCommitRelease(current)
  const latestCommit = parseCommitRelease(latest)
  if (currentCommit && latestCommit) {
    if (currentCommit.prefix !== latestCommit.prefix) return null
    if (currentCommit.sha === latestCommit.sha) return 0
    if (!commitShas) return null
    const currentIndex = locateCommit(currentCommit.sha, commitShas)
    const latestIndex = locateCommit(latestCommit.sha, commitShas)
    if (currentIndex === 'ambiguous' || latestIndex === 'ambiguous') return null
    if (typeof currentIndex !== 'number' || typeof latestIndex !== 'number') {
      return null
    }
    if (latestIndex < currentIndex) return -1
    if (currentIndex < latestIndex) return 1
    return 0
  }
  return compareSystemVersions(current, latest)
}

const publishedReleaseSchema = systemReleaseSchema.extend({
  draft: z.boolean(),
})

export function selectLatestRelease(
  payload: unknown,
  currentVersion?: string | null,
  commitShas?: readonly string[]
): SystemRelease | null {
  if (!Array.isArray(payload)) throw new Error('Unexpected release payload')

  const currentCommit = parseCommitRelease(currentVersion)
  let latest: SystemRelease | null = null
  let newestCommitIndex = Number.POSITIVE_INFINITY
  let validPayload = payload.length === 0
  for (const item of payload) {
    const parsed = publishedReleaseSchema.safeParse(item)
    if (!parsed.success) continue
    validPayload = true
    const release = parsed.data
    if (release.draft) continue
    if (currentCommit) {
      if (!commitShas) continue
      const candidate = parseCommitRelease(release.tag_name)
      if (!candidate || candidate.prefix !== currentCommit.prefix) continue
      const index = locateCommit(candidate.sha, commitShas)
      if (typeof index !== 'number' || index >= newestCommitIndex) continue
      newestCommitIndex = index
      latest = systemReleaseSchema.parse(release)
      continue
    }
    if (!parseSystemVersion(release.tag_name)) continue
    if (
      !latest ||
      compareSystemVersions(release.tag_name, latest.tag_name) === 1
    ) {
      latest = systemReleaseSchema.parse(release)
    }
  }
  if (!validPayload) throw new Error('Unexpected release payload')
  return latest
}

/**
 * GitHub compare of `current...release`. `ahead` means the release commit is
 * newer. A fork or an unknown status is not an order.
 */
export function parseCompareStatus(payload: unknown): -1 | 0 | 1 | null {
  if (!payload || typeof payload !== 'object' || !('status' in payload)) {
    throw new Error('Unexpected compare payload')
  }
  switch (payload.status) {
    case 'ahead':
      return -1
    case 'behind':
      return 1
    case 'identical':
      return 0
    case 'diverged':
      return null
    default:
      throw new Error('Unexpected compare payload')
  }
}

/** The full SHA from this history, when the version names exactly one commit. */
export function commitShaInHistory(
  version: string | null | undefined,
  commitShas: readonly string[]
): string | null {
  const commit = parseCommitRelease(version)
  if (!commit) return null
  const index = locateCommit(commit.sha, commitShas)
  return typeof index === 'number' ? (commitShas[index] ?? null) : null
}

/**
 * Keeps a newest-first list the existing comparison can reuse. A confirmed
 * relation supplies the missing side without inventing an order.
 */
export function orderedCommitHistory(
  current: string,
  latest: string,
  history: readonly string[],
  relation: -1 | 0 | 1 | null
): string[] {
  const bounded = history.slice(0, 100)
  const currentCommit = parseCommitRelease(current)
  const latestCommit = parseCommitRelease(latest)
  if (
    !currentCommit ||
    !latestCommit ||
    currentCommit.prefix !== latestCommit.prefix
  ) {
    return bounded
  }
  const currentIndex = locateCommit(currentCommit.sha, bounded)
  const latestIndex = locateCommit(latestCommit.sha, bounded)
  if (typeof currentIndex === 'number' && typeof latestIndex === 'number') {
    return bounded
  }
  if (typeof latestIndex !== 'number' || relation === null || relation === 0) {
    return typeof latestIndex === 'number' && relation === 0
      ? [bounded[latestIndex]]
      : bounded
  }
  const releaseSha = bounded[latestIndex]
  return relation === -1
    ? [releaseSha, currentCommit.sha]
    : [currentCommit.sha, releaseSha]
}

export function getSystemReleaseApiUrl(): string {
  return `https://api.github.com/repos/${SYSTEM_UPDATE_REPOSITORY}/releases?per_page=100`
}

export function getSystemCompareApiUrl(base: string, head: string): string {
  return `https://api.github.com/repos/${SYSTEM_UPDATE_REPOSITORY}/compare/${encodeURIComponent(base)}...${encodeURIComponent(head)}`
}

export function getSystemCommitsApiUrl(branch: string): string {
  return `https://api.github.com/repos/${SYSTEM_UPDATE_REPOSITORY}/commits?sha=${encodeURIComponent(branch)}&per_page=100`
}

export function getSystemReleaseUrl(release: SystemRelease): string {
  return `https://github.com/${SYSTEM_UPDATE_REPOSITORY}/releases/tag/${encodeURIComponent(release.tag_name)}`
}
