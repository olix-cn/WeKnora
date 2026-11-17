import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { parseDocumentCatalog, parseDocumentCenterTab } from './documentCenter'
import { documentCenterMessages } from '../i18n/documentCenter'

test('document tabs default to files without accepting query arrays or unknown values', () => {
  for (const value of [undefined, null, '', 'unknown', ['tasks']]) {
    assert.equal(parseDocumentCenterTab(value), 'files')
  }
  assert.equal(parseDocumentCenterTab('templates'), 'templates')
  assert.equal(parseDocumentCenterTab('tasks'), 'tasks')
})

test('old or malformed backend cannot enable a sample download', () => {
  for (const value of [null, {}, { templates: null }, { templates: [null, {}, { id: 'evil' }] }]) {
    assert.deepEqual(parseDocumentCatalog(value), [])
  }
  const template = { id: 'engineering_bom_v1', version: 1, format: 'xlsx', builtin: true, preview_available: true }
  assert.deepEqual(parseDocumentCatalog({ templates: [template] }), [template])
  assert.deepEqual(parseDocumentCatalog({ templates: [{ ...template, preview_available: false }] }), [])
})

test('legacy artifacts route remains an authenticated alias with the original route name', () => {
  const routes = readFileSync(new URL('../router/index.ts', import.meta.url), 'utf8')
  assert.match(routes, /path: "documents",\s+alias: "artifacts",\s+name: "artifactLibrary"/)
  const route = routes.slice(routes.indexOf('path: "documents"'), routes.indexOf('path: "toolbox/'))
  assert.match(route, /requiresAuth: true/)
  assert.doesNotMatch(route, /settings\.sandbox/)
})

test('all locales include explicit staged availability notices and distinct tabs', () => {
  for (const messages of Object.values(documentCenterMessages)) {
    assert.equal(new Set(Object.values(messages.tabs)).size, 3)
    for (const value of [messages.title, messages.notAvailable, messages.tasksNotice, messages.failed]) {
      assert.ok(value.length > 0)
    }
  }
})
