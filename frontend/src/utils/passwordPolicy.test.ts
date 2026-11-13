import assert from 'node:assert/strict'
import test from 'node:test'
import { newPasswordRules } from './passwordPolicy'

const t = (key: string) => key

test('default password rules require a value but have no minimum or character classes', () => {
  const rules = newPasswordRules(t, false)
  assert.equal(rules.some(rule => rule.required), true)
  assert.equal(rules.some(rule => rule.min || rule.pattern), false)
  const bytes = rules.find(rule => rule.validator)!.validator as (value: string) => boolean
  for (const value of ['1', 'a', '密码', ' ', 'a'.repeat(72), '密'.repeat(24)]) assert.equal(bytes(value), true)
  assert.equal(bytes('a'.repeat(73)), false)
  assert.equal(bytes('密'.repeat(25)), false)
})

test('administrators may still explicitly enable complex policy', () => {
  const rules = newPasswordRules(t, true)
  assert.equal(rules.some(rule => rule.min === 8), true)
  assert.equal(rules.filter(rule => rule.pattern).length, 4)
})
