import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture'

function setup(t: TestContext, overrides: Record<string, unknown> = {}) {
  const f = fixture(t)
  const auth = vue.reactive({ currentUserId: 'owner', effectiveTenantId: 7, hasRole: (): boolean => true })
  const calls: unknown[] = []
  const version = { id: 'draft', name: 'Synthetic', version: 1, status: 'draft', can_manage: true }
  const inspection = { sheets: ['Sheet1'], header_candidates: [{ sheet: 'Sheet1', header_row: 1, start_row: 2,
    fields: [{ key: 'column_A', label: 'Part', column: 'A' }, { key: 'column_C', label: 'Name', column: 'C' }] }] }
  const api = {
    listCustomTemplates: async () => ({ data: [version] }),
    uploadCustomTemplate: async (...args: unknown[]) => { calls.push(args); return { data: version, inspection } },
    inspectCustomTemplate: async () => ({ data: inspection }),
    configureCustomTemplate: async (...args: unknown[]) => { calls.push(args) },
    getTemplateLayout: async () => ({ data: { sheet: 'Sheet1', header_row: 1, start_row: 2, fields: [{ key: 'part', label: 'Part', column: 'A' }] } }),
    testCustomTemplate: async () => ({}),
    publishCustomTemplate: async (...args: unknown[]) => { calls.push(args) },
    disableCustomTemplate: async () => ({}), ...overrides,
    previewCustomTemplate: overrides.previewCustomTemplate || (async () => ({})),
  }
  const { vm, close } = f.component(new URL('./TemplateLibrary.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-icons-vue-next': { UploadIcon: {} },
    '@/stores/auth': { useAuthStore: () => auth }, '@/api/documentCenter': api,
  })
  return { vm, auth, calls, version, close }
}

test('draft upload configures exact coordinates; edits invalidate test approval', async t => {
  const { vm, calls } = setup(t)
  vm.file = { name: 'synthetic.xlsx', size: 1024 }; vm.name = 'Synthetic'
  await vm.upload()
  assert.equal(vm.editing.id, 'draft')
  assert.equal(vm.layout.sheet, 'Sheet1')
  assert.equal(vm.saved, false)
  assert.equal(vm.layout.fields.length, 2)
  assert.equal(vm.layout.fields[0].label, 'Part')
  await vm.saveLayout()
  assert.equal(vm.saved, false)
  vm.headerConfirmed = true
  await vm.saveLayout()
  assert.equal(vm.saved, true)
  vm.tested = true; vm.sampleCleared = true
  vm.layout.fields[0].column = 'B'
  assert.equal(vm.saved, false)
  assert.equal(vm.tested, false)
  assert.equal(vm.sampleCleared, false)
  await vm.publish()
  assert.equal(calls.length, 2)
})

test('published versions are separated from private drafts and preview uses authenticated bytes', async t => {
  const blob = { private: true }
  const f = setup(t, { listCustomTemplates: async () => ({ data: [
    { id: 'published', name: 'Published', version: 1, status: 'published', can_manage: false },
    { id: 'draft', name: 'Draft', version: 1, status: 'draft', can_manage: true },
  ] }), previewCustomTemplate: async () => blob })
  await flush()
  assert.equal(f.vm.publishedVersions.length, 1)
  assert.equal(f.vm.managedDrafts.length, 1)
  await f.vm.openPreview(f.vm.publishedVersions[0])
  assert.equal(f.vm.previewVisible, true)
  assert.deepEqual(f.vm.previewBlob, blob)
  f.vm.closePreview()
  assert.equal(f.vm.previewBlob, null)
})

test('workspace switch discards late template preview bytes', async t => {
  let finish: (value: unknown) => void = () => {}
  const f = setup(t, { previewCustomTemplate: () => new Promise(resolve => { finish = resolve }) })
  const pending = f.vm.openPreview({ id: 'old', name: 'Old', version: 1 })
  f.auth.effectiveTenantId = 8
  await flush()
  finish({ private: true }); await pending
  assert.equal(f.vm.previewVisible, false)
  assert.equal(f.vm.previewBlob, null)
})

test('guided setup changes worksheets and columns without typing coordinates', async t => {
  const f = setup(t)
  f.vm.file = { name: 'synthetic.xlsx', size: 1024 }; f.vm.name = 'Synthetic'
  await f.vm.upload()
  assert.equal(f.vm.layout.header_row, 1)
  assert.equal(f.vm.layout.start_row, 2)
  f.vm.headerConfirmed = true
  f.vm.toggleColumn(f.vm.currentCandidate.fields[1], false)
  assert.equal(f.vm.layout.fields.length, 1)
  assert.equal(f.vm.layout.fields[0].column, 'A')
  assert.equal(f.vm.headerConfirmed, false)
  f.vm.chooseSheet('missing')
  assert.equal(f.vm.layout.fields.length, 0)
})

