import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import { fixture, flush } from '../../../test-utils/vueFixture'

function setup(t: TestContext, overrides: Record<string, unknown> = {}) {
  const f = fixture(t)
  const auth = vue.reactive({ currentUserId: 'manager', effectiveTenantId: 7 })
  const saved: any[] = []
  const api = {
    getKBAccessPolicy: async () => ({ data: { mode: 'inherit', version: 3, members: [] } }),
    searchKBAccessMembers: async () => ({ data: [
      { user_id: 'reader', username: 'Reader', role: 'viewer' },
      { user_id: 'editor', username: 'Editor', role: 'contributor' },
    ], total: 2 }),
    replaceKBAccessPolicy: async (...args: any[]) => { saved.push(args); return { data: {} } },
    ...overrides,
  }
  const { vm, close } = f.component(new URL('./KBMemberAccessPanel.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ locale: vue.ref('zh-CN') }) },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/api/knowledge-base/memberAccess': api,
  }, { kbId: 'kb', creatorId: 'creator' })
  return { vm, auth, saved, close }
}

test('restricted mode defaults creator into roster and requires explicit confirmation', async t => {
  const f = setup(t)
  await flush()
  f.vm.changeMode('restricted')
  assert.equal(f.vm.members[0].user_id, 'creator')
  await f.vm.save()
  assert.equal(f.saved.length, 0)
  f.vm.confirmed = true
  await f.vm.save()
  assert.equal(f.saved.length, 1)
  assert.equal(f.saved[0][1].version, 3)
  assert.equal(f.saved[0][1].mode, 'restricted')
  assert.equal(f.vm.policy, null, 'save may revoke the manager: clear the old private roster')
})

test('viewer role cannot be upgraded through member selector', async t => {
  const f = setup(t)
  await flush()
  await f.vm.searchMembers()
  f.vm.addMember(f.vm.candidates[0])
  f.vm.setPermission('reader', 'admin')
  assert.equal(f.vm.members[0].permission, 'viewer')
  f.vm.addMember(f.vm.candidates[1])
  f.vm.setPermission('editor', 'editor')
  assert.equal(f.vm.members[1].permission, 'editor')
  await f.vm.save()
  assert.equal(f.saved.length, 1)
})

test('version conflict locks editing until reloading and does not overwrite', async t => {
  let attempts = 0
  const f = setup(t, { replaceKBAccessPolicy: async () => {
    attempts++; throw { code: 'policy_conflict' }
  } })
  await flush()
  f.vm.changeMode('restricted'); f.vm.confirmed = true
  await f.vm.save()
  assert.equal(f.vm.conflict, true)
  f.vm.confirmed = true
  await f.vm.save()
  assert.equal(attempts, 1)
  await f.vm.load()
  assert.equal(f.vm.conflict, false)
})

test('workspace switch drops old roster and ignores late policy result', async t => {
  const finishes: Array<(value: unknown) => void> = []
  const f = setup(t, { getKBAccessPolicy: () => new Promise(resolve => finishes.push(resolve)) })
  await flush()
  f.auth.effectiveTenantId = 8
  await flush()
  finishes[0]!({ data: { mode: 'restricted', version: 99, members: [{ user_id: 'private', permission: 'admin' }] } })
  await flush()
  assert.equal(f.vm.policy, null)
  finishes[1]!({ data: { mode: 'inherit', version: 0, members: [] } })
  await flush()
  assert.equal(f.vm.policy.version, 0)
  assert.equal(f.vm.members.length, 0)
})

test('editing a confirmed restricted roster invalidates confirmation', async t => {
  const f = setup(t)
  await flush()
  f.vm.changeMode('restricted'); f.vm.confirmed = true
  f.vm.removeMember('creator')
  assert.equal(f.vm.confirmed, false)
  assert.equal(f.vm.members.length, 0)
  await f.vm.save()
  assert.equal(f.saved.length, 0)
})

test('revoked management permission clears the private roster rather than leaving an editable form', async t => {
  const f = setup(t, { replaceKBAccessPolicy: async () => { throw { code: 'policy_forbidden' } } })
  await flush()
  f.vm.changeMode('restricted'); f.vm.confirmed = true
  await f.vm.save()
  assert.equal(f.vm.policy, null)
  assert.equal(f.vm.members.length, 0)
  assert.equal(f.vm.canSave, false)
})

test('changing a member search ignores the previous query response', async t => {
  let finish: (value: unknown) => void = () => {}
  const f = setup(t, { searchKBAccessMembers: () => new Promise(resolve => { finish = resolve }) })
  await flush()
  const pending = f.vm.searchMembers()
  f.vm.search = 'new query'
  await flush()
  finish({ data: [{ user_id: 'stale', username: 'Old query', role: 'contributor' }], total: 1 })
  await pending
  assert.equal(f.vm.candidates.length, 0)
})

test('server-provided member names resolve grants before directory search', async t => {
  const f = setup(t, { getKBAccessPolicy: async () => ({ data: {
    mode: 'restricted', version: 2, members: [{ user_id: 'reader', permission: 'viewer' }],
    member_details: [{ user_id: 'reader', username: 'Readable name', role: 'viewer' }],
  } }) })
  await flush()
  assert.equal(f.vm.names.reader.username, 'Readable name')
  assert.equal(f.vm.permissionOptions('reader').find((p: any) => p.value === 'admin').disabled, true)
})
