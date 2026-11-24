import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed } from 'vue'
import { INTEGRATION_PREVIEW_ITEMS } from '../../config/integrations'
import { integrationSectionKey } from '../../config/settingsRoute'
import { MEMBER_SETTINGS_SECTIONS, SETTINGS_SECTION_MIN_ROLE, SYSTEM_ADMIN_SETTINGS_SECTIONS } from '../../config/settingsAccess'
import { isHiddenSettingsSection, visibleSettingsSection } from '../../config/settingsVisibility'
import { isToolboxSection } from '../../config/toolbox'

// Exercise the actual sidebar grouping without mounting the settings panels.
const source = readFileSync(new URL('./Settings.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('Settings.ts', script, ts.ScriptTarget.Latest, true)
const grouping = ast.statements.find((statement) =>
  ts.isVariableStatement(statement) && statement.declarationList.declarations.some(
    (declaration) => ts.isIdentifier(declaration.name) && declaration.name.text === 'navGroups',
  ),
)
assert.ok(grouping, 'Settings must define sidebar groups')
const compiled = ts.transpileModule(grouping.getText(ast), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText

function integrationMenu(visibleKeys: string[]): string[] {
  const groups = runInNewContext(`${compiled}\nnavGroups.value`, {
    computed,
    navItems: { value: visibleKeys.map((key) => ({ key })) },
    t: (key: string) => key,
    integrationSectionKey,
    INTEGRATION_PREVIEW_ITEMS,
  }) as Array<{ key: string; items: Array<{ key: string }> }>
  const integrationGroup = groups.find((group) => group.key === 'integrations')
  return Array.from(integrationGroup?.items ?? [], (item) => item.key)
}

const integrationKeys = INTEGRATION_PREVIEW_ITEMS.map((item) => integrationSectionKey(item.key))

test('settings sidebar hides the entire integrations group for every role', () => {
  assert.deepEqual(integrationMenu(['general', ...integrationKeys]), [])
})

test('settings sidebar never exposes a partial integrations group', () => {
  const visible = integrationKeys.filter((key) => key !== 'integration-api' && key !== 'integration-im')
  assert.deepEqual(integrationMenu(visible), [])
  assert.deepEqual(integrationMenu(['general']), [])
})

// Keep navigation tests tied to the real close handler, without mounting
// settings panels that require backend services.
const closeHandler = ast.statements.find((statement) =>
  ts.isVariableStatement(statement) && statement.declarationList.declarations.some(
    (declaration) => ts.isIdentifier(declaration.name) && declaration.name.text === 'handleClose',
  ),
)
assert.ok(closeHandler, 'Settings must define a close handler')
const compiledClose = ts.transpileModule(closeHandler.getText(ast), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText

function closeSettings(back: unknown, section = 'members', path = '/platform/settings'): string[] {
  const actions: string[] = []
  class FocusedElement {
    blur() { actions.push('blur') }
  }
  runInNewContext(`${compiledClose}\nhandleClose()`, {
    document: { activeElement: new FocusedElement() },
    HTMLElement: FocusedElement,
    route: { path, query: { section } },
    uiStore: { closeSettings: () => actions.push('close') },
    router: {
      options: { history: { state: { back } } },
      push: (target: string) => actions.push(`push:${target}`),
      back: () => actions.push('back'),
    },
  })
  return actions
}

test('closing a directly opened settings route stays in the application', () => {
  for (const back of [null, undefined, '', 'https://example.com/', '//example.com/', '/login']) {
    assert.deepEqual(closeSettings(back), ['blur', 'close', 'push:/platform/knowledge-bases'])
  }
})

test('closing settings preserves a known in-app previous route', () => {
  for (const back of ['/platform/knowledge-bases', '/platform/chat/session-123']) {
    assert.deepEqual(closeSettings(back), ['blur', 'close', 'back'])
  }
})

test('closing system settings retains its explicit knowledge-base destination', () => {
  for (const section of ['model-catalog', 'system-global', 'runtime-queues', 'platform-api-keys', 'system-audit-log']) {
    assert.deepEqual(closeSettings('/platform/chat/session-123', section), [
      'blur', 'close', 'push:/platform/knowledge-bases',
    ])
  }
})

test('closing an overlaid settings panel leaves the underlying route alone', () => {
  assert.deepEqual(closeSettings(null, 'general', '/platform/chat/session-123'), ['blur', 'close'])
})

function getDeclaration(name: string): string {
  const node = ast.statements.find(statement => ts.isVariableStatement(statement)
    && statement.declarationList.declarations.some(declaration => ts.isIdentifier(declaration.name) && declaration.name.text === name))
  assert.ok(node, name)
  return ts.transpileModule(node.getText(ast), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
}

test('member settings expose only general preferences and profile, including when role is loading', () => {
  const context = {
    isHiddenSettingsSection, visibleSettingsSection, isToolboxSection, MEMBER_SETTINGS_SECTIONS,
    SETTINGS_SECTION_MIN_ROLE, SYSTEM_ADMIN_SECTIONS: SYSTEM_ADMIN_SETTINGS_SECTIONS,
    route: { query: {} },
    authStore: { hasManagementNavigation: false, isSystemAdmin: false, canAccessAllTenants: false, hasRole: () => false },
  }
  const navigation = runInNewContext(`${getDeclaration('canSeeSection')}\n${getDeclaration('normalizeSettingsSection')}\n({canSeeSection, normalizeSettingsSection})`, context)
  for (const key of ['general', 'userprofile']) {
    assert.equal(navigation.canSeeSection(key), true)
    assert.equal(navigation.normalizeSettingsSection(key), key)
  }
  for (const key of ['models', 'defaults', 'tenant', 'members', 'envvars', 'credentialvault', 'assistant', 'mymemory', 'system', ...SYSTEM_ADMIN_SETTINGS_SECTIONS]) {
    assert.equal(navigation.canSeeSection(key), false)
    assert.equal(navigation.normalizeSettingsSection(key), 'general')
  }
  assert.equal(navigation.normalizeSettingsSection('browserconnection'), 'browserconnection', 'toolbox links retain their own checks')
  context.authStore.hasManagementNavigation = true
  context.authStore.hasRole = () => true
  assert.equal(navigation.canSeeSection('models'), true)
  assert.equal(navigation.canSeeSection('organization-management'), false)
  context.authStore.isSystemAdmin = true
  assert.equal(navigation.canSeeSection('organization-management'), true)
})
