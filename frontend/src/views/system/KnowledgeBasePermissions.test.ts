import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import * as vue from 'vue'
import { fixture, flush } from '../../test-utils/vueFixture'

test('permissions center is a system administration peer, not a library-detail dialog', () => {
  const settings = readFileSync(new URL('../settings/Settings.vue', import.meta.url), 'utf8')
  const detail = readFileSync(new URL('../knowledge/KnowledgeBase.vue', import.meta.url), 'utf8')
  assert.match(settings, /pickItems\(\['organization-management', 'knowledge-base-permissions', 'system-global'/)
  assert.doesNotMatch(detail, /KBMemberAccessPanel|memberAccessVisible/)
  const directory = readFileSync(new URL('./OrganizationManagement.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(directory, /v-model="form.code"/)
  assert.match(directory, /autoCode/)
})

test('list uses global policy metadata, pages all results and reloads after save', async t => {
  const f = fixture(t), queries: any[] = []
  const { vm } = f.component(new URL('./KnowledgeBasePermissions.vue', import.meta.url), {
    'vue-i18n': { useI18n: () => ({ locale: vue.ref('zh-CN') }) },
    '../knowledge/components/KBMemberAccessPanel.vue': {},
    '@/api/system/kbPermissions': { listKBPermissions: async (...args: any[]) => { queries.push(args); return { data: [{ id: 'kb', tenant_name: 'Other workspace', version: 5 }], total: 41 } } },
  })
  await flush()
  assert.equal(vm.total, 41)
  await vm.load(20)
  assert.equal(queries[1][1], 20)
  vm.openEditor(vm.rows[0]); assert.equal(vm.selected.id, 'kb')
  vm.saved(); await flush()
  assert.equal(vm.editorVisible, false)
  assert.equal(queries[2][1], 20)
})
