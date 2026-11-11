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
  const auth = vue.reactive({ isLiteMode: false, currentTenantRole: 'owner', canAccessAllTenants: false,
    hasRole: (minimum: string): boolean => (levels[auth.currentTenantRole] || 0) >= (levels[minimum] || 0),
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

test('home menus are hidden by default for all roles, including administrators', t => {
  const { store, auth } = setup(t)
  for (const role of ['viewer', 'contributor', 'admin', 'owner']) {
    auth.currentTenantRole = role
    for (const allTenants of [false, true]) {
      auth.canAccessAllTenants = allTenants
      const paths = store.visibleMenuArr.map((item: { path: string }) => item.path)
      assert.equal(paths.includes('automations'), false)
      assert.equal(paths.includes('toolbox'), false)
      assert.equal(paths.includes('knowledge-bases'), true)
    }
  }
})

test('administrator switches independently reveal and hide home menus reactively', t => {
  const { store, flags } = setup(t)
  const has = (path: string) => store.visibleMenuArr.some((item: { path: string }) => item.path === path)
  flags['navigation.automations'] = { supported: true }
  assert.equal(has('automations'), true)
  assert.equal(has('toolbox'), false)
  flags['navigation.toolbox'] = { supported: true }
  assert.equal(has('toolbox'), true)
  flags['navigation.automations'] = { supported: false }
  assert.equal(has('automations'), false)
  assert.equal(has('toolbox'), true)
})

test('agent navigation follows the active workspace role without flashing during hydration', t => {
  const { store, auth, flags } = setup(t)
  const hasAgent = () => store.visibleMenuArr.some((item: { path: string }) => item.path === 'agents')
  for (const role of ['', 'viewer']) {
    auth.currentTenantRole = role
    assert.equal(hasAgent(), false)
  }
  for (const role of ['contributor', 'admin', 'owner']) {
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
  assert.match(source, /if \(command.id === 'open-agents'\) \{\s*return authStore.hasRole\('contributor'\) && deploymentCapabilities.isSupported\('agents'\)/)
})
