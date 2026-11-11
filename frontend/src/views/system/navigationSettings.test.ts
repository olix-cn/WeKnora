import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import zhCN from '../../i18n/locales/zh-CN'
import enUS from '../../i18n/locales/en-US'

const source = readFileSync(new URL('./SystemSettings.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const ast = ts.createSourceFile('SystemSettings.ts', script, ts.ScriptTarget.Latest, true)
const refresh = ast.statements.find(statement => ts.isFunctionDeclaration(statement) && statement.name?.text === 'refreshSandboxDockerCapability')!

test('both menu switches refresh the shared capabilities after save and reset', async () => {
  const calls: boolean[] = []
  const compiled = ts.transpileModule(refresh.getText(ast), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
  const update = runInNewContext(`${compiled}\nrefreshSandboxDockerCapability`, {
    deploymentCapabilities: { ensureLoaded: async (force: boolean) => { calls.push(force) } },
  })
  for (const key of ['ui.navigation.automations_visible', 'ui.navigation.toolbox_visible', 'sandbox.docker_enabled']) await update(key)
  await update('model.max_concurrency')
  assert.deepEqual(calls, [true, true, true])
  assert.equal(source.match(/await refreshSandboxDockerCapability\(item.key\)/g)?.length, 2)
})

test('menu switches are in the administrator access settings and have localized labels', () => {
  const access = source.slice(source.indexOf('const SETTINGS_SECTION_KEYS'), source.indexOf('const activeSettingsSection'))
  assert.match(access, /access:\s*\[\s*'ui.navigation.automations_visible',\s*'ui.navigation.toolbox_visible'/)
  for (const locale of [zhCN, enUS]) {
    assert.ok(locale.system.globalSettings.keyLabels.ui.navigation.automations_visible)
    assert.ok(locale.system.globalSettings.keyLabels.ui.navigation.toolbox_visible)
  }
})
