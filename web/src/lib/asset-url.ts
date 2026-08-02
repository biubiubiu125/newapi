import { DEFAULT_LOGO } from './constants'

function normalizeBaseUrl(value?: string | null): string {
  return (value || '').trim().replace(/\/+$/, '')
}

function isSameOrigin(value: string): boolean {
  if (typeof window === 'undefined') return false
  try {
    return new URL(value).origin === window.location.origin
  } catch {
    return false
  }
}

export function resolveAssetUrl(
  url?: string | null,
  fallback = DEFAULT_LOGO,
  serverAddress?: string | null
): string {
  const raw = (url || '').trim()
  const fallbackUrl = fallback || DEFAULT_LOGO
  if (!raw) return fallbackUrl

  if (/^https?:\/\//i.test(raw) || raw.startsWith('data:')) return raw

  const base = normalizeBaseUrl(serverAddress)
  const isBundledAsset =
    raw === fallbackUrl || raw === DEFAULT_LOGO || raw === '/favicon.ico'
  if (
    base &&
    raw.startsWith('/') &&
    !isBundledAsset &&
    (typeof window === 'undefined' || isSameOrigin(base))
  ) {
    return `${base}${raw}`
  }

  if (typeof window !== 'undefined') {
    try {
      return new URL(raw, window.location.origin).href
    } catch {
      return fallbackUrl
    }
  }

  return raw.startsWith('/') ? raw : fallbackUrl
}
