import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import * as pinia from 'pinia'
import * as capabilities from '../config/deploymentCapabilities'
import { fixture } from '../test-utils/vueFixture'

function setup(t: TestContext) {
  const f = fixture(t)
  const levels: Record<string, number> = { viewer: 1, contributor: 2, admin: 3, owner: 4 }
  const auth = vue.reactive({ isLiteMode: false, currentTenantRole: 'owner', canAccessAllTenants: false, isSystemAdmin: false,
    hasRole: (minimum: string): boolean => (levels[auth.currentTenantRole] || 0) >= (levels[minimum] || 0),
    get hasManagementNavigation(): boolean {
      return auth.isLiteMode || auth.isSystemAdmin || auth.canAccessAllTenants || auth.hasRole('admin')
    },
  })
  const flags = vue.reactive<capabilities.DeploymentCapabilityMap>({})
  const { useMenuStore } = f.evaluate(readFileSync(new URL('./menu.ts', import.meta.url), 'utf8'), {
    vue, pinia,
    '@/i18n': { __esModule: true, default: { global: { t: (key: string) => key, locale: vue.ref('zh-CN') } } },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({
      isSupported: (key?: capabilities.DeploymentCapabilityKey) => capabilities.isDeploymentCapabilitySupported(flags, key),
    }) },
  })
  const root = pinia.createPinia()
  pinia.setActivePinia(root)
  t.after(() => pinia.disposePinia(root))
  return { store: useMenuStore(), auth, flags }
}

test('toolbox menu is always visible to administrators and respects the switch for members', t => {
  const { store, auth, flags } = setup(t)
  const has = (path: string) => store.visibleMenuArr.some((item: { path: string }) => item.path === path)
  for (const supported of [undefined, false, true]) {
    flags['navigation.toolbox'] = supported === undefined ? undefined : { supported }
    for (const role of ['', 'viewer', 'contributor', 'admin', 'owner']) {
      auth.currentTenantRole = role
      assert.equal(has('toolbox'), supported === true || role === 'admin' || role === 'owner')
      assert.equal(has('automations'), false)
      assert.equal(has('knowledge-bases'), true)
    }
    auth.currentTenantRole = 'viewer'
    auth.isSystemAdmin = true
    assert.equal(has('toolbox'), true)
    auth.isSystemAdmin = false
    auth.canAccessAllTenants = true
    assert.equal(has('toolbox'), true)
    auth.canAccessAllTenants = false
  }
})

test('automation settings still work independently of the toolbox switch', t => {
  const { store, auth, flags } = setup(t)
  auth.currentTenantRole = 'viewer'
  const has = (path: string) => store.visibleMenuArr.some((item: { path: string }) => item.path === path)
  flags['navigation.automations'] = { supported: true }
  assert.equal(has('automations'), true)
  assert.equal(has('toolbox'), false)
  flags['navigation.toolbox'] = { supported: true }
  flags['navigation.automations'] = { supported: false }
  assert.equal(has('automations'), false)
  assert.equal(has('toolbox'), true)
})

test('agent navigation follows the active workspace role without flashing during hydration', t => {
  const { store, auth, flags } = setup(t)
  const hasAgent = () => store.visibleMenuArr.some((item: { path: string }) => item.path === 'agents')
  for (const role of ['', 'viewer', 'contributor']) {
    auth.currentTenantRole = role
    assert.equal(hasAgent(), false)
  }
  for (const role of ['admin', 'owner']) {
    auth.currentTenantRole = role
    assert.equal(hasAgent(), true)
  }
  auth.currentTenantRole = 'viewer' // workspace switched from owner to viewer
  assert.equal(hasAgent(), false)
  auth.currentTenantRole = 'owner'
  flags.agents = { supported: false }
  assert.equal(hasAgent(), false)
})

test('command palette agent-list shortcut uses the same minimum role', () => {
  const source = readFileSync(new URL('../components/GlobalCommandPalette.vue', import.meta.url), 'utf8')
  assert.match(source, /if \(command.id === 'open-agents'\) \{\s*return authStore.hasManagementNavigation && authStore.hasRole\('contributor'\) && deploymentCapabilities.isSupported\('agents'\)/)
})

test('document center remains visible without a sandbox and does not duplicate artifacts', t => {
  const { store, auth, flags } = setup(t)
  flags['settings.sandbox'] = { supported: false }
  for (const role of ['viewer', 'contributor', 'admin', 'owner']) {
    auth.currentTenantRole = role
    const paths = store.visibleMenuArr.map((item: { path: string }) => item.path)
    assert.equal(paths.filter((path: string) => path === 'documents').length, 1)
    assert.equal(paths.includes('artifacts'), false)
  }
})
