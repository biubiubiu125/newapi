import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { UserAuthForm } from './user-auth-form'

const mocks = vi.hoisted(() => ({
  login: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: {
      user_agreement_enabled: true,
      password_login_enabled: true,
    },
  }),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, ...props }: { children?: unknown }) => (
    <a {...props}>{children as never}</a>
  ),
}))

vi.mock('@/features/auth/hooks/use-turnstile', () => ({
  useTurnstile: () => ({
    enabled: false,
    token: '',
    setToken: vi.fn(),
    reset: vi.fn(),
  }),
}))

vi.mock('@/features/auth/hooks/use-auth-redirect', () => ({
  useAuthRedirect: () => ({ redirect: vi.fn() }),
}))

vi.mock('@/features/auth/api', () => ({
  login: mocks.login,
  wechatLoginByCode: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { error: mocks.toastError, success: vi.fn(), info: vi.fn() },
}))

vi.mock('@/lib/passkey', () => ({
  isPasskeySupported: vi.fn(async () => false),
}))

vi.mock('@/features/auth/components/oauth-providers', () => ({
  OAuthProviders: ({ disabled }: { disabled?: boolean }) => (
    <button type='button' disabled={disabled}>
      OAuth
    </button>
  ),
}))

vi.mock('@/components/turnstile', () => ({
  Turnstile: () => null,
}))

describe('UserAuthForm legal consent', () => {
  beforeEach(() => {
    mocks.login.mockReset()
    mocks.toastError.mockReset()
  })

  it('shows the password-login warning when legal terms are not accepted', async () => {
    const user = userEvent.setup()
    render(<UserAuthForm />)

    const signIn = screen.getByRole('button', { name: 'Sign in' })
    expect(signIn).toBeEnabled()
    expect(screen.getByRole('button', { name: 'OAuth' })).toBeDisabled()

    await user.type(screen.getByLabelText('Username or Email'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(signIn)

    expect(mocks.toastError).toHaveBeenCalledWith('Please agree to the legal terms first')
    expect(mocks.login).not.toHaveBeenCalled()
  })
})
