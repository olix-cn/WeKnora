import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { userAlreadyExisted, userCreationAuditAction } from './userCreationAudit'

test('new no-op audit event is not presented as user creation', () => {
  const row = { action: 'system.user_already_exists', details: { idempotent: true } }
  assert.equal(userAlreadyExisted(row), true)
  assert.equal(userCreationAuditAction(row), 'system.user_already_exists')
})

test('audit table and detail drawer share the same no-op presentation', () => {
  const source = readFileSync(new URL('./SystemAuditLog.vue', import.meta.url), 'utf8')
  assert.match(source, /formatAuditAction\(userCreationAuditAction\(row\)\)/)
  assert.match(source, /value: formatAuditAction\(userCreationAuditAction\(row\)\)/)
  assert.match(source, /formatAuditAction\(userCreationAuditAction\(selectedAuditEntry.value\)\)/)
  assert.equal(source.match(/system.globalSettings.audit.noChanges/g)?.length, 2)
})

test('historical idempotent creation records are shown as no-op without altering the source', () => {
  const row = { action: 'system.user_created', details: { idempotent: true } }
  assert.equal(userCreationAuditAction(row), 'system.user_already_exists')
  assert.equal(row.action, 'system.user_created')
  assert.equal(userCreationAuditAction({ action: 'system.user_created', details: { idempotent: false } }), 'system.user_created')
  assert.equal(userAlreadyExisted({ action: 'system.user_created' }), false)
  assert.equal(userAlreadyExisted({ action: 'system.user_created', details: { idempotent: 'true' } }), false)
  assert.equal(userAlreadyExisted({ action: 'system.admin_promoted', details: { idempotent: true } }), false)
})
