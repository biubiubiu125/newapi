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
  commitShaInHistory,
  getSystemCommitsApiUrl,
  getSystemCompareApiUrl,
  getSystemReleaseApiUrl,
  orderedCommitHistory,
  parseCommitHistory,
  parseCommitRelease,
  parseCompareStatus,
  selectLatestRelease,
  SYSTEM_UPDATE_REPOSITORY,
  type SystemRelease,
} from './releases'

export type SystemReleaseCheck = {
  release: SystemRelease | null
  commitShas: string[]
}

export type UpdateCheckErrorCode =
  | 'network'
  | 'rate-limit'
  | 'timeout'
  | 'payload'

export class UpdateCheckError extends Error {
  constructor(public readonly code: UpdateCheckErrorCode) {
    super(code)
    this.name = 'UpdateCheckError'
  }
}

/** Enough to cross a short release outage without spending the hourly quota. */
const MAX_COMMIT_PAGES = 10
const GITHUB_GRAPHQL_API = 'https://api.github.com/graphql'
/** Compare calls or later commit pages allowed when the rate-limit header is missing. */
const DEEP_COMPARE_BUDGET = 8

function rateLimitRemaining(header: string | null): number | null {
  if (!header) return null
  const value = Number(header)
  return Number.isSafeInteger(value) && value >= 0 ? value : null
}

function compareBudget(
  rateRemaining: number | null,
  pagesFetched: number
): number {
  if (rateRemaining === null) return DEEP_COMPARE_BUDGET
  return Math.max(0, rateRemaining - pagesFetched)
}

function commitReleaseCandidates(payload: unknown, prefix: string): string[] {
  if (!Array.isArray(payload)) return []
  const candidates: string[] = []
  for (const item of payload) {
    if (!item || typeof item !== 'object') continue
    if ('draft' in item && item.draft === true) continue
    const tag =
      'tag_name' in item && typeof item.tag_name === 'string'
        ? item.tag_name
        : ''
    const parsed = parseCommitRelease(tag)
    if (
      !parsed ||
      parsed.prefix !== prefix ||
      candidates.includes(parsed.sha)
    ) {
      continue
    }
    candidates.push(parsed.sha)
  }
  return candidates
}

function historyQuery(branch: string, shas: string[]): string {
  const [owner, name] = SYSTEM_UPDATE_REPOSITORY.split('/')
  const fields = shas.map(
    (sha, index) =>
      `c${index}: object(expression: "${sha}") { ... on Commit { history { totalCount } } }`
  )
  return `query { repository(owner: "${owner}", name: "${name}") { ref(qualifiedName: "refs/heads/${branch}") { target { ... on Commit { history { totalCount } } } } ${fields.join(' ')} } }`
}

function historyCounts(payload: unknown, shas: string[]): string[] | null {
  if (!payload || typeof payload !== 'object') return null
  if (
    'errors' in payload &&
    Array.isArray(payload.errors) &&
    payload.errors.length > 0
  ) {
    return null
  }
  if (
    !('data' in payload) ||
    !payload.data ||
    typeof payload.data !== 'object'
  ) {
    return null
  }
  const data = payload.data as { repository?: unknown }
  const repository = data.repository
  if (!repository || typeof repository !== 'object') return null
  const record = repository as Record<string, unknown>
  const ref = record.ref
  const tip =
    ref &&
    typeof ref === 'object' &&
    'target' in ref &&
    ref.target &&
    typeof ref.target === 'object' &&
    'history' in ref.target &&
    ref.target.history &&
    typeof ref.target.history === 'object' &&
    'totalCount' in ref.target.history
      ? ref.target.history.totalCount
      : null
  if (typeof tip !== 'number' || !Number.isFinite(tip)) return null

  const ranked: { sha: string; count: number }[] = []
  shas.forEach((sha, index) => {
    const entry = record[`c${index}`]
    const count =
      entry &&
      typeof entry === 'object' &&
      'history' in entry &&
      entry.history &&
      typeof entry.history === 'object' &&
      'totalCount' in entry.history
        ? entry.history.totalCount
        : null
    if (typeof count === 'number' && Number.isFinite(count) && count <= tip) {
      ranked.push({ sha, count })
    }
  })
  ranked.sort((left, right) => right.count - left.count)
  return ranked.map((item) => item.sha)
}