test('saving unchanged configuration invalidates earlier test and publish approval', async t => {
  const f = setup(t)
  await f.vm.edit({ ...f.version, status: 'tested' })
  f.vm.sampleCleared = true
  await f.vm.saveLayout()
  assert.equal(f.vm.saved, true)
  assert.equal(f.vm.tested, false)
  assert.equal(f.vm.sampleCleared, false)
})

test('one-click trial requires confirmation; failed trial never enables publishing', async t => {
  let tests = 0
  const f = setup(t, { testCustomTemplate: async () => { tests++; throw new Error('trial failed') } })
  await f.vm.edit(f.version)
  await f.vm.confirmAndTest()
  assert.equal(f.calls.length, 0)
  f.vm.headerConfirmed = true
  await f.vm.confirmAndTest()
  assert.equal(f.calls.length, 1)
  assert.equal(tests, 1)
  assert.equal(f.vm.saved, true)
  assert.equal(f.vm.tested, false)
  assert.equal(f.vm.error, 'trial failed')
  await f.vm.publish()
  assert.equal(f.calls.length, 1)
})

test('filename supplies an editable template name', t => {
  const f = setup(t)
  f.vm.chooseFile({ target: { files: [{ name: 'Synthetic.xlsx' }] } })
  assert.equal(f.vm.name, 'Synthetic')
  f.vm.name = 'Custom'
  f.vm.chooseFile({ target: { files: [{ name: 'Other.xlsx' }] } })
  assert.equal(f.vm.name, 'Custom')
})

test('header suggestion favors headings over a single-cell title and requires confirmation', async t => {
  const f = setup(t, { inspectCustomTemplate: async () => ({ data: {
    sheets: ['First', 'Second'], header_candidates: [
      { sheet: 'First', header_row: 1, start_row: 2, fields: [{ key: 'title', label: 'Title', column: 'A' }] },
      { sheet: 'First', header_row: 3, start_row: 4, fields: [
        { key: 'a', label: 'Part', column: 'A' }, { key: 'b', label: 'Name', column: 'B' }] },
      { sheet: 'Second', header_row: 5, start_row: 6, fields: [{ key: 'x', label: 'Code', column: 'D' }] },
    ],
  } }) })
  await f.vm.edit(f.version)
  assert.equal(f.vm.layout.header_row, 3)
  assert.equal(f.vm.headerConfirmed, false)
  f.vm.chooseSheet('Second')
  assert.equal(f.vm.layout.header_row, 5)
  assert.equal(f.vm.layout.fields[0].column, 'D')
})

test('late inspection cannot reveal a previous workspace template', async t => {
  let finish: (v: unknown) => void = () => {}
  const f = setup(t, { inspectCustomTemplate: () => new Promise(resolve => { finish = resolve }) })
  const pending = f.vm.edit(f.version)
  f.auth.effectiveTenantId = 8
  await flush()
  finish({ data: { sheets: ['private'], header_candidates: [] } }); await pending
  assert.equal(f.vm.inspection, null)
  assert.equal(f.vm.editing, null)
})

test('late configuration response cannot restore approval after workspace switch', async t => {
  let finish: () => void = () => {}
  const f = setup(t, { configureCustomTemplate: () => new Promise<void>(resolve => { finish = resolve }) })
  await f.vm.edit({ ...f.version, status: 'configured' })
  const pending = f.vm.saveLayout()
  f.auth.effectiveTenantId = 8
  await flush()
  finish(); await pending
  assert.equal(f.vm.editing, null)
  assert.equal(f.vm.saved, false)
  assert.equal(f.vm.file, null)
})

test('non-admin and oversized upload never send file bytes', async t => {
  const f = setup(t)
  f.vm.file = { name: 'large.xlsx', size: 11 * 1024 * 1024 }; f.vm.name = 'Synthetic'
  await f.vm.upload()
  assert.equal(f.vm.error, 'documentCenter.templateTooLarge')
  f.auth.hasRole = () => false
  f.vm.file = { name: 'small.xlsx', size: 1024 }
  await f.vm.upload()
  assert.equal(f.calls.length, 0)
})
