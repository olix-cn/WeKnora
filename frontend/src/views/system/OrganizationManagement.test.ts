import assert from 'node:assert/strict'
import test from 'node:test'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture'

test('organization management stays under system administration, not the main menu', async () => {
  const fs = await import('node:fs')
  const settings = fs.readFileSync(new URL('../settings/Settings.vue', import.meta.url), 'utf8')
  const menu = fs.readFileSync(new URL('../../stores/menu.ts', import.meta.url), 'utf8')
  const system = fs.readFileSync(new URL('./SystemSettings.vue', import.meta.url), 'utf8')
  assert.match(settings, /pickItems\(\['organization-management', 'knowledge-base-permissions', 'system-global'/)
  assert.match(settings, /currentSection === 'organization-management' && authStore.isSystemAdmin/)
  assert.doesNotMatch(menu, /organization-management/)
  assert.doesNotMatch(system, /<UserManagement|key: 'users'/)
})

test('department tree excludes descendants from parent choices and moves versioned selections', async t => {
  const f = fixture(t), writes: any[] = []
  const departments = [
    { id: 'a', parent_id: null, name: 'Engineering', version: 2, member_count: 1 },
    { id: 'b', parent_id: 'a', name: 'Development', version: 1, member_count: 0 },
  ]
  const { vm } = f.component(new URL('./OrganizationManagement.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { error() {} } },
    './UserManagement.vue': {},
    '@/api/system/directory': {
      getDirectory: async () => ({ directory: { name: 'Company' }, departments }),
      listPersonnel: async () => ({ users: [] }),
      saveDepartment: async () => ({}), deleteDepartment: async () => ({}),
      movePersonnel: async (...args: any[]) => { writes.push(args) },
    },
  })
  await flush()
  vm.editDepartment(departments[0]); await flush()
  assert.equal(vm.parentOptions.length, 1, 'cannot parent a department to itself or its descendants')
  vm.selection = [{ id: 'member', version: 7 }]; vm.moveTarget = 'b'
  await vm.move()
  assert.deepEqual(JSON.parse(JSON.stringify(writes)), [['b', [{ user_id: 'member', expected_version: 7 }]]])
})

test('member searches run on the server and late results cannot replace a newer filter', async t => {
  const f = fixture(t), pending: Array<(value: any) => void> = [], queries: any[] = []
  const { vm } = f.component(new URL('./UserManagement.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key, locale: vue.ref('en-US') }) },
    'tdesign-vue-next': { MessagePlugin: { error() {}, success() {} } },
    '@/api/system': {}, '@/stores/auth': { useAuthStore: () => ({ currentUserId: 'admin' }) },
    './CreateUserDialog.vue': {}, './ResetPasswordDialog.vue': {},
    '@/api/system/directory': { updatePersonnel: async () => {}, listPersonnel: (query: any) => { queries.push(query); return new Promise(resolve => pending.push(resolve)) } },
  }, { active: true, departmentId: 'department' })
  await flush()
  vm.search = 'a user on another page'; await flush()
  const newer = pending.length - 1
  pending[newer]!({ users: [{ id: 'new' }], total: 33, has_more: true, offset: 0 })
  await flush()
  pending[0]!({ users: [{ id: 'old' }], total: 1, has_more: false, offset: 0 }); await flush()
  assert.equal(vm.users[0].id, 'new'); assert.equal(vm.total, 33)
  assert.equal(queries[newer].keyword, 'a user on another page')
  assert.equal(queries[newer].department_id, 'department')
})