async function rankByHistory(
  branch: string,
  shas: string[],
  init: RequestInit
): Promise<string[] | null> {
  const response = await fetch(GITHUB_GRAPHQL_API, {
    ...init,
    method: 'POST',
    headers: {
      ...(init.headers as Record<string, string>),
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ query: historyQuery(branch, shas) }),
  })
  // Anonymous GraphQL has a zero quota, so this 403 is not the REST budget.
  if (!response.ok) return null
  return historyCounts(await response.json(), shas)
}

async function readAheadDistance(response: Response): Promise<number | null> {
  if (response.status === 404) return null
  const payload = await readGitHubJson(response)
  if (!payload || typeof payload !== 'object' || !('status' in payload)) {
    throw new Error('Unexpected compare payload')
  }
  if (payload.status !== 'ahead' && payload.status !== 'identical') return null
  const distance =
    'ahead_by' in payload
      ? payload.ahead_by
      : payload.status === 'identical'
        ? 0
        : null
  if (
    typeof distance !== 'number' ||
    !Number.isFinite(distance) ||
    distance < 0
  ) {
    throw new Error('Unexpected compare payload')
  }
  return distance
}

async function firstAncestor(
  shas: readonly string[],
  branch: string,
  init: RequestInit
): Promise<string | null> {
  for (const sha of shas) {
    const response = await fetch(getSystemCompareApiUrl(sha, branch), init)
    const relation = await readCompareRelation(response)
    if (relation === -1 || relation === 0) return sha
  }
  return null
}

async function rankByCompare(
  branch: string,
  shas: string[],
  init: RequestInit
): Promise<string | null> {
  let best: { sha: string; distance: number } | null = null
  for (const sha of shas) {
    const response = await fetch(getSystemCompareApiUrl(sha, branch), init)
    const distance = await readAheadDistance(response)
    if (distance === null || (best && distance >= best.distance)) continue
    best = { sha, distance }
  }
  return best?.sha ?? null
}

async function fetchHistoryPage(
  branch: string,
  page: number,
  init: RequestInit
): Promise<{ history: string[]; nextPage: number | null }> {
  const url =
    page <= 1
      ? getSystemCommitsApiUrl(branch)
      : `${getSystemCommitsApiUrl(branch)}&page=${page}`
  const response = await fetch(url, init)
  const nextPage = nextPageFromLink(response.headers.get('Link'))
  return {
    history: parseCommitHistory(await readGitHubJson(response)),
    nextPage,
  }
}

function newestCandidateOnPage(
  history: readonly string[],
  shas: readonly string[]
): string | null {
  for (const fullSha of history) {
    if (shas.some((sha) => fullSha === sha || fullSha.startsWith(sha))) {
      return fullSha
    }
  }
  return null
}

/** Commit pages are newest-first, so the first later page with a candidate is the newest release. */
async function scanLaterCommitPages(
  branch: string,
  shas: readonly string[],
  init: RequestInit,
  budget: number
): Promise<string | null> {
  if (budget < 1) throw new UpdateCheckError('rate-limit')
  let page = MAX_COMMIT_PAGES + 1
  let fetched = 0
  while (fetched < budget) {
    const historyPage = await fetchHistoryPage(branch, page, init)
    fetched += 1
    const found = newestCandidateOnPage(historyPage.history, shas)
    if (found) return found
    if (historyPage.nextPage === null || historyPage.nextPage <= page)
      return null
    page = historyPage.nextPage
  }
  throw new UpdateCheckError('rate-limit')
}

async function rankNewestReleaseSha(
  branch: string,
  shas: readonly string[],
  init: RequestInit,
  rateRemaining: number | null,
  pagesFetched: number
): Promise<string | null> {
  if (shas.length === 0) return null
  const ranked = await rankByHistory(branch, [...shas], init)
  if (ranked) return firstAncestor(ranked, branch, init)
  const budget = compareBudget(rateRemaining, pagesFetched)
  if (shas.length <= budget) return rankByCompare(branch, [...shas], init)
  return scanLaterCommitPages(branch, shas, init, budget)
}

