import assert from 'node:assert/strict'
import { webcrypto } from 'node:crypto'

import { afterEach, describe, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  clearPasswordEncryptionCache,
  encryptPassword,
  encryptPasswordFields,
} from '../password-encryption'

function pemEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  const base64 = btoa(binary)
  return `-----BEGIN PUBLIC KEY-----\n${base64.match(/.{1,64}/g)?.join('\n')}\n-----END PUBLIC KEY-----`
}

describe('password encryption', () => {
  afterEach(() => {
    clearPasswordEncryptionCache()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  test('encrypts the password with the server RSA-OAEP public key', async () => {
    const keyPair = await webcrypto.subtle.generateKey(
      {
        name: 'RSA-OAEP',
        modulusLength: 2048,
        publicExponent: new Uint8Array([1, 0, 1]),
        hash: 'SHA-256',
      },
      true,
      ['encrypt', 'decrypt']
    )
    const publicKey = pemEncode(
      await webcrypto.subtle.exportKey('spki', keyPair.publicKey)
    )
    vi.stubGlobal('crypto', webcrypto)
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { kid: 'test-key-id', public_key: publicKey },
      },
    } as never)

    const encrypted = await encryptPassword('correct horse battery staple')
    assert.equal(encrypted.encryption_key_id, 'test-key-id')
    assert.notEqual(
      encrypted.password_encrypted,
      'correct horse battery staple'
    )

    const ciphertext = Uint8Array.from(
      atob(encrypted.password_encrypted),
      (character) => character.charCodeAt(0)
    )
    const plaintext = await webcrypto.subtle.decrypt(
      { name: 'RSA-OAEP' },
      keyPair.privateKey,
      ciphertext
    )
    assert.equal(
      new TextDecoder().decode(plaintext),
      'correct horse battery staple'
    )

    const fields = (await encryptPasswordFields(
      {
        password: 'new-password',
        original_password: 'old-password',
      },
      ['password', 'original_password']
    )) as Record<string, unknown>
    assert.equal('password' in fields, false)
    assert.equal('original_password' in fields, false)
    assert.equal(typeof fields.password_encrypted, 'string')
    assert.equal(typeof fields.original_password_encrypted, 'string')
    assert.equal(fields.encryption_key_id, 'test-key-id')
  })

  test('keeps plaintext fields when the server disables encryption', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: { enabled: false },
      },
    } as never)

    const fields = await encryptPasswordFields(
      { password: 'legacy-password' },
      ['password']
    )
    assert.deepEqual(fields, { password: 'legacy-password' })
  })
})
