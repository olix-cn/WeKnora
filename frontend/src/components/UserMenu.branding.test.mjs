import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('user menu has no documentation or GitHub links but retains settings and logout', () => {
  const source = readFileSync(new URL('./UserMenu.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /openDocs|openGithub|helpAndDocs|common\.github|logo-github/)
  assert.match(source, /@click="handleSettings"/)
  assert.match(source, /@click="handleLogout"/)
})
