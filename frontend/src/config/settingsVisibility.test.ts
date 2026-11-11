import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { INTEGRATION_TABS } from './integrations'
import { visibleSettingsSection } from './settingsVisibility'

test('all canonical and legacy integration deep links fall back to general', () => {
  for (const tab of INTEGRATION_TABS) {
    assert.equal(visibleSettingsSection(`integration-${tab}`), 'general')
    assert.equal(visibleSettingsSection(tab), 'general')
    assert.equal(visibleSettingsSection('integrations', tab), 'general')
  }
  assert.equal(visibleSettingsSection('integrations'), 'general')
  assert.equal(visibleSettingsSection('integration-unknown'), 'general')
})

test('ordinary settings and MCP client tools are not hidden', () => {
  for (const section of ['general', 'models', 'members', 'mcp', 'skills', 'system-global']) {
    assert.equal(visibleSettingsSection(section), section)
  }
})

test('hidden integration panels are not imported or mounted, even during capability loading', () => {
  const source = readFileSync(new URL('../views/settings/Settings.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /IntegrationSettingsSection|INTEGRATION_PREVIEW_ITEMS|integrations\.title/)
  assert.match(source, /if \(isIntegrationSection\(key\)\) return false/)
  assert.match(source, /const normalizedSection = visibleSettingsSection\(/)
  const editor = readFileSync(new URL('../views/agent/AgentEditorModal.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(editor, /gotoIntegrations|loadAgentIntegrationCounts|integrations.agentEditor.label/)
})
