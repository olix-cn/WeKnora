import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture'

function setup(t: TestContext, overrides: Record<string, unknown> = {}) {
  const f = fixture(t)
  const auth = vue.reactive({ currentUserId: 'owner', effectiveTenantId: 7 })
  const calls: unknown[][] = []
  let fail = false
  const source = { session_id: '11111111-1111-1111-1111-111111111111', message_id: 'message', result_id: 'result',
    row_count: 12, column_count: 16, partial: true }
  const api = {
    listDocumentSources: async (id: string) => { calls.push(['sources', id]); return { data: [source] } },
    importDocumentDataset: async () => ({ data: { ...source, id: 'dataset', issues: ['partial'], fields: [{ id: 'source-col', title: 'Part' }] } }),
    generateDocument: async (...args: unknown[]) => {
      calls.push(args)
      if (fail) throw new Error('network')
      return { data: { id: 'task', status: 'completed', file_name: 'bom.xlsx' } }
    },
    listGenerationTasks: async () => ({ data: [], total: 0 }),
    listCustomTemplates: async () => ({ data: [] }),
    getTemplateLayout: async () => ({ data: { sheet: 'Sheet1', header_row: 1, start_row: 2,
      fields: [{ key: 'part', label: 'Part', column: 'A' }] } }),
    downloadGenerationFile: async () => ({}), ...overrides,
  }
  const { vm } = f.component(new URL('./GenerationPanel.vue', import.meta.url), {
    'vue-router': { useRoute: () => ({ query: {} }) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { error() {} } },
    '@/stores/auth': { useAuthStore: () => auth },
    '@/utils/sessionArtifacts': { formatArtifactDateTime: (v: string) => v },
    '@/config/documentCenter': { newGenerationRequestKey: () => 'fixed-request-key' },
    '@/api/documentCenter': api,
  }, { mode: 'create' })
  return { vm, auth, source, calls, fail: (value: boolean) => { fail = value } }
}

test('import only uses source coordinates, partial requires acceptance, retry reuses request key', async t => {
  const f = setup(t)
  await f.vm.selectSource(f.source)
  await f.vm.generate()
  assert.equal(f.calls.length, 0)
  f.vm.acceptPartial = true
  f.fail(true)
  await f.vm.generate()
  assert.equal(f.vm.uncertain, true)
  f.fail(false)
  await f.vm.generate()
  assert.equal(f.vm.result.id, 'task')
  assert.equal(JSON.stringify(f.calls[0]), JSON.stringify(['dataset', 'fixed-request-key', true]))
  assert.equal(JSON.stringify(f.calls[1]), JSON.stringify(f.calls[0]))
})

test('workspace switch discards private dataset and stale import responses', async t => {
  let finish: (v: unknown) => void = () => {}
  const f = setup(t, { importDocumentDataset: () => new Promise(resolve => { finish = resolve }) })
  const pending = f.vm.selectSource(f.source)
  f.auth.effectiveTenantId = 8
  await flush()
  finish({ data: { ...f.source, id: 'old-private-data' } })
  await pending
  assert.equal(f.vm.dataset, null)
  assert.equal(f.vm.importing, false)
})

test('missing source identifier never calls the backend', async t => {
  const f = setup(t)
  f.vm.sessionId = 'not a session'
  await flush()
  await f.vm.findSources()
  assert.equal(f.calls.length, 0)
  assert.equal(f.vm.error, 'documentCenter.invalidSession')
})

test('custom template requires explicit binding and sends immutable template ID', async t => {
  const f = setup(t)
  await f.vm.selectSource(f.source)
  f.vm.acceptPartial = true; f.vm.templateId = 'custom-v1'
  await f.vm.selectTemplate()
  assert.equal(f.vm.bindingsReady, false)
  await f.vm.generate()
  assert.equal(f.calls.length, 0)
  f.vm.bindings.part = 'source-col'
  assert.equal(f.vm.bindingsReady, true)
  await f.vm.generate()
  assert.equal(JSON.stringify(f.calls[0]), JSON.stringify(['dataset', 'fixed-request-key', true, 'custom-v1', { part: 'source-col' }]))
})

test('late layout response is ignored after choosing another template', async t => {
  let finish: (value: unknown) => void = () => {}
  const f = setup(t, { getTemplateLayout: () => new Promise(resolve => { finish = resolve }) })
  f.vm.templateId = 'custom-v1'
  const pending = f.vm.selectTemplate()
  f.vm.templateId = 'engineering_bom_v1'
  await f.vm.selectTemplate()
  finish({ data: { fields: [{ key: 'stale' }] } }); await pending
  assert.equal(f.vm.templateLayout, null)
})
