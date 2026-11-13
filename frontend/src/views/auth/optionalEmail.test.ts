import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('registration and admin-create email fields are optional and post-register login uses username', () => {
  const login = readFileSync(new URL('./Login.vue', import.meta.url), 'utf8')
  const rules = login.slice(login.indexOf('const registerRules'), login.indexOf('// Toggle login'))
  assert.doesNotMatch(rules, /auth.emailRequired/)
  assert.match(rules, /email: true/)
  assert.match(login, /formData.email = registerData.username/)
  const admin = readFileSync(new URL('../system/CreateUserDialog.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(admin, /validation.emailRequired/)
  assert.match(admin, /auth.emailOptional/)
})
