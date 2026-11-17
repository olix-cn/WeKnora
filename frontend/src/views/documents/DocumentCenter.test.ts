import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import * as model from '../../config/documentCenter'
import { fixture, flush } from '../../test-utils/vueFixture'

test('template library does not advertise a specific business workflow', () => {
  const center = readFileSync(new URL('./DocumentCenter.vue', import.meta.url), 'utf8')
  const library = readFileSync(new URL('./TemplateLibrary.vue', import.meta.url), 'utf8')
  const messages = readFileSync(new URL('../../i18n/documentCenter.ts', import.meta.url), 'utf8')
  assert.ok(!center.includes('chatTemplateNotice'))
  assert.ok(!library.includes('readyForChat'))
  assert.ok(!center.includes('EBOM') && !library.includes('EBOM'))
  assert.ok(!messages.includes('在聊天中指定一套图纸的文件夹'))
  assert.ok(!messages.includes('聊天中可直接指定此模板生成 EBOM'))
})

function setup(t: TestContext, query: Record<string, unknown>, load: () => Promise<unknown>) {
  const f = fixture(t)
  const route = vue.reactive({ query, hash: '#position' })
  const pushed: unknown[] = []
  const auth = vue.reactive({ currentUserId: 'owner', effectiveTenantId: 7 })
  const { vm } = f.component(new URL('./DocumentCenter.vue', import.meta.url), {
    'vue-router': { useRoute: () => route, useRouter: () => ({ push: (value: unknown) => pushed.push(value) }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { error() {} } },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/api/documentCenter': { getDocumentTemplates: load, downloadBOMTemplatePreview: () => Promise.reject() },
    '@/config/documentCenter': model,
  })
  return { vm, route, pushed, auth }
}

test('files load no template capability; tasks verify availability before enabling jobs', async t => {
  let calls = 0
  const { vm, route } = setup(t, {}, async () => { calls++; return {} })
  await flush()
  assert.equal(calls, 0)
  assert.equal(vm.activeTab, 'files')
  route.query = { tab: 'tasks' }
  await flush()
  assert.equal(calls, 1)
  assert.equal(vm.generationAvailable, false)
})

test('template deep link loads verified catalog and reuses it', async t => {
  let calls = 0
  const item = { id: 'engineering_bom_v1', version: 1, format: 'xlsx', builtin: true, preview_available: true }
  const { vm, route } = setup(t, { tab: 'templates' }, async () => {
    calls++
    return { success: true, data: { templates: [item] } }
  })
  await flush()
  assert.equal(vm.templates.length, 1)
  route.query = { tab: 'files' }
  await flush()
  route.query = { tab: 'templates' }
  await flush()
  assert.equal(calls, 1)
})

test('unavailable backend is visible as error and can be retried', async t => {
  let fail = true
  const { vm } = setup(t, { tab: 'templates' }, async () => {
    if (fail) throw new Error('404')
    return { success: true, data: { templates: [] } }
  })
  await flush()
  assert.equal(vm.loadFailed, true)
  assert.equal(vm.templates.length, 0)
  fail = false
  await vm.loadCatalog()
  assert.equal(vm.loadFailed, false)
})

test('tab selection keeps existing file search and hash in its deep link', t => {
  const { vm, pushed } = setup(t, { q: 'BOM', type: 'spreadsheet' }, async () => ({}))
  vm.selectTab('templates')
  assert.equal(JSON.stringify(pushed[0]), JSON.stringify({
    query: { q: 'BOM', type: 'spreadsheet', tab: 'templates' }, hash: '#position',
  }))
})

test('custom template management is enabled only by an explicit capability', async t => {
  const { vm, auth } = setup(t, { tab: 'templates' }, async () => ({ success: true,
    data: { templates: [], generation_available: true, custom_templates_available: true } }))
  await flush()
  assert.equal(vm.customTemplatesAvailable, true)
  auth.effectiveTenantId = 8
  await vue.nextTick()
  assert.equal(vm.customTemplatesAvailable, false)
  await flush()
})