async function readGitHubJson(response: Response): Promise<unknown> {
  if (response.status === 403 || response.status === 429) {
    throw new UpdateCheckError('rate-limit')
  }
  if (!response.ok) throw new UpdateCheckError('network')
  return response.json()
}

function nextPageFromLink(link: string | null): number | null {
  if (!link) return null
  const match = /[?&]page=(\d+)>;\s*rel="next"/.exec(link)
  if (!match) return null
  const page = Number(match[1])
  return Number.isSafeInteger(page) && page > 1 ? page : null
}

async function readCompareRelation(
  response: Response
): Promise<-1 | 0 | 1 | null> {
  if (response.status === 404) return null
  return parseCompareStatus(await readGitHubJson(response))
}

export async function fetchLatestSystemRelease(
  signal: AbortSignal,
  currentVersion?: string | null
): Promise<SystemReleaseCheck> {
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal.addEventListener('abort', cancel, { once: true })
  if (signal.aborted) controller.abort()
  const timeout = setTimeout(cancel, 10_000)
  const currentCommit = parseCommitRelease(currentVersion)

  try {
    const init = {
      credentials: 'omit' as const,
      headers: { Accept: 'application/vnd.github+json' },
      signal: controller.signal,
    }
    const releaseResponse = await fetch(getSystemReleaseApiUrl(), init)
    if (!currentCommit || !currentVersion) {
      const releasePayload = await readGitHubJson(releaseResponse)
      return {
        release: selectLatestRelease(releasePayload, currentVersion),
        commitShas: [],
      }
    }

    const rateRemaining = rateLimitRemaining(
      releaseResponse.headers.get('X-RateLimit-Remaining')
    )
    const releasePayload = await readGitHubJson(releaseResponse)
    let page = 1
    let nextPage: number | null = 2
    let pagesFetched = 0
    let history: string[] = []
    let release: SystemRelease | null = null
    while (page <= MAX_COMMIT_PAGES && nextPage !== null) {
      pagesFetched += 1
      const url =
        page === 1
          ? getSystemCommitsApiUrl(currentCommit.prefix)
          : `${getSystemCommitsApiUrl(currentCommit.prefix)}&page=${page}`
      const commitsResponse = await fetch(url, init)
      nextPage = nextPageFromLink(commitsResponse.headers.get('Link'))
      history = parseCommitHistory(await readGitHubJson(commitsResponse))
      release = selectLatestRelease(releasePayload, currentVersion, history)
      if (release || commitShaInHistory(currentVersion, history)) break
      if (nextPage === null || nextPage <= page) break
      page = nextPage
    }

    if (
      !release &&
      nextPage !== null &&
      !commitShaInHistory(currentVersion, history)
    ) {
      const sha = await rankNewestReleaseSha(
        currentCommit.prefix,
        commitReleaseCandidates(releasePayload, currentCommit.prefix),
        init,
        rateRemaining,
        pagesFetched
      )
      if (sha) {
        history = [sha]
        release = selectLatestRelease(releasePayload, currentVersion, history)
      }
    }

    if (!release) return { release: null, commitShas: history }
    if (commitShaInHistory(currentVersion, history)) {
      return {
        release,
        commitShas: orderedCommitHistory(
          currentVersion,
          release.tag_name,
          history,
          null
        ),
      }
    }

    const head =
      commitShaInHistory(release.tag_name, history) ??
      parseCommitRelease(release.tag_name)?.sha
    if (!head) return { release, commitShas: history }
    const compareResponse = await fetch(
      getSystemCompareApiUrl(currentCommit.sha, head),
      init
    )
    const relation = await readCompareRelation(compareResponse)
    return {
      release,
      commitShas: orderedCommitHistory(
        currentVersion,
        release.tag_name,
        history,
        relation
      ),
    }
  } catch (error) {
    if (signal.aborted) throw error
    if (controller.signal.aborted) throw new UpdateCheckError('timeout')
    if (error instanceof UpdateCheckError) throw error
    if (error instanceof TypeError) throw new UpdateCheckError('network')
    throw new UpdateCheckError('payload')
  } finally {
    clearTimeout(timeout)
    signal.removeEventListener('abort', cancel)
  }
}
