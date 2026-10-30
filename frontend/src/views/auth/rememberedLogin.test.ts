import assert from 'node:assert/strict'
import test from 'node:test'
import { loadRememberedLogin, saveRememberedLogin, REMEMBERED_LOGIN_KEY } from './rememberedLogin.ts'

test('credentials require an explicit save and are removed on opt-out', () => {
  const values = new Map<string, string>()
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  } })
  try {
    assert.equal(loadRememberedLogin(), null)
    const credentials = { email: 'test@example.com', password: 'Example9!' }
    assert.equal(saveRememberedLogin(credentials), true)
    assert.deepEqual(loadRememberedLogin(), credentials)
    assert.equal(saveRememberedLogin(null), true)
    assert.equal(loadRememberedLogin(), null)
    values.set(REMEMBERED_LOGIN_KEY, '{bad json')
    assert.equal(loadRememberedLogin(), null)
    values.set(REMEMBERED_LOGIN_KEY, JSON.stringify({ email: 123, password: 'x' }))
    assert.equal(loadRememberedLogin(), null)
  } finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})

test('blocked browser storage does not prevent the login flow', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, get() { throw new Error('storage denied') } })
  try {
    assert.equal(loadRememberedLogin(), null)
    assert.equal(saveRememberedLogin({ email: 'test@example.com', password: 'Example9!' }), false)
    assert.equal(saveRememberedLogin(null), false)
  } finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})
