import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { savedLoginAccount, rememberLoginAccount, saveVerifiedPassword } from './loginCredentials.ts'

test('only the account is persisted and can be cleared', () => {
  const data = new Map<string, string>()
  const storage = { getItem: (key: string) => data.get(key) || null, setItem: (key: string, value: string) => { data.set(key, value) }, removeItem: (key: string) => { data.delete(key) } }
  rememberLoginAccount(storage, ' alice ')
  assert.equal(savedLoginAccount(storage), 'alice')
  assert.deepEqual([...data.values()], ['alice'])
  rememberLoginAccount(storage, '')
  assert.equal(savedLoginAccount(storage), '')
})

test('blocked local storage does not break login', () => {
  const fail = () => { throw new Error('blocked') }
  const storage = { getItem: fail, setItem: fail, removeItem: fail }
  assert.equal(savedLoginAccount(storage), '')
  assert.doesNotThrow(() => rememberLoginAccount(storage, 'alice'))
})

test('password saving is opt-in and happens after successful authentication', () => {
  const source = readFileSync(new URL('../views/auth/Login.vue', import.meta.url), 'utf8')
  assert.match(source, /if \(response.success\) \{\s*if \(saveCredentials.value\)/)
  assert.match(source, /name="username" autocomplete="username"/)
  assert.match(source, /name="password" autocomplete="current-password"/)
  assert.match(source, /<t-checkbox v-model="saveCredentials" :disabled="loading" @change="handleSaveCredentialsChange">/)
  assert.doesNotMatch(source, /aria-pressed="saveCredentials"|toggleSaveCredentials/)
  assert.match(source, /const handleSaveCredentialsChange = \(\) => \{\s*if \(!saveCredentials.value\) rememberLoginAccount\(localStorage, ''\)/)
})

test('unsupported browser falls back without storing a password', async () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'window')
  Object.defineProperty(globalThis, 'window', { configurable: true, value: { isSecureContext: false } })
  try { assert.equal(await saveVerifiedPassword('alice', 'not-a-real-password'), false) }
  finally {
    if (original) Object.defineProperty(globalThis, 'window', original)
    else Reflect.deleteProperty(globalThis, 'window')
  }
})

test('supported browsers receive verified credentials; rejected saves do not block login', async () => {
  const descriptors = ['window', 'navigator'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const)
  const received: unknown[] = []
  class PasswordCredential {
    constructor(public data: { id: string; password: string }) {}
  }
  Object.defineProperty(globalThis, 'window', { configurable: true, value: { isSecureContext: true, PasswordCredential } })
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: {
    credentials: { store: async (credential: unknown) => { received.push(credential) } },
  } })
  try {
    assert.equal(await saveVerifiedPassword('alice', 'test-password'), true)
    assert.deepEqual((received[0] as PasswordCredential).data, { id: 'alice', password: 'test-password' })
    navigator.credentials.store = async () => { throw new Error('denied') }
    assert.equal(await saveVerifiedPassword('alice', 'test-password'), false)
  } finally {
    for (const [key, descriptor] of descriptors) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else Reflect.deleteProperty(globalThis, key)
    }
  }
})
