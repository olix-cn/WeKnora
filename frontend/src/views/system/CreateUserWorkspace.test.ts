import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { fixture } from '../../test-utils/vueFixture'
import * as state from './createUserDialogState'

function setup(t: TestContext) {
  const f = fixture(t)
  const loads: Array<{ keyword: string; resolve: (value: any) => void; reject: (error: Error) => void }> = []
  const requests: any[] = []
  const props = { visible: true, active: true }
  const { vm, close } = f.component(new URL('./CreateUserDialog.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {}, error() {} } },
    '@/api/auth': { getAuthConfig: async () => ({ success: true, complex_password_enabled: false }) },
    '@/api/system': {
      listUserCreationWorkspaces: (keyword: string) => new Promise((resolve, reject) => loads.push({ keyword, resolve, reject })),
      createSystemUser: async (request: any) => { requests.push(request); return { created: true } },
    },
    '@/utils/clipboard': { copyWithToast() {} },
    '@/utils/passwordPolicy': { newPasswordRules: () => [] },
    './createUserDialogState': state,
  }, props)
  return { ...f, vm, close, loads, requests }
}

test('new user defaults to read-only and sends the selected workspace with the creation request', async t => {
  const { vm, requests } = setup(t)
  assert.equal(vm.form.role, 'viewer')
  assert.equal(vm.form.defaultTenantId, undefined)
  assert.equal(vm.rules.defaultTenantId[0].required, true)
  vm.form.username = 'alice'
  vm.form.defaultTenantId = 42
  vm.form.role = 'contributor'
  vm.form.autoGenerate = false
  vm.form.newPassword = '1'
  vm.formRef = { validate: async () => true }
  await vm.submit()
  assert.deepEqual(JSON.parse(JSON.stringify(requests[0])), {
    username: 'alice', email: '', password: '1', default_tenant_id: 42, role: 'contributor',
  })
})

test('late workspace responses and unmounted requests cannot overwrite a newer search', async t => {
  const { vm, loads, close } = setup(t)
  const first = vm.loadWorkspaces('old')
  const second = vm.loadWorkspaces('new')
  loads[1].resolve({ items: [{ id: 42, name: 'New' }] })
  await second
  loads[0].resolve({ items: [{ id: 99, name: 'Old' }] })
  await first
  assert.equal(vm.workspaceOptions[0].value, 42)
  const last = vm.loadWorkspaces('unmounted')
  close()
  loads[2].resolve({ items: [{ id: 88, name: 'Closed' }] })
  await last
  assert.equal(vm.workspaceOptions[0].value, 42)
})

test('workspace search keeps the selected option and exposes retry after failure', async t => {
  const { vm, loads } = setup(t)
  vm.workspaceOptions = [{ label: 'Selected (42)', value: 42 }]
  vm.form.defaultTenantId = 42
  const pending = vm.loadWorkspaces('another')
  loads[0].resolve({ items: [{ id: 99, name: 'Other' }] })
  await pending
  assert.equal(vm.workspaceOptions[0].value, 42)
  const failed = vm.loadWorkspaces('offline')
  loads[1].reject(new Error('offline'))
  await failed
  assert.equal(vm.workspaceError, true)
  assert.equal(vm.workspaceLoading, false)
})
